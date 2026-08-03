package jmap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Thread Handlers (RFC 8621 Section 3)

func handleThreadGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, _ := args["ids"].([]any)

		ids := make([]Id, 0, len(idsRaw))
		for _, item := range idsRaw {
			if idStr, ok := item.(string); ok {
				ids = append(ids, Id(idStr))
			}
		}

		list, notFound, _ := backend.GetThreads(ctx, ids)
		if list == nil {
			list = []*Thread{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Thread/get", map[string]any{
			"accountId": accountID,
			"state":     backend.State(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleThreadChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		created, updated, destroyed, newState, hasMore := backend.ThreadChanges(ctx, sinceState)
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

		return "Email/get", map[string]any{
			"accountId": accountID,
			"state":     backend.EmailState(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleEmailChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		created, updated, destroyed, newState, hasMore := backend.EmailChanges(ctx, sinceState)
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
		destroyed := []Id{}

		if createMap, ok := args["create"].(map[string]any); ok {
			for clientKey, raw := range createMap {
				if emData, ok := raw.(map[string]any); ok {
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

					em := &Email{
						Subject:    subject,
						BlobID:     Id(blobIDStr),
						ReceivedAt: receivedAt,
						SentAt:     sentAt,
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
						em.TextBody = []EmailBodyPart{{PartID: "1", Type: "text/plain", Size: uint64(len(textBody))}}
						em.Preview = textBody
					} else if bodyValObj, ok := emData["bodyValues"].(map[string]any); ok {
						bvMap := make(map[string]EmailBodyValue)
						for k, v := range bodyValObj {
							if bvData, ok := v.(map[string]any); ok {
								val, _ := bvData["value"].(string)
								bvMap[k] = EmailBodyValue{Value: val}
							}
						}
						em.BodyValues = bvMap
					}

					createdEM, err := backend.CreateEmail(ctx, em)
					if err == nil {
						created[clientKey] = createdEM
					}
				}
			}
		}

		if updateMap, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateMap {
				if patch, ok := patchRaw.(map[string]any); ok {
					updatedEM, err := backend.UpdateEmail(ctx, Id(idStr), patch)
					if err != nil {
						notUpdated[idStr] = map[string]any{
							"type":        "notFound",
							"description": err.Error(),
						}
					} else {
						updated[idStr] = updatedEM
					}
				}
			}
		}

		if destroyList, ok := args["destroy"].([]any); ok {
			for _, rawID := range destroyList {
				if idStr, ok := rawID.(string); ok {
					if ok, _ := backend.DeleteEmail(ctx, Id(idStr)); ok {
						destroyed = append(destroyed, Id(idStr))
					}
				}
			}
		}

		return "Email/set", map[string]any{
			"accountId":  accountID,
			"oldState":   oldState,
			"newState":   backend.EmailState(ctx),
			"created":    created,
			"updated":    updated,
			"notUpdated": notUpdated,
			"destroyed":  destroyed,
		}
	}
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
		filter, _ := args["filter"].(map[string]any)

		var comparators []Comparator
		if sortRaw, ok := args["sort"].([]any); ok {
			for _, item := range sortRaw {
				if compMap, ok := item.(map[string]any); ok {
					prop, _ := compMap["property"].(string)
					asc, isBool := compMap["isAscending"].(bool)
					if !isBool {
						asc = true
					}
					coll, _ := compMap["collation"].(string)
					comparators = append(comparators, Comparator{
						Property:    prop,
						IsAscending: asc,
						Collation:   coll,
					})
				}
			}
		}

		position := 0
		if posVal, ok := args["position"].(float64); ok {
			position = int(posVal)
		}

		var limit *uint64
		if limVal, ok := args["limit"].(float64); ok {
			l := uint64(limVal)
			limit = &l
		}

		ids, total, _ := backend.QueryEmails(ctx, filter, comparators, position, limit)

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
		res := map[string]any{
			"accountId":     accountID,
			"oldQueryState": args["sinceQueryState"],
			"newQueryState": backend.EmailState(ctx),
			"added":         []any{},
			"removed":       []Id{},
		}
		if upToID != "" {
			res["upToId"] = upToID
		}
		return "Email/queryChanges", res
	}
}

func handleEmailImport(backend MailBackend, blobBackend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)
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
			if recvAt, ok := emData["receivedAt"].(string); ok {
				em.ReceivedAt = recvAt
			}

			imported, err := backend.CreateEmail(ctx, em)
			if err != nil {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Description: err.Error()}
			} else {
				created[clientKey] = imported
			}
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

func handleEmailParse(backend MailBackend, blobBackend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		blobIDs, _ := args["blobIds"].([]any)
		parsed := make(map[string]*Email)
		notParsable := make([]Id, 0)
		notFound := make([]Id, 0)

		for _, item := range blobIDs {
			blobIDStr, ok := item.(string)
			if !ok {
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
		emailIDsRaw, _ := args["emailIds"].([]any)

		var filterText string
		if filterMap, ok := args["filter"].(map[string]any); ok {
			if txt, ok := filterMap["text"].(string); ok {
				filterText = txt
			} else if body, ok := filterMap["body"].(string); ok {
				filterText = body
			}
		}

		ids := make([]Id, 0, len(emailIDsRaw))
		for _, item := range emailIDsRaw {
			if idStr, ok := item.(string); ok {
				ids = append(ids, Id(idStr))
			}
		}

		emails, notFound, _ := backend.GetEmails(ctx, ids)
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

		created, updated, destroyed, newState, hasMore := backend.IdentityChanges(ctx, sinceState)
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
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				patch, _ := patchRaw.(map[string]any)
				_, err := backend.UpdateIdentity(ctx, Id(idStr), patch)
				if err != nil {
					notUpdated[idStr] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					updated[idStr] = nil
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					okDel, err := backend.DeleteIdentity(ctx, Id(idStr))
					if err != nil {
						notDestroyed[idStr] = SetError{Type: "serverFail", Description: err.Error()}
					} else if !okDel {
						notDestroyed[idStr] = SetError{Type: "notFound", Description: "identity not found"}
					} else {
						destroyed = append(destroyed, Id(idStr))
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


