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
			"state":     backend.SubmissionState(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleEmailSubmissionChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		created, updated, destroyed, newState, hasMore := backend.SubmissionChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "EmailSubmission/changes", map[string]any{
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

func handleEmailSubmissionSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.SubmissionState(ctx)
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

						// RFC 8621 Section 7.5: onSuccessUpdateEmail
						if patch, ok := subData["onSuccessUpdateEmail"].(map[string]any); ok && emailID != "" {
							_, _ = backend.UpdateEmail(ctx, Id(emailID), patch)
						}
						// RFC 8621 Section 7.5: onSuccessDestroyEmail
						if destroy, ok := subData["onSuccessDestroyEmail"].(bool); ok && destroy && emailID != "" {
							_, _ = backend.DeleteEmail(ctx, Id(emailID))
						}
					}
				}
			}
		}

		return "EmailSubmission/set", map[string]any{
			"accountId": accountID,
			"oldState":  oldState,
			"newState":  backend.SubmissionState(ctx),
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
