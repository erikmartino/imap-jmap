package jmap

import (
	"context"
	"fmt"
	"time"
)

// RegisterMailHandlers registers all RFC 8621 JMAP Mail and RFC 9219 S/MIME methods into MethodRegistry.
func RegisterMailHandlers(r *MethodRegistry, backend MailBackend) {
	// Mailbox (Section 2)
	r.Register("Mailbox/get", handleMailboxGet(backend))
	r.Register("Mailbox/changes", handleMailboxChanges(backend))
	r.Register("Mailbox/set", handleMailboxSet(backend))
	r.Register("Mailbox/query", handleMailboxQuery(backend))
	r.Register("Mailbox/queryChanges", handleMailboxQueryChanges(backend))

	// Thread (Section 3)
	r.Register("Thread/get", handleThreadGet(backend))
	r.Register("Thread/changes", handleThreadChanges(backend))

	// Email (Section 4)
	r.Register("Email/get", handleEmailGet(backend))
	r.Register("Email/changes", handleEmailChanges(backend))
	r.Register("Email/set", handleEmailSet(backend))
	r.Register("Email/copy", handleEmailCopy(backend))
	r.Register("Email/query", handleEmailQuery(backend))
	r.Register("Email/queryChanges", handleEmailQueryChanges(backend))
	r.Register("Email/import", handleEmailImport(backend))
	r.Register("Email/parse", handleEmailParse(backend))

	// S/MIME Verification (RFC 9219 Section 4)
	r.Register("Email/verifySmime", handleEmailVerifySmime(backend))

	// SearchSnippet (Section 5)
	r.Register("SearchSnippet/get", handleSearchSnippetGet(backend))

	// Identity (Section 6)
	r.Register("Identity/get", handleIdentityGet(backend))
	r.Register("Identity/changes", handleIdentityChanges(backend))
	r.Register("Identity/set", handleIdentitySet(backend))

	// EmailSubmission (Section 7)
	r.Register("EmailSubmission/get", handleEmailSubmissionGet(backend))
	r.Register("EmailSubmission/changes", handleEmailSubmissionChanges(backend))
	r.Register("EmailSubmission/set", handleEmailSubmissionSet(backend))
	r.Register("EmailSubmission/query", handleEmailSubmissionQuery(backend))
	r.Register("EmailSubmission/queryChanges", handleEmailSubmissionQueryChanges(backend))
}

// Mailbox Handlers (RFC 8621 Section 2)

func handleMailboxGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)

		var list []*Mailbox
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetMailboxes(ctx, ids)
		} else {
			list, err = backend.GetAllMailboxes(ctx)
		}

		if err != nil || list == nil {
			list = []*Mailbox{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Mailbox/get", map[string]any{
			"accountId": accountID,
			"state":     backend.State(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleMailboxChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Mailbox/changes", map[string]any{
			"accountId":         accountID,
			"oldState":          args["sinceState"],
			"newState":          backend.State(ctx),
			"hasMoreChanges":    false,
			"created":           []Id{},
			"updated":           []Id{},
			"destroyed":         []Id{},
			"updatedProperties": []string{},
		}
	}
}

func handleMailboxSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		created := make(map[string]*Mailbox)
		destroyed := []Id{}

		if createMap, ok := args["create"].(map[string]any); ok {
			for clientKey, raw := range createMap {
				if mbData, ok := raw.(map[string]any); ok {
					name, _ := mbData["name"].(string)
					mb, err := backend.CreateMailbox(ctx, &Mailbox{Name: name})
					if err == nil {
						created[clientKey] = mb
					}
				}
			}
		}

		if destroyList, ok := args["destroy"].([]any); ok {
			for _, rawID := range destroyList {
				if idStr, ok := rawID.(string); ok {
					if ok, _ := backend.DeleteMailbox(ctx, Id(idStr)); ok {
						destroyed = append(destroyed, Id(idStr))
					}
				}
			}
		}

		return "Mailbox/set", map[string]any{
			"accountId": accountID,
			"oldState":  backend.State(ctx),
			"newState":  backend.State(ctx),
			"created":   created,
			"destroyed": destroyed,
		}
	}
}

func handleMailboxQuery(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		all, _ := backend.GetAllMailboxes(ctx)
		ids := make([]Id, len(all))
		for i, mb := range all {
			ids[i] = mb.ID
		}
		return "Mailbox/query", map[string]any{
			"accountId":           accountID,
			"queryState":          backend.State(ctx),
			"canCalculateChanges": true,
			"position":            0,
			"ids":                 ids,
			"total":               len(ids),
		}
	}
}

func handleMailboxQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Mailbox/queryChanges", map[string]any{
			"accountId":     accountID,
			"oldQueryState": args["sinceQueryState"],
			"newQueryState": backend.State(ctx),
			"added":         []any{},
			"removed":       []Id{},
		}
	}
}

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
		created := make(map[string]*Email)
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
					createdEM, err := backend.CreateEmail(ctx, em)
					if err == nil {
						created[clientKey] = createdEM
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
			"accountId": accountID,
			"oldState":  backend.State(ctx),
			"newState":  backend.State(ctx),
			"created":   created,
			"destroyed": destroyed,
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

		res := map[string]any{
			"accountId":           accountID,
			"queryState":          backend.State(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"ids":                 ids,
			"total":               total,
		}
		return "Email/query", res
	}
}

func handleEmailQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Email/queryChanges", map[string]any{
			"accountId":     accountID,
			"oldQueryState": args["sinceQueryState"],
			"newQueryState": backend.State(ctx),
			"added":         []any{},
			"removed":       []Id{},
		}
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

// EmailSubmission Handlers (RFC 8621 Section 7)

func handleEmailSubmissionGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, _ := args["ids"].([]any)

		ids := make([]Id, 0, len(idsRaw))
		for _, item := range idsRaw {
			if idStr, ok := item.(string); ok {
				ids = append(ids, Id(idStr))
			}
		}

		list, notFound, _ := backend.GetSubmissions(ctx, ids)
		if list == nil {
			list = []*EmailSubmission{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "EmailSubmission/get", map[string]any{
			"accountId": accountID,
			"state":     backend.State(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleEmailSubmissionChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "EmailSubmission/changes", map[string]any{
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

func handleEmailSubmissionSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		created := make(map[string]*EmailSubmission)

		if createMap, ok := args["create"].(map[string]any); ok {
			for clientKey, raw := range createMap {
				if subData, ok := raw.(map[string]any); ok {
					identityID, _ := subData["identityId"].(string)
					emailID, _ := subData["emailId"].(string)
					sub, err := backend.CreateSubmission(ctx, &EmailSubmission{
						IdentityID: Id(identityID),
						EmailID:    Id(emailID),
					})
					if err == nil {
						created[clientKey] = sub
					}
				}
			}
		}

		return "EmailSubmission/set", map[string]any{
			"accountId": accountID,
			"oldState":  backend.State(ctx),
			"newState":  backend.State(ctx),
			"created":   created,
		}
	}
}

func handleEmailSubmissionQuery(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "EmailSubmission/query", map[string]any{
			"accountId":           accountID,
			"queryState":          backend.State(ctx),
			"canCalculateChanges": true,
			"position":            0,
			"ids":                 []Id{},
			"total":               0,
		}
	}
}

func handleEmailSubmissionQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "EmailSubmission/queryChanges", map[string]any{
			"accountId":     accountID,
			"oldQueryState": args["sinceQueryState"],
			"newQueryState": backend.State(ctx),
			"added":         []any{},
			"removed":       []Id{},
		}
	}
}
