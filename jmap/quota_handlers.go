package jmap

import (
	"context"
	"strings"
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

		var maxChanges *uint64
		if mc, ok := args["maxChanges"].(float64); ok {
			if mc < 0 {
				return "error", MethodErrorArgs(MethodErrorInvalidArguments, "maxChanges must be non-negative")
			}
			m := uint64(mc)
			maxChanges = &m
		}

		created, updated, destroyed, newState, hasMore := backend.QuotaChanges(ctx, sinceState, maxChanges)
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

func matchQuotaFilter(q *Quota, filter map[string]any) bool {
	if match, isOp := EvalFilterOperator(filter, func(cond map[string]any) bool {
		return matchQuotaFilter(q, cond)
	}); isOp {
		return match
	}
	if name, ok := filter["name"].(string); ok && q.Name != name {
		return false
	}
	if scope, ok := filter["scope"].(string); ok && q.Scope != scope {
		return false
	}
	if resType, ok := filter["resourceType"].(string); ok && q.ResourceType != resType {
		return false
	}
	if dt, ok := filter["dataTypes"].(string); ok {
		found := false
		for _, d := range q.DataTypes {
			if strings.EqualFold(d, dt) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// handleQuotaQuery implements Quota/query per RFC 9425 Section 4.
func handleQuotaQuery(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		if sortVal, exists := args["sort"]; exists && sortVal != nil {
			if sortList, ok := sortVal.([]any); ok && len(sortList) > 0 {
				return "error", MethodErrorArgs("unsupportedSort", "sort is not supported for Quota/query")
			}
		}

		all, _ := backend.GetAllQuotas(ctx)

		if filterVal, exists := args["filter"]; exists && filterVal != nil {
			filterMap, ok := filterVal.(map[string]any)
			if !ok {
				return "error", MethodErrorArgs(MethodErrorInvalidArguments, "filter must be an object")
			}
			var filtered []*Quota
			for _, q := range all {
				if matchQuotaFilter(q, filterMap) {
					filtered = append(filtered, q)
				}
			}
			all = filtered
		}

		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := parseQueryAnchor(args)
		if anchorErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, anchorErr)
		}

		total := len(all)
		position = NormalizePosition(position, total)
		var pagedIDs []Id
		if anchor != "" {
			allIDs := make([]Id, 0, len(all))
			for _, quota := range all {
				allIDs = append(allIDs, quota.ID)
			}
			var limit *uint64
			if limVal, ok := args["limit"].(float64); ok {
				l := uint64(limVal)
				limit = &l
			}
			var found bool
			position, pagedIDs, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else {
			end := total
			if limVal, ok := args["limit"].(float64); ok {
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

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.QuotaChanges(ctx, sinceState, nil)
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
