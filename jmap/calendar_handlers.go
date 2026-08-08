package jmap

import (
	"context"
	"encoding/json"
	"strings"
)

// RegisterCalendarHandlers registers JMAP for Calendars & JSCalendar method handlers into MethodRegistry.
func RegisterCalendarHandlers(r *MethodRegistry, backend CalendarsBackend, mailBackend MailBackend, blobBackend BlobBackend) {
	if backend == nil {
		return
	}
	r.Register("Calendar/get", handleCalendarGet(backend))
	r.Register("Calendar/changes", handleCalendarChanges(backend))
	r.Register("Calendar/set", handleCalendarSet(backend))
	r.Register("Calendar/copy", handleCalendarCopy(backend))

	r.Register("CalendarEvent/get", handleCalendarEventGet(backend))
	r.Register("CalendarEvent/changes", handleCalendarEventChanges(backend))
	r.Register("CalendarEvent/set", handleCalendarEventSet(backend, mailBackend))
	r.Register("CalendarEvent/query", handleCalendarEventQuery(backend))
	r.Register("CalendarEvent/queryChanges", handleCalendarEventQueryChanges(backend))
	r.Register("CalendarEvent/copy", handleCalendarEventCopy(backend))
	r.Register("CalendarEvent/parse", handleCalendarEventParse(backend, blobBackend))

	// ParticipantIdentity (draft-ietf-jmap-calendars Section 3)
	r.Register("ParticipantIdentity/get", handleParticipantIdentityGet(backend))
	r.Register("ParticipantIdentity/changes", handleParticipantIdentityChanges(backend))
	r.Register("ParticipantIdentity/set", handleParticipantIdentitySet(backend))

	// CalendarEventNotification (draft-ietf-jmap-calendars Section 7)
	r.Register("CalendarEventNotification/get", handleCalendarEventNotificationGet(backend))
	r.Register("CalendarEventNotification/changes", handleCalendarEventNotificationChanges(backend))
	r.Register("CalendarEventNotification/set", handleCalendarEventNotificationSet(backend))
	r.Register("CalendarEventNotification/query", handleCalendarEventNotificationQuery(backend))
	r.Register("CalendarEventNotification/queryChanges", handleCalendarEventNotificationQueryChanges(backend))
}

func handleCalendarGet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*Calendar
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetCalendars(ctx, ids)
		} else {
			list, err = backend.GetAllCalendars(ctx)
		}

		if err != nil || list == nil {
			list = []*Calendar{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Calendar/get", map[string]any{
			"accountId": accountID,
			"state":     backend.CalendarState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleCalendarChanges(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.CalendarChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}
		return "Calendar/changes", map[string]any{
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

func handleCalendarSet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.CalendarState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		created := make(map[string]*Calendar)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)
		creationRefs := newSetCreationRefs(ctx)

		onDestroyRemoveEvents, _ := args["onDestroyRemoveEvents"].(bool)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, calMapRaw := range createRaw {
				calMap, _ := calMapRaw.(map[string]any)
				if _, hasIsDefault := calMap["isDefault"]; hasIsDefault {
					notCreated[creationID] = SetError{
						Type:        "invalidProperties",
						Description: "isDefault is server-set and cannot be set directly",
						Properties:  []string{"isDefault"},
					}
					continue
				}
				calBytes, _ := json.Marshal(calMap)
				var cal Calendar
				_ = json.Unmarshal(calBytes, &cal)

				createdCal, err := backend.CreateCalendar(ctx, &cal)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdCal
					recordCreationRefs(ctx, creationRefs, creationID, createdCal.ID)
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				patch, _ := patchRaw.(map[string]any)
				if _, hasIsDefault := patch["isDefault"]; hasIsDefault {
					resolvedID := resolveCreationID(idStr, creationRefs)
					notUpdated[string(resolvedID)] = SetError{
						Type:        "invalidProperties",
						Description: "isDefault is server-set and cannot be set directly",
						Properties:  []string{"isDefault"},
					}
					continue
				}
				resolvedID := resolveCreationID(idStr, creationRefs)
				updatedCal, err := backend.UpdateCalendar(ctx, Id(resolvedID), resolvePatchCreationRefs(patch, creationRefs))
				if err != nil {
					notUpdated[string(resolvedID)] = SetError{Type: "notFound", Description: err.Error()}
				} else {
					_ = updatedCal
					updated[string(resolvedID)] = nil
				}
			}
		}

		if setDefaultRaw, ok := args["onSuccessSetIsDefault"].(string); ok && setDefaultRaw != "" {
			targetID := resolveCreationID(setDefaultRaw, creationRefs)
			_ = backend.SetDefaultCalendar(ctx, Id(targetID))
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					if !onDestroyRemoveEvents {
						hasEvs, _ := backend.CalendarHasEvents(ctx, Id(resolvedID))
						if hasEvs {
							notDestroyed[string(resolvedID)] = SetError{
								Type:        "calendarHasEvents",
								Description: "calendar contains events; use onDestroyRemoveEvents to delete",
							}
							continue
						}
					}
					okDel, err := backend.DeleteCalendar(ctx, Id(resolvedID))
					if err != nil || !okDel {
						notDestroyed[string(resolvedID)] = SetError{Type: "notFound", Description: "calendar cannot be deleted"}
					} else {
						destroyed = append(destroyed, Id(resolvedID))
					}
				}
			}
		}

		return "Calendar/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.CalendarState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

