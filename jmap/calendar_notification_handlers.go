package jmap

import (
	"context"
	"encoding/json"
)

// --- ParticipantIdentity (draft-ietf-jmap-calendars Section 3) ---

func handleParticipantIdentityGet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*ParticipantIdentity
		var notFound []Id
		var err error
		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetParticipantIdentities(ctx, ids)
		} else {
			list, err = backend.GetAllParticipantIdentities(ctx)
		}
		if err != nil || list == nil {
			list = []*ParticipantIdentity{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "ParticipantIdentity/get", map[string]any{
			"accountId": accountID,
			"state":     backend.ParticipantIdentityState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleParticipantIdentityChanges(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.ParticipantIdentityChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}
		return "ParticipantIdentity/changes", map[string]any{
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

// validateParticipantIdentityPayload rejects server-set properties and non-conforming
// sendTo keys ("MUST only contain ASCII alphanumeric characters", Section 3).
func validateParticipantIdentityPayload(m map[string]any) error {
	if _, hasIsDefault := m["isDefault"]; hasIsDefault {
		return SetError{
			Type:        "invalidProperties",
			Description: "isDefault is server-set and cannot be set directly",
			Properties:  []string{"isDefault"},
		}
	}
	if sendTo, ok := m["sendTo"].(map[string]any); ok {
		for k := range sendTo {
			for _, c := range k {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
					return SetError{
						Type:        "invalidProperties",
						Description: "sendTo keys must only contain ASCII alphanumeric characters: " + k,
						Properties:  []string{"sendTo"},
					}
				}
			}
		}
	}
	return nil
}

func handleParticipantIdentitySet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.ParticipantIdentityState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		created := make(map[string]*ParticipantIdentity)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)
		creationRefs := newSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			notCreated = runCreateLoop(createRaw, creationRefs, func(creationID string, resolvedMap map[string]any) (string, error) {
				if err := validateParticipantIdentityPayload(resolvedMap); err != nil {
					return "", err
				}
				piBytes, _ := json.Marshal(resolvedMap)
				var pi ParticipantIdentity
				_ = json.Unmarshal(piBytes, &pi)

				createdPI, err := backend.CreateParticipantIdentity(ctx, &pi)
				if err != nil {
					return "", err
				}
				created[creationID] = createdPI
				recordCreationRefs(ctx, creationRefs, creationID, createdPI.ID)
				return string(createdPI.ID), nil
			})
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				rawPatch, _ := patchRaw.(map[string]any)
				if err := validateParticipantIdentityPayload(rawPatch); err != nil {
					notUpdated[string(resolveCreationID(idStr, creationRefs))] = err
					continue
				}
				patch := resolvePatchCreationRefs(rawPatch, creationRefs)
				resolvedID := resolveCreationID(idStr, creationRefs)
				updatedPI, err := backend.UpdateParticipantIdentity(ctx, Id(resolvedID), patch)
				if err != nil {
					notUpdated[string(resolvedID)] = SetError{Type: "notFound", Description: err.Error()}
				} else {
					// RFC 8620 Section 5.3: the value is null unless the server changed
					// properties beyond those the client sent. A plain update reports null.
					updated[string(resolvedID)] = nil
					_ = updatedPI
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, idItem := range destroyRaw {
				if idStr, ok := idItem.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					okDel, err := backend.DeleteParticipantIdentity(ctx, Id(resolvedID))
					if err != nil || !okDel {
						notDestroyed[string(resolvedID)] = SetError{Type: "notFound", Description: "participant identity not found"}
					} else {
						destroyed = append(destroyed, Id(resolvedID))
					}
				}
			}
		}

		// onSuccessSetIsDefault (Section 3.3): applied only after every create/update/destroy
		// succeeded; a missing or forbidden id is silently ignored, and the identity whose
		// server-set isDefault value changed is reported in "updated".
		if defaultRaw, ok := args["onSuccessSetIsDefault"].(string); ok && defaultRaw != "" {
			targetID := resolveCreationID(defaultRaw, creationRefs)
			if err := backend.SetDefaultParticipantIdentity(ctx, Id(targetID)); err == nil {
				updated[targetID] = map[string]any{"isDefault": true}
			}
		}

		return "ParticipantIdentity/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.ParticipantIdentityState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

// --- CalendarEventNotification (draft-ietf-jmap-calendars Section 7) ---

func handleCalendarEventNotificationGet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*CalendarEventNotification
		var notFound []Id
		var err error
		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetCalendarEventNotifications(ctx, ids)
		} else {
			list, err = backend.GetAllCalendarEventNotifications(ctx)
		}
		if err != nil || list == nil {
			list = []*CalendarEventNotification{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "CalendarEventNotification/get", map[string]any{
			"accountId": accountID,
			"state":     backend.CalendarEventNotificationState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleCalendarEventNotificationChanges(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.CalendarEventNotificationChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}
		return "CalendarEventNotification/changes", map[string]any{
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

// handleCalendarEventNotificationSet only supports destroy: notifications are created by
// the server, so any create/update attempt is rejected with a forbidden SetError
// (draft-ietf-jmap-calendars Section 7.5).
func handleCalendarEventNotificationSet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.CalendarEventNotificationState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		created := make(map[string]*CalendarEventNotification)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID := range createRaw {
				notCreated[creationID] = SetError{
					Type:        "forbidden",
					Description: "CalendarEventNotification objects are created by the server and cannot be created by clients",
				}
			}
		}
		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr := range updateRaw {
				notUpdated[string(resolveCreationID(idStr, newSetCreationRefs(ctx)))] = SetError{
					Type:        "forbidden",
					Description: "CalendarEventNotification objects are managed by the server and cannot be updated by clients",
				}
			}
		}

		creationRefs := newSetCreationRefs(ctx)
		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, idItem := range destroyRaw {
				if idStr, ok := idItem.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					okDel, err := backend.DeleteCalendarEventNotification(ctx, Id(resolvedID))
					if err != nil || !okDel {
						notDestroyed[string(resolvedID)] = SetError{Type: "notFound", Description: "calendar event notification not found"}
					} else {
						destroyed = append(destroyed, Id(resolvedID))
					}
				}
			}
		}

		return "CalendarEventNotification/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.CalendarEventNotificationState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

