package jmap

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func handleEmailCopy(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		fromAccountID, _ := args["fromAccountId"].(string)
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)
		onSuccessDestroy, _ := args["onSuccessDestroyOriginal"].(bool)

		if fromAccountID == "" || accountID == "" || fromAccountID == accountID {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, "fromAccountId and accountId must be present and distinct")
		}

		fromCtx := ContextWithAccountID(ctx, fromAccountID)
		destCtx := ContextWithAccountID(ctx, accountID)

		oldState := backend.EmailState(destCtx)
		created := make(map[string]*Email)
		notCreated := make(map[string]SetError)

		for clientKey, raw := range createMap {
			if emData, ok := raw.(map[string]any); ok {
				if idStr, ok := emData["id"].(string); ok {
					list, _, _ := backend.GetEmails(fromCtx, []Id{Id(idStr)})
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

						createdEM, err := backend.CreateEmail(destCtx, &cp)
						if err == nil {
							created[clientKey] = createdEM
							if onSuccessDestroy {
								_, _ = backend.DeleteEmail(fromCtx, Id(idStr))
							}
						} else {
							notCreated[clientKey] = SetError{Type: "serverFail", Description: err.Error()}
						}
					} else {
						notCreated[clientKey] = SetError{Type: "notFound", Description: "email not found"}
					}
				} else {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Description: "missing id"}
				}
			} else {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Description: "invalid create entry"}
			}
		}

		return "Email/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.EmailState(destCtx),
			"created":       created,
			"notCreated":    notCreated,
		}
	}
}

