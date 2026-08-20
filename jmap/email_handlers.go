package jmap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Thread Handlers (RFC 8621 Section 3)

func handleThreadGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*Thread
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetThreads(ctx, ids)
		} else {
			list, err = backend.GetAllThreads(ctx)
		}

		if err != nil || list == nil {
			list = []*Thread{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Thread/get", map[string]any{
			"accountId": accountID,
			"state":     backend.ThreadState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleThreadChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		var maxChanges *uint64
		if mc, ok := args["maxChanges"].(float64); ok {
			if mc < 0 {
				return "error", MethodErrorArgs(MethodErrorInvalidArguments, "maxChanges must be non-negative")
			}
			m := uint64(mc)
			maxChanges = &m
		}

		created, updated, destroyed, newState, hasMore := backend.ThreadChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "Thread/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       sinceState,
			"newState":       newState,
			"hasMoreChanges": hasMore,
			"created":        created,
			"updated":        updated,
			"destroyed":      destroyed,
		}
	}
}

// Email Handlers (RFC 8621 Section 4)

func handleEmailGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		// Validate any header properties in props
		var parsedHeaderProps []*ParsedHeaderProperty
		if props != nil {
			for _, p := range props {
				if strings.HasPrefix(p, "header:") {
					hp, err := ParseHeaderProperty(p)
					if err != nil {
						return "error", MethodErrorArgs(MethodErrorInvalidArguments, err.Error())
					}
					parsedHeaderProps = append(parsedHeaderProps, hp)
				}
			}
		}

		bodyProps := parsePropertiesBody(args)
		fetchTextBodyValues, _ := args["fetchTextBodyValues"].(bool)
		fetchHTMLBodyValues, _ := args["fetchHTMLBodyValues"].(bool)
		fetchAllBodyValues, _ := args["fetchAllBodyValues"].(bool)
		var maxBodyValueBytes uint64
		if rawMBV, present := args["maxBodyValueBytes"]; present {
			if mbv, ok := rawMBV.(float64); ok && mbv > 0 && mbv == float64(uint64(mbv)) {
				maxBodyValueBytes = uint64(mbv)
			} else {
				return "error", InvalidArgumentsErrorArgs([]string{"maxBodyValueBytes"}, "")
			}
		}

		var list []*Email
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetEmails(ctx, ids)
		} else {
			list, err = backend.GetAllEmails(ctx)
		}

		if err != nil || list == nil {
			list = []*Email{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		// Filter and format each email
		formattedList := make([]any, 0, len(list))
		for _, em := range list {
			formattedList = append(formattedList, formatEmailGet(em, props, parsedHeaderProps, bodyProps, fetchTextBodyValues, fetchHTMLBodyValues, fetchAllBodyValues, maxBodyValueBytes))
		}

		return "Email/get", map[string]any{
			"accountId": accountID,
			"state":     backend.EmailState(ctx),
			"list":      formattedList,
			"notFound":  notFound,
		}
	}
}

func parsePropertiesBody(args map[string]any) []string {
	rawVal, ok := args["bodyProperties"]
	if !ok || rawVal == nil {
		return nil
	}
	raw, ok := rawVal.([]any)
	if !ok {
		return nil
	}
	props := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			props = append(props, s)
		}
	}
	return props
}

func formatEmailGet(em *Email, props []string, parsedHeaderProps []*ParsedHeaderProperty, bodyProps []string, fetchText, fetchHTML, fetchAll bool, maxBytes uint64) any {
	effectiveTextBody := em.TextBody
	effectiveHTMLBody := em.HTMLBody
	if len(effectiveHTMLBody) == 0 && len(effectiveTextBody) > 0 {
		effectiveHTMLBody = effectiveTextBody
	} else if len(effectiveTextBody) == 0 && len(effectiveHTMLBody) > 0 {
		effectiveTextBody = effectiveHTMLBody
	}

	bodyValues := make(map[string]EmailBodyValue)
	if fetchAll {
		for k, v := range em.BodyValues {
			bodyValues[k] = applyMaxBodyValueBytes(v, maxBytes)
		}
	} else {
		if fetchText {
			for _, part := range effectiveTextBody {
				if part.PartID != nil {
					if bv, ok := em.BodyValues[*part.PartID]; ok {
						bodyValues[*part.PartID] = applyMaxBodyValueBytes(bv, maxBytes)
					}
				}
			}
		}
		if fetchHTML {
			for _, part := range effectiveHTMLBody {
				if part.PartID != nil {
					if bv, ok := em.BodyValues[*part.PartID]; ok {
						bodyValues[*part.PartID] = applyMaxBodyValueBytes(bv, maxBytes)
					}
				}
			}
		}
	}

	filteredBodyStructure := FilterEmailBodyPart(em.BodyStructure, bodyProps)
	filteredTextBody := make([]any, 0, len(effectiveTextBody))
	for _, p := range effectiveTextBody {
		filteredTextBody = append(filteredTextBody, FilterEmailBodyPart(p, bodyProps))
	}
	filteredHTMLBody := make([]any, 0, len(effectiveHTMLBody))
	for _, p := range effectiveHTMLBody {
		filteredHTMLBody = append(filteredHTMLBody, FilterEmailBodyPart(p, bodyProps))
	}
	filteredAttachments := make([]any, 0, len(em.Attachments))
	for _, p := range em.Attachments {
		filteredAttachments = append(filteredAttachments, FilterEmailBodyPart(p, bodyProps))
	}

	if props == nil {
		out := map[string]any{
			"id":            em.ID,
			"blobId":        em.BlobID,
			"threadId":      em.ThreadID,
			"mailboxIds":    em.MailboxIDs,
			"keywords":      em.Keywords,
			"size":          em.Size,
			"receivedAt":    em.ReceivedAt,
			"messageId":     em.MessageID,
			"inReplyTo":     em.InReplyTo,
			"references":    em.References,
			"sender":        em.Sender,
			"from":          em.From,
			"to":            em.To,
			"cc":            em.CC,
			"bcc":           em.BCC,
			"replyTo":       em.ReplyTo,
			"subject":       em.Subject,
			"sentAt":        em.SentAt,
			"textBody":      filteredTextBody,
			"htmlBody":      filteredHTMLBody,
			"attachments":   filteredAttachments,
			"hasAttachment": em.HasAttachment,
			"preview":       em.Preview,
		}
		out["bodyValues"] = bodyValues
		if em.SMIMEStatus != nil {
			out["smimeStatus"] = *em.SMIMEStatus
		}
		if em.SMIMEStatusAt != nil {
			out["smimeStatusAt"] = *em.SMIMEStatusAt
		}
		if em.SMIMEVerifiedWith != nil {
			out["smimeVerifiedWith"] = *em.SMIMEVerifiedWith
		}
		if len(em.SMIMEErrors) > 0 {
			out["smimeErrors"] = em.SMIMEErrors
		}
		return out
	}

	out := make(map[string]any, len(props)+1)
	out["id"] = em.ID

	hpMap := make(map[string]*ParsedHeaderProperty, len(parsedHeaderProps))
	for _, hp := range parsedHeaderProps {
		hpMap[hp.RawProp] = hp
	}

	for _, p := range props {
		if hp, ok := hpMap[p]; ok {
			out[p] = EvaluateHeaderProperty(em, hp)
			continue
		}
		switch p {
		case "id":
			out["id"] = em.ID
		case "blobId":
			out["blobId"] = em.BlobID
		case "threadId":
			out["threadId"] = em.ThreadID
		case "mailboxIds":
			out["mailboxIds"] = em.MailboxIDs
		case "keywords":
			out["keywords"] = em.Keywords
		case "size":
			out["size"] = em.Size
		case "receivedAt":
			out["receivedAt"] = em.ReceivedAt
		case "messageId":
			out["messageId"] = em.MessageID
		case "inReplyTo":
			out["inReplyTo"] = em.InReplyTo
		case "references":
			out["references"] = em.References
		case "sender":
			out["sender"] = em.Sender
		case "from":
			out["from"] = em.From
		case "to":
			out["to"] = em.To
		case "cc":
			out["cc"] = em.CC
		case "bcc":
			out["bcc"] = em.BCC
		case "replyTo":
			out["replyTo"] = em.ReplyTo
		case "subject":
			out["subject"] = em.Subject
		case "sentAt":
			out["sentAt"] = em.SentAt
		case "headers":
			out["headers"] = em.Headers
		case "bodyStructure":
			out["bodyStructure"] = filteredBodyStructure
		case "bodyValues":
			out["bodyValues"] = bodyValues
		case "textBody":
			out["textBody"] = filteredTextBody
		case "htmlBody":
			out["htmlBody"] = filteredHTMLBody
		case "attachments":
			out["attachments"] = filteredAttachments
		case "hasAttachment":
			out["hasAttachment"] = em.HasAttachment
		case "preview":
			out["preview"] = em.Preview
		case "smimeStatus":
			if em.SMIMEStatus != nil {
				out["smimeStatus"] = *em.SMIMEStatus
			} else {
				out["smimeStatus"] = nil
			}
		case "smimeStatusAt":
			if em.SMIMEStatusAt != nil {
				out["smimeStatusAt"] = *em.SMIMEStatusAt
			} else {
				out["smimeStatusAt"] = nil
			}
		case "smimeVerifiedWith":
			if em.SMIMEVerifiedWith != nil {
				out["smimeVerifiedWith"] = *em.SMIMEVerifiedWith
			} else {
				out["smimeVerifiedWith"] = nil
			}
		case "smimeErrors":
			if em.SMIMEErrors != nil {
				out["smimeErrors"] = em.SMIMEErrors
			} else {
				out["smimeErrors"] = []string{}
			}
		}
	}

	return out
}

