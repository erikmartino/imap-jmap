package jmap

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func handleCalendarEventGet(backend CalendarsBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*CalendarEvent
		var notFound []Id
		var err error

		if hasIDs {
			if len(idsRaw) == 0 {
				list = []*CalendarEvent{}
				notFound = []Id{}
				_ = backend.CalendarEventState(ctx)
			} else {
				ids := make([]Id, 0, len(idsRaw))
				for _, item := range idsRaw {
					if idStr, ok := item.(string); ok {
						ids = append(ids, Id(idStr))
					}
				}
				list, notFound, err = backend.GetCalendarEvents(ctx, ids)
			}
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
			clone := *ev
			if clone.TimeZone == "" || clone.TimeZone == "UTC" {
				clone.TimeZone = "Etc/UTC"
			}
			if clone.Start != "" {
				loc := loadLocation(clone.TimeZone)
				if t, ok := parseLocalDateTimeBound(clone.Start, loc); ok {
					clone.UTCStart = t.UTC().Format("2006-01-02T15:04:05Z")
					dur := 1 * time.Hour
					if clone.Duration != "" {
						if d, ok := parseISODuration(clone.Duration); ok {
							dur = d
						}
					}
					clone.UTCEnd = t.Add(dur).UTC().Format("2006-01-02T15:04:05Z")
				}
				if strings.HasSuffix(clone.Start, "Z") {
					clone.Start = strings.TrimSuffix(clone.Start, "Z")
				}
			}
			if len(clone.RecurrenceRules) > 0 && clone.RecurrenceRule == nil {
				clone.RecurrenceRule = clone.RecurrenceRules[0]
			} else if clone.RecurrenceRule != nil && len(clone.RecurrenceRules) == 0 {
				clone.RecurrenceRules = []*JSCalendarRecurrenceRule{clone.RecurrenceRule}
			}
			if len(clone.ExcludedRecurrenceRules) > 0 && clone.ExcludedRecurrenceRule == nil {
				clone.ExcludedRecurrenceRule = clone.ExcludedRecurrenceRules[0]
			} else if clone.ExcludedRecurrenceRule != nil && len(clone.ExcludedRecurrenceRules) == 0 {
				clone.ExcludedRecurrenceRules = []*JSCalendarRecurrenceRule{clone.ExcludedRecurrenceRule}
			}
			filteredList = append(filteredList, &clone)
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

func handleCalendarEventSet(backend CalendarsBackend, mailBackend MailBackend, principalsBackend PrincipalsBackend, resolver AccountResolver) MethodHandler {
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
				cleanMap := sanitizeEventMap(resolvedMap)
				if err := validateCalendarEventMap(cleanMap); err != nil {
					return "", err
				}
				evBytes, _ := json.Marshal(cleanMap)
				var ev CalendarEvent
				_ = json.Unmarshal(evBytes, &ev)

				if ev.Type == "" {
					ev.Type = "Event"
				}
				if ev.TimeZone == "" {
					ev.TimeZone = "Etc/UTC"
				}
				if ev.Duration == "" {
					ev.Duration = "PT1H"
				}
				ev.Start = strings.TrimSuffix(ev.Start, "Z")

				createdEv, err := backend.CreateCalendarEvent(ctx, &ev)
				if err != nil {
					return "", err
				}
				if createdEv != nil {
					if createdEv.Type == "" {
						createdEv.Type = "Event"
					}
					if createdEv.TimeZone == "" {
						createdEv.TimeZone = "Etc/UTC"
					}
					if createdEv.UTCStart == "" {
						createdEv.UTCStart = computeUTCStart(createdEv.Start, createdEv.TimeZone)
					}
					if createdEv.UTCEnd == "" {
						createdEv.UTCEnd = computeUTCEnd(createdEv.Start, createdEv.Duration, createdEv.TimeZone)
					}
					createdEv.Start = strings.TrimSuffix(createdEv.Start, "Z")
				}
				created[creationID] = createdEv
				recordCreationRefs(ctx, creationRefs, creationID, createdEv.ID)

				// REQUEST to every participant except the calendar owner
				// (draft-ietf-jmap-calendars-27 Section 5.9.2.1).
				if sendSchedulingMessages && mailBackend != nil {
					orgEmail := organizerAddress(createdEv)
					if orgEmail == "" {
						if subj, ok := SubjectFromContext(ctx); ok && subj != "" {
							orgEmail = subj
						} else if subj, ok := SubjectForAccountID(accountID); ok && subj != "" {
							orgEmail = subj
						}
					}
					dispatchITIPRequests(ctx, mailBackend, backend, principalsBackend, resolver, createdEv, "Invitation: ", orgEmail)
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
				cleanPatch := sanitizeEventMap(rawPatch)
				patch := resolvePatchCreationRefs(cleanPatch, creationRefs)
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

					// A bare RSVP (participationStatus changed to non-needs-action) is a
					// REPLY to the organizer (Section 5.9.2.3); any other change is a
					// re-invitation REQUEST to the attendees (Section 5.9.2.1).
					if sendSchedulingMessages && mailBackend != nil && updatedEv != nil {
						orgEmail := organizerAddress(updatedEv)
						if orgEmail == "" {
							if subj, ok := SubjectFromContext(ctx); ok && subj != "" {
								orgEmail = subj
							} else if subj, ok := SubjectForAccountID(accountID); ok && subj != "" {
								orgEmail = subj
							}
						}
						if !dispatchITIPRepliesForPatch(ctx, mailBackend, backend, resolver, updatedEv, patch) {
							dispatchITIPRequests(ctx, mailBackend, backend, principalsBackend, resolver, updatedEv, "Updated Invitation: ", orgEmail)
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

						// CANCEL to every participant except the calendar owner when the
						// event is destroyed (draft-ietf-jmap-calendars-27 Section 5.9.2.2).
						if mailBackend != nil && len(events) > 0 && events[0] != nil {
							orgEmail := organizerAddress(events[0])
							if orgEmail == "" {
								if subj, ok := SubjectFromContext(ctx); ok && subj != "" {
									orgEmail = subj
								} else if subj, ok := SubjectForAccountID(accountID); ok && subj != "" {
									orgEmail = subj
								}
							}
							dispatchITIPCancels(ctx, mailBackend, backend, principalsBackend, resolver, events[0], orgEmail)
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
		if errType, errMsg := validateCalendarEventFilter(filter); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}
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

		expandRecurrences, _ := args["expandRecurrences"].(bool)
		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, calendarEventSortProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		// The "timeZone" argument (default Etc/UTC) interprets the before/after
		// LocalDateTime bounds (draft-ietf-jmap-calendars-27 Section 5.11). Thread it to
		// the backend matcher via an internal marker (validated client filter is untouched).
		if tz, ok := args["timeZone"].(string); ok && tz != "" {
			if filter == nil {
				filter = map[string]any{}
			}
			filter["__timeZone"] = tz
		}

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
		position = NormalizePosition(position, total)

		return "CalendarEvent/query", map[string]any{
			"accountId":  accountID,
			"queryState": backend.CalendarEventState(ctx),
			// When expandRecurrences is set the result ids are synthetic per-occurrence ids
			// (evtId#recurrenceId) that the change system does not track, so CalendarEvent/
			// queryChanges cannot compute deltas over them: report canCalculateChanges=false.
			"canCalculateChanges": !expandRecurrences,
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

// calendarEventFilterConditions are the CalendarEvent/query FilterCondition properties the
// server understands (draft-ietf-jmap-calendars Section 5.9). Any other condition property is
// rejected with unsupportedFilter rather than silently matching everything.
var calendarEventFilterConditions = map[string]bool{
	"inCalendar": true, "inCalendars": true, "title": true, "description": true,
	"location": true, "text": true, "after": true, "before": true, "uid": true,
	"owner": true, "attendee": true, "updatedBefore": true, "updatedAfter": true,
}

// calendarEventSortProperties are the CalendarEvent/query sort comparators the server supports:
// start/uid/recurrenceId are MUST, created/updated are SHOULD (draft-ietf-jmap-calendars
// Section 5.10); title is offered as an additional convenience.
var calendarEventSortProperties = map[string]bool{
	"start": true, "uid": true, "recurrenceId": true, "created": true, "updated": true, "title": true,
}

// validCalendarFilterOperators are the FilterOperator operators (RFC 8620 Section 5.5).
var validCalendarFilterOperators = map[string]bool{"AND": true, "OR": true, "NOT": true}

// validateCalendarEventFilter walks a CalendarEvent/query filter (a FilterCondition or a
// FilterOperator tree) and rejects any unknown condition property with unsupportedFilter, per
// the "No Fallthrough Match Defaults" rule. Returns ("","") when the filter is valid.
func validateCalendarEventFilter(filter map[string]any) (errType, errMsg string) {
	if filter == nil {
		return "", ""
	}
	if opVal, ok := filter["operator"]; ok {
		op, _ := opVal.(string)
		if !validCalendarFilterOperators[strings.ToUpper(op)] {
			return "unsupportedFilter", "unknown filter operator: " + op
		}
		conds, ok := filter["conditions"].([]any)
		if !ok || len(conds) == 0 {
			return "unsupportedFilter", "filter operator requires a non-empty conditions array"
		}
		for _, c := range conds {
			cm, ok := c.(map[string]any)
			if !ok {
				return "unsupportedFilter", "filter condition must be an object"
			}
			if et, em := validateCalendarEventFilter(cm); et != "" {
				return et, em
			}
		}
		return "", ""
	}
	for k := range filter {
		if !calendarEventFilterConditions[k] {
			return "unsupportedFilter", "unknown filter condition: " + k
		}
	}
	return "", ""
}

var validCalendarEventProperties = map[string]bool{
	"@type": true, "type": true, "id": true, "calendarIds": true, "calendarId": true, "calendar": true,
	"title": true, "summary": true, "description": true, "descriptionContentType": true, "showWithoutTime": true, "allDay": true,
	"start": true, "end": true, "utcStart": true, "utcEnd": true, "duration": true, "timeZone": true,
	"locations": true, "location": true, "virtualLocations": true, "links": true, "locale": true,
	"categories": true, "color": true, "status": true, "freeBusyStatus": true, "privacy": true,
	"hideAttendees": true, "priority": true, "replyTo": true, "sentBy": true, "requestStatus": true,
	"useDefaultAlerts": true, "localizations": true, "timeZones": true, "participants": true,
	"attendees": true, "organizer": true, "organizerCalendarAddress": true, "rrule": true,
	"recurrenceRule": true, "recurrenceRules": true, "recurrenceId": true, "recurrenceIdTimeZone": true,
	"excludedRecurrenceRule": true, "excludedRecurrenceRules": true, "recurrenceOverrides": true, "excluded": true, "alerts": true, "alarms": true, "reminders": true, "reminder": true,
	"relatedTo": true, "prodId": true, "sequence": true, "method": true, "due": true,
	"estimatedDuration": true, "percentComplete": true, "progress": true, "progressUpdated": true,
	"entries": true, "source": true, "created": true, "updated": true, "uid": true, "keywords": true,
	"isDraft": true, "isOrigin": true, "mayInviteSelf": true, "mayInviteOthers": true, "blobId": true, "baseObjectId": true, "comments": true, "contact": true,
	"features": true, "attachments": true,
}

func sanitizeEventMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cleaned := make(map[string]any, len(m))
	for k, v := range m {
		cleanKey := strings.TrimPrefix(k, "/")
		cleaned[cleanKey] = v
	}

	// 1. calendarId / calendar -> calendarIds
	if cid, ok := cleaned["calendarId"].(string); ok && cid != "" {
		if cids, hasCids := cleaned["calendarIds"].(map[string]any); !hasCids || len(cids) == 0 {
			cleaned["calendarIds"] = map[string]bool{cid: true}
		}
	} else if cid, ok := cleaned["calendar"].(string); ok && cid != "" {
		if cids, hasCids := cleaned["calendarIds"].(map[string]any); !hasCids || len(cids) == 0 {
			cleaned["calendarIds"] = map[string]bool{cid: true}
		}
	}

	// 2. allDay -> showWithoutTime
	if allDay, ok := cleaned["allDay"].(bool); ok {
		cleaned["showWithoutTime"] = allDay
	}

	// 3. summary -> title
	if title, ok := cleaned["title"].(string); !ok || title == "" {
		if sum, okS := cleaned["summary"].(string); okS && sum != "" {
			cleaned["title"] = sum
		}
	}

	// 4. end / utcEnd -> duration
	if dur, ok := cleaned["duration"].(string); !ok || dur == "" {
		start, _ := cleaned["start"].(string)
		end, _ := cleaned["end"].(string)
		if end == "" {
			end, _ = cleaned["utcEnd"].(string)
		}
		if start != "" && end != "" {
			cleaned["duration"] = icalDurationBetween(start, end)
		}
	}

	// 5. Default timeZone to Etc/UTC if empty
	if tz, ok := cleaned["timeZone"].(string); !ok || tz == "" {
		cleaned["timeZone"] = "Etc/UTC"
	}

	// 6. location string -> locations map
	if locStr, ok := cleaned["location"].(string); ok && locStr != "" {
		if locs, hasLocs := cleaned["locations"].(map[string]any); !hasLocs || len(locs) == 0 {
			cleaned["locations"] = map[string]any{
				"loc-1": map[string]any{
					"@type": "Location",
					"name":  locStr,
				},
			}
		}
	}

	// 7. recurrenceRule -> recurrenceRules
	if rrule, hasRrule := cleaned["recurrenceRule"]; hasRrule {
		if rrule == nil {
			cleaned["recurrenceRules"] = nil
		} else if rruleMap, ok := rrule.(map[string]any); ok {
			cleaned["recurrenceRules"] = []any{rruleMap}
		}
	}
	if exrule, hasExrule := cleaned["excludedRecurrenceRule"]; hasExrule {
		if exrule == nil {
			cleaned["excludedRecurrenceRules"] = nil
		} else if exruleMap, ok := exrule.(map[string]any); ok {
			cleaned["excludedRecurrenceRules"] = []any{exruleMap}
		}
	}

	return cleaned
}

func validateCalendarEventMap(m map[string]any) error {
	for k, v := range m {
		baseKey := strings.TrimPrefix(k, "/")
		if strings.Contains(baseKey, "/") {
			baseKey = strings.Split(baseKey, "/")[0]
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
			// "status" is an Event property (RFC 8984 Section 4.4.2); its only valid values
			// are confirmed/tentative/cancelled. JSCalendar Tasks track state via "progress"
			// (Section 5.2.5), not "status", so the Task states must not be accepted here.
			if s, ok := v.(string); ok && s != "" {
				switch strings.ToLower(s) {
				case "confirmed", "tentative", "cancelled", "canceled":
				default:
					return SetError{Type: "invalidProperties", Description: "invalid status value: " + s, Properties: []string{k}}
				}
			}
		case "privacy":
			if s, ok := v.(string); ok && s != "" {
				switch strings.ToLower(s) {
				case "public", "private", "secret", "confidential":
				default:
					return SetError{Type: "invalidProperties", Description: "invalid privacy value: " + s, Properties: []string{k}}
				}
			}
		case "freeBusyStatus":
			if s, ok := v.(string); ok && s != "" {
				switch strings.ToLower(s) {
				case "free", "busy", "tentative", "opaque", "transparent":
				default:
					return SetError{Type: "invalidProperties", Description: "invalid freeBusyStatus value: " + s, Properties: []string{k}}
				}
			}
		case "progress":
			// JSCalendar Task progress (RFC 8984 Section 5.2.5).
			if s, ok := v.(string); ok && s != "" {
				switch strings.ToLower(s) {
				case "needs-action", "in-process", "completed", "failed", "pending", "cancelled", "canceled":
				default:
					return SetError{Type: "invalidProperties", Description: "invalid progress value: " + s, Properties: []string{k}}
				}
			}
		}
	}
	return nil
}