func handleCalendarEventGet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*CalendarEvent
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetCalendarEvents(ctx, ids)
		} else {
			list, err = backend.GetAllCalendarEvents(ctx)
		}

		if err != nil || list == nil {
			list = []*CalendarEvent{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		// Privacy (draft-ietf-jmap-calendars-27 Section 4.2.10) governs what NON-owner
		// sharees see: "private" returns only a reduced property set and "secret" makes the
		// server behave as though the event does not exist. The Principal that owns the
		// calendar always sees the full event, including its private and secret events.
		// CalendarEvent/get only ever runs against the caller's own account (SelfAccessGuard),
		// i.e. the owner, so no censoring is applied here. Cross-principal disclosure is
		// limited to the free-busy windows returned by Principal/getAvailability, which never
		// expose event titles or details.
		filteredList := make([]*CalendarEvent, 0, len(list))
		for _, ev := range list {
			if ev == nil {
				continue
			}
			filteredList = append(filteredList, ev)
		}

		return "CalendarEvent/get", map[string]any{
			"accountId": accountID,
			"state":     backend.CalendarEventState(ctx),
			"list":      filterList(filteredList, props),
			"notFound":  notFound,
		}
	}
}

func handleCalendarEventChanges(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.CalendarEventChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}
		return "CalendarEvent/changes", map[string]any{
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

func handleCalendarEventSet(backend CalendarsBackend, mailBackend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.CalendarEventState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		sendSchedulingMessages, _ := args["sendSchedulingMessages"].(bool)

		created := make(map[string]*CalendarEvent)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		creationRefs := newSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			notCreated = runCreateLoop(createRaw, creationRefs, func(creationID string, resolvedMap map[string]any) (string, error) {
				if sendSchedulingMessages && mailBackend == nil {
					return "", SetError{Type: "noSupportedScheduleMethods", Description: "no supported schedule methods available for scheduling"}
				}
				if err := validateCalendarEventMap(resolvedMap); err != nil {
					return "", err
				}
				evBytes, _ := json.Marshal(resolvedMap)
				var ev CalendarEvent
				_ = json.Unmarshal(evBytes, &ev)

				createdEv, err := backend.CreateCalendarEvent(ctx, &ev)
				if err != nil {
					return "", err
				}
				created[creationID] = createdEv
				recordCreationRefs(ctx, creationRefs, creationID, createdEv.ID)

				if sendSchedulingMessages && mailBackend != nil && len(createdEv.Participants) > 0 {
					reqICS, _ := BuildITIPRequest(createdEv, accountID)
					for emailStr, p := range createdEv.Participants {
						recipientEmail := emailStr
						if p != nil && p.Email != "" {
							recipientEmail = p.Email
						}
						if recipientEmail != "" {
							inviteEmail := &Email{
								Subject: "Invitation: " + createdEv.Title,
								To:      []EmailAddress{{Email: recipientEmail}},
								TextBody: []EmailBodyPart{{
									Type: "text/calendar; method=REQUEST",
									Size: uint64(len(reqICS)),
								}},
								BodyValues: map[string]EmailBodyValue{
									"1": {Value: reqICS},
								},
							}
							savedEmail, err := mailBackend.CreateEmail(ctx, inviteEmail)
							if err == nil && savedEmail != nil {
								_, _ = mailBackend.CreateSubmission(ctx, &EmailSubmission{
									EmailID:  savedEmail.ID,
									ThreadID: savedEmail.ThreadID,
								})
							}
						}
					}
				}
				// A scheduling change (iTIP dispatch) is recorded as a CalendarEventNotification
				// (Section 7): the event data after creation.
				if sendSchedulingMessages {
					_, _ = backend.CreateCalendarEventNotification(ctx, &CalendarEventNotification{
						Type:            "created",
						CalendarEventID: createdEv.ID,
						ChangedBy:       notificationChangedBy(createdEv),
						Event:           createdEv,
					})
				}
				return string(createdEv.ID), nil
			})
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				rawPatch, _ := patchRaw.(map[string]any)
				patch := resolvePatchCreationRefs(rawPatch, creationRefs)
				resolvedID := resolveCreationID(idStr, creationRefs)
				if sendSchedulingMessages && mailBackend == nil {
					notUpdated[string(resolvedID)] = SetError{Type: "noSupportedScheduleMethods", Description: "no supported schedule methods available for scheduling"}
					continue
				}
				if err := validateCalendarEventMap(patch); err != nil {
					if setErr, isSetErr := err.(SetError); isSetErr {
						notUpdated[string(resolvedID)] = setErr
					} else {
						notUpdated[string(resolvedID)] = SetError{Type: "invalidProperties", Description: err.Error()}
					}
					continue
				}
				beforeList, _, _ := backend.GetCalendarEvents(ctx, []Id{Id(resolvedID)})
				var beforeEv *CalendarEvent
				if len(beforeList) > 0 {
					// Deep-copy: the memory backend returns its stored pointer and the
					// update below mutates it in place; the notification must carry the
					// pre-change data.
					beforeBytes, _ := json.Marshal(beforeList[0])
					beforeEv = &CalendarEvent{}
					_ = json.Unmarshal(beforeBytes, beforeEv)
				}
				updatedEv, err := backend.UpdateCalendarEvent(ctx, Id(resolvedID), patch)
				if err != nil {
					notUpdated[string(resolvedID)] = SetError{Type: "notFound", Description: err.Error()}
				} else {
					updated[string(resolvedID)] = nil

					// Record the scheduling change as a CalendarEventNotification: the
					// "event" carries the data before the change and "eventPatch" encodes
					// the change itself (Section 7.2).
					if sendSchedulingMessages {
						_, _ = backend.CreateCalendarEventNotification(ctx, &CalendarEventNotification{
							Type:            "updated",
							CalendarEventID: updatedEv.ID,
							ChangedBy:       notificationChangedBy(updatedEv),
							Event:           beforeEv,
							EventPatch:      patch,
						})
					}

					if sendSchedulingMessages && mailBackend != nil && updatedEv != nil && len(updatedEv.Participants) > 0 {
						reqICS, _ := BuildITIPRequest(updatedEv, accountID)
						for emailStr, p := range updatedEv.Participants {
							recipientEmail := emailStr
							if p != nil && p.Email != "" {
								recipientEmail = p.Email
							}
							if recipientEmail != "" {
								inviteEmail := &Email{
									Subject: "Updated Invitation: " + updatedEv.Title,
									To:      []EmailAddress{{Email: recipientEmail}},
									TextBody: []EmailBodyPart{{
										Type: "text/calendar; method=REQUEST",
										Size: uint64(len(reqICS)),
									}},
									BodyValues: map[string]EmailBodyValue{
										"1": {Value: reqICS},
									},
								}
								savedEmail, err := mailBackend.CreateEmail(ctx, inviteEmail)
								if err == nil && savedEmail != nil {
									_, _ = mailBackend.CreateSubmission(ctx, &EmailSubmission{
										EmailID:  savedEmail.ID,
										ThreadID: savedEmail.ThreadID,
									})
								}
							}
						}
					}
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					evID := Id(resolveCreationID(idStr, creationRefs))
					events, _, _ := backend.GetCalendarEvents(ctx, []Id{evID})

					okDel, err := backend.DeleteCalendarEvent(ctx, evID)
					if err != nil || !okDel {
						notDestroyed[string(evID)] = SetError{Type: "notFound", Description: "calendar event not found"}
					} else {
						destroyed = append(destroyed, evID)

						// Record the cancellation as a CalendarEventNotification carrying the
						// pre-destroy event data (Section 7.2).
						if sendSchedulingMessages {
							_, _ = backend.CreateCalendarEventNotification(ctx, &CalendarEventNotification{
								Type:            "destroyed",
								CalendarEventID: evID,
								ChangedBy:       notificationChangedBy(events[0]),
								Event:           events[0],
							})
						}

						// Auto-dispatch iMIP METHOD:CANCEL notice to participants if mailBackend is available
						if mailBackend != nil && len(events) > 0 && events[0] != nil && len(events[0].Participants) > 0 {
							ev := events[0]
							cancelICS, _ := BuildITIPCancel(ev, accountID)
							for emailStr, p := range ev.Participants {
								recipientEmail := emailStr
								if p != nil && p.Email != "" {
									recipientEmail = p.Email
								}
								if recipientEmail != "" {
									cancelEmail := &Email{
										Subject: "Cancelled: " + ev.Title,
										To:      []EmailAddress{{Email: recipientEmail}},
										TextBody: []EmailBodyPart{{
											Type: "text/calendar; method=CANCEL",
											Size: uint64(len(cancelICS)),
										}},
										BodyValues: map[string]EmailBodyValue{
											"1": {Value: cancelICS},
										},
									}
									savedEmail, err := mailBackend.CreateEmail(ctx, cancelEmail)
									if err == nil && savedEmail != nil {
										_, _ = mailBackend.CreateSubmission(ctx, &EmailSubmission{
											EmailID:  savedEmail.ID,
											ThreadID: savedEmail.ThreadID,
										})
									}
								}
							}
						}
					}
				}
			}
		}

		return "CalendarEvent/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.CalendarEventState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

