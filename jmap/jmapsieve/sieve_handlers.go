package jmapsieve

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// validateSieveScriptName enforces RFC 9661 Section 2.1: a non-null name MUST be
// a Net-Unicode string of at least one character that does not contain any of
// U+0000-U+001F, U+007F-U+009F, U+2028, or U+2029.
func validateSieveScriptName(name string) *jmapcore.SetError {
	if name == "" {
		return nil
	}
	for _, r := range name {
		if r <= 0x1F || (r >= 0x7F && r <= 0x9F) || r == 0x2028 || r == 0x2029 {
			return &jmapcore.SetError{Type: "invalidProperties", Description: "name contains a forbidden character", Properties: []string{"name"}}
		}
	}
	return nil
}

// sieveScriptSortableProperties are the SieveScript properties that MUST be
// supported for sorting (RFC 9661 Section 2.5).
var sieveScriptSortableProperties = map[string]bool{"name": true, "isActive": true}

// sortSieveScripts applies the (already validated) comparators in order.
func sortSieveScripts(scripts []*SieveScript, comparators []jmapcore.Comparator) {
	if len(comparators) == 0 {
		return
	}
	sort.SliceStable(scripts, func(i, j int) bool {
		for _, c := range comparators {
			var less, greater bool
			switch c.Property {
			case "name":
				less = scripts[i].Name < scripts[j].Name
				greater = scripts[i].Name > scripts[j].Name
			case "isActive":
				less = !scripts[i].IsActive && scripts[j].IsActive
				greater = scripts[i].IsActive && !scripts[j].IsActive
			}
			if !less && !greater {
				continue
			}
			if c.IsAscending {
				return less
			}
			return greater
		}
		return false
	})
}

// RegisterSieveHandlers registers RFC 9661 JMAP for Sieve Scripts method handlers into MethodRegistry.
func RegisterSieveHandlers(r *jmaphandler.MethodRegistry, backend SieveBackend) {
	r.Register("SieveScript/get", handleSieveScriptGet(backend))
	r.Register("SieveScript/changes", handleSieveScriptChanges(backend))
	r.Register("SieveScript/set", handleSieveScriptSet(backend))
	r.Register("SieveScript/query", handleSieveScriptQuery(backend))
	r.Register("SieveScript/queryChanges", handleSieveScriptQueryChanges(backend))
	r.Register("SieveScript/validate", handleSieveScriptValidate(backend))
}

