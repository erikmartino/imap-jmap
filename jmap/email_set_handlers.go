package jmap

import (
	"context"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

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
						if strings.EqualFold(em.TextBody[0].Type, "text/html") {
							em.Preview = preview(stripHTMLTags(bv.Value), 256)
						} else {
							em.Preview = preview(bv.Value, 256)
						}
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