func handleCalendarEventQuery(backend CalendarsBackend) MethodHandler {
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

		filter, _ = args["filter"].(map[string]any)
		if tz, ok := args["timeZone"].(string); ok && tz != "" {
			if filter == nil {
				filter = make(map[string]any)
			}
			filter["timeZone"] = tz
		}

		expandRecurrences, _ := args["expandRecurrences"].(bool)
		comparators := parseComparators(args)

		var ids []Id
		var total int
		var err error
		if anchor != "" {
			allIDs, allTotal, _ := backend.QueryCalendarEvents(ctx, filter, comparators, 0, nil, expandRecurrences)
			total = allTotal
			var found bool
			position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else {
			ids, total, err = backend.QueryCalendarEvents(ctx, filter, comparators, position, limit, expandRecurrences)
		}
		if err != nil {
			ids = []Id{}
			total = 0
		}

		return "CalendarEvent/query", map[string]any{
			"accountId":           accountID,
			"queryState":          backend.CalendarEventState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"total":               total,
			"ids":                 ids,
		}
	}
}

func handleCalendarEventQueryChanges(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)

		if sinceState == "" {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is required")
		}

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.CalendarEventChanges(ctx, sinceState)
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		comparators := parseComparators(args)
		filter, _ := args["filter"].(map[string]any)
		currentIDs, _, _ := backend.QueryCalendarEvents(ctx, filter, comparators, 0, nil, false)
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
		return "CalendarEvent/queryChanges", res
	}
}

