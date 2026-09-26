package jmapmail

import (
	"context"
	"fmt"
	"strings"
	"time"

	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcopy"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

func HandleEmailCopy(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, fromAccountID := jmapcopy.ResolveCopyAccountIDs(args)
		srcCtx := jmapcopy.SourceAccountContext(ctx, args)

		oldState, errInv := jmapcopy.ValidateCopyStates(ctx, srcCtx, args, backend.EmailState, backend.EmailState)
		if errInv != nil {
			return errInv.Name, errInv.Args
		}

		onSuccessDestroy, _ := args["onSuccessDestroyOriginal"].(bool)
		created := make(map[string]*Email)
		notCreated := make(map[string]any)
		destroyOriginals := make([]jmapcore.Id, 0)
		creationRefs := jmaphandler.NewSetCreationRefs(ctx)

		if createMap, ok := args["create"].(map[string]any); ok {
			for clientKey, raw := range createMap {
				emData, ok := raw.(map[string]any)
				if !ok {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Description: "invalid create entry"}
					continue
				}
				idStr, _ := emData["id"].(string)
				if idStr == "" {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Description: "missing id"}
					continue
				}
				resolvedID := jmaphandler.ResolveCreationID(idStr, creationRefs)
				list, notFound, _ := backend.GetEmails(srcCtx, []jmapcore.Id{jmapcore.Id(resolvedID)})
				if len(list) == 0 || len(notFound) > 0 {
					notCreated[clientKey] = jmapcore.SetError{Type: "notFound", Description: "email not found"}
					continue
				}

				cp := *list[0]
				cp.ID = ""
				cp.ThreadID = ""

				// Apply property overrides if specified (RFC 8621 Section 4.6)
				if mbMap, ok := emData["mailboxIds"].(map[string]any); ok {
					cp.MailboxIDs = make(map[jmapcore.Id]bool)
					for k, v := range mbMap {
						if v != nil {
							resolvedMBID := jmaphandler.ResolveCreationID(k, creationRefs)
							cp.MailboxIDs[jmapcore.Id(resolvedMBID)] = true
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
				if err != nil {
					if setErr, ok := err.(jmapcore.SetError); ok {
						notCreated[clientKey] = setErr
					} else if setErrPtr, ok := err.(*jmapcore.SetError); ok && setErrPtr != nil {
						notCreated[clientKey] = *setErrPtr
					} else {
						notCreated[clientKey] = jmapcore.SetError{Type: "serverFail", Description: err.Error()}
					}
				} else {
					created[clientKey] = createdEM
					jmaphandler.RecordCreationRefs(ctx, creationRefs, clientKey, createdEM.ID)
					destroyOriginals = append(destroyOriginals, jmapcore.Id(resolvedID))
				}
			}
		}

		if onSuccessDestroy {
			for _, srcID := range destroyOriginals {
				_, _ = backend.DeleteEmail(srcCtx, srcID)
			}
		}

		return "Email/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.EmailState(ctx),
			"created":       jmaphandler.NilIfEmpty(created),
			"notCreated":    jmaphandler.NilIfEmpty(notCreated),
		}
	}
}

