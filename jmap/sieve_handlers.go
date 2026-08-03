package jmap

import (
	"context"
	"encoding/json"
	"errors"
)

// RegisterSieveHandlers registers RFC 9661 JMAP for Sieve Scripts method handlers into MethodRegistry.
func RegisterSieveHandlers(r *MethodRegistry, backend SieveBackend) {
	r.Register("SieveScript/get", handleSieveScriptGet(backend))
	r.Register("SieveScript/changes", handleSieveScriptChanges(backend))
	r.Register("SieveScript/set", handleSieveScriptSet(backend))
	r.Register("SieveScript/query", handleSieveScriptQuery(backend))
	r.Register("SieveScript/queryChanges", handleSieveScriptQueryChanges(backend))
	r.Register("SieveScript/validate", handleSieveScriptValidate(backend))
}

func handleSieveScriptGet(backend SieveBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

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
			"list":      filterList(list, props),
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

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
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
						if errors.Is(err, ErrNotFound) {
							notUpdated[idStr] = SetError{Type: "notFound", Description: err.Error()}
						} else {
							notUpdated[idStr] = SetError{Type: "invalidScript", Description: err.Error()}
						}
					} else {
						_ = updatedScript
						updated[idStr] = nil
					}
				}
			}

			if actID, ok := args["onSuccessActivateScript"].(string); ok && actID != "" {
				_, _ = backend.UpdateSieveScript(ctx, Id(actID), map[string]any{"isActive": true})
			}
			if deactID, ok := args["onSuccessDeactivateScript"].(string); ok && deactID != "" {
				_, _ = backend.UpdateSieveScript(ctx, Id(deactID), map[string]any{"isActive": false})
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
		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := parseQueryAnchor(args)
		if anchorErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, anchorErr)
		}

		var limit *uint64
		if limitFloat, ok := args["limit"].(float64); ok {
			l := uint64(limitFloat)
			limit = &l
		}

		var ids []Id
		var total int
		var err error
		queryState := "0"

		if backend != nil {
			if anchor != "" {
				var allIDs []Id
				allIDs, total, err = backend.QuerySieveScripts(ctx, filter, 0, nil)
				if err == nil {
					var found bool
					position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
					if !found {
						return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
					}
				}
			} else {
				ids, total, err = backend.QuerySieveScripts(ctx, filter, position, limit)
			}
			queryState = backend.SieveScriptState(ctx)
		}
		if err != nil || ids == nil {
			ids = []Id{}
			total = 0
		}

		return "SieveScript/query", map[string]any{
			"accountId":           accountID,
			"queryState":          queryState,
			"canCalculateChanges": true,
			"position":            position,
			"total":               total,
			"ids":                 ids,
		}
	}
}

// handleSieveScriptQueryChanges implements SieveScript/queryChanges per RFC 8620
// Section 5.6: deltas respect the query's filter, updated or destroyed scripts are removed
// from the client's view, created or updated scripts still matching the filter are re-added
// at their real index, and upToId truncates added ids beyond the anchor.
func handleSieveScriptQueryChanges(backend SieveBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)
		filter, _ := args["filter"].(map[string]any)

		var created, updated, destroyed []Id
		newQueryState := "0"
		hasMore := false

		if backend != nil {
			created, updated, destroyed, newQueryState, hasMore = backend.SieveScriptChanges(ctx, sinceState)
		}
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		var currentIDs []Id
		if backend != nil {
			currentIDs, _, _ = backend.QuerySieveScripts(ctx, filter, 0, nil)
		}
		added, removed := computeQueryChanges(created, updated, destroyed, currentIDs, upToID)

		res := map[string]any{
			"accountId":     accountID,
			"oldQueryState": sinceState,
			"newQueryState": newQueryState,
			"added":         added,
			"removed":       removed,
		}
		if upToID != "" {
			res["upToId"] = upToID
		}
		if calcTotal, _ := args["calculateTotal"].(bool); calcTotal {
			res["total"] = len(currentIDs)
		}
		return "SieveScript/queryChanges", res
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
