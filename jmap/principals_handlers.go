package jmap

import (
	"context"
	"fmt"
)

// RegisterPrincipalsHandlers registers JMAP Principals & Availability method handlers into MethodRegistry.
func RegisterPrincipalsHandlers(r *MethodRegistry, backend PrincipalsBackend) {
	r.Register("Principal/get", handlePrincipalGet(backend))
	r.Register("Principal/changes", handlePrincipalChanges(backend))
	r.Register("Principal/query", handlePrincipalQuery(backend))
	r.Register("Principal/queryChanges", handlePrincipalQueryChanges(backend))
	r.Register("Principal/set", handlePrincipalSet(backend))
	r.Register("Principal/getAvailability", handlePrincipalGetAvailability(backend))
}

func handlePrincipalGet(backend PrincipalsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		if backend == nil {
			return "error", MethodErrorArgs(MethodErrorUnknownMethod, "Principals capability not supported")
		}
		accountID, _ := args["accountId"].(string)
		props := parseProperties(args)

		var list []*Principal
		var notFound []Id
		var err error

		if idsRaw, hasIDs := args["ids"].([]any); hasIDs && idsRaw != nil {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if s, ok := item.(string); ok {
					ids = append(ids, Id(s))
				}
			}
			list, notFound, err = backend.GetPrincipals(ctx, ids)
		} else {
			list, err = backend.GetAllPrincipals(ctx)
		}

		if err != nil {
			return "error", MethodErrorArgs(MethodErrorServerFail, err.Error())
		}
		if list == nil {
			list = []*Principal{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Principal/get", map[string]any{
			"accountId": accountID,
			"state":     backend.PrincipalState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handlePrincipalChanges(backend PrincipalsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		if backend == nil {
			return "error", MethodErrorArgs(MethodErrorUnknownMethod, "Principals capability not supported")
		}
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		created, updated, destroyed, newState, hasMore := backend.PrincipalChanges(ctx, sinceState)

		return "Principal/changes", map[string]any{
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

func handlePrincipalQuery(backend PrincipalsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		if backend == nil {
			return "error", MethodErrorArgs(MethodErrorUnknownMethod, "Principals capability not supported")
		}
		accountID, _ := args["accountId"].(string)
		filter, _ := args["filter"].(map[string]any)

		position := 0
		if posVal, ok := args["position"].(float64); ok {
			position = int(posVal)
		}
		var limit *uint64
		if limVal, ok := args["limit"].(float64); ok {
			l := uint64(limVal)
			limit = &l
		}

		ids, total, err := backend.QueryPrincipals(ctx, filter, position, limit)
		if err != nil {
			return "error", MethodErrorArgs(MethodErrorServerFail, err.Error())
		}

		return "Principal/query", map[string]any{
			"accountId":           accountID,
			"queryState":          backend.PrincipalState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"total":               total,
			"ids":                 ids,
		}
	}
}

func handlePrincipalQueryChanges(backend PrincipalsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		if backend == nil {
			return "error", MethodErrorArgs(MethodErrorUnknownMethod, "Principals capability not supported")
		}
		accountID, _ := args["accountId"].(string)
		sinceQueryState, _ := args["sinceQueryState"].(string)

		return "Principal/queryChanges", map[string]any{
			"accountId":     accountID,
			"oldQueryState": sinceQueryState,
			"newQueryState": backend.PrincipalState(ctx),
			"added":         []map[string]any{},
			"removed":       []Id{},
		}
	}
}

func handlePrincipalSet(backend PrincipalsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		if backend == nil {
			return "error", MethodErrorArgs(MethodErrorUnknownMethod, "Principals capability not supported")
		}
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)
		updateMap, _ := args["update"].(map[string]any)
		destroyList, _ := args["destroy"].([]any)
		creationRefs := newSetCreationRefs(ctx)

		created := make(map[string]*Principal)
		updated := make(map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		// Create
		for clientKey, raw := range createMap {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := item["name"].(string)
			pType, _ := item["type"].(string)
			if pType == "" {
				pType = "individual"
			}
			email, _ := item["email"].(string)

			p := &Principal{
				Type:               pType,
				Name:               name,
				Email:              email,
				CalendarAddress:    "mailto:" + email,
				MayGetAvailability: true,
				MayShareWith:       true,
				AccountIDs:         map[string]bool{accountID: true},
			}
			res, err := backend.CreatePrincipal(ctx, p)
			if err != nil {
				notCreated[clientKey] = SetError{Type: "invalidProperties", Description: err.Error()}
			} else {
				created[clientKey] = res
				recordCreationRefs(ctx, creationRefs, clientKey, res.ID)
			}
		}

		// Update
		for idStr, patchRaw := range updateMap {
			patch, ok := patchRaw.(map[string]any)
			if !ok {
				continue
			}
			res, err := backend.UpdatePrincipal(ctx, Id(idStr), patch)
			if err != nil {
				notUpdated[idStr] = SetError{Type: "notFound", Description: err.Error()}
			} else {
				if res != nil {
					updated[idStr] = res
				} else {
					updated[idStr] = nil
				}
			}
		}

		// Destroy
		for _, item := range destroyList {
			idStr, ok := item.(string)
			if !ok {
				continue
			}
			ok, err := backend.DeletePrincipal(ctx, Id(idStr))
			if err != nil || !ok {
				notDestroyed[idStr] = SetError{Type: "notFound", Description: "principal not found"}
			} else {
				destroyed = append(destroyed, Id(idStr))
			}
		}

		return "Principal/set", map[string]any{
			"accountId":    accountID,
			"oldState":     backend.PrincipalState(ctx),
			"newState":     backend.PrincipalState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

func handlePrincipalGetAvailability(backend PrincipalsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		if backend == nil {
			return "error", MethodErrorArgs(MethodErrorUnknownMethod, "Principals capability not supported")
		}
		accountID, _ := args["accountId"].(string)
		principalIDStr, _ := args["principalId"].(string)
		utcStart, _ := args["utcStart"].(string)
		utcEnd, _ := args["utcEnd"].(string)

		if principalIDStr == "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, "principalId required")
		}

		principals, notFound, err := backend.GetPrincipals(ctx, []Id{Id(principalIDStr)})
		if err != nil || len(notFound) > 0 || len(principals) == 0 {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, fmt.Sprintf("principal %q not found", principalIDStr))
		}
		principal := principals[0]

		if !principal.MayGetAvailability {
			return "error", MethodErrorArgs(MethodErrorForbidden, "mayGetAvailability is false for principal")
		}

		windows, err := backend.GetAvailability(ctx, principal.ID, utcStart, utcEnd)
		if err != nil {
			return "error", MethodErrorArgs(MethodErrorServerFail, err.Error())
		}
		if windows == nil {
			windows = []*AvailabilityWindow{}
		}

		return "Principal/getAvailability", map[string]any{
			"accountId":   accountID,
			"principalId": principal.ID,
			"list":        windows,
		}
	}
}
