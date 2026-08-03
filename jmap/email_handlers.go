package jmap

import (
	"context"
	"fmt"
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
		return "Thread/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       args["sinceState"],
			"newState":       backend.State(ctx),
			"hasMoreChanges": false,
			"created":        []Id{},
			"updated":        []Id{},
			"destroyed":      []Id{},
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
			"state":     backend.State(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleEmailChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Email/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       args["sinceState"],
			"newState":       backend.State(ctx),
			"hasMoreChanges": false,
			"created":        []Id{},
			"updated":        []Id{},
			"destroyed":      []Id{},
		}
	}
}

func handleEmailSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.State(ctx)

		created := make(map[string]*Email)
		updated := make(map[string]any)
		notUpdated := make(map[string]any)
		destroyed := []Id{}

		if createMap, ok := args["create"].(map[string]any); ok {
			for clientKey, raw := range createMap {
				if emData, ok := raw.(map[string]any); ok {
					subject, _ := emData["subject"].(string)
					em := &Email{
						Subject: subject,
						BlobID:  Id(fmt.Sprintf("blob-%d", time.Now().UnixNano())),
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
			"newState":   backend.State(ctx),
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

		created := make(map[string]*Email)
		for clientKey, raw := range createMap {
			if emData, ok := raw.(map[string]any); ok {
				if idStr, ok := emData["id"].(string); ok {
					list, _, _ := backend.GetEmails(ctx, []Id{Id(idStr)})
					if len(list) > 0 {
						cp := *list[0]
						cp.ID = ""
						cp.ThreadID = ""
						createdEM, err := backend.CreateEmail(ctx, &cp)
						if err == nil {
							created[clientKey] = createdEM
						}
					}
				}
			}
		}

		return "Email/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      backend.State(ctx),
			"newState":      backend.State(ctx),
			"created":       created,
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
			"queryState":          backend.State(ctx),
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
			"newQueryState": backend.State(ctx),
			"added":         []any{},
			"removed":       []Id{},
		}
		if upToID != "" {
			res["upToId"] = upToID
		}
		return "Email/queryChanges", res
	}
}

func handleEmailImport(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)
		created := make(map[string]*Email)

		for clientKey, raw := range createMap {
			if emData, ok := raw.(map[string]any); ok {
				blobID, _ := emData["blobId"].(string)
				em, err := backend.CreateEmail(ctx, &Email{
					BlobID:  Id(blobID),
					Subject: "Imported Message",
				})
				if err == nil {
					created[clientKey] = em
				}
			}
		}

		return "Email/import", map[string]any{
			"accountId": accountID,
			"oldState":  backend.State(ctx),
			"newState":  backend.State(ctx),
			"created":   created,
		}
	}
}

func handleEmailParse(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		blobIDs, _ := args["blobIds"].([]any)
		parsed := make(map[string]*Email)

		for _, item := range blobIDs {
			if blobIDStr, ok := item.(string); ok {
				parsed[blobIDStr] = &Email{
					ID:      Id("parsed-" + blobIDStr),
					BlobID:  Id(blobIDStr),
					Subject: "Parsed Email Subject",
				}
			}
		}

		return "Email/parse", map[string]any{
			"accountId": accountID,
			"parsed":    parsed,
			"notFound":  []Id{},
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

		ids := make([]Id, 0, len(emailIDsRaw))
		for _, item := range emailIDsRaw {
			if idStr, ok := item.(string); ok {
				ids = append(ids, Id(idStr))
			}
		}

		emails, _, _ := backend.GetEmails(ctx, ids)
		var list []SearchSnippet
		for _, em := range emails {
			subj := em.Subject
			prev := em.Preview
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
			"notFound":  []Id{},
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
			"state":     backend.State(ctx),
			"list":      list,
			"notFound":  []Id{},
		}
	}
}

func handleIdentityChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Identity/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       args["sinceState"],
			"newState":       backend.State(ctx),
			"hasMoreChanges": false,
			"created":        []Id{},
			"updated":        []Id{},
			"destroyed":      []Id{},
		}
	}
}

func handleIdentitySet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Identity/set", map[string]any{
			"accountId": accountID,
			"oldState":  backend.State(ctx),
			"newState":  backend.State(ctx),
		}
	}
}

// Mailbox/copy Handler (RFC 8621 Section 2.4)
func handleMailboxCopy(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		createRaw, _ := args["create"].(map[string]any)

		copied := make(map[string]*Mailbox)
		notCopied := make(map[string]SetError)

		for idStr := range createRaw {
			notCopied[idStr] = SetError{Type: "cannotCalculate", Description: "cross-account copy not supported"}
		}

		return "Mailbox/copy", map[string]any{
			"accountId": accountID,
			"oldState":  backend.State(ctx),
			"newState":  backend.State(ctx),
			"copied":    copied,
			"notCopied": notCopied,
		}
	}
}