func applyMaxBodyValueBytes(bv EmailBodyValue, maxBytes uint64) EmailBodyValue {
	if maxBytes == 0 || uint64(len(bv.Value)) <= maxBytes {
		return bv
	}
	b := []byte(bv.Value)[:maxBytes]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return EmailBodyValue{
		Value:             string(b),
		IsTruncated:       true,
		IsEncodingProblem: bv.IsEncodingProblem,
	}
}

func handleEmailChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		var maxChanges *uint64
		if mc, ok := args["maxChanges"].(float64); ok {
			if mc < 0 {
				return "error", MethodErrorArgs(MethodErrorInvalidArguments, "maxChanges must be non-negative")
			}
			m := uint64(mc)
			maxChanges = &m
		}

		created, updated, destroyed, newState, hasMore := backend.EmailChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "Email/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       sinceState,
			"newState":       newState,
			"hasMoreChanges": hasMore,
			"created":        created,
			"updated":        updated,
			"destroyed":      destroyed,
		}
	}
}

func handleEmailSet(backend MailBackend, blobBackend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.EmailState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", fmt.Sprintf("state token %q does not match current state %q", ifInState, oldState))
		}

		created := make(map[string]any)
		updated := make(map[string]any)
		notUpdated := make(map[string]any)
		notCreated := make(map[string]any)
		destroyed := []Id{}
		notDestroyed := make(map[string]any)

		// creationRefs maps a creation id to the real id the server assigned (seeded from
		// the request-scoped createdIds map), so #creationId references in this call and
		// in later method calls of the same request resolve (RFC 8620 Section 5.3).
		creationRefs := newSetCreationRefs(ctx)

		if createMap, ok := args["create"].(map[string]any); ok {
			notCreated = runCreateLoop(createMap, creationRefs, func(clientKey string, emData map[string]any) (string, error) {
				if setErr := validateEmailCreateData(ctx, accountID, emData, blobBackend); setErr != nil {
					return "", *setErr
				}
				subject, _ := emData["subject"].(string)
				blobIDStr, _ := emData["blobId"].(string)
				if blobIDStr == "" {
					blobIDStr = fmt.Sprintf("blob-%d", time.Now().UnixNano())
				}
				receivedAt, _ := emData["receivedAt"].(string)
				if receivedAt == "" {
					receivedAt = time.Now().UTC().Format(time.RFC3339)
				}
				sentAt, _ := emData["sentAt"].(string)

				parseAddresses := func(key string) []EmailAddress {
					var res []EmailAddress
					if list, ok := emData[key].([]any); ok {
						for _, item := range list {
							if addrMap, ok := item.(map[string]any); ok {
								name, _ := addrMap["name"].(string)
								email, _ := addrMap["email"].(string)
								if email != "" {
									res = append(res, EmailAddress{Name: name, Email: email})
								}
							}
						}
					}
					return res
				}

				var sentAtPtr *string
				if sentAt != "" {
					sentAtPtr = &sentAt
				} else {
					now := time.Now().UTC().Format(time.RFC3339)
					sentAtPtr = &now
				}

				em := &Email{
					Subject:    subject,
					BlobID:     Id(blobIDStr),
					ReceivedAt: receivedAt,
					SentAt:     sentAtPtr,
					From:       parseAddresses("from"),
					To:         parseAddresses("to"),
					CC:         parseAddresses("cc"),
					BCC:        parseAddresses("bcc"),
					ReplyTo:    parseAddresses("replyTo"),
					Sender:     parseAddresses("sender"),
					BodyValues: make(map[string]EmailBodyValue),
				}

				if msgIDRaw, ok := emData["messageId"].([]any); ok && len(msgIDRaw) > 0 {
					for _, m := range msgIDRaw {
						if s, ok := m.(string); ok && s != "" {
							em.MessageID = append(em.MessageID, strings.Trim(s, "<> "))
						}
					}
				}

				if inReplyToRaw, ok := emData["inReplyTo"].([]any); ok {
					for _, m := range inReplyToRaw {
						if s, ok := m.(string); ok && s != "" {
							em.InReplyTo = append(em.InReplyTo, strings.Trim(s, "<>"))
						}
					}
				}
				if referencesRaw, ok := emData["references"].([]any); ok {
					for _, m := range referencesRaw {
						if s, ok := m.(string); ok && s != "" {
							em.References = append(em.References, strings.Trim(s, "<>"))
						}
					}
				}

				if mbMap, ok := emData["mailboxIds"].(map[string]any); ok {
					em.MailboxIDs = make(map[Id]bool)
					for k := range mbMap {
						em.MailboxIDs[Id(k)] = true
					}
				}
				if kwMap, ok := emData["keywords"].(map[string]any); ok {
					em.Keywords = make(map[string]bool)
					for k, v := range kwMap {
						if boolVal, ok := v.(bool); ok {
							em.Keywords[strings.ToLower(k)] = boolVal
						}
					}
				}

				processHeaders := func(sourceMap map[string]any) {
					for k, v := range sourceMap {
						if strings.HasPrefix(strings.ToLower(k), "header:") {
							hdrPart := strings.TrimPrefix(k, "header:")
							hdrSubParts := strings.Split(hdrPart, ":")
							rawName := hdrSubParts[0]
							form := ""
							if len(hdrSubParts) > 1 {
								form = hdrSubParts[1]
							}
							var valStr string
							switch form {
							case "all":
								if list, ok := v.([]any); ok {
									for _, item := range list {
										s := fmt.Sprintf("%v", item)
										em.Headers = append(em.Headers, EmailHeader{Name: rawName, Value: s})
									}
								}
								continue
							case "asAddresses":
								if list, ok := v.([]any); ok {
									valStr = formatAddresses(list)
								}
							case "asMessageIds":
								if list, ok := v.([]any); ok {
									var mids []string
									for _, item := range list {
										if s, ok := item.(string); ok {
											mids = append(mids, "<"+strings.Trim(s, "<> ")+">")
										}
									}
									valStr = strings.Join(mids, " ")
								}
							case "asDate":
								if s, ok := v.(string); ok {
									if t, err := time.Parse(time.RFC3339, s); err == nil {
										rfcDate := t.UTC().Format("Mon, 02 Jan 2006 15:04:05 -0700")
										valStr = rfcDate
									} else {
										valStr = s
									}
								}
							case "asURLs":
								if list, ok := v.([]any); ok {
									var urls []string
									for _, item := range list {
										if s, ok := item.(string); ok {
											urls = append(urls, "<"+s+">")
										}
									}
									valStr = strings.Join(urls, ",\r\n ")
								}
							default:
								valStr = fmt.Sprintf("%v", v)
							}

							em.Headers = append(em.Headers, EmailHeader{Name: rawName, Value: valStr})

							if strings.EqualFold(rawName, "from") && len(em.From) == 0 {
								if addrs := parseEmailAddressesFromString(valStr); len(addrs) > 0 {
									em.From = addrs
								}
							}
							if strings.EqualFold(rawName, "to") && len(em.To) == 0 {
								if addrs := parseEmailAddressesFromString(valStr); len(addrs) > 0 {
									em.To = addrs
								}
							}
							if strings.EqualFold(rawName, "cc") && len(em.CC) == 0 {
								if addrs := parseEmailAddressesFromString(valStr); len(addrs) > 0 {
									em.CC = addrs
								}
							}
							if strings.EqualFold(rawName, "bcc") && len(em.BCC) == 0 {
								if addrs := parseEmailAddressesFromString(valStr); len(addrs) > 0 {
									em.BCC = addrs
								}
							}
							if strings.EqualFold(rawName, "subject") && em.Subject == "" {
								em.Subject = valStr
							}
							if strings.EqualFold(rawName, "message-id") && len(em.MessageID) == 0 {
								em.MessageID = []string{strings.Trim(valStr, "<> ")}
							}
						}
					}
				}

				processHeaders(emData)
				if parts, ok := emData["textBody"].([]any); ok {
					for _, pRaw := range parts {
						if pMap, ok := pRaw.(map[string]any); ok {
							processHeaders(pMap)
						}
					}
				}
				if parts, ok := emData["htmlBody"].([]any); ok {
					for _, pRaw := range parts {
						if pMap, ok := pRaw.(map[string]any); ok {
							processHeaders(pMap)
						}
					}
				}
				if bsMap, ok := emData["bodyStructure"].(map[string]any); ok {
					processHeaders(bsMap)
				}

				if len(em.MessageID) == 0 {
					em.MessageID = []string{fmt.Sprintf("%d@example.com", time.Now().UnixNano())}
				}

				if bodyValObj, ok := emData["bodyValues"].(map[string]any); ok {
					for k, v := range bodyValObj {
						if bvData, ok := v.(map[string]any); ok {
							val, _ := bvData["value"].(string)
							em.BodyValues[k] = EmailBodyValue{Value: val}
						}
					}
				}

				partCounter := 1
				var totalSize uint64
				populatePartBlob := func(p *EmailBodyPart) {
					if p.Type == "" {
						p.Type = "text/plain"
					}
					if p.Charset == nil && strings.HasPrefix(p.Type, "text/") {
						defCS := "us-ascii"
						p.Charset = &defCS
					}
					if p.PartID == nil {
						pID := strconv.Itoa(partCounter)
						partCounter++
						p.PartID = &pID
					}
					if p.BlobID != nil {
						if blobBackend != nil {
							if blob, found, _ := blobBackend.GetBlob(ctx, accountID, string(*p.BlobID)); found && blob != nil {
								if p.Size == 0 {
									p.Size = uint64(len(blob.Data))
								}
								if p.PartID != nil {
									em.BodyValues[*p.PartID] = EmailBodyValue{Value: string(blob.Data)}
								}
							}
						}
					} else {
						var partData []byte
						if p.PartID != nil {
							if bv, ok := em.BodyValues[*p.PartID]; ok {
								partData = []byte(bv.Value)
								p.Size = uint64(len(partData))
							}
						}
						bID := Id(fmt.Sprintf("blob-%d-%d", time.Now().UnixNano(), len(partData)))
						p.BlobID = &bID
						if blobBackend != nil && len(partData) > 0 {
							blobBackend.PutBlob(ctx, accountID, string(bID), partData)
						}
					}
					totalSize += p.Size
				}

				var walkAndPopulate func(p *EmailBodyPart)
				walkAndPopulate = func(p *EmailBodyPart) {
					if len(p.SubParts) > 0 {
						for i := range p.SubParts {
							walkAndPopulate(&p.SubParts[i])
						}
					} else {
						populatePartBlob(p)
					}
				}

				if bsRaw, hasBS := emData["bodyStructure"].(map[string]any); hasBS {
					em.BodyStructure = parseBodyPartFromMap(bsRaw, em.BodyValues)
					if em.BodyStructure.Type == "" {
						if len(em.BodyStructure.SubParts) > 0 {
							em.BodyStructure.Type = "multipart/mixed"
						} else {
							em.BodyStructure.Type = "text/plain"
						}
					}
					walkAndPopulate(&em.BodyStructure)
					tb, hb, atts := extractBodyStructureParts(&em.BodyStructure)
					em.TextBody = tb
					em.HTMLBody = hb
					em.Attachments = atts
					em.HasAttachment = len(atts) > 0
				} else {
					if parts, ok := emData["textBody"].([]any); ok {
						for _, raw := range parts {
							if part, ok := raw.(map[string]any); ok {
								p := parseBodyPartFromMap(part, em.BodyValues)
								if p.Type == "" {
									p.Type = "text/plain"
								}
								populatePartBlob(&p)
								em.TextBody = append(em.TextBody, p)
							}
						}
					}
					if parts, ok := emData["htmlBody"].([]any); ok {
						for _, raw := range parts {
							if part, ok := raw.(map[string]any); ok {
								p := parseBodyPartFromMap(part, em.BodyValues)
								if p.Type == "" {
									p.Type = "text/html"
								}
								populatePartBlob(&p)
								em.HTMLBody = append(em.HTMLBody, p)
							}
						}
					}
					if parts, ok := emData["attachments"].([]any); ok {
						for _, raw := range parts {
							if part, ok := raw.(map[string]any); ok {
								p := parseBodyPartFromMap(part, em.BodyValues)
								populatePartBlob(&p)
								em.Attachments = append(em.Attachments, p)
								em.HasAttachment = true
							}
						}
					}
					if len(em.TextBody) > 0 && len(em.HTMLBody) > 0 {
						em.BodyStructure = EmailBodyPart{
							Type:     "multipart/alternative",
							SubParts: []EmailBodyPart{em.TextBody[0], em.HTMLBody[0]},
						}
						if len(em.Attachments) > 0 {
							em.BodyStructure = EmailBodyPart{
								Type:     "multipart/mixed",
								SubParts: append([]EmailBodyPart{em.BodyStructure}, em.Attachments...),
							}
						}
					} else if len(em.TextBody) > 0 {
						if len(em.Attachments) > 0 {
							em.BodyStructure = EmailBodyPart{
								Type:     "multipart/mixed",
								SubParts: append([]EmailBodyPart{em.TextBody[0]}, em.Attachments...),
							}
						} else {
							em.BodyStructure = em.TextBody[0]
						}
					} else if len(em.HTMLBody) > 0 {
						if len(em.Attachments) > 0 {
							em.BodyStructure = EmailBodyPart{
								Type:     "multipart/mixed",
								SubParts: append([]EmailBodyPart{em.HTMLBody[0]}, em.Attachments...),
							}
						} else {
							em.BodyStructure = em.HTMLBody[0]
						}
					} else if len(em.Attachments) > 0 {
						em.BodyStructure = em.Attachments[0]
					}
				}

				if len(em.HTMLBody) == 0 && len(em.TextBody) > 0 {
					em.HTMLBody = em.TextBody
				} else if len(em.TextBody) == 0 && len(em.HTMLBody) > 0 {
					em.TextBody = em.HTMLBody
				}

				if len(em.TextBody) > 0 && em.TextBody[0].PartID != nil {
					if bv, ok := em.BodyValues[*em.TextBody[0].PartID]; ok {
						em.Preview = preview(bv.Value, 256)
					}
				} else if len(em.HTMLBody) > 0 && em.HTMLBody[0].PartID != nil {
					if bv, ok := em.BodyValues[*em.HTMLBody[0].PartID]; ok {
						em.Preview = preview(stripHTMLTags(bv.Value), 256)
					}
				}

				if totalSize == 0 {
					for _, bv := range em.BodyValues {
						totalSize += uint64(len(bv.Value))
					}
				}
				if totalSize == 0 {
					totalSize = 100
				} else {
					totalSize += 100
				}
				em.Size = totalSize

				if blobBackend != nil {
					if _, found, _ := blobBackend.GetBlob(ctx, accountID, string(em.BlobID)); !found {
						blobBackend.PutBlob(ctx, accountID, string(em.BlobID), make([]byte, em.Size))
					}
				}

				createdEM, err := backend.CreateEmail(ctx, em)
				if err != nil {
					return "", err
				}
				created[clientKey] = map[string]any{
					"id":       createdEM.ID,
					"blobId":   createdEM.BlobID,
					"threadId": createdEM.ThreadID,
					"size":     createdEM.Size,
				}
				recordCreationRefs(ctx, creationRefs, clientKey, createdEM.ID)
				return string(createdEM.ID), nil
			})
		}

		if updateMap, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateMap {
				if rawPatch, ok := patchRaw.(map[string]any); ok {
					patch := resolvePatchCreationRefs(rawPatch, creationRefs)
					resolvedID := resolveCreationID(idStr, creationRefs)
					updatedEM, err := backend.UpdateEmail(ctx, Id(resolvedID), patch)
					if err != nil {
						notUpdated[string(resolvedID)] = map[string]any{
							"type":        "notFound",
							"description": err.Error(),
						}
					} else {
						updated[string(resolvedID)] = updatedEM
					}
				}
			}
		}

		if destroyList, ok := args["destroy"].([]any); ok {
			for _, rawID := range destroyList {
				if idStr, ok := rawID.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					if ok, _ := backend.DeleteEmail(ctx, Id(resolvedID)); ok {
						destroyed = append(destroyed, Id(resolvedID))
					} else {
						notDestroyed[string(resolvedID)] = SetError{Type: "notFound", Description: "email not found: " + string(resolvedID)}
					}
				}
			}
		}

		return "Email/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.EmailState(ctx),
			"created":      nilIfEmpty(created),
			"updated":      nilIfEmpty(updated),
			"notUpdated":   nilIfEmpty(notUpdated),
			"notCreated":   nilIfEmpty(notCreated),
			"destroyed":    nilIfEmpty(destroyed),
			"notDestroyed": nilIfEmpty(notDestroyed),
		}
	}
}