func handleEmailImport(backend MailBackend, blobBackend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		rawEmails, hasEmails := args["emails"]
		if !hasEmails {
			rawEmails, hasEmails = args["create"]
		}
		if !hasEmails || rawEmails == nil {
			return "error", map[string]any{
				"type":      "invalidArguments",
				"arguments": []string{"emails"},
			}
		}
		emailsMap, ok := rawEmails.(map[string]any)
		if !ok {
			return "error", map[string]any{
				"type":      "invalidArguments",
				"arguments": []string{"emails"},
			}
		}

		accountID, _ := args["accountId"].(string)
		oldState := backend.EmailState(ctx)
		created := make(map[string]any)
		notCreated := make(map[string]SetError)

		for clientKey, raw := range emailsMap {
			emData, ok := raw.(map[string]any)
			if !ok {
				notCreated[clientKey] = SetError{Type: "invalidProperties"}
				continue
			}

			var missingProps []string
			blobIDRaw, hasBlobID := emData["blobId"]
			blobID, isBlobStr := blobIDRaw.(string)
			if !hasBlobID || !isBlobStr || blobID == "" {
				missingProps = append(missingProps, "blobId")
			}

			mbIDsRaw, hasMbIDs := emData["mailboxIds"]
			mbIDs, isMbMap := mbIDsRaw.(map[string]any)
			if !hasMbIDs || !isMbMap || len(mbIDs) == 0 {
				missingProps = append(missingProps, "mailboxIds")
			}

			if len(missingProps) > 0 {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: missingProps}
				continue
			}

			// Validate mailbox existence
			var mbIDsList []Id
			for id := range mbIDs {
				mbIDsList = append(mbIDsList, Id(id))
			}
			_, notFoundMBs, err := backend.GetMailboxes(ctx, mbIDsList)
			if err != nil || len(notFoundMBs) > 0 {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"mailboxIds"}}
				continue
			}

			var kwMap map[string]bool
			if kwRaw, hasKw := emData["keywords"]; hasKw && kwRaw != nil {
				kwRawMap, ok := kwRaw.(map[string]any)
				if !ok {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"keywords"}}
					continue
				}
				kwMap = make(map[string]bool, len(kwRawMap))
				invalidKw := false
				for k, v := range kwRawMap {
					b, ok := v.(bool)
					if !ok || !isValidKeyword(k) {
						invalidKw = true
						break
					}
					kwMap[k] = b
				}
				if invalidKw {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"keywords"}}
					continue
				}
			}

			var rcptAtStr string
			if rcptAt, hasRcpt := emData["receivedAt"]; hasRcpt && rcptAt != nil {
				s, ok := rcptAt.(string)
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
					notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"blobId"}}
					continue
				}
				blobData = blob.Data
			} else {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Properties: []string{"blobId"}}
				continue
			}

			em, err := parseRFC822WithAccount(accountID, blobData, blobBackend)
			if err != nil {
				notCreated[clientKey] = SetError{Type: "invalidEmail", Description: fmt.Sprintf("invalid email: %v", err)}
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

		newState := backend.EmailState(ctx)
		return "Email/import", map[string]any{
			"accountId":  accountID,
			"oldState":   oldState,
			"newState":   newState,
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
		props := parseProperties(args)
		bodyProps := parsePropertiesBody(args)
		fetchText, _ := args["fetchTextBodyValues"].(bool)
		fetchHTML, _ := args["fetchHTMLBodyValues"].(bool)
		fetchAll, _ := args["fetchAllBodyValues"].(bool)
		var maxBytes uint64
		if rawMBV, present := args["maxBodyValueBytes"]; present {
			if mbv, ok := rawMBV.(float64); ok && mbv > 0 && mbv == float64(uint64(mbv)) {
				maxBytes = uint64(mbv)
			}
		}

		var parsedHeaderProps []*ParsedHeaderProperty
		if props != nil {
			for _, p := range props {
				if strings.HasPrefix(p, "header:") {
					if hp, err := ParseHeaderProperty(p); err == nil {
						parsedHeaderProps = append(parsedHeaderProps, hp)
					}
				}
			}
		}

		parsed := make(map[string]any)
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
			em.BlobID = Id(blobIDStr)
			formatted := formatEmailGet(em, props, parsedHeaderProps, bodyProps, fetchText, fetchHTML, fetchAll, maxBytes)
			if emMap, ok := formatted.(map[string]any); ok {
				if _, ok := emMap["id"]; ok {
					emMap["id"] = nil
				}
				if _, ok := emMap["threadId"]; ok {
					emMap["threadId"] = nil
				}
				if _, ok := emMap["mailboxIds"]; ok {
					emMap["mailboxIds"] = nil
				}
				if _, ok := emMap["keywords"]; ok {
					emMap["keywords"] = nil
				}
				if _, ok := emMap["receivedAt"]; ok {
					emMap["receivedAt"] = nil
				}
				parsed[blobIDStr] = emMap
			} else {
				parsed[blobIDStr] = formatted
			}
		}

		res := map[string]any{
			"accountId":   accountID,
			"parsed":      parsed,
			"notParsable": notParsable,
			"notFound":    notFound,
		}
		if len(notParsable) == 0 {
			res["notParsable"] = nil
		}
		if len(notFound) == 0 {
			res["notFound"] = nil
		}
		return "Email/parse", res
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
			var subjPtr *string
			var prevPtr *string

			if filterText != "" {
				// Highlight matching terms with <mark> tags per RFC 8621 Section 5
				idx := strings.Index(strings.ToLower(em.Subject), strings.ToLower(filterText))
				if idx >= 0 {
					matchedText := em.Subject[idx : idx+len(filterText)]
					s := em.Subject[:idx] + "<mark>" + matchedText + "</mark>" + em.Subject[idx+len(filterText):]
					subjPtr = &s
				}

				idxP := strings.Index(strings.ToLower(em.Preview), strings.ToLower(filterText))
				if idxP >= 0 {
					matchedText := em.Preview[idxP : idxP+len(filterText)]
					p := em.Preview[:idxP] + "<mark>" + matchedText + "</mark>" + em.Preview[idxP+len(filterText):]
					prevPtr = &p
				}
			}

			list = append(list, SearchSnippet{
				AccountID: accountID,
				EmailID:   em.ID,
				Subject:   subjPtr,
				Preview:   prevPtr,
			})
		}

		res := map[string]any{
			"accountId": accountID,
			"list":      list,
			"notFound":  notFound,
		}
		if len(notFound) == 0 {
			res["notFound"] = nil
		}
		return "SearchSnippet/get", res
	}
}

// Identity Handlers (RFC 8621 Section 6)
