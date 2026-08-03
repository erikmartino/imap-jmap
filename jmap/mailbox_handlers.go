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
		sinceState, _ := args["sinceState"].(string)

		created, updated, destroyed, newState, hasMore := backend.MailboxChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "Mailbox/changes", map[string]any{
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

func handleMailboxSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.MailboxState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}
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
			"newState":     backend.MailboxState(ctx),
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

		filter, _ := args["filter"].(map[string]any)
		var filtered []*Mailbox
		for _, mb := range all {
			match := true
			if filter != nil {
				if roleReq, ok := filter["role"].(string); ok {
					if mb.Role == nil || *mb.Role != roleReq {
						match = false
					}
				}
				if parentReq, ok := filter["parentId"].(string); ok {
					if mb.ParentID == nil || string(*mb.ParentID) != parentReq {
						match = false
					}
				}
				if nameReq, ok := filter["name"].(string); ok {
					if mb.Name != nameReq {
						match = false
					}
				}
			}
			if match {
				filtered = append(filtered, mb)
			}
		}

		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}

		total := len(filtered)
		var pagedIDs []Id
		if position < total {
			end := total
			if limVal, ok := args["limit"].(float64); ok && limVal > 0 {
				if position+int(limVal) < end {
					end = position + int(limVal)
				}
			}
			for i := position; i < end; i++ {
				pagedIDs = append(pagedIDs, filtered[i].ID)
			}
		}
		if pagedIDs == nil {
			pagedIDs = []Id{}
		}

		res := map[string]any{
			"accountId":           accountID,
			"queryState":          backend.MailboxState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"ids":                 pagedIDs,
			"total":               total,
		}
		if calcTotal, _ := args["calculateTotal"].(bool); calcTotal {
			res["calculateTotal"] = true
		}
		return "Mailbox/query", res
	}
}

func handleMailboxQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)

		createdIDs, _, destroyedIDs, newState, hasMore := backend.MailboxChanges(ctx, sinceState)
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		added := make([]map[string]any, 0, len(createdIDs))
		for idx, id := range createdIDs {
			added = append(added, map[string]any{
				"id":    id,
				"index": idx,
			})
		}
		if destroyedIDs == nil {
			destroyedIDs = []Id{}
		}

		res := map[string]any{
			"accountId":     accountID,
			"oldQueryState": sinceState,
			"newQueryState": newState,
			"added":         added,
			"removed":       destroyedIDs,
		}
		if upToID != "" {
			res["upToId"] = upToID
		}
		return "Mailbox/queryChanges", res
	}
}

func handleMailboxCopy(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		fromAccountID, _ := args["fromAccountId"].(string)
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)
		onDestroy, _ := args["onSuccessDestroyOriginal"].(bool)

		oldState := backend.MailboxState(ctx)
		created := make(map[string]*Mailbox)
		notCreated := make(map[string]SetError)

		for clientKey, raw := range createMap {
			if mbData, ok := raw.(map[string]any); ok {
				if idStr, ok := mbData["id"].(string); ok {
					list, _, _ := backend.GetMailboxes(ctx, []Id{Id(idStr)})
					if len(list) > 0 {
						cp := *list[0]
						cp.ID = ""

						// Apply overrides (RFC 8621 Section 2.5)
						if nameOverride, ok := mbData["name"].(string); ok && nameOverride != "" {
							cp.Name = nameOverride
						}
						if parentIDOverride, ok := mbData["parentId"].(string); ok {
							if parentIDOverride == "" {
								cp.ParentID = nil
							} else {
								pid := Id(parentIDOverride)
								cp.ParentID = &pid
							}
						}

						createdMB, err := backend.CreateMailbox(ctx, &cp)
						if err == nil {
							created[clientKey] = createdMB
							if onDestroy {
								_, _ = backend.DeleteMailbox(ctx, Id(idStr))
							}
						} else {
							notCreated[clientKey] = SetError{Type: "serverFail", Description: err.Error()}
						}
					} else {
						notCreated[clientKey] = SetError{Type: "notFound", Description: "mailbox not found"}
					}
				}
			}
		}

		return "Mailbox/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.MailboxState(ctx),
			"created":       created,
			"notCreated":    notCreated,
		}
	}
}