func isValidKeyword(k string) bool {
	if len(k) == 0 || len(k) > 255 {
		return false
	}
	for _, r := range k {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '$' || r == '_' {
			continue
		}
		return false
	}
	return true
}

// validateEmailCreateData enforces the RFC 8621 Section 4.6 constraints on Email
// objects submitted for creation, returning a SetError or nil if acceptable.
func validateEmailCreateData(ctx context.Context, accountID string, emData map[string]any, blobBackend BlobBackend) *SetError {
	if mbMap, ok := emData["mailboxIds"].(map[string]any); !ok || len(mbMap) == 0 {
		return &SetError{Type: "invalidProperties", Properties: []string{"mailboxIds"}}
	}

	if _, has := emData["headers"]; has {
		return &SetError{Type: "invalidProperties", Properties: []string{"headers"}}
	}

	convenienceHeaders := map[string]string{
		"subject":    "Subject",
		"from":       "From",
		"to":         "To",
		"cc":         "Cc",
		"bcc":        "Bcc",
		"replyTo":    "Reply-To",
		"sender":     "Sender",
		"messageId":  "Message-ID",
		"inReplyTo":  "In-Reply-To",
		"references": "References",
		"sentAt":     "Date",
	}

	for k, v := range emData {
		kLower := strings.ToLower(k)
		if strings.HasPrefix(kLower, "header:content-") {
			return &SetError{Type: "invalidProperties", Properties: []string{k}}
		}
		if strings.HasPrefix(kLower, "header:") {
			hdrPart := strings.TrimPrefix(k, "header:")
			hdrSubParts := strings.Split(hdrPart, ":")
			rawName := hdrSubParts[0]
			form := ""
			if len(hdrSubParts) > 1 {
				form = hdrSubParts[1]
			}

			if form != "all" && form != "asAddresses" && form != "asMessageIds" && form != "asURLs" {
				if _, isList := v.([]any); isList {
					return &SetError{Type: "invalidProperties", Properties: []string{"header:" + rawName}}
				}
			}

			for convProp, stdHdrName := range convenienceHeaders {
				if strings.EqualFold(rawName, stdHdrName) {
					if _, hasConv := emData[convProp]; hasConv {
						return &SetError{Type: "invalidProperties", Properties: []string{k}}
					}
				}
			}
		}
	}

	var missingBlobs []string
	if bsRaw, hasBS := emData["bodyStructure"]; hasBS {
		var conflictProps []string
		if _, has := emData["textBody"]; has {
			conflictProps = append(conflictProps, "textBody")
		}
		if _, has := emData["htmlBody"]; has {
			conflictProps = append(conflictProps, "htmlBody")
		}
		if _, has := emData["attachments"]; has {
			conflictProps = append(conflictProps, "attachments")
		}
		if len(conflictProps) > 0 {
			return &SetError{Type: "invalidProperties", Properties: conflictProps}
		}

		if bsMap, ok := bsRaw.(map[string]any); ok {
			if err := validateBodyPartTree(ctx, accountID, bsMap, "bodyStructure", blobBackend, &missingBlobs); err != nil {
				return err
			}
		}
	}

	if tbRaw, hasTB := emData["textBody"]; hasTB {
		if tbList, ok := tbRaw.([]any); ok {
			if len(tbList) != 1 {
				return &SetError{Type: "invalidProperties", Properties: []string{"textBody"}}
			}
			for i, pRaw := range tbList {
				if pMap, ok := pRaw.(map[string]any); ok {
					if t, hasT := pMap["type"].(string); hasT && t != "" && t != "text/plain" {
						return &SetError{Type: "invalidProperties", Properties: []string{"textBody"}}
					}
					path := fmt.Sprintf("textBody/%d", i)
					if err := validateBodyPartTree(ctx, accountID, pMap, path, blobBackend, &missingBlobs); err != nil {
						return err
					}
				}
			}
		}
	}

	if hbRaw, hasHB := emData["htmlBody"]; hasHB {
		if hbList, ok := hbRaw.([]any); ok {
			if len(hbList) != 1 {
				return &SetError{Type: "invalidProperties", Properties: []string{"htmlBody"}}
			}
			for i, pRaw := range hbList {
				if pMap, ok := pRaw.(map[string]any); ok {
					if t, hasT := pMap["type"].(string); hasT && t != "" && t != "text/html" {
						return &SetError{Type: "invalidProperties", Properties: []string{"htmlBody"}}
					}
					path := fmt.Sprintf("htmlBody/%d", i)
					if err := validateBodyPartTree(ctx, accountID, pMap, path, blobBackend, &missingBlobs); err != nil {
						return err
					}
				}
			}
		}
	}

	if attRaw, hasAtt := emData["attachments"]; hasAtt {
		if attList, ok := attRaw.([]any); ok {
			for i, pRaw := range attList {
				if pMap, ok := pRaw.(map[string]any); ok {
					path := fmt.Sprintf("attachments/%d", i)
					if err := validateBodyPartTree(ctx, accountID, pMap, path, blobBackend, &missingBlobs); err != nil {
						return err
					}
				}
			}
		}
	}

	if len(missingBlobs) > 0 {
		return &SetError{Type: "blobNotFound", NotFound: missingBlobs}
	}

	if bvRaw, hasBV := emData["bodyValues"]; hasBV {
		if bvMap, ok := bvRaw.(map[string]any); ok {
			var invalidProps []string
			for partID, valRaw := range bvMap {
				if vMap, ok := valRaw.(map[string]any); ok {
					if t, ok := vMap["isTruncated"].(bool); ok && t {
						invalidProps = append(invalidProps, fmt.Sprintf("bodyValues/%s/isTruncated", partID))
					}
					if e, ok := vMap["isEncodingProblem"].(bool); ok && e {
						invalidProps = append(invalidProps, fmt.Sprintf("bodyValues/%s/isEncodingProblem", partID))
					}
				}
			}
			if len(invalidProps) > 0 {
				return &SetError{Type: "invalidProperties", Properties: invalidProps}
			}
		}
	}

	return nil
}

