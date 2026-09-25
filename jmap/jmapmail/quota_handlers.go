package jmapmail

import (
	"context"
	"strings"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// RegisterQuotaHandlers registers RFC 9425 Quota method handlers into MethodRegistry.
func RegisterQuotaHandlers(r *jmaphandler.MethodRegistry, backend MailBackend) {
	r.Register("Quota/get", HandleQuotaGet(backend))
	r.Register("Quota/changes", HandleQuotaChanges(backend))
	r.Register("Quota/query", HandleQuotaQuery(backend))
	r.Register("Quota/queryChanges", HandleQuotaQueryChanges(backend))
}

// HandleQuotaGet implements Quota/get per RFC 9425 Section 4.
func HandleQuotaGet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := jmaphandler.ParseProperties(args)

		var list []*Quota
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			ids := make([]jmapcore.Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, jmapcore.Id(idStr))
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
			notFound = []jmapcore.Id{}
		}

		return "Quota/get", map[string]any{
			"accountId": accountID,
			"state":     backend.QuotaState(ctx),
			"list":      jmaphandler.FilterList(list, props),
			"notFound":  notFound,
		}
	}
}

// HandleQuotaChanges implements Quota/changes per RFC 9425 Section 4.
func HandleQuotaChanges(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		maxChanges, errArgs := jmaphandler.ParseMaxChanges(args)
		if errArgs != nil {
			return "error", errArgs
		}

		created, updated, destroyed, newState, hasMore := backend.QuotaChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
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
	if match, isOp := jmapcore.EvalFilterOperator(filter, func(cond map[string]any) bool {
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

// HandleQuotaQuery implements Quota/query per RFC 9425 Section 4.
func HandleQuotaQuery(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		if sortVal, exists := args["sort"]; exists && sortVal != nil {
			if sortList, ok := sortVal.([]any); ok && len(sortList) > 0 {
				return "error", jmapcore.MethodErrorArgs("unsupportedSort", "sort is not supported for Quota/query")
			}
		}

		all, _ := backend.GetAllQuotas(ctx)

		if filterVal, exists := args["filter"]; exists && filterVal != nil {
			filterMap, ok := filterVal.(map[string]any)
			if !ok {
				return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, "filter must be an object")
			}
			var filtered []*Quota
			for _, q := range all {
				if matchQuotaFilter(q, filterMap) {
					filtered = append(filtered, q)
				}
			}
			all = filtered
		}

		position, posErr := jmapcore.ParseQueryPosition(args)
		if posErr != "" {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := jmapcore.ParseQueryAnchor(args)
		if anchorErr != "" {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, anchorErr)
		}

		total := len(all)
		position = jmapcore.NormalizePosition(position, total)
		var pagedIDs []jmapcore.Id
		if anchor != "" {
			allIDs := make([]jmapcore.Id, 0, len(all))
			for _, quota := range all {
				allIDs = append(allIDs, quota.ID)
			}
			var limit *uint64
			if limVal, ok := args["limit"].(float64); ok {
				l := uint64(limVal)
				limit = &l
			}
			var found bool
			position, pagedIDs, found = jmapcore.ApplyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
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
			pagedIDs = []jmapcore.Id{}
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

// HandleQuotaQueryChanges implements Quota/queryChanges per RFC 9425 Section 4.
func HandleQuotaQueryChanges(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.QuotaChanges(ctx, sinceState, nil)
		if hasMore {
			return "error", jmapcore.MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		all, _ := backend.GetAllQuotas(ctx)
		currentIDs := make([]jmapcore.Id, 0, len(all))
		for _, quota := range all {
			currentIDs = append(currentIDs, quota.ID)
		}

		added, removed := jmapcore.ComputeQueryChanges(createdIDs, updatedIDs, destroyedIDs, currentIDs, upToID)

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
