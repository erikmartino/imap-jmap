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

	r.Register("CalendarEvent/get", handleCalendarEventGet(backend))
	r.Register("CalendarEvent/changes", handleCalendarEventChanges(backend))
	r.Register("CalendarEvent/set", handleCalendarEventSet(backend, mailBackend))
	r.Register("CalendarEvent/query", handleCalendarEventQuery(backend))
	r.Register("CalendarEvent/copy", handleCalendarEventCopy(backend))
	r.Register("CalendarEvent/parseInvitation", handleCalendarEventParseInvitation(backend))
	r.Register("CalendarEvent/sendResponse", handleCalendarEventSendResponse(backend))
}

func handleCalendarGet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)

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
			"state":     "0",
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleCalendarChanges(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Calendar/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       args["sinceState"],
			"newState":       "0",
			"hasMoreChanges": false,
			"created":        []Id{},
			"updated":        []Id{},
			"destroyed":      []Id{},
		}
	}
}

func handleCalendarSet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		created := make(map[string]*Calendar)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, calMap := range createRaw {
				calBytes, _ := json.Marshal(calMap)
				var cal Calendar
				_ = json.Unmarshal(calBytes, &cal)

				createdCal, err := backend.CreateCalendar(ctx, &cal)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdCal
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				patch, _ := patchRaw.(map[string]any)
				updatedCal, err := backend.UpdateCalendar(ctx, Id(idStr), patch)
				if err != nil {
					notUpdated[idStr] = SetError{Type: "notFound", Description: err.Error()}
				} else {
					_ = updatedCal
					updated[idStr] = nil
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					okDel, err := backend.DeleteCalendar(ctx, Id(idStr))
					if err != nil || !okDel {
						notDestroyed[idStr] = SetError{Type: "notFound", Description: "calendar cannot be deleted"}
					} else {
						destroyed = append(destroyed, Id(idStr))
					}
				}
			}
		}

		return "Calendar/set", map[string]any{
			"accountId":    accountID,
			"oldState":     "0",
			"newState":     "0",
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
			"state":     "0",
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleCalendarEventChanges(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "CalendarEvent/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       args["sinceState"],
			"newState":       "0",
			"hasMoreChanges": false,
			"created":        []Id{},
			"updated":        []Id{},
			"destroyed":      []Id{},
		}
	}
}

func handleCalendarEventSet(backend CalendarsBackend, mailBackend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		created := make(map[string]*CalendarEvent)
		updated := make(map[string]map[string]any)
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, evMap := range createRaw {
				evBytes, _ := json.Marshal(evMap)
				var ev CalendarEvent
				_ = json.Unmarshal(evBytes, &ev)

				createdEv, err := backend.CreateCalendarEvent(ctx, &ev)
				if err != nil {
					notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdEv

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
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				patch, _ := patchRaw.(map[string]any)
				updatedEv, err := backend.UpdateCalendarEvent(ctx, Id(idStr), patch)
				if err != nil {
					notUpdated[idStr] = SetError{Type: "notFound", Description: err.Error()}
				} else {
					updated[idStr] = nil

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
					evID := Id(idStr)
					events, _, _ := backend.GetCalendarEvents(ctx, []Id{evID})

					okDel, err := backend.DeleteCalendarEvent(ctx, evID)
					if err != nil || !okDel {
						notDestroyed[idStr] = SetError{Type: "notFound", Description: "calendar event not found"}
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
			"oldState":     "0",
			"newState":     "0",
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
		positionFloat, _ := args["position"].(float64)
		position := int(positionFloat)

		var limit *uint64
		if limitFloat, ok := args["limit"].(float64); ok {
			l := uint64(limitFloat)
			limit = &l
		}

		ids, total, err := backend.QueryCalendarEvents(ctx, filter, position, limit)
		if err != nil {
			ids = []Id{}
			total = 0
		}

		return "CalendarEvent/query", map[string]any{
			"accountId": accountID,
			"queryState": "0",
			"canCalculateChanges": false,
			"position":  position,
			"total":     total,
			"ids":       ids,
		}
	}
}

func handleCalendarEventCopy(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "CalendarEvent/copy", map[string]any{
			"accountId": accountID,
			"oldState":  "0",
			"newState":  "0",
			"created":   map[string]*CalendarEvent{},
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