func validateBodyPartTree(ctx context.Context, accountID string, pMap map[string]any, path string, blobBackend BlobBackend, missingBlobs *[]string) *SetError {
	if _, has := pMap["headers"]; has {
		return &SetError{Type: "invalidProperties", Properties: []string{path + "/headers"}}
	}
	for k := range pMap {
		kLower := strings.ToLower(k)
		if kLower == "header:content-transfer-encoding" || kLower == "header:content-type" {
			return &SetError{Type: "invalidProperties", Properties: []string{path + "/" + k}}
		}
	}
	if _, hasPartID := pMap["partId"]; hasPartID {
		if _, hasSize := pMap["size"]; hasSize {
			return &SetError{Type: "invalidProperties", Properties: []string{path + "/size"}}
		}
	}
	if bID, ok := pMap["blobId"].(string); ok && bID != "" {
		if blobBackend != nil {
			if _, found, _ := blobBackend.GetBlob(ctx, accountID, bID); !found {
				*missingBlobs = append(*missingBlobs, bID)
			}
		}
	}
	if subParts, ok := pMap["subParts"].([]any); ok {
		for i, sp := range subParts {
			if spMap, ok := sp.(map[string]any); ok {
				subPath := fmt.Sprintf("%s/subParts/%d", path, i)
				if err := validateBodyPartTree(ctx, accountID, spMap, subPath, blobBackend, missingBlobs); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func parseBodyPartFromMap(m map[string]any, bvs map[string]EmailBodyValue) EmailBodyPart {
	p := EmailBodyPart{}
	if pID, ok := m["partId"].(string); ok && pID != "" {
		p.PartID = &pID
		if bv, ok := bvs[pID]; ok {
			p.Size = uint64(len(bv.Value))
		}
	}
	if bID, ok := m["blobId"].(string); ok && bID != "" {
		id := Id(bID)
		p.BlobID = &id
	}
	if s, ok := m["size"].(float64); ok {
		p.Size = uint64(s)
	}
	if t, ok := m["type"].(string); ok && t != "" {
		p.Type = t
	}
	if cs, ok := m["charset"].(string); ok {
		p.Charset = &cs
	} else if strings.HasPrefix(p.Type, "text/") {
		defCS := "us-ascii"
		p.Charset = &defCS
	}
	if d, ok := m["disposition"].(string); ok {
		p.Disposition = &d
	}
	if cid, ok := m["cid"].(string); ok {
		c := strings.Trim(cid, "<>")
		p.CID = &c
	}
	if name, ok := m["name"].(string); ok {
		p.Name = &name
	}
	if loc, ok := m["location"].(string); ok {
		p.Location = &loc
	}
	if lang, ok := m["language"].([]any); ok {
		for _, item := range lang {
			if s, ok := item.(string); ok {
				p.Language = append(p.Language, s)
			}
		}
	}
	if subPartsRaw, ok := m["subParts"].([]any); ok {
		for _, spRaw := range subPartsRaw {
			if spMap, ok := spRaw.(map[string]any); ok {
				p.SubParts = append(p.SubParts, parseBodyPartFromMap(spMap, bvs))
			}
		}
	}
	return p
}

func formatAddresses(list []any) string {
	var parts []string
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			email, _ := m["email"].(string)
			name, _ := m["name"].(string)
			if name != "" {
				parts = append(parts, fmt.Sprintf("%q <%s>", name, email))
			} else if email != "" {
				parts = append(parts, fmt.Sprintf("<%s>", email))
			}
		}
	}
	return strings.Join(parts, ", ")
}

func parseEmailAddressesFromString(s string) []EmailAddress {
	addrs, err := mail.ParseAddressList(s)
	if err != nil {
		addr, err := mail.ParseAddress(s)
		if err != nil {
			return nil
		}
		return []EmailAddress{{Name: addr.Name, Email: addr.Address}}
	}
	res := make([]EmailAddress, 0, len(addrs))
	for _, a := range addrs {
		res = append(res, EmailAddress{Name: a.Name, Email: a.Address})
	}
	return res
}

func handleEmailCopy(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		fromAccountID, _ := args["fromAccountId"].(string)
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)
		onSuccessDestroy, _ := args["onSuccessDestroyOriginal"].(bool)

		oldState := backend.EmailState(ctx)
		created := make(map[string]*Email)
		notCreated := make(map[string]SetError)

		for clientKey, raw := range createMap {
			if emData, ok := raw.(map[string]any); ok {
				if idStr, ok := emData["id"].(string); ok {
					list, _, _ := backend.GetEmails(ctx, []Id{Id(idStr)})
					if len(list) > 0 {
						cp := *list[0]
						cp.ID = ""
						cp.ThreadID = ""

						// Apply property overrides if specified (RFC 8621 Section 4.6)
						if mbMap, ok := emData["mailboxIds"].(map[string]any); ok {
							cp.MailboxIDs = make(map[Id]bool)
							for k, v := range mbMap {
								if v != nil {
									cp.MailboxIDs[Id(k)] = true
								}
							}
						}
						if kwMap, ok := emData["keywords"].(map[string]any); ok {
							cp.Keywords = make(map[string]bool)
							for k, v := range kwMap {
								if boolVal, ok := v.(bool); ok {
									cp.Keywords[k] = boolVal
								}
							}
						}

						createdEM, err := backend.CreateEmail(ctx, &cp)
						if err == nil {
							created[clientKey] = createdEM
							if onSuccessDestroy {
								_, _ = backend.DeleteEmail(ctx, Id(idStr))
							}
						} else {
							notCreated[clientKey] = SetError{Type: "serverFail", Description: err.Error()}
						}
					} else {
						notCreated[clientKey] = SetError{Type: "notFound", Description: "email not found"}
					}
				}
			}
		}

		return "Email/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.EmailState(ctx),
			"created":       created,
			"notCreated":    notCreated,
		}
	}
}

