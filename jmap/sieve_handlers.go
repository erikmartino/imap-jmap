package jmap

import (
	"context"
	"encoding/json"
)

// RegisterSieveHandlers registers RFC 9661 JMAP for Sieve Scripts method handlers into MethodRegistry.
func RegisterSieveHandlers(r *MethodRegistry, backend SieveBackend) {
	r.Register("SieveScript/get", handleSieveScriptGet(backend))
	r.Register("SieveScript/changes", handleSieveScriptChanges(backend))
	r.Register("SieveScript/set", handleSieveScriptSet(backend))
	r.Register("SieveScript/query", handleSieveScriptQuery(backend))
	r.Register("SieveScript/validate", handleSieveScriptValidate(backend))
}

func handleSieveScriptGet(backend SieveBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)

		var list []*SieveScript
		var notFound []Id
		var err error
		state := "0"

		if backend != nil {
			state = backend.SieveScriptState(ctx)
			if hasIDs {
				ids := make([]Id, 0, len(idsRaw))
				for _, item := range idsRaw {
					if idStr, ok := item.(string); ok {
						ids = append(ids, Id(idStr))
					}
				}
				list, notFound, err = backend.GetSieveScripts(ctx, ids)
			} else {
				list, err = backend.GetAllSieveScripts(ctx)
			}
		}

		if err != nil || list == nil {
			list = []*SieveScript{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "SieveScript/get", map[string]any{
			"accountId": accountID,
			"state":     state,
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleSieveScriptChanges(backend SieveBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		var created, updated, destroyed []Id
		newState := "0"
		hasMore := false

		if backend != nil {
			created, updated, destroyed, newState, hasMore = backend.SieveScriptChanges(ctx, sinceState)
		}

		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}
		return "SieveScript/changes", map[string]any{
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

func handleSieveScriptSet(backend SieveBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := "0"
		newState := "0"
		if backend != nil {
			oldState = backend.SieveScriptState(ctx)
		}

		created := make(map[string]*SieveScript)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		if backend != nil {
			if createRaw, ok := args["create"].(map[string]any); ok {
				for creationID, scriptMap := range createRaw {
					scriptBytes, _ := json.Marshal(scriptMap)
					var script SieveScript
					_ = json.Unmarshal(scriptBytes, &script)

					createdScript, err := backend.CreateSieveScript(ctx, &script)
					if err != nil {
						notCreated[creationID] = SetError{Type: "invalidScript", Description: err.Error()}
					} else {
						created[creationID] = createdScript
					}
				}
			}

			if updateRaw, ok := args["update"].(map[string]any); ok {
				for idStr, patchRaw := range updateRaw {
					patch, _ := patchRaw.(map[string]any)
					updatedScript, err := backend.UpdateSieveScript(ctx, Id(idStr), patch)
					if err != nil {
						notUpdated[idStr] = SetError{Type: "invalidScript", Description: err.Error()}
					} else {
						_ = updatedScript
						updated[idStr] = nil
					}
				}
			}

			if destroyRaw, ok := args["destroy"].([]any); ok {
				for _, item := range destroyRaw {
					if idStr, ok := item.(string); ok {
						okDel, err := backend.DeleteSieveScript(ctx, Id(idStr))
						if err != nil || !okDel {
							notDestroyed[idStr] = SetError{Type: "notFound", Description: "sieve script not found"}
						} else {
							destroyed = append(destroyed, Id(idStr))
						}
					}
				}
			}
			newState = backend.SieveScriptState(ctx)
		}

		return "SieveScript/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     newState,
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

func handleSieveScriptQuery(backend SieveBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		filter, _ := args["filter"].(map[string]any)
		positionFloat, _ := args["position"].(float64)
		position := int(positionFloat)

		var limit *uint64
		if limitFloat, ok := args["limit"].(float64); ok {
			l := uint64(limitFloat)
			limit = &l
		}

		var ids []Id
		var total int
		var err error

		if backend != nil {
			ids, total, err = backend.QuerySieveScripts(ctx, filter, position, limit)
		}
		if err != nil || ids == nil {
			ids = []Id{}
			total = 0
		}

		return "SieveScript/query", map[string]any{
			"accountId":           accountID,
			"queryState":          "0",
			"canCalculateChanges": false,
			"position":            position,
			"total":               total,
			"ids":                 ids,
		}
	}
}

func handleSieveScriptValidate(backend SieveBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		content, _ := args["content"].(string)

		isValid := true
		errDetail := ""
		if backend != nil {
			isValid, errDetail = backend.ValidateSieveScript(ctx, content)
		}

		resp := map[string]any{
			"accountId": accountID,
			"isValid":   isValid,
		}

		if !isValid {
			resp["error"] = SetError{
				Type:        "invalidScript",
				Description: errDetail,
			}
		}

		return "SieveScript/validate", resp
	}
}