func HandleEmailImport(backend MailBackend, blobBackend jmapblob.BlobBackend) jmaphandler.MethodHandler {
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
		notCreated := make(map[string]jmapcore.SetError)
		creationRefs := jmaphandler.NewSetCreationRefs(ctx)

		for clientKey, raw := range emailsMap {
			emData, ok := raw.(map[string]any)
			if !ok {
				notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties"}
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
				notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Properties: missingProps}
				continue
			}

			blobID = jmaphandler.ResolveCreationID(blobID, creationRefs)

			// Validate mailbox existence
			var mbIDsList []jmapcore.Id
			for id := range mbIDs {
				resolvedMBID := jmaphandler.ResolveCreationID(id, creationRefs)
				mbIDsList = append(mbIDsList, jmapcore.Id(resolvedMBID))
			}
			_, notFoundMBs, err := backend.GetMailboxes(ctx, mbIDsList)
			if err != nil || len(notFoundMBs) > 0 {
				notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Properties: []string{"mailboxIds"}}
				continue
			}

			var kwMap map[string]bool
			if kwRaw, hasKw := emData["keywords"]; hasKw && kwRaw != nil {
				kwRawMap, ok := kwRaw.(map[string]any)
				if !ok {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Properties: []string{"keywords"}}
					continue
				}
				kwMap = make(map[string]bool, len(kwRawMap))
				invalidKw := false
				for k, v := range kwRawMap {
					b, ok := v.(bool)
					if !ok || !IsValidKeyword(k) {
						invalidKw = true
						break
					}
					kwMap[k] = b
				}
				if invalidKw {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Properties: []string{"keywords"}}
					continue
				}
			}

			var rcptAtStr string
			if rcptAt, hasRcpt := emData["receivedAt"]; hasRcpt && rcptAt != nil {
				s, ok := rcptAt.(string)
				if !ok || s == "" {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Properties: []string{"receivedAt"}}
					continue
				}
				if _, err := time.Parse(time.RFC3339, s); err != nil {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Properties: []string{"receivedAt"}}
					continue
				}
				rcptAtStr = s
			}

			var blobData []byte
			if blobBackend != nil {
				blob, found, _ := blobBackend.GetBlob(ctx, accountID, blobID)
				if !found || blob == nil {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Properties: []string{"blobId"}}
					continue
				}
				blobData = blob.Data
			} else {
				notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Properties: []string{"blobId"}}
				continue
			}

			em, err := ParseRFC822WithAccount(accountID, blobData, blobBackend)
			if err != nil {
				notCreated[clientKey] = jmapcore.SetError{Type: "invalidEmail", Description: fmt.Sprintf("invalid email: %v", err)}
				continue
			}
			em.BlobID = jmapcore.Id(blobID)

			em.MailboxIDs = make(map[jmapcore.Id]bool, len(mbIDs))
			for id := range mbIDs {
				resolvedMBID := jmaphandler.ResolveCreationID(id, creationRefs)
				em.MailboxIDs[jmapcore.Id(resolvedMBID)] = true
			}

			if kwMap != nil {
				em.Keywords = kwMap
			}
			if rcptAtStr != "" {
				em.ReceivedAt = rcptAtStr
			}

			createdEm, err := backend.CreateEmail(ctx, em)
			if err != nil {
				if setErr, ok := err.(jmapcore.SetError); ok {
					notCreated[clientKey] = setErr
				} else if setErrPtr, ok := err.(*jmapcore.SetError); ok && setErrPtr != nil {
					notCreated[clientKey] = *setErrPtr
				} else {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
				}
				continue
			}
			created[clientKey] = map[string]any{
				"id":       createdEm.ID,
				"blobId":   createdEm.BlobID,
				"threadId": createdEm.ThreadID,
				"size":     createdEm.Size,
			}
			jmaphandler.RecordCreationRefs(ctx, creationRefs, clientKey, createdEm.ID)
		}

		newState := backend.EmailState(ctx)
		return "Email/import", map[string]any{
			"accountId":  accountID,
			"oldState":   oldState,
			"newState":   newState,
			"created":    jmaphandler.NilIfEmpty(created),
			"notCreated": jmaphandler.NilIfEmpty(notCreated),
		}
	}
}

func HandleEmailParse(backend MailBackend, blobBackend jmapblob.BlobBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		blobIDsRaw, _ := args["blobIds"].([]any)
		props := jmaphandler.ParseProperties(args)
		bodyProps := ParsePropertiesBody(args)
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
		notParsable := []jmapcore.Id{}
		notFound := []jmapcore.Id{}

		for _, blobIDRaw := range blobIDsRaw {
			blobIDStr, ok := blobIDRaw.(string)
			if !ok || blobIDStr == "" {
				continue
			}

			if blobBackend == nil {
				notFound = append(notFound, jmapcore.Id(blobIDStr))
				continue
			}

			blob, found, _ := blobBackend.GetBlob(ctx, accountID, blobIDStr)
			if !found || blob == nil {
				notFound = append(notFound, jmapcore.Id(blobIDStr))
				continue
			}

			em, err := ParseRFC822(blob.Data)
			if err != nil {
				notParsable = append(notParsable, jmapcore.Id(blobIDStr))
				continue
			}
			em.BlobID = jmapcore.Id(blobIDStr)
			formatted := FormatEmailGet(em, props, parsedHeaderProps, bodyProps, fetchText, fetchHTML, fetchAll, maxBytes)
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

func HandleEmailVerifySmime(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		emailIDsRaw, _ := args["emailIds"].([]any)

		ids := make([]jmapcore.Id, 0, len(emailIDsRaw))
		for _, item := range emailIDsRaw {
			if idStr, ok := item.(string); ok {
				ids = append(ids, jmapcore.Id(idStr))
			}
		}

		verified, notFound, _ := backend.VerifySmime(ctx, ids)
		if verified == nil {
			verified = make(map[jmapcore.Id]*SmimeVerificationResult)
		}
		if notFound == nil {
			notFound = []jmapcore.Id{}
		}

		return "Email/verifySmime", map[string]any{
			"accountId": accountID,
			"verified":  verified,
			"notFound":  notFound,
		}
	}
}

func HandleSearchSnippetGet(backend MailBackend) jmaphandler.MethodHandler {
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
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			ids := make([]jmapcore.Id, 0, len(emailIDsRaw))
			for _, item := range emailIDsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, jmapcore.Id(idStr))
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
			notFound = []jmapcore.Id{}
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
					if len(p) > 255 {
						p = p[:255]
					}
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