func handleEmailQuery(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		if accountID != "" {
			ctx = ContextWithAccountID(ctx, accountID)
		}
		filter, _ := args["filter"].(map[string]any)

		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, emailSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := parseQueryAnchor(args)
		if anchorErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, anchorErr)
		}

		var limit *uint64
		if limVal, ok := args["limit"].(float64); ok {
			l := uint64(limVal)
			limit = &l
		}

		var ids []Id
		var total int
		if anchor != "" {
			allIDs, allTotal, _ := backend.QueryEmails(ctx, filter, comparators, 0, nil)
			total = allTotal
			var found bool
			position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else {
			ids, total, _ = backend.QueryEmails(ctx, filter, comparators, position, limit)
			position = NormalizePosition(position, total)
		}

		calcTotal, _ := args["calculateTotal"].(bool)
		res := map[string]any{
			"accountId":           accountID,
			"queryState":          backend.EmailState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"ids":                 ids,
			"total":               total,
		}
		if calcTotal {
			res["calculateTotal"] = true
		}
		return "Email/query", res
	}
}

func handleEmailQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)
		filter, _ := args["filter"].(map[string]any)
		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, emailSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.EmailChanges(ctx, sinceState, nil)
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		currentIDs, _, _ := backend.QueryEmails(ctx, filter, comparators, 0, nil)
		added, removed := computeQueryChanges(createdIDs, updatedIDs, destroyedIDs, currentIDs, upToID)

		res := map[string]any{
			"accountId":     accountID,
			"oldQueryState": sinceState,
			"newQueryState": newState,
			"added":         added,
			"removed":       removed,
		}
		if f, ok := args["filter"]; ok && f != nil {
			res["filter"] = f
		}
		if s, ok := args["sort"]; ok && s != nil {
			res["sort"] = s
		}
		if upToID != "" {
			res["upToId"] = upToID
		}
		if calcTotal, _ := args["calculateTotal"].(bool); calcTotal {
			res["total"] = len(currentIDs)
		}
		return "Email/queryChanges", res
	}
}