// handleCalendarCopy implements Calendar/copy per RFC 8620 Section 5.4: each create entry names a
// source calendar by id, optionally overriding properties, and is recreated in the target account.
func handleCalendarCopy(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		fromAccountID, _ := args["fromAccountId"].(string)
		if fromAccountID == "" {
			fromAccountID = accountID
		}
		// Read the source objects from the "from" account, not the destination account.
		srcCtx := sourceAccountContext(ctx, args)
		oldState := backend.CalendarState(ctx)

		onSuccessDestroyOriginal, _ := args["onSuccessDestroyOriginal"].(bool)
		if dfis, ok := args["destroyFromIfInState"].(string); ok && dfis != "" && dfis != backend.CalendarState(srcCtx) {
			return "error", MethodErrorArgs("stateMismatch", "destroyFromIfInState does not match the source account state")
		}

		created := make(map[string]*Calendar)
		notCreated := make(map[string]any)
		destroyOriginals := make([]Id, 0)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				m, _ := raw.(map[string]any)
				srcID, _ := m["id"].(string)
				if srcID == "" {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: "copy create entry must reference a source id"}
					continue
				}
				srcs, notFound, _ := backend.GetCalendars(srcCtx, []Id{Id(srcID)})
				if len(srcs) == 0 || len(notFound) > 0 {
					notCreated[creationID] = SetError{Type: "notFound", Description: "source calendar not found: " + srcID}
					continue
				}

				merged := mergeCopyOverrides(srcs[0], m)
				calBytes, _ := json.Marshal(merged)
				var cal Calendar
				_ = json.Unmarshal(calBytes, &cal)
				cal.ID = ""

				newCal, err := backend.CreateCalendar(ctx, &cal)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = newCal
					destroyOriginals = append(destroyOriginals, Id(srcID))
				}
			}
		}

		if onSuccessDestroyOriginal {
			for _, srcID := range destroyOriginals {
				_, _ = backend.DeleteCalendar(srcCtx, srcID)
			}
		}

		return "Calendar/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.CalendarState(ctx),
			"created":       created,
			"notCreated":    notCreated,
		}
	}
}

