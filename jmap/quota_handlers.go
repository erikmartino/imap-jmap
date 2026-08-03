package jmap

import (
	"context"
)

// RegisterQuotaHandlers registers RFC 9425 Quota method handlers into MethodRegistry.
func RegisterQuotaHandlers(r *MethodRegistry, backend MailBackend) {
	r.Register("Quota/get", handleQuotaGet(backend))
	r.Register("Quota/changes", handleQuotaChanges(backend))
	r.Register("Quota/query", handleQuotaQuery(backend))
	r.Register("Quota/queryChanges", handleQuotaQueryChanges(backend))
}

// handleQuotaGet implements Quota/get per RFC 9425 Section 4.
func handleQuotaGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)

		var list []*Quota
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetQuotas(ctx, ids)
		} else {
			list, err = backend.GetAllQuotas(ctx)
		}

		if err != nil || list == nil {
			list = []*Quota{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Quota/get", map[string]any{
			"accountId": accountID,
			"state":     backend.QuotaState(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

// handleQuotaChanges implements Quota/changes per RFC 9425 Section 4.
func handleQuotaChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		created, updated, destroyed, newState, hasMore := backend.QuotaChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "Quota/changes", map[string]any{
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

// handleQuotaQuery implements Quota/query per RFC 9425 Section 4.
func handleQuotaQuery(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		all, _ := backend.GetAllQuotas(ctx)
		ids := make([]Id, len(all))
		for i, q := range all {
			ids[i] = q.ID
		}
		return "Quota/query", map[string]any{
			"accountId":           accountID,
			"queryState":          backend.State(ctx),
			"canCalculateChanges": true,
			"position":            0,
			"ids":                 ids,
			"total":               len(ids),
		}
	}
}

// handleQuotaQueryChanges implements Quota/queryChanges per RFC 9425 Section 4.
func handleQuotaQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Quota/queryChanges", map[string]any{
			"accountId":     accountID,
			"oldQueryState": args["sinceQueryState"],
			"newQueryState": backend.State(ctx),
			"added":         []any{},
			"removed":       []Id{},
		}
	}
}
