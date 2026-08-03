package jmap

import (
	"context"
)

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
		oldState := backend.State(ctx)
		created := make(map[string]*Mailbox)
		updated := make(map[string]any)
		destroyed := []Id{}
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		if createMap, ok := args["create"].(map[string]any); ok {
			for clientKey, raw := range createMap {
				mbData, ok := raw.(map[string]any)
				if !ok {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Description: "invalid Mailbox object"}
					continue
				}
				name, _ := mbData["name"].(string)
				if name == "" {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Description: "name is required"}
					continue
				}
				m := &Mailbox{Name: name}
				if pid, ok := mbData["parentId"].(string); ok && pid != "" {
					p := Id(pid)
					m.ParentID = &p
				}
				if role, ok := mbData["role"].(string); ok && role != "" {
					m.Role = &role
				}
				if so, ok := mbData["sortOrder"].(float64); ok {
					m.SortOrder = uint64(so)
				}
				if sub, ok := mbData["isSubscribed"].(bool); ok {
					m.IsSubscribed = sub
				}
				mb, err := backend.CreateMailbox(ctx, m)
				if err != nil {
					notCreated[clientKey] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[clientKey] = mb
				}
			}
		}

		if updateMap, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateMap {
				patch, _ := patchRaw.(map[string]any)
				_, err := backend.UpdateMailbox(ctx, Id(idStr), patch)
				if err != nil {
					notUpdated[idStr] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					updated[idStr] = nil
				}
			}
		}

		if destroyList, ok := args["destroy"].([]any); ok {
			for _, rawID := range destroyList {
				if idStr, ok := rawID.(string); ok {
					okDel, err := backend.DeleteMailbox(ctx, Id(idStr))
					if err != nil {
						notDestroyed[idStr] = SetError{Type: "serverFail", Description: err.Error()}
					} else if !okDel {
						notDestroyed[idStr] = SetError{Type: "notFound", Description: "mailbox not found"}
					} else {
						destroyed = append(destroyed, Id(idStr))
					}
				}
			}
		}

		return "Mailbox/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.State(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
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