// sourceAccountContext returns a context scoped to the copy's fromAccountId so source objects
// are read from the correct account. An empty or "primary" fromAccountId means the caller's own
// account, i.e. the context is left unchanged.
func sourceAccountContext(ctx context.Context, args map[string]any) context.Context {
	if raw, _ := args["fromAccountId"].(string); raw != "" && raw != "primary" {
		return ContextWithAccountID(ctx, raw)
	}
	return ctx
}

// handleCalendarEventCopy implements CalendarEvent/copy per RFC 8620 Section 5.4.
func handleCalendarEventCopy(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		fromAccountID, _ := args["fromAccountId"].(string)
		if fromAccountID == "" {
			fromAccountID = accountID
		}
		srcCtx := sourceAccountContext(ctx, args)
		oldState := backend.CalendarEventState(ctx)

		onSuccessDestroyOriginal, _ := args["onSuccessDestroyOriginal"].(bool)
		if dfis, ok := args["destroyFromIfInState"].(string); ok && dfis != "" && dfis != backend.CalendarEventState(srcCtx) {
			return "error", MethodErrorArgs("stateMismatch", "destroyFromIfInState does not match the source account state")
		}

		created := make(map[string]*CalendarEvent)
		notCreated := make(map[string]any)
		destroyOriginals := make([]Id, 0)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				m, _ := raw.(map[string]any)
				srcID, _ := m["id"].(string)
				if srcID == "" {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: "copy create entry must reference a source id"}
					continue
				}
				srcs, notFound, _ := backend.GetCalendarEvents(srcCtx, []Id{Id(srcID)})
				if len(srcs) == 0 || len(notFound) > 0 {
					notCreated[creationID] = SetError{Type: "notFound", Description: "source event not found: " + srcID}
					continue
				}

				merged := mergeCopyOverrides(srcs[0], m)
				evBytes, _ := json.Marshal(merged)
				var ev CalendarEvent
				_ = json.Unmarshal(evBytes, &ev)
				ev.ID = ""

				newEv, err := backend.CreateCalendarEvent(ctx, &ev)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = newEv
					destroyOriginals = append(destroyOriginals, Id(srcID))
				}
			}
		}

		if onSuccessDestroyOriginal {
			for _, srcID := range destroyOriginals {
				_, _ = backend.DeleteCalendarEvent(srcCtx, srcID)
			}
		}

		return "CalendarEvent/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.CalendarEventState(ctx),
			"created":       created,
			"notCreated":    notCreated,
		}
	}
}

// handleCalendarEventParse implements CalendarEvent/parse per draft-ietf-jmap-calendars
// Section 5.12: the client supplies blob ids of iCalendar files and the server returns the
// parsed JSCalendar CalendarEvent objects. Support is advertised via the
// "urn:ietf:params:jmap:calendars:parse" capability.
func handleCalendarEventParse(backend CalendarsBackend, blobBackend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		props := parseProperties(args)
		creationRefs := newSetCreationRefs(ctx)

		parsed := make(map[string][]any)
		var notFound []Id
		var notParsable []Id

		blobIDsRaw, _ := args["blobIds"].([]any)
		for _, item := range blobIDsRaw {
			idStr, ok := item.(string)
			if !ok || idStr == "" {
				continue
			}
			blobID := Id(resolveCreationID(idStr, creationRefs))
			if blobBackend == nil {
				notParsable = append(notParsable, blobID)
				continue
			}
			blob, found, err := blobBackend.GetBlob(ctx, accountID, string(blobID))
			if err != nil || !found || blob == nil {
				notFound = append(notFound, blobID)
				continue
			}
			events, err := ParseICalendar(blob.Data)
			if err != nil || len(events) == 0 {
				notParsable = append(notParsable, blobID)
				continue
			}
			converted := make([]any, 0, len(events))
			for _, ev := range events {
				converted = append(converted, filterParsedEvent(ev, props))
			}
			parsed[string(blobID)] = converted
		}

		res := map[string]any{"accountId": accountID}
		if len(parsed) > 0 {
			res["parsed"] = parsed
		}
		if len(notFound) > 0 {
			res["notFound"] = notFound
		}
		if len(notParsable) > 0 {
			res["notParsable"] = notParsable
		}
		return "CalendarEvent/parse", res
	}
}

