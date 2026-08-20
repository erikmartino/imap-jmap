package jmap

import (
	"context"
	"encoding/json"
	"errors"
)

func handleIdentityGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		all, _ := backend.GetIdentities(ctx)
		var list []*Identity
		var notFound []Id

		if idsRaw, ok := args["ids"].([]any); ok {
			idMap := make(map[Id]*Identity, len(all))
			for _, item := range all {
				idMap[item.ID] = item
			}
			for _, rawID := range idsRaw {
				if s, ok := rawID.(string); ok {
					id := Id(s)
					if item, found := idMap[id]; found {
						list = append(list, item)
					} else {
						notFound = append(notFound, id)
					}
				}
			}
		} else {
			list = all
		}
		if list == nil {
			list = []*Identity{}
		}

		res := map[string]any{
			"accountId": accountID,
			"state":     backend.IdentityState(ctx),
			"list":      list,
			"notFound":  notFound,
		}
		if len(notFound) == 0 {
			res["notFound"] = []Id{}
		}
		return "Identity/get", res
	}
}

func handleIdentityChanges(backend MailBackend) MethodHandler {
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

		created, updated, destroyed, newState, hasMore := backend.IdentityChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "Identity/changes", map[string]any{
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

func handleIdentitySet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.IdentityState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		created := make(map[string]*Identity)
		updated := make(map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)
		creationRefs := newSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				idMap, _ := raw.(map[string]any)
				idBytes, _ := json.Marshal(idMap)
				var identity Identity
				_ = json.Unmarshal(idBytes, &identity)
				identity.ID = ""

				createdIdentity, err := backend.CreateIdentity(ctx, &identity)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdIdentity
					recordCreationRefs(ctx, creationRefs, creationID, createdIdentity.ID)
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				patch, _ := patchRaw.(map[string]any)
				resolvedID := resolveCreationID(idStr, creationRefs)
				_, err := backend.UpdateIdentity(ctx, Id(resolvedID), resolvePatchCreationRefs(patch, creationRefs))
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						notUpdated[string(resolvedID)] = SetError{Type: "notFound", Description: err.Error()}
					} else {
						notUpdated[string(resolvedID)] = SetError{Type: "invalidProperties", Description: err.Error()}
					}
				} else {
					updated[string(resolvedID)] = nil
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					okDel, err := backend.DeleteIdentity(ctx, Id(resolvedID))
					if err != nil {
						notDestroyed[string(resolvedID)] = SetError{Type: "serverFail", Description: err.Error()}
					} else if !okDel {
						notDestroyed[string(resolvedID)] = SetError{Type: "notFound", Description: "identity not found"}
					} else {
						destroyed = append(destroyed, Id(resolvedID))
					}
				}
			}
		}

		return "Identity/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.IdentityState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

// VacationResponse handlers (RFC 8621 Section 8). VacationResponse is a per-account
// singleton whose id is always "singleton"; it has only /get and /set (no /changes).

func handleVacationResponseGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		vr, _ := backend.GetVacationResponse(ctx)

		list := make([]*VacationResponse, 0, 1)
		notFound := []Id{}
		if idsRaw, ok := args["ids"].([]any); ok {
			// Explicit ids: only "singleton" resolves; anything else is notFound.
			for _, item := range idsRaw {
				s, _ := item.(string)
				if s == "singleton" && vr != nil {
					list = append(list, vr)
				} else {
					notFound = append(notFound, Id(s))
				}
			}
		} else if vr != nil {
			// ids null/absent means "all", which is just the singleton.
			list = append(list, vr)
		}

		return "VacationResponse/get", map[string]any{
			"accountId": accountID,
			"state":     backend.VacationResponseState(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleVacationResponseSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.VacationResponseState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		updated := make(map[string]any)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		// A singleton cannot be created or destroyed (RFC 8621 Section 8.2).
		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID := range createRaw {
				notCreated[creationID] = SetError{Type: "singleton", Description: "VacationResponse is a singleton and cannot be created"}
			}
		}
		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if s, ok := item.(string); ok {
					notDestroyed[s] = SetError{Type: "singleton", Description: "VacationResponse is a singleton and cannot be destroyed"}
				}
			}
		}
		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				if idStr != "singleton" {
					notUpdated[idStr] = SetError{Type: "notFound", Description: `the only VacationResponse id is "singleton"`}
					continue
				}
				patch, _ := patchRaw.(map[string]any)
				if _, err := backend.UpdateVacationResponse(ctx, patch); err != nil {
					notUpdated[idStr] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					updated[idStr] = nil
				}
			}
		}

		return "VacationResponse/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.VacationResponseState(ctx),
			"created":      map[string]any{},
			"updated":      updated,
			"destroyed":    []Id{},
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}