func handleEmailImport(backend MailBackend, blobBackend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		emailsRaw, hasEmails := args["emails"]
		if !hasEmails || emailsRaw == nil {
			emailsRaw = args["create"]
		}
		if emailsRaw == nil {
			return "error", InvalidArgumentsErrorArgs([]string{"emails"}, "")
		}
		createMap, ok := emailsRaw.(map[string]any)
		if !ok {
			return "error", InvalidArgumentsErrorArgs([]string{"emails"}, "")
		}

		created := make(map[string]any)
		notCreated := make(map[string]any)
		oldState := backend.EmailState(ctx)

		for clientKey, raw := range createMap {
			emData, ok := raw.(map[string]any)
			if !ok {
				notCreated[clientKey] = SetError{Type: "invalidProperties"}
				continue
			}

			var missingProps []string
			blobIDRaw, hasBlobID := emData["blobId"]
			if !hasBlobID {
				missingProps = append(missingProps, "blobId")
			}
			mbIDsRaw, hasMB := emData["mailboxIds"]
			if !hasMB || mbIDsRaw == nil {
				missingProps = append(missingProps, "mailboxIds")
			}
			if len(missingProps) > 0 {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: missingProps}
				continue
			}

			blobID, ok := blobIDRaw.(string)
			if !ok || blobID == "" {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"blobId"}}
				continue
			}

			mbIDs, ok := mbIDsRaw.(map[string]any)
			if !ok || len(mbIDs) == 0 {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"mailboxIds"}}
				continue
			}

			mbValid := true
			checkIDs := make([]Id, 0, len(mbIDs))
			for id, v := range mbIDs {
				b, ok := v.(bool)
				if !ok || !b {
					mbValid = false
					break
				}
				checkIDs = append(checkIDs, Id(id))
			}
			if mbValid {
				_, notFound, err := backend.GetMailboxes(ctx, checkIDs)
				if err != nil || len(notFound) > 0 {
					mbValid = false
				}
			}
			if !mbValid {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"mailboxIds"}}
				continue
			}

			var kwMap map[string]bool
			if kwRaw, hasKW := emData["keywords"]; hasKW {
				kws, ok := kwRaw.(map[string]any)
				if !ok {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"keywords"}}
					continue
				}
				kwMap = make(map[string]bool, len(kws))
				kwValid := true
				for k, v := range kws {
					b, ok := v.(bool)
					if !ok || !b || !isValidKeyword(k) {
						kwValid = false
						break
					}
					kwMap[strings.ToLower(k)] = true
				}
				if !kwValid {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"keywords"}}
					continue
				}
			}

			var rcptAtStr string
			if rcptRaw, hasRcpt := emData["receivedAt"]; hasRcpt {
				s, ok := rcptRaw.(string)
				if !ok || s == "" {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"receivedAt"}}
					continue
				}
				if _, err := time.Parse(time.RFC3339, s); err != nil {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"receivedAt"}}
					continue
				}
				rcptAtStr = s
			}

			var blobData []byte
			if blobBackend != nil {
				blob, found, _ := blobBackend.GetBlob(ctx, accountID, blobID)
				if !found || blob == nil {
					errType := "blobNotFound"
					if len(blobID) < 8 {
						errType = "invalidProperties"
					}
					notCreated[clientKey] = SetError{Type: errType, Properties: []string{"blobId"}}
					continue
				}
				blobData = blob.Data
			} else {
				errType := "blobNotFound"
				if len(blobID) < 8 {
					errType = "invalidProperties"
				}
				notCreated[clientKey] = SetError{Type: errType, Properties: []string{"blobId"}}
				continue
			}

			em, err := parseRFC822WithAccount(accountID, blobData, blobBackend)
			if err != nil {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"blobId"}}
				continue
			}
			em.BlobID = Id(blobID)

			em.MailboxIDs = make(map[Id]bool, len(mbIDs))
			for id := range mbIDs {
				em.MailboxIDs[Id(id)] = true
			}

			if kwMap != nil {
				em.Keywords = kwMap
			}
			if rcptAtStr != "" {
				em.ReceivedAt = rcptAtStr
			}

			createdEm, err := backend.CreateEmail(ctx, em)
			if err != nil {
				notCreated[clientKey] = SetError{Type: "invalidProperties"}
				continue
			}
			created[clientKey] = map[string]any{
				"id":       createdEm.ID,
				"blobId":   createdEm.BlobID,
				"threadId": createdEm.ThreadID,
				"size":     createdEm.Size,
			}
		}

		return "Email/import", map[string]any{
			"accountId":  accountID,
			"oldState":   oldState,
			"newState":   backend.EmailState(ctx),
			"created":    created,
			"notCreated": notCreated,
		}
	}
}