func handleSieveScriptGet(backend SieveBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*SieveScript
		var notFound []jmapcore.Id
		var err error
		state := "0"

		if backend != nil {
			state = backend.SieveScriptState(ctx)
			if hasIDs {
				ids := make([]jmapcore.Id, 0, len(idsRaw))
				for _, item := range idsRaw {
					if idStr, ok := item.(string); ok {
						ids = append(ids, jmapcore.Id(idStr))
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
			notFound = []jmapcore.Id{}
		}

		return "SieveScript/get", map[string]any{
			"accountId": accountID,
			"state":     state,
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleSieveScriptChanges(backend SieveBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		var created, updated, destroyed []jmapcore.Id
		newState := "0"
		hasMore := false

		if backend != nil {
			created, updated, destroyed, newState, hasMore = backend.SieveScriptChanges(ctx, sinceState)
		}

		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
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

func handleSieveScriptSet(backend SieveBackend) jmaphandler.MethodHandler {
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
		destroyed := make([]jmapcore.Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)
		creationRefs := newSetCreationRefs(ctx)

		// Existing script names, so create/update can enforce uniqueness
		// (RFC 9661 Section 2.1).
		existingNames := make(map[string]jmapcore.Id)
		if backend != nil {
			if all, err := backend.GetAllSieveScripts(ctx); err == nil {
				for _, s := range all {
					if s != nil && s.Name != "" {
						existingNames[s.Name] = s.ID
					}
				}
			}
		}

		if backend != nil {
			if createRaw, ok := args["create"].(map[string]any); ok {
				for creationID, scriptMap := range createRaw {
					scriptBytes, _ := json.Marshal(scriptMap)
					var script SieveScript
					_ = json.Unmarshal(scriptBytes, &script)

					if se := validateSieveScriptName(script.Name); se != nil {
						notCreated[creationID] = *se
						continue
					}
					if script.Name != "" {
						if existingID, exists := existingNames[script.Name]; exists {
							notCreated[creationID] = jmapcore.SetError{
								Type:        "alreadyExists",
								Description: "a SieveScript with this name already exists",
								ExistingID:  existingID,
							}
							continue
						}
					}

					createdScript, err := backend.CreateSieveScript(ctx, &script)
					if err != nil {
						notCreated[creationID] = jmapcore.SetError{Type: "invalidScript", Description: err.Error()}
					} else {
						created[creationID] = createdScript
						if createdScript.Name != "" {
							existingNames[createdScript.Name] = createdScript.ID
						}
						recordCreationRefs(ctx, creationRefs, creationID, createdScript.ID)
					}
				}
			}

			if updateRaw, ok := args["update"].(map[string]any); ok {
				for idStr, patchRaw := range updateRaw {
					patch, _ := patchRaw.(map[string]any)
					resolvedID := resolveCreationID(idStr, creationRefs)
					if nameRaw, hasName := patch["name"]; hasName {
						name, _ := nameRaw.(string)
						if se := validateSieveScriptName(name); se != nil {
							notUpdated[resolvedID] = *se
							continue
						}
						if name != "" {
							if existingID, exists := existingNames[name]; exists && existingID != jmapcore.Id(resolvedID) {
								notUpdated[resolvedID] = jmapcore.SetError{
									Type:        "alreadyExists",
									Description: "a SieveScript with this name already exists",
									ExistingID:  existingID,
								}
								continue
							}
							existingNames[name] = jmapcore.Id(resolvedID)
						}
					}
					updatedScript, err := backend.UpdateSieveScript(ctx, jmapcore.Id(resolvedID), resolvePatchCreationRefs(patch, creationRefs))
					if err != nil {
						if errors.Is(err, ErrNotFound) {
							notUpdated[resolvedID] = jmapcore.SetError{Type: "notFound", Description: err.Error()}
						} else {
							notUpdated[resolvedID] = jmapcore.SetError{Type: "invalidScript", Description: err.Error()}
						}
					} else {
						_ = updatedScript
						updated[resolvedID] = nil
					}
				}
			}

			if actID, ok := args["onSuccessActivateScript"].(string); ok && actID != "" {
				resolvedActID := resolveCreationID(actID, creationRefs)
				_, _ = backend.UpdateSieveScript(ctx, jmapcore.Id(resolvedActID), map[string]any{"isActive": true})
			}
			if deactID, ok := args["onSuccessDeactivateScript"].(string); ok && deactID != "" {
				resolvedDeactID := resolveCreationID(deactID, creationRefs)
				_, _ = backend.UpdateSieveScript(ctx, jmapcore.Id(resolvedDeactID), map[string]any{"isActive": false})
			}

			if destroyRaw, ok := args["destroy"].([]any); ok {
				for _, item := range destroyRaw {
					if idStr, ok := item.(string); ok {
						resolvedID := resolveCreationID(idStr, creationRefs)
						okDel, err := backend.DeleteSieveScript(ctx, jmapcore.Id(resolvedID))
						if err != nil || !okDel {
							notDestroyed[resolvedID] = jmapcore.SetError{Type: "notFound", Description: "sieve script not found"}
						} else {
							destroyed = append(destroyed, jmapcore.Id(resolvedID))
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
			"created":      nilIfEmpty(created),
			"updated":      nilIfEmpty(updated),
			"destroyed":    nilIfEmpty(destroyed),
			"notCreated":   nilIfEmpty(notCreated),
			"notUpdated":   nilIfEmpty(notUpdated),
			"notDestroyed": nilIfEmpty(notDestroyed),
		}
	}
}

func handleSieveScriptQuery(backend SieveBackend) jmaphandler.MethodHandler {
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

		comparators := jmapcore.ParseComparators(args)
		if errType, errMsg := jmapcore.ValidateComparators(comparators, sieveScriptSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		var ids []jmapcore.Id
		var total int
		var err error
		queryState := "0"

		if backend != nil {
			switch {
			case len(comparators) > 0:
				// Sorting must happen before pagination, so fetch the filtered set,
				// order it, then slice.
				var allIDs []jmapcore.Id
				allIDs, total, err = backend.QuerySieveScripts(ctx, filter, 0, nil)
				if err == nil {
					scripts, _, _ := backend.GetSieveScripts(ctx, allIDs)
					sortSieveScripts(scripts, comparators)
					allIDs = allIDs[:0]
					for _, s := range scripts {
						allIDs = append(allIDs, s.ID)
					}
					if anchor != "" {
						var found bool
						position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
						if !found {
							return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
						}
					} else {
						position = normalizePosition(position, total)
						end := total
						if limit != nil && position+int(*limit) < end {
							end = position + int(*limit)
						}
						ids = append(ids, allIDs[position:end]...)
					}
				}
			case anchor != "":
				var allIDs []jmapcore.Id
				allIDs, total, err = backend.QuerySieveScripts(ctx, filter, 0, nil)
				if err == nil {
					var found bool
					position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
					if !found {
						return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
					}
				}
			default:
				ids, total, err = backend.QuerySieveScripts(ctx, filter, position, limit)
			}
			queryState = backend.SieveScriptState(ctx)
		}
		if err != nil || ids == nil {
			ids = []jmapcore.Id{}
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
func handleSieveScriptQueryChanges(backend SieveBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)
		filter, _ := args["filter"].(map[string]any)

		var created, updated, destroyed []jmapcore.Id
		newQueryState := "0"
		hasMore := false

		if backend != nil {
			created, updated, destroyed, newQueryState, hasMore = backend.SieveScriptChanges(ctx, sinceState)
		}
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		var currentIDs []jmapcore.Id
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

func handleSieveScriptValidate(backend SieveBackend) jmaphandler.MethodHandler {
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
			resp["error"] = jmapcore.SetError{
				Type:        "invalidScript",
				Description: errDetail,
			}
		}

		return "SieveScript/validate", resp
	}
}
