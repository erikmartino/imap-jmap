package jmap

import (
	"context"
)

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
