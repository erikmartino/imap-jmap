package jmapmail

import (
	"context"
	"encoding/json"
	"errors"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// RegisterIdentityHandlers registers RFC 8621 Section 6 Identity method handlers into MethodRegistry.
func RegisterIdentityHandlers(r *jmaphandler.MethodRegistry, backend MailBackend) {
	r.Register("Identity/get", HandleIdentityGet(backend))
	r.Register("Identity/changes", HandleIdentityChanges(backend))
	r.Register("Identity/set", HandleIdentitySet(backend))
}

// RegisterVacationResponseHandlers registers RFC 8621 Section 8 VacationResponse method handlers into MethodRegistry.
func RegisterVacationResponseHandlers(r *jmaphandler.MethodRegistry, backend MailBackend) {
	r.Register("VacationResponse/get", HandleVacationResponseGet(backend))
	r.Register("VacationResponse/set", HandleVacationResponseSet(backend))
}

// HandleIdentityGet implements Identity/get per RFC 8621 Section 6.
func HandleIdentityGet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		all, _ := backend.GetIdentities(ctx)
		var list []*Identity
		var notFound []jmapcore.Id

		if idsRaw, ok := args["ids"].([]any); ok {
			idMap := make(map[jmapcore.Id]*Identity, len(all))
			for _, item := range all {
				idMap[item.ID] = item
			}
			for _, rawID := range idsRaw {
				if s, ok := rawID.(string); ok {
					id := jmapcore.Id(s)
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
			res["notFound"] = []jmapcore.Id{}
		}
		return "Identity/get", res
	}
}

// HandleIdentityChanges implements Identity/changes per RFC 8621 Section 6.
func HandleIdentityChanges(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		maxChanges, errArgs := jmaphandler.ParseMaxChanges(args)
		if errArgs != nil {
			return "error", errArgs
		}

		created, updated, destroyed, newState, hasMore := backend.IdentityChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
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

// HandleIdentitySet implements Identity/set per RFC 8621 Section 6.
func HandleIdentitySet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.IdentityState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", jmapcore.MethodErrorArgs("stateMismatch", "state mismatch")
		}

		created := make(map[string]*Identity)
		updated := make(map[string]any)
		destroyed := make([]jmapcore.Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)
		creationRefs := jmaphandler.NewSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				idMap, _ := raw.(map[string]any)
				idBytes, _ := json.Marshal(idMap)
				var identity Identity
				_ = json.Unmarshal(idBytes, &identity)
				identity.ID = ""

				createdIdentity, err := backend.CreateIdentity(ctx, &identity)
				if err != nil {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdIdentity
					jmaphandler.RecordCreationRefs(ctx, creationRefs, creationID, createdIdentity.ID)
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				patch, _ := patchRaw.(map[string]any)
				resolvedID := jmaphandler.ResolveCreationID(idStr, creationRefs)
				_, err := backend.UpdateIdentity(ctx, jmapcore.Id(resolvedID), jmaphandler.ResolvePatchCreationRefs(patch, creationRefs))
				if err != nil {
					if errors.Is(err, jmapcore.ErrNotFound) {
						notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: err.Error()}
					} else {
						notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
					}
				} else {
					updated[string(resolvedID)] = nil
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					resolvedID := jmaphandler.ResolveCreationID(idStr, creationRefs)
					okDel, err := backend.DeleteIdentity(ctx, jmapcore.Id(resolvedID))
					if err != nil {
						notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "serverFail", Description: err.Error()}
					} else if !okDel {
						notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: "identity not found"}
					} else {
						destroyed = append(destroyed, jmapcore.Id(resolvedID))
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

// HandleVacationResponseGet implements VacationResponse/get per RFC 8621 Section 8.1.
func HandleVacationResponseGet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		vr, _ := backend.GetVacationResponse(ctx)

		list := make([]*VacationResponse, 0, 1)
		notFound := []jmapcore.Id{}
		if idsRaw, ok := args["ids"].([]any); ok {
			// Explicit ids: only "singleton" resolves; anything else is notFound.
			for _, item := range idsRaw {
				s, _ := item.(string)
				if s == "singleton" && vr != nil {
					list = append(list, vr)
				} else {
					notFound = append(notFound, jmapcore.Id(s))
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

// HandleVacationResponseSet implements VacationResponse/set per RFC 8621 Section 8.2.
func HandleVacationResponseSet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.VacationResponseState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", jmapcore.MethodErrorArgs("stateMismatch", "state mismatch")
		}

		updated := make(map[string]any)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		// A singleton cannot be created or destroyed (RFC 8621 Section 8.2).
		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID := range createRaw {
				notCreated[creationID] = jmapcore.SetError{Type: "singleton", Description: "VacationResponse is a singleton and cannot be created"}
			}
		}
		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if s, ok := item.(string); ok {
					notDestroyed[s] = jmapcore.SetError{Type: "singleton", Description: "VacationResponse is a singleton and cannot be destroyed"}
				}
			}
		}
		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				if idStr != "singleton" {
					notUpdated[idStr] = jmapcore.SetError{Type: "notFound", Description: `the only VacationResponse id is "singleton"`}
					continue
				}
				patch, _ := patchRaw.(map[string]any)
				if _, err := backend.UpdateVacationResponse(ctx, patch); err != nil {
					notUpdated[idStr] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
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
			"destroyed":    []jmapcore.Id{},
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}
