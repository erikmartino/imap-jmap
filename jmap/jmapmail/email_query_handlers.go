package jmapmail

import (
	"context"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

func HandleEmailQuery(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		if accountID != "" {
			ctx = jmapauth.ContextWithAccountID(ctx, accountID)
		}
		filter, _ := args["filter"].(map[string]any)

		comparators := jmapcore.ParseComparators(args)
		if errType, errMsg := jmapcore.ValidateComparators(comparators, emailSortableProperties); errType != "" {
			return "error", jmapcore.MethodErrorArgs(errType, errMsg)
		}

		position, posErr := jmapcore.ParseQueryPosition(args)
		if posErr != "" {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := jmapcore.ParseQueryAnchor(args)
		if anchorErr != "" {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, anchorErr)
		}

		var limit *uint64
		if limVal, ok := args["limit"].(float64); ok {
			l := uint64(limVal)
			limit = &l
		}

		collapseThreads, _ := args["collapseThreads"].(bool)
		var ids []jmapcore.Id
		var total int
		if collapseThreads {
			allIDs, _, _ := backend.QueryEmails(ctx, filter, comparators, 0, nil)
			var collapsedIDs []jmapcore.Id
			seenThreads := make(map[jmapcore.Id]bool)
			if len(allIDs) > 0 {
				emails, _, _ := backend.GetEmails(ctx, allIDs)
				emailMap := make(map[jmapcore.Id]*Email, len(emails))
				for _, em := range emails {
					emailMap[em.ID] = em
				}
				for _, id := range allIDs {
					if em, ok := emailMap[id]; ok {
						if !seenThreads[em.ThreadID] {
							seenThreads[em.ThreadID] = true
							collapsedIDs = append(collapsedIDs, id)
						}
					} else {
						collapsedIDs = append(collapsedIDs, id)
					}
				}
			}
			total = len(collapsedIDs)
			if anchor != "" {
				var found bool
				position, ids, found = jmapcore.ApplyQueryAnchor(anchor, anchorOffset, collapsedIDs, limit)
				if !found {
					return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
				}
			} else {
				position = jmapcore.NormalizePosition(position, total)
				if position > len(collapsedIDs) {
					ids = []jmapcore.Id{}
				} else {
					end := len(collapsedIDs)
					if limit != nil {
						l := int(*limit)
						if position+l < end {
							end = position + l
						}
					}
					ids = collapsedIDs[position:end]
				}
			}
		} else if anchor != "" {
			allIDs, allTotal, _ := backend.QueryEmails(ctx, filter, comparators, 0, nil)
			total = allTotal
			var found bool
			position, ids, found = jmapcore.ApplyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else {
			ids, total, _ = backend.QueryEmails(ctx, filter, comparators, position, limit)
			position = jmapcore.NormalizePosition(position, total)
		}

		calcTotal, _ := args["calculateTotal"].(bool)
		res := map[string]any{
			"accountId":           accountID,
			"queryState":          backend.EmailState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"ids":                 ids,
			"total":               total,
		}
		if calcTotal {
			res["calculateTotal"] = true
		}
		if _, ok := args["collapseThreads"]; ok {
			res["collapseThreads"] = collapseThreads
		}
		return "Email/query", res
	}
}

func HandleEmailQueryChanges(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)
		filter, _ := args["filter"].(map[string]any)
		comparators := jmapcore.ParseComparators(args)
		if errType, errMsg := jmapcore.ValidateComparators(comparators, emailSortableProperties); errType != "" {
			return "error", jmapcore.MethodErrorArgs(errType, errMsg)
		}

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.EmailChanges(ctx, sinceState, nil)
		if hasMore {
			return "error", jmapcore.MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		currentIDs, _, _ := backend.QueryEmails(ctx, filter, comparators, 0, nil)
		added, removed := jmapcore.ComputeQueryChanges(createdIDs, updatedIDs, destroyedIDs, currentIDs, upToID)

		res := map[string]any{
			"accountId":     accountID,
			"oldQueryState": sinceState,
			"newQueryState": newState,
			"added":         added,
			"removed":       removed,
		}
		if f, ok := args["filter"]; ok && f != nil {
			res["filter"] = f
		}
		if s, ok := args["sort"]; ok && s != nil {
			res["sort"] = s
		}
		if upToID != "" {
			res["upToId"] = upToID
		}
		if calcTotal, _ := args["calculateTotal"].(bool); calcTotal {
			res["total"] = len(currentIDs)
		}
		return "Email/queryChanges", res
	}
}