// notificationSortableProperties are the CalendarEventNotification sort properties per
// Section 7.6.2: "created" is the only one that MUST be supported.
var notificationSortableProperties = map[string]bool{"created": true}

func handleCalendarEventNotificationQuery(backend CalendarsBackend) MethodHandler {
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
		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, notificationSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		var limit *uint64
		if lim, ok := args["limit"].(float64); ok {
			l := uint64(lim)
			limit = &l
		}

		var ids []Id
		var total int
		if anchor != "" {
			allIDs, allTotal, _ := backend.QueryCalendarEventNotifications(ctx, filter, comparators, 0, nil)
			total = allTotal
			var found bool
			position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else {
			ids, total, _ = backend.QueryCalendarEventNotifications(ctx, filter, comparators, position, limit)
		}
		if ids == nil {
			ids = []Id{}
		}

		return "CalendarEventNotification/query", map[string]any{
			"accountId":           accountID,
			"queryState":          backend.CalendarEventNotificationState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"total":               total,
			"ids":                 ids,
		}
	}
}

func handleCalendarEventNotificationQueryChanges(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)

		if sinceState == "" {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is required")
		}

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.CalendarEventNotificationChanges(ctx, sinceState)
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		filter, _ := args["filter"].(map[string]any)
		comparators := parseComparators(args)
		currentIDs, _, _ := backend.QueryCalendarEventNotifications(ctx, filter, comparators, 0, nil)
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
		return "CalendarEventNotification/queryChanges", res
	}
}

// notificationChangedBy builds the "changedBy" Person for a notification from the event's
// owner participant, falling back to an empty Person when the event has no owner.
func notificationChangedBy(ev *CalendarEvent) CalendarEventNotificationPerson {
	if ev == nil {
		return CalendarEventNotificationPerson{}
	}
	for _, p := range ev.Participants {
		if p == nil {
			continue
		}
		if (p.Roles != nil && p.Roles["owner"]) || p.Role == "owner" {
			email := p.Email
			return CalendarEventNotificationPerson{
				Name:            p.Name,
				Email:           &email,
				CalendarAddress: &email,
			}
		}
	}
	return CalendarEventNotificationPerson{}
}