// Parse Handler (RFC 8621 Section 4.9)

func handleEmailParse(backend MailBackend, blobBackend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		blobIDsRaw, _ := args["blobIds"].([]any)
		parsed := make(map[string]*Email)
		notParsable := []Id{}
		notFound := []Id{}

		for _, blobIDRaw := range blobIDsRaw {
			blobIDStr, ok := blobIDRaw.(string)
			if !ok || blobIDStr == "" {
				continue
			}

			if blobBackend == nil {
				notFound = append(notFound, Id(blobIDStr))
				continue
			}

			blob, found, _ := blobBackend.GetBlob(ctx, accountID, blobIDStr)
			if !found || blob == nil {
				notFound = append(notFound, Id(blobIDStr))
				continue
			}

			em, err := parseRFC822(blob.Data)
			if err != nil {
				notParsable = append(notParsable, Id(blobIDStr))
				continue
			}
			// A parsed Email is not a stored object: it has a blobId but no server id (RFC 8621 §4.9).
			em.BlobID = Id(blobIDStr)
			parsed[blobIDStr] = em
		}

		return "Email/parse", map[string]any{
			"accountId":   accountID,
			"parsed":      parsed,
			"notParsable": notParsable,
			"notFound":    notFound,
		}
	}
}

// S/MIME Verification Handler (RFC 9219 Section 4)