// parseMetadataProperties are the CalendarEvent metadata properties that are null in
// CalendarEvent/parse output (draft-ietf-jmap-calendars Section 5.12).
var parseMetadataProperties = map[string]bool{
	"id": true, "baseEventId": true, "calendarIds": true, "isDraft": true, "isOrigin": true,
}

// filterParsedEvent reduces a parsed CalendarEvent to the requested properties. Metadata
// properties (id, baseEventId, calendarIds, isDraft, isOrigin) are omitted from the default
// output and returned as explicit nulls when requested. When no properties are given all
// properties are returned.
func filterParsedEvent(ev *CalendarEvent, properties []string) map[string]any {
	data, _ := json.Marshal(ev)
	var m map[string]any
	_ = json.Unmarshal(data, &m)

	if len(properties) == 0 {
		for key := range parseMetadataProperties {
			delete(m, key)
		}
		return m
	}
	out := make(map[string]any, len(properties))
	for _, p := range properties {
		if parseMetadataProperties[p] {
			out[p] = nil
			continue
		}
		if v, ok := m[p]; ok {
			out[p] = v
		}
	}
	return out
}

var validCalendarEventProperties = map[string]bool{
	"@type": true, "id": true, "calendarIds": true, "title": true, "description": true,
	"descriptionContentType": true, "showWithoutTime": true, "start": true, "duration": true,
	"timeZone": true, "locations": true, "location": true, "virtualLocations": true,
	"links": true, "locale": true, "categories": true, "color": true, "status": true,
	"freeBusyStatus": true, "privacy": true, "hideAttendees": true, "priority": true, "replyTo": true,
	"sentBy": true, "requestStatus": true, "useDefaultAlerts": true, "localizations": true,
	"timeZones": true, "participants": true, "recurrenceRules": true, "recurrenceId": true,
	"recurrenceIdTimeZone": true, "excludedRecurrenceRules": true, "recurrenceOverrides": true,
	"excluded": true, "alerts": true, "relatedTo": true, "prodId": true, "sequence": true,
	"method": true, "due": true, "estimatedDuration": true, "percentComplete": true,
	"progress": true, "progressUpdated": true, "entries": true, "source": true,
	"created": true, "updated": true, "uid": true, "keywords": true,
}

func validateCalendarEventMap(m map[string]any) error {
	for k, v := range m {
		baseKey := k
		if strings.Contains(k, "/") {
			baseKey = strings.Split(k, "/")[0]
		}
		if !validCalendarEventProperties[baseKey] {
			return SetError{
				Type:        "invalidProperties",
				Description: "unknown property: " + k,
				Properties:  []string{k},
			}
		}
		switch baseKey {
		case "status":
			if s, ok := v.(string); ok && s != "" {
				switch s {
				case "confirmed", "tentative", "cancelled", "needs-action", "completed", "in-progress":
				default:
					return SetError{Type: "invalidProperties", Description: "invalid status value: " + s, Properties: []string{"status"}}
				}
			}
		case "privacy":
			if s, ok := v.(string); ok && s != "" {
				switch s {
				case "public", "private", "secret":
				default:
					return SetError{Type: "invalidProperties", Description: "invalid privacy value: " + s, Properties: []string{"privacy"}}
				}
			}
		case "freeBusyStatus":
			if s, ok := v.(string); ok && s != "" {
				switch s {
				case "free", "busy", "tentative":
				default:
					return SetError{Type: "invalidProperties", Description: "invalid freeBusyStatus value: " + s, Properties: []string{"freeBusyStatus"}}
				}
			}
		}
	}
	return nil
}

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
