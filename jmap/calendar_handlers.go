package jmap

import (
	"context"
	"encoding/json"
)

// RegisterCalendarHandlers registers JMAP for Calendars & JSCalendar method handlers into MethodRegistry.
func RegisterCalendarHandlers(r *MethodRegistry, backend CalendarsBackend, mailBackend MailBackend) {
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
	r.Register("CalendarEvent/parseInvitation", handleCalendarEventParseInvitation(backend))
	r.Register("CalendarEvent/sendResponse", handleCalendarEventSendResponse(backend))
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

		return "CalendarEvent/get", map[string]any{
			"accountId": accountID,
			"state":     backend.CalendarEventState(ctx),
			"list":      filterList(list, props),
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

		created := make(map[string]*CalendarEvent)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		// creationRefs maps a creation id to the real id the server assigned (seeded from
		// the request-scoped createdIds map), so #creationId references in this call and
		// in later method calls of the same request resolve (RFC 8620 Section 5.3).
		creationRefs := newSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			notCreated = runCreateLoop(createRaw, creationRefs, func(creationID string, resolvedMap map[string]any) (string, error) {
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

				// Auto-dispatch iMIP email invitation to external participants if mailBackend is available
				if mailBackend != nil && len(createdEv.Participants) > 0 {
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
				return string(createdEv.ID), nil
			})
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				rawPatch, _ := patchRaw.(map[string]any)
				patch := resolvePatchCreationRefs(rawPatch, creationRefs)
				resolvedID := resolveCreationID(idStr, creationRefs)
				if err := validateCalendarEventMap(patch); err != nil {
					if setErr, isSetErr := err.(SetError); isSetErr {
						notUpdated[string(resolvedID)] = setErr
					} else {
						notUpdated[string(resolvedID)] = SetError{Type: "invalidProperties", Description: err.Error()}
					}
					continue
				}
				updatedEv, err := backend.UpdateCalendarEvent(ctx, Id(resolvedID), patch)
				if err != nil {
					notUpdated[string(resolvedID)] = SetError{Type: "notFound", Description: err.Error()}
				} else {
					updated[string(resolvedID)] = nil

					// Auto-dispatch iMIP email invitation to participants if updated event has participants
					if mailBackend != nil && updatedEv != nil && len(updatedEv.Participants) > 0 {
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
		oldState := backend.CalendarState(ctx)

		created := make(map[string]*Calendar)
		notCreated := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				m, _ := raw.(map[string]any)
				srcID, _ := m["id"].(string)
				if srcID == "" {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: "copy create entry must reference a source id"}
					continue
				}
				srcs, notFound, _ := backend.GetCalendars(ctx, []Id{Id(srcID)})
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
				}
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

// handleCalendarEventCopy implements CalendarEvent/copy per RFC 8620 Section 5.4.
func handleCalendarEventCopy(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		fromAccountID, _ := args["fromAccountId"].(string)
		if fromAccountID == "" {
			fromAccountID = accountID
		}
		oldState := backend.CalendarEventState(ctx)

		created := make(map[string]*CalendarEvent)
		notCreated := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				m, _ := raw.(map[string]any)
				srcID, _ := m["id"].(string)
				if srcID == "" {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: "copy create entry must reference a source id"}
					continue
				}
				srcs, notFound, _ := backend.GetCalendarEvents(ctx, []Id{Id(srcID)})
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
				}
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

func handleCalendarEventParseInvitation(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		content, _ := args["content"].(string)

		msg, err := ParseITIPMessage(content)
		if err != nil {
			return "CalendarEvent/parseInvitation", map[string]any{
				"accountId": accountID,
				"error":     SetError{Type: "invalidProperties", Description: err.Error()},
			}
		}

		return "CalendarEvent/parseInvitation", map[string]any{
			"accountId": accountID,
			"method":    msg.Method,
			"uid":       msg.UID,
			"summary":   msg.Summary,
			"start":     msg.Start,
			"organizer": msg.Organizer,
			"status":    msg.Status,
		}
	}
}

func handleCalendarEventSendResponse(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		eventIDStr, _ := args["id"].(string)
		attendeeEmail, _ := args["attendeeEmail"].(string)
		status, _ := args["status"].(string) // "accepted", "declined", "tentative"

		eventID := Id(eventIDStr)
		events, _, err := backend.GetCalendarEvents(ctx, []Id{eventID})
		if err != nil || len(events) == 0 {
			return "CalendarEvent/sendResponse", map[string]any{
				"accountId": accountID,
				"error":     SetError{Type: "notFound", Description: "calendar event not found"},
			}
		}

		ev := events[0]
		if ev.Participants == nil {
			ev.Participants = make(map[string]*JSCalendarParticipant)
		}
		if p, ok := ev.Participants[attendeeEmail]; ok && p != nil {
			p.Status = status
		} else {
			ev.Participants[attendeeEmail] = &JSCalendarParticipant{
				Email:  attendeeEmail,
				Status: status,
			}
		}

		// Update participant status in storage
		_, _ = backend.UpdateCalendarEvent(ctx, eventID, map[string]any{
			"status": status,
		})

		// Build iTIP reply string
		replyICS, err := BuildITIPReply(ev, attendeeEmail, status)
		if err != nil {
			return "CalendarEvent/sendResponse", map[string]any{
				"accountId": accountID,
				"error":     SetError{Type: "invalidProperties", Description: err.Error()},
			}
		}

		return "CalendarEvent/sendResponse", map[string]any{
			"accountId":     accountID,
			"id":            eventID,
			"attendeeEmail": attendeeEmail,
			"status":        status,
			"itipReply":     replyICS,
		}
	}
}

var validCalendarEventProperties = map[string]bool{
	"@type": true, "id": true, "calendarIds": true, "title": true, "description": true,
	"descriptionContentType": true, "showWithoutTime": true, "start": true, "duration": true,
	"timeZone": true, "locations": true, "location": true, "virtualLocations": true,
	"links": true, "locale": true, "categories": true, "color": true, "status": true,
	"freeBusyStatus": true, "privacy": true, "priority": true, "replyTo": true,
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
		if !validCalendarEventProperties[k] {
			return SetError{
				Type:        "invalidProperties",
				Description: "unknown property: " + k,
				Properties:  []string{k},
			}
		}
		switch k {
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