func handleEmailVerifySmime(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		emailIDsRaw, _ := args["emailIds"].([]any)

		ids := make([]Id, 0, len(emailIDsRaw))
		for _, item := range emailIDsRaw {
			if idStr, ok := item.(string); ok {
				ids = append(ids, Id(idStr))
			}
		}

		verified, notFound, _ := backend.VerifySmime(ctx, ids)
		if verified == nil {
			verified = make(map[Id]*SmimeVerificationResult)
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Email/verifySmime", map[string]any{
			"accountId": accountID,
			"verified":  verified,
			"notFound":  notFound,
		}
	}
}

// SearchSnippet Handler (RFC 8621 Section 5)

func handleSearchSnippetGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		emailIDsRaw, hasIDs := args["emailIds"].([]any)

		var filterText string
		if filterMap, ok := args["filter"].(map[string]any); ok {
			if txt, ok := filterMap["text"].(string); ok {
				filterText = txt
			} else if body, ok := filterMap["body"].(string); ok {
				filterText = body
			}
		}

		var emails []*Email
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(emailIDsRaw))
			for _, item := range emailIDsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			emails, notFound, err = backend.GetEmails(ctx, ids)
		} else {
			emails, err = backend.GetAllEmails(ctx)
		}

		if err != nil || emails == nil {
			emails = []*Email{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		var list []SearchSnippet
		for _, em := range emails {
			subj := em.Subject
			prev := em.Preview

			if filterText != "" {
				// Highlight matching terms with <mark> tags per RFC 8621 Section 5
				idx := strings.Index(strings.ToLower(subj), strings.ToLower(filterText))
				if idx >= 0 {
					matchedText := subj[idx : idx+len(filterText)]
					subj = subj[:idx] + "<mark>" + matchedText + "</mark>" + subj[idx+len(filterText):]
				}

				idxP := strings.Index(strings.ToLower(prev), strings.ToLower(filterText))
				if idxP >= 0 {
					matchedText := prev[idxP : idxP+len(filterText)]
					prev = prev[:idxP] + "<mark>" + matchedText + "</mark>" + prev[idxP+len(filterText):]
				}
			}

			list = append(list, SearchSnippet{
				AccountID: accountID,
				EmailID:   em.ID,
				Subject:   &subj,
				Preview:   &prev,
			})
		}

		return "SearchSnippet/get", map[string]any{
			"accountId": accountID,
			"list":      list,
			"notFound":  notFound,
		}
	}
}

// Identity Handlers (RFC 8621 Section 6)

func handleIdentityGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		list, _ := backend.GetIdentities(ctx)
		return "Identity/get", map[string]any{
			"accountId": accountID,
			"state":     backend.IdentityState(ctx),
			"list":      list,
			"notFound":  []Id{},
		}
	}
}

func handleIdentityChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		var maxChanges *uint64
		if mc, ok := args["maxChanges"].(float64); ok {
			if mc < 0 {
				return "error", MethodErrorArgs(MethodErrorInvalidArguments, "maxChanges must be non-negative")
			}
			m := uint64(mc)
			maxChanges = &m
		}

		created, updated, destroyed, newState, hasMore := backend.IdentityChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "Identity/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       sinceState,
			"newState":       newState,
			"hasMoreChanges": hasMore,
			"created":        created,
			"updated":        updated,
			"destroyed":      destroyed,
		}
	}
}

func handleIdentitySet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.IdentityState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		created := make(map[string]*Identity)
		updated := make(map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)
		creationRefs := newSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				idMap, _ := raw.(map[string]any)
				idBytes, _ := json.Marshal(idMap)
				var identity Identity
				_ = json.Unmarshal(idBytes, &identity)
				identity.ID = ""

				createdIdentity, err := backend.CreateIdentity(ctx, &identity)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdIdentity
					recordCreationRefs(ctx, creationRefs, creationID, createdIdentity.ID)
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				patch, _ := patchRaw.(map[string]any)
				resolvedID := resolveCreationID(idStr, creationRefs)
				_, err := backend.UpdateIdentity(ctx, Id(resolvedID), resolvePatchCreationRefs(patch, creationRefs))
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						notUpdated[string(resolvedID)] = SetError{Type: "notFound", Description: err.Error()}
					} else {
						notUpdated[string(resolvedID)] = SetError{Type: "invalidProperties", Description: err.Error()}
					}
				} else {
					updated[string(resolvedID)] = nil
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					okDel, err := backend.DeleteIdentity(ctx, Id(resolvedID))
					if err != nil {
						notDestroyed[string(resolvedID)] = SetError{Type: "serverFail", Description: err.Error()}
					} else if !okDel {
						notDestroyed[string(resolvedID)] = SetError{Type: "notFound", Description: "identity not found"}
					} else {
						destroyed = append(destroyed, Id(resolvedID))
					}
				}
			}
		}

		return "Identity/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.IdentityState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

// VacationResponse handlers (RFC 8621 Section 8). VacationResponse is a per-account
// singleton whose id is always "singleton"; it has only /get and /set (no /changes).

func handleVacationResponseGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		vr, _ := backend.GetVacationResponse(ctx)

		list := make([]*VacationResponse, 0, 1)
		notFound := []Id{}
		if idsRaw, ok := args["ids"].([]any); ok {
			// Explicit ids: only "singleton" resolves; anything else is notFound.
			for _, item := range idsRaw {
				s, _ := item.(string)
				if s == "singleton" && vr != nil {
					list = append(list, vr)
				} else {
					notFound = append(notFound, Id(s))
				}
			}
		} else if vr != nil {
			// ids null/absent means "all", which is just the singleton.
			list = append(list, vr)
		}

		return "VacationResponse/get", map[string]any{
			"accountId": accountID,
			"state":     backend.VacationResponseState(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleVacationResponseSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.VacationResponseState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		updated := make(map[string]any)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		// A singleton cannot be created or destroyed (RFC 8621 Section 8.2).
		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID := range createRaw {
				notCreated[creationID] = SetError{Type: "singleton", Description: "VacationResponse is a singleton and cannot be created"}
			}
		}
		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if s, ok := item.(string); ok {
					notDestroyed[s] = SetError{Type: "singleton", Description: "VacationResponse is a singleton and cannot be destroyed"}
				}
			}
		}
		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				if idStr != "singleton" {
					notUpdated[idStr] = SetError{Type: "notFound", Description: `the only VacationResponse id is "singleton"`}
					continue
				}
				patch, _ := patchRaw.(map[string]any)
				if _, err := backend.UpdateVacationResponse(ctx, patch); err != nil {
					notUpdated[idStr] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					updated[idStr] = nil
				}
			}
		}

		return "VacationResponse/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.VacationResponseState(ctx),
			"created":      map[string]any{},
			"updated":      updated,
			"destroyed":    []Id{},
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}
