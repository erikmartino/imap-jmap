package jmap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
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
		if mbv, ok := args["maxBodyValueBytes"].(float64); ok && mbv > 0 {
			maxBodyValueBytes = uint64(mbv)
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
	bodyValues := make(map[string]EmailBodyValue)
	if fetchAll {
		for k, v := range em.BodyValues {
			bodyValues[k] = applyMaxBodyValueBytes(v, maxBytes)
		}
	} else {
		if fetchText {
			for _, part := range em.TextBody {
				if part.PartID != nil {
					if bv, ok := em.BodyValues[*part.PartID]; ok {
						bodyValues[*part.PartID] = applyMaxBodyValueBytes(bv, maxBytes)
					}
				}
			}
		}
		if fetchHTML {
			for _, part := range em.HTMLBody {
				if part.PartID != nil {
					if bv, ok := em.BodyValues[*part.PartID]; ok {
						bodyValues[*part.PartID] = applyMaxBodyValueBytes(bv, maxBytes)
					}
				}
			}
		}
	}

	filteredBodyStructure := FilterEmailBodyPart(em.BodyStructure, bodyProps)
	filteredTextBody := make([]any, 0, len(em.TextBody))
	for _, p := range em.TextBody {
		filteredTextBody = append(filteredTextBody, FilterEmailBodyPart(p, bodyProps))
	}
	filteredHTMLBody := make([]any, 0, len(em.HTMLBody))
	for _, p := range em.HTMLBody {
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
			"headers":       em.Headers,
			"bodyStructure": filteredBodyStructure,
			"textBody":      filteredTextBody,
			"htmlBody":      filteredHTMLBody,
			"attachments":   filteredAttachments,
			"hasAttachment": em.HasAttachment,
			"preview":       em.Preview,
		}
		if len(bodyValues) > 0 {
			out["bodyValues"] = bodyValues
		} else if len(em.BodyValues) > 0 {
			out["bodyValues"] = em.BodyValues
		}
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
			if len(bodyValues) > 0 {
				out["bodyValues"] = bodyValues
			} else {
				out["bodyValues"] = em.BodyValues
			}
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
	return EmailBodyValue{
		Value:             bv.Value[:maxBytes],
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

func handleEmailSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.EmailState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", fmt.Sprintf("state token %q does not match current state %q", ifInState, oldState))
		}

		created := make(map[string]*Email)
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
				if problem := validateEmailCreateData(emData); problem != "" {
					return "", fmt.Errorf("%s", problem)
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
				}
				partID1 := "1"
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
							em.Keywords[k] = boolVal
						}
					}
				}

				if textBody, ok := emData["textBody"].(string); ok {
					em.BodyValues = map[string]EmailBodyValue{"1": {Value: textBody}}
					em.TextBody = []EmailBodyPart{{PartID: &partID1, Type: "text/plain", Size: uint64(len(textBody))}}
					em.Preview = preview(textBody, 256)
					em.BodyStructure = EmailBodyPart{PartID: &partID1, Type: "text/plain", Size: uint64(len(textBody))}
				} else {
					if bodyValObj, ok := emData["bodyValues"].(map[string]any); ok {
						bvMap := make(map[string]EmailBodyValue)
						for k, v := range bodyValObj {
							if bvData, ok := v.(map[string]any); ok {
								val, _ := bvData["value"].(string)
								bvMap[k] = EmailBodyValue{Value: val}
							}
						}
						em.BodyValues = bvMap
					}
					// RFC 8621 Section 4.6: the server MUST reconstruct bodyStructure,
					// textBody, htmlBody, and preview from the given body parts and values.
					if parts, ok := emData["textBody"].([]any); ok {
						for _, raw := range parts {
							if part, ok := raw.(map[string]any); ok {
								partID, _ := part["partId"].(string)
								typ, _ := part["type"].(string)
								if typ == "" {
									typ = "text/plain"
								}
								size := uint64(0)
								if v, has := em.BodyValues[partID]; has {
									size = uint64(len(v.Value))
								}
								pID := partID
								em.TextBody = append(em.TextBody, EmailBodyPart{PartID: &pID, Type: typ, Size: size})
								em.BodyStructure = EmailBodyPart{PartID: &pID, Type: typ, Size: size}
								if v, has := em.BodyValues[partID]; has {
									em.Preview = preview(v.Value, 256)
								}
							}
						}
					}
					if parts, ok := emData["htmlBody"].([]any); ok {
						for _, raw := range parts {
							if part, ok := raw.(map[string]any); ok {
								partID, _ := part["partId"].(string)
								typ, _ := part["type"].(string)
								if typ == "" {
									typ = "text/html"
								}
								size := uint64(0)
								if v, has := em.BodyValues[partID]; has {
									size = uint64(len(v.Value))
								}
								pID := partID
								em.HTMLBody = append(em.HTMLBody, EmailBodyPart{PartID: &pID, Type: typ, Size: size})
								if em.BodyStructure.Type == "" {
									em.BodyStructure = EmailBodyPart{PartID: &pID, Type: typ, Size: size}
								}
								if em.Preview == "" {
									if v, has := em.BodyValues[partID]; has {
										em.Preview = preview(v.Value, 256)
									}
								}
							}
						}
					}
				}

				createdEM, err := backend.CreateEmail(ctx, em)
				if err != nil {
					return "", err
				}
				created[clientKey] = createdEM
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

