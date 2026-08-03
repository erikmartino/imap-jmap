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
		props := parseProperties(args)

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
			"list":      filterList(list, props),
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

		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := parseQueryAnchor(args)
		if anchorErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, anchorErr)
		}

		total := len(all)
		var pagedIDs []Id
		if anchor != "" {
			allIDs := make([]Id, 0, len(all))
			for _, quota := range all {
				allIDs = append(allIDs, quota.ID)
			}
			var limit *uint64
			if limVal, ok := args["limit"].(float64); ok && limVal > 0 {
				l := uint64(limVal)
				limit = &l
			}
			var found bool
			position, pagedIDs, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else if position < total {
			end := total
			if limVal, ok := args["limit"].(float64); ok && limVal > 0 {
				if position+int(limVal) < end {
					end = position + int(limVal)
				}
			}
			for i := position; i < end; i++ {
				pagedIDs = append(pagedIDs, all[i].ID)
			}
		}
		if pagedIDs == nil {
			pagedIDs = []Id{}
		}

		res := map[string]any{
			"accountId":           accountID,
			"queryState":          backend.QuotaState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"ids":                 pagedIDs,
			"total":               total,
		}
		if calcTotal, _ := args["calculateTotal"].(bool); calcTotal {
			res["calculateTotal"] = true
		}
		return "Quota/query", res
	}
}

// handleQuotaQueryChanges implements Quota/queryChanges per RFC 9425 Section 4.
func handleQuotaQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.QuotaChanges(ctx, sinceState)
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		all, _ := backend.GetAllQuotas(ctx)
		currentIDs := make([]Id, 0, len(all))
		for _, quota := range all {
			currentIDs = append(currentIDs, quota.ID)
		}

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
		return "Quota/queryChanges", res
	}
}
