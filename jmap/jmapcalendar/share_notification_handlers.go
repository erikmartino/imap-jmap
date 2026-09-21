package jmapcalendar

import (
	"context"
	"strings"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

func handleShareNotificationGet(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*ShareNotification
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			if len(idsRaw) == 0 {
				list = []*ShareNotification{}
				notFound = []jmapcore.Id{}
				_ = backend.ShareNotificationState(ctx)
			} else {
				ids := make([]jmapcore.Id, 0, len(idsRaw))
				for _, item := range idsRaw {
					if idStr, ok := item.(string); ok {
						ids = append(ids, jmapcore.Id(idStr))
					}
				}
				list, notFound, err = backend.GetShareNotifications(ctx, ids)
			}
		} else {
			list, err = backend.GetAllShareNotifications(ctx)
		}

		if err != nil || list == nil {
			list = []*ShareNotification{}
		}
		if notFound == nil {
			notFound = []jmapcore.Id{}
		}

		return "ShareNotification/get", map[string]any{
			"accountId": accountID,
			"state":     backend.ShareNotificationState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleShareNotificationChanges(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.ShareNotificationChanges(ctx, sinceState)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
		}
		return "ShareNotification/changes", map[string]any{
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

func handleShareNotificationSet(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.ShareNotificationState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		destroyed := make([]jmapcore.Id, 0)
		notDestroyed := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID := range createRaw {
				notCreated[creationID] = jmapcore.SetError{
					Type:        "forbidden",
					Description: "Cannot create share notifications.",
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr := range updateRaw {
				notUpdated[idStr] = jmapcore.SetError{
					Type:        "forbidden",
					Description: "Cannot update share notifications.",
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					okDel, err := backend.DeleteShareNotification(ctx, jmapcore.Id(idStr))
					if err != nil || !okDel {
						notDestroyed[idStr] = jmapcore.SetError{Type: "notFound", Description: "share notification not found"}
					} else {
						destroyed = append(destroyed, jmapcore.Id(idStr))
					}
				}
			}
		}

		return "ShareNotification/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.ShareNotificationState(ctx),
			"created":      nil,
			"updated":      nil,
			"destroyed":    destroyed,
			"notCreated":   nilIfEmpty(notCreated),
			"notUpdated":   nilIfEmpty(notUpdated),
			"notDestroyed": nilIfEmpty(notDestroyed),
		}
	}
}

// shareNotificationSortableProperties are the ShareNotification/query sort properties
// per RFC 9670 Section 3.4.2: only "created" is required to be supported.
var shareNotificationSortableProperties = map[string]bool{"created": true}

// shareNotificationFilterConditions are the RFC 9670 Section 3.4.1 FilterCondition
// properties. Any other condition property is rejected rather than silently matching.
var shareNotificationFilterConditions = map[string]bool{
	"after": true, "before": true, "objectType": true, "objectAccountId": true,
}

func handleShareNotificationQuery(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		filter, _ := args["filter"].(map[string]any)
		if errType, errMsg := validateShareNotificationFilter(filter); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}
		anchor, anchorOffset, anchorErr := parseQueryAnchor(args)
		if anchorErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, anchorErr)
		}
		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, shareNotificationSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		var limit *uint64
		if lim, ok := args["limit"].(float64); ok {
			l := uint64(lim)
			limit = &l
		}

		var ids []jmapcore.Id
		var total int
		if anchor != "" {
			allIDs, allTotal, _ := backend.QueryShareNotifications(ctx, filter, comparators, 0, nil)
			total = allTotal
			var found bool
			position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else {
			ids, total, _ = backend.QueryShareNotifications(ctx, filter, comparators, position, limit)
		}
		if ids == nil {
			ids = []jmapcore.Id{}
		}

		return "ShareNotification/query", map[string]any{
			"accountId":           accountID,
			"queryState":          backend.ShareNotificationState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"total":               total,
			"ids":                 ids,
		}
	}
}

func handleShareNotificationQueryChanges(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)
		if sinceState == "" {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is required")
		}

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.ShareNotificationChanges(ctx, sinceState)
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		filter, _ := args["filter"].(map[string]any)
		if errType, errMsg := validateShareNotificationFilter(filter); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}
		comparators := parseComparators(args)
		currentIDs, _, _ := backend.QueryShareNotifications(ctx, filter, comparators, 0, nil)
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
		return "ShareNotification/queryChanges", res
	}
}

// validateShareNotificationFilter rejects unknown FilterCondition properties and
// unknown FilterOperator operators (RFC 8620 Section 5.5), returning ("","") when the
// filter is valid.
func validateShareNotificationFilter(filter map[string]any) (errType, errMsg string) {
	if filter == nil {
		return "", ""
	}
	if opVal, ok := filter["operator"]; ok {
		op, _ := opVal.(string)
		if !validCalendarFilterOperators[strings.ToUpper(op)] {
			return "unsupportedFilter", "unknown filter operator: " + op
		}
		conds, ok := filter["conditions"].([]any)
		if !ok || len(conds) == 0 {
			return "unsupportedFilter", "filter operator requires a non-empty conditions array"
		}
		for _, c := range conds {
			cm, ok := c.(map[string]any)
			if !ok {
				return "unsupportedFilter", "filter conditions must be objects"
			}
			if et, em := validateShareNotificationFilter(cm); et != "" {
				return et, em
			}
		}
		return "", ""
	}
	for k := range filter {
		if !shareNotificationFilterConditions[k] {
			return "unsupportedFilter", "unknown filter condition: " + k
		}
	}
	return "", ""
}
