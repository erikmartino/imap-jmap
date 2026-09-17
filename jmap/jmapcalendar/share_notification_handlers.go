package jmapcalendar

import (
	"context"

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