// validateEmailCreateData enforces the RFC 8621 Section 4.6 constraints on Email
// objects submitted for creation, returning a human-readable problem description or
// "" if the object is acceptable.
func validateEmailCreateData(emData map[string]any) string {
	if _, has := emData["headers"]; has {
		return "the \"headers\" property MUST NOT be given on Email creation (RFC 8621 Section 4.6)"
	}
	if _, hasBS := emData["bodyStructure"]; hasBS {
		if _, has := emData["textBody"]; has {
			return "if \"bodyStructure\" is given, \"textBody\" MUST NOT be given (RFC 8621 Section 4.6)"
		}
		if _, has := emData["htmlBody"]; has {
			return "if \"bodyStructure\" is given, \"htmlBody\" MUST NOT be given (RFC 8621 Section 4.6)"
		}
		if _, has := emData["attachments"]; has {
			return "if \"bodyStructure\" is given, \"attachments\" MUST NOT be given (RFC 8621 Section 4.6)"
		}
	}
	if parts, ok := emData["textBody"].([]any); ok {
		if len(parts) != 1 {
			return "\"textBody\" MUST contain exactly one body part of type \"text/plain\" (RFC 8621 Section 4.6)"
		}
		if part, ok := parts[0].(map[string]any); !ok {
			return "\"textBody\" MUST contain exactly one body part of type \"text/plain\" (RFC 8621 Section 4.6)"
		} else if t, has := part["type"]; has && t != "text/plain" {
			return "\"textBody\" MUST contain exactly one body part of type \"text/plain\" (RFC 8621 Section 4.6)"
		}
	}
	if parts, ok := emData["htmlBody"].([]any); ok {
		if len(parts) != 1 {
			return "\"htmlBody\" MUST contain exactly one body part of type \"text/html\" (RFC 8621 Section 4.6)"
		}
		if part, ok := parts[0].(map[string]any); !ok {
			return "\"htmlBody\" MUST contain exactly one body part of type \"text/html\" (RFC 8621 Section 4.6)"
		} else if t, has := part["type"]; has && t != "text/html" {
			return "\"htmlBody\" MUST contain exactly one body part of type \"text/html\" (RFC 8621 Section 4.6)"
		}
	}
	if mbMap, ok := emData["mailboxIds"].(map[string]any); !ok || len(mbMap) == 0 {
		return "an Email in the mail store MUST belong to one or more Mailboxes; \"mailboxIds\" is required (RFC 8621 Section 4.1.1)"
	}
	return ""
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
		createMap, _ := args["emails"].(map[string]any)
		if createMap == nil {
			createMap, _ = args["create"].(map[string]any)
		}
		created := make(map[string]*Email)
		notCreated := make(map[string]any)
		oldState := backend.State(ctx)

		for clientKey, raw := range createMap {
			emData, ok := raw.(map[string]any)
			if !ok {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Description: "invalid EmailImport object"}
				continue
			}
			blobID, _ := emData["blobId"].(string)
			if blobID == "" {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Description: "blobId is required"}
				continue
			}

			// Parse the raw message blob so the imported Email carries real headers/body,
			// not a fabricated subject.
			em := &Email{BlobID: Id(blobID)}
			if blobBackend != nil {
				if blob, found, _ := blobBackend.GetBlob(ctx, accountID, blobID); found && blob != nil {
					if parsed, err := parseRFC822(blob.Data); err == nil {
						em = parsed
						em.BlobID = Id(blobID)
					}
				} else {
					notCreated[clientKey] = SetError{Type: "blobNotFound", Description: "blob not found: " + blobID}
					continue
				}
			}

			// Apply the client-supplied mailboxIds, keywords, and receivedAt (RFC 8621 Section 4.8).
			if mbIDs, ok := emData["mailboxIds"].(map[string]any); ok {
				em.MailboxIDs = make(map[Id]bool, len(mbIDs))
				for id, v := range mbIDs {
					if b, ok := v.(bool); ok {
						em.MailboxIDs[Id(id)] = b
					}
				}
			}
			if kws, ok := emData["keywords"].(map[string]any); ok {
				em.Keywords = make(map[string]bool, len(kws))
				for k, v := range kws {
					if b, ok := v.(bool); ok {
						em.Keywords[k] = b
					}
				}
			}
			if rcptAt, ok := emData["receivedAt"].(string); ok && rcptAt != "" {
				em.ReceivedAt = rcptAt
			}

			createdEm, err := backend.CreateEmail(ctx, em)
			if err != nil {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Description: err.Error()}
				continue
			}
			created[clientKey] = createdEm
		}

		return "Email/import", map[string]any{
			"accountId":  accountID,
			"oldState":   oldState,
			"newState":   backend.State(ctx),
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
