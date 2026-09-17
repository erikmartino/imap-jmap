package jmapcalendar

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcopy"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmapprincipals"
)

func handleCalendarEventGet(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*CalendarEvent
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			if len(idsRaw) == 0 {
				list = []*CalendarEvent{}
				notFound = []jmapcore.Id{}
				_ = backend.CalendarEventState(ctx)
			} else {
				ids := make([]jmapcore.Id, 0, len(idsRaw))
				for _, item := range idsRaw {
					if idStr, ok := item.(string); ok {
						ids = append(ids, jmapcore.Id(idStr))
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
			notFound = []jmapcore.Id{}
		}

		// Privacy (draft-ietf-jmap-calendars-27 Section 4.2.10) governs what NON-owner
		// sharees see: "private" returns only a reduced property set and "secret" makes the
		// server behave as though the event does not exist. The Principal that owns the
		// calendar always sees the full event, including its private and secret events.
		// CalendarEvent/get only ever runs against the caller's own account (SelfAccessGuard),
		// i.e. the owner, so no censoring is applied here. Cross-principal disclosure is
		// limited to the free-busy windows returned by Principal/getAvailability, which never
		// expose event titles or details.
		recBeforeStr, _ := args["recurrenceOverridesBefore"].(string)
		recAfterStr, _ := args["recurrenceOverridesAfter"].(string)
		reduceParticipants, _ := args["reduceParticipants"].(bool)

		accountUser := accountID
		if subj, ok := SubjectFromContext(ctx); ok && subj != "" {
			accountUser = subj
		}

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

			// isOrigin: true if caller is organizer or no organizer specified, false otherwise.
			if clone.OrganizerCalendarAddress == "" || strings.EqualFold(clone.OrganizerCalendarAddress, "mailto:"+accountUser) {
				clone.IsOrigin = true
			} else {
				clone.IsOrigin = false
			}

			// useDefaultAlerts: populate alerts from calendar if true and alerts empty
			if clone.UseDefaultAlerts {
				if len(clone.Alerts) == 0 {
					for cid := range clone.CalendarIDs {
						if cals, _, err := backend.GetCalendars(ctx, []jmapcore.Id{cid}); err == nil && len(cals) > 0 {
							cal := cals[0]
							var defAlerts map[string]*JSCalendarAlert
							if clone.ShowWithoutTime {
								defAlerts = cal.DefaultAlertsWithoutTime
							} else {
								defAlerts = cal.DefaultAlertsWithTime
							}
							if len(defAlerts) > 0 {
								clone.Alerts = make(map[string]*JSCalendarAlert, len(defAlerts))
								for k, a := range defAlerts {
									if a != nil {
										aCopy := *a
										if aCopy.Type == "" {
											aCopy.Type = "Alert"
										}
										if aCopy.Trigger != nil {
											if tm, ok := aCopy.Trigger.(map[string]any); ok {
												tmCopy := make(map[string]any, len(tm)+1)
												for tk, tv := range tm {
													tmCopy[tk] = tv
												}
												if tmCopy["@type"] == nil && tmCopy["offset"] != nil {
													tmCopy["@type"] = "OffsetTrigger"
												}
												aCopy.Trigger = tmCopy
											}
										}
										clone.Alerts[k] = &aCopy
									}
								}
								break
							}
						}
					}
				}
			}

			// Filter recurrenceOverrides
			if (recBeforeStr != "" || recAfterStr != "") && len(clone.RecurrenceOverrides) > 0 {
				loc := loadLocation(clone.TimeZone)
				filteredOverrides := make(map[string]map[string]any)
				var beforeT, afterT time.Time
				var hasBefore, hasAfter bool
				if recBeforeStr != "" {
					beforeT, hasBefore = parseRFC3339(recBeforeStr)
				}
				if recAfterStr != "" {
					afterT, hasAfter = parseRFC3339(recAfterStr)
				}
				for recKey, ov := range clone.RecurrenceOverrides {
					if t, ok := parseLocalDateTimeBound(recKey, loc); ok {
						tUTC := t.UTC()
						if hasBefore && !tUTC.Before(beforeT) {
							continue
						}
						if hasAfter && tUTC.Before(afterT) {
							continue
						}
						filteredOverrides[recKey] = ov
					}
				}
				clone.RecurrenceOverrides = filteredOverrides
			}

			// Reduce participants
			if reduceParticipants && len(clone.Participants) > 0 {
				reduced := make(map[string]*JSCalendarParticipant)
				for pid, p := range clone.Participants {
					if p != nil {
						isOwner := p.Role == "owner" || (p.Roles != nil && p.Roles["owner"])
						isUser := strings.EqualFold(p.CalendarAddress, "mailto:"+accountUser)
						if isOwner || isUser {
							reduced[pid] = p
						}
					}
				}
				clone.Participants = reduced
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

func handleCalendarEventChanges(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.CalendarEventChanges(ctx, sinceState)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
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

func handleCalendarEventSet(backend CalendarsBackend, mailBackend jmapmail.MailBackend, principalsBackend jmapprincipals.PrincipalsBackend, resolver jmapauth.AccountResolver) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.CalendarEventState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		sendSchedulingMessages, _ := args["sendSchedulingMessages"].(bool)

		created := make(map[string]*CalendarEvent)
		updated := make(map[string]map[string]any)
		destroyed := make([]jmapcore.Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		creationRefs := newSetCreationRefs(ctx)
		calCap := CalendarsCapabilityFromContext(ctx)

		callerAccountID, hasCaller := PrincipalAccountIDFromContext(ctx)
		targetAccountID, _ := AccountIDFromContext(ctx)
		isSharedCaller := hasCaller && callerAccountID != "" && callerAccountID != targetAccountID

		if createRaw, ok := args["create"].(map[string]any); ok {
			notCreated = runCreateLoop(createRaw, creationRefs, func(creationID string, resolvedMap map[string]any) (string, error) {
				if sendSchedulingMessages && mailBackend == nil {
					return "", jmapcore.SetError{Type: "noSupportedScheduleMethods", Description: "no supported schedule methods available for scheduling"}
				}
				cleanMap := sanitizeEventMap(resolvedMap)
				if err := validateCalendarEventMap(cleanMap, calCap); err != nil {
					return "", err
				}
				evBytes, _ := json.Marshal(cleanMap)
				var ev CalendarEvent
				_ = json.Unmarshal(evBytes, &ev)

				if isSharedCaller {
					for cid := range ev.CalendarIDs {
						cals, _, _ := backend.GetCalendars(ctx, []jmapcore.Id{cid})
						if len(cals) == 0 || !cals[0].MyRights.MayWriteAll {
							return "", jmapcore.SetError{
								Type:        "forbidden",
								Description: "You are not allowed to create calendar events.",
							}
						}
					}
				}

				if ev.Type == "" {
					ev.Type = "Event"
				}
				if ev.TimeZone == "" && ev.Type == "Event" {
					ev.TimeZone = "Etc/UTC"
				}
				if ev.Duration == "" && ev.Type == "Event" {
					ev.Duration = "PT1H"
				}
				if ev.UID != "" {
					existing, _, _ := backend.GetCalendarEvents(ctx, nil)
					for _, ex := range existing {
						if ex != nil && ex.UID == ev.UID {
							return "", jmapcore.SetError{
								Type:        "invalidProperties",
								Description: fmt.Sprintf("An event with UID %s already exists.", ev.UID),
								Properties:  []string{"uid"},
							}
						}
					}
				}

				if len(ev.Participants) > 0 && ev.OrganizerCalendarAddress == "" {
					accountUser := accountID
					if subj, ok := SubjectFromContext(ctx); ok && subj != "" {
						accountUser = subj
					}
					for _, p := range ev.Participants {
						if p != nil && strings.EqualFold(p.CalendarAddress, "mailto:"+accountUser) {
							ev.OrganizerCalendarAddress = p.CalendarAddress
							break
						}
					}
					if ev.OrganizerCalendarAddress == "" {
						for _, p := range ev.Participants {
							if p != nil && (p.Role == "owner" || (p.Roles != nil && p.Roles["owner"])) {
								ev.OrganizerCalendarAddress = p.CalendarAddress
								break
							}
						}
					}
					if ev.OrganizerCalendarAddress == "" && accountUser != "" {
						ev.OrganizerCalendarAddress = "mailto:" + accountUser
					}
				}

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
					org := organizerAddress(createdEv)
					caller := accountID
					if subj, ok := SubjectFromContext(ctx); ok && subj != "" {
						caller = subj
					} else if subj, ok := SubjectForAccountID(accountID); ok && subj != "" {
						caller = subj
					}
					if org != "" && caller != "" && !strings.EqualFold(org, caller) && !strings.EqualFold(org, "mailto:"+caller) {
						_, _ = backend.CreateCalendarEventNotification(ctx, &CalendarEventNotification{
							Type:            "created",
							CalendarEventID: createdEv.ID,
							ChangedBy:       notificationChangedBy(createdEv),
							Event:           createdEv,
						})
					}
				}
				return string(createdEv.ID), nil
			})
		}

		destroySet := make(map[string]bool)
		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if s, ok := item.(string); ok {
					destroySet[s] = true
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			hasBase := make(map[string]bool)
			hasInst := make(map[string]bool)
			for idStr := range updateRaw {
				if strings.Contains(idStr, "#") {
					parts := strings.SplitN(idStr, "#", 2)
					hasInst[parts[0]] = true
				} else {
					hasBase[idStr] = true
				}
			}
			for baseID := range hasBase {
				if hasInst[baseID] {
					conflictErr := jmapcore.SetError{
						Type:        "invalidProperties",
						Description: "A base event and its instances cannot be modified in the same request.",
						Properties:  []string{"id"},
					}
					notUpdated[baseID] = conflictErr
					for idStr := range updateRaw {
						if strings.HasPrefix(idStr, baseID+"#") {
							notUpdated[idStr] = conflictErr
						}
					}
				}
			}

			for idStr, patchRaw := range updateRaw {
				resolvedID := resolveCreationID(idStr, creationRefs)
				if notUpdated[string(resolvedID)] != nil {
					continue
				}
				if isSharedCaller {
					baseLookupID := string(resolvedID)
					if strings.Contains(baseLookupID, "#") {
						baseLookupID = strings.SplitN(baseLookupID, "#", 2)[0]
					}
					events, _, _ := backend.GetCalendarEvents(ctx, []jmapcore.Id{jmapcore.Id(baseLookupID)})
					if len(events) > 0 && events[0] != nil {
						allowed := true
						for cid := range events[0].CalendarIDs {
							cals, _, _ := backend.GetCalendars(ctx, []jmapcore.Id{cid})
							if len(cals) == 0 || !cals[0].MyRights.MayWriteAll {
								allowed = false
								break
							}
						}
						if !allowed {
							notUpdated[string(resolvedID)] = jmapcore.SetError{
								Type:        "forbidden",
								Description: "You are not allowed to modify calendar events.",
							}
							continue
						}
					}
				}
				rawPatch, _ := patchRaw.(map[string]any)

				if strings.Contains(idStr, "#") {
					parts := strings.SplitN(idStr, "#", 2)
					if destroySet[parts[0]] {
						notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "willDestroy"}
						continue
					}
					var foundBadProp string
					for _, badProp := range []string{"calendarIds", "isDraft", "utcStart", "utcEnd", "mayInviteSelf", "useDefaultAlerts"} {
						if _, has := rawPatch[badProp]; has {
							foundBadProp = badProp
							break
						}
					}
					if foundBadProp != "" {
						notUpdated[string(resolvedID)] = jmapcore.SetError{
							Type:        "invalidProperties",
							Description: "This property cannot be modified on a single occurrence.",
							Properties:  []string{foundBadProp},
						}
						continue
					}
				}

				cleanPatch := sanitizeEventPatch(rawPatch)
				patch := resolvePatchCreationRefs(cleanPatch, creationRefs)
				if sendSchedulingMessages && mailBackend == nil {
					notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "noSupportedScheduleMethods", Description: "no supported schedule methods available for scheduling"}
					continue
				}
				if err := validateCalendarEventMap(patch, calCap); err != nil {
					if setErr, isSetErr := err.(jmapcore.SetError); isSetErr {
						notUpdated[string(resolvedID)] = setErr
					} else {
						notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
					}
					continue
				}
				var beforeEv *CalendarEvent
				if sendSchedulingMessages {
					beforeList, _, _ := backend.GetCalendarEvents(ctx, []jmapcore.Id{jmapcore.Id(resolvedID)})
					if len(beforeList) > 0 {
						// Deep-copy: the memory backend returns its stored pointer and the
						// update below mutates it in place; the notification must carry the
						// pre-change data.
						beforeBytes, _ := json.Marshal(beforeList[0])
						beforeEv = &CalendarEvent{}
						_ = json.Unmarshal(beforeBytes, beforeEv)
					}
				}
				updatedEv, err := backend.UpdateCalendarEvent(ctx, jmapcore.Id(resolvedID), patch)
				if err != nil {
					notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: err.Error()}
				} else {
					updated[string(resolvedID)] = nil

					// Record the scheduling change as a CalendarEventNotification: the
					// "event" carries the data before the change and "eventPatch" encodes
					// the change itself (Section 7.2). A user does not receive notifications
					// for their own actions.
					if sendSchedulingMessages {
						caller := accountID
						if subj, ok := SubjectFromContext(ctx); ok && subj != "" {
							caller = subj
						} else if subj, ok := SubjectForAccountID(accountID); ok && subj != "" {
							caller = subj
						}
						org := organizerAddress(updatedEv)
						isRSVP := false
						for path := range patch {
							if strings.HasPrefix(path, "participants/") && (strings.HasSuffix(path, "/participationStatus") || strings.HasSuffix(path, "/status")) {
								isRSVP = true
								break
							}
						}
						if !isRSVP && org != "" && caller != "" && !strings.EqualFold(org, caller) && !strings.EqualFold(org, "mailto:"+caller) {
							_, _ = backend.CreateCalendarEventNotification(ctx, &CalendarEventNotification{
								Type:            "updated",
								CalendarEventID: updatedEv.ID,
								ChangedBy:       notificationChangedBy(updatedEv),
								Event:           beforeEv,
								EventPatch:      patch,
							})
						}
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
					evID := jmapcore.Id(resolveCreationID(idStr, creationRefs))
					events, _, _ := backend.GetCalendarEvents(ctx, []jmapcore.Id{evID})
					if isSharedCaller {
						allowed := true
						if len(events) > 0 && events[0] != nil {
							for cid := range events[0].CalendarIDs {
								cals, _, _ := backend.GetCalendars(ctx, []jmapcore.Id{cid})
								if len(cals) == 0 || !cals[0].MyRights.MayDelete {
									allowed = false
									break
								}
							}
						}
						if !allowed {
							notDestroyed[string(evID)] = jmapcore.SetError{
								Type:        "forbidden",
								Description: "You are not allowed to remove events from calendar",
							}
							continue
						}
					}

					okDel, err := backend.DeleteCalendarEvent(ctx, evID)
					if err != nil || !okDel {
						notDestroyed[string(evID)] = jmapcore.SetError{Type: "notFound", Description: "calendar event not found"}
					} else {
						destroyed = append(destroyed, evID)

						// Record the cancellation as a CalendarEventNotification carrying the
						// pre-destroy event data (Section 7.2) if not performed by the organizer.
						if sendSchedulingMessages && len(events) > 0 && events[0] != nil {
							caller := accountID
							if subj, ok := SubjectFromContext(ctx); ok && subj != "" {
								caller = subj
							} else if subj, ok := SubjectForAccountID(accountID); ok && subj != "" {
								caller = subj
							}
							org := organizerAddress(events[0])
							if org != "" && caller != "" && !strings.EqualFold(org, caller) && !strings.EqualFold(org, "mailto:"+caller) {
								_, _ = backend.CreateCalendarEventNotification(ctx, &CalendarEventNotification{
									Type:            "destroyed",
									CalendarEventID: evID,
									ChangedBy:       notificationChangedBy(events[0]),
									Event:           events[0],
								})
							}
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

func handleCalendarEventQuery(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		calCap := CalendarsCapabilityFromContext(ctx)
		tz, _ := args["timeZone"].(string)
		if tz == "" {
			tz = "Etc/UTC"
		}
		loc := loadLocation(tz)

		filter, _ := args["filter"].(map[string]any)
		if errType, errMsg := validateCalendarEventFilter(filter, calCap, loc); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		expandRecurrences, _ := args["expandRecurrences"].(bool)
		if expandRecurrences {
			if durErr := validateExpandDuration(filter, calCap, loc); durErr != "" {
				return "error", MethodErrorArgs("expandDurationTooLarge", durErr)
			}
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

		var ids []jmapcore.Id
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
			ids = []jmapcore.Id{}
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

func handleCalendarEventQueryChanges(backend CalendarsBackend) jmaphandler.MethodHandler {
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
func handleCalendarEventCopy(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, fromAccountID := jmapcopy.ResolveCopyAccountIDs(args)
		srcCtx := SourceAccountContext(ctx, args)

		oldState, errInv := jmapcopy.ValidateCopyStates(ctx, srcCtx, args, backend.CalendarEventState, backend.CalendarEventState)
		if errInv != nil {
			return errInv.Name, errInv.Args
		}

		onSuccessDestroyOriginal, _ := args["onSuccessDestroyOriginal"].(bool)

		created := make(map[string]*CalendarEvent)
		notCreated := make(map[string]any)
		destroyOriginals := make([]jmapcore.Id, 0)
		creationRefs := newSetCreationRefs(ctx)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, raw := range createRaw {
				m, _ := raw.(map[string]any)
				srcID, _ := m["id"].(string)
				if srcID == "" {
					srcID = creationID
				}
				if srcID == "" {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: "copy create entry must reference a source id"}
					continue
				}
				resolvedSrcID := resolveCreationID(srcID, creationRefs)
				srcs, notFound, _ := backend.GetCalendarEvents(srcCtx, []jmapcore.Id{jmapcore.Id(resolvedSrcID)})
				if len(srcs) == 0 || len(notFound) > 0 {
					notCreated[creationID] = jmapcore.SetError{Type: "notFound", Description: "source event not found: " + srcID}
					continue
				}

				merged := mergeCopyOverrides(srcs[0], m)
				evBytes, _ := json.Marshal(merged)
				var ev CalendarEvent
				_ = json.Unmarshal(evBytes, &ev)
				ev.ID = ""

				newEv, err := backend.CreateCalendarEvent(ctx, &ev)
				if err != nil {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = newEv
					recordCreationRefs(ctx, creationRefs, creationID, newEv.ID)
					destroyOriginals = append(destroyOriginals, jmapcore.Id(resolvedSrcID))
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
			"created":       nilIfEmpty(created),
			"notCreated":    nilIfEmpty(notCreated),
		}
	}
}

// handleCalendarEventParse implements CalendarEvent/parse per draft-ietf-jmap-calendars
// Section 5.12: the client supplies blob ids of iCalendar files and the server returns the
// parsed JSCalendar CalendarEvent objects. Support is advertised via the
// "urn:ietf:params:jmap:calendars:parse" capability.
func handleCalendarEventParse(backend CalendarsBackend, blobBackend jmapblob.BlobBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		props := parseProperties(args)
		creationRefs := newSetCreationRefs(ctx)

		parsed := make(map[string][]any)
		var notFound []jmapcore.Id
		var notParsable []jmapcore.Id

		blobIDsRaw, _ := args["blobIds"].([]any)
		for _, item := range blobIDsRaw {
			idStr, ok := item.(string)
			if !ok || idStr == "" {
				continue
			}
			blobID := jmapcore.Id(resolveCreationID(idStr, creationRefs))
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
// the "No Fallthrough Match Defaults" rule. It also validates date bounds against minDateTime
// and maxDateTime capability limits. Returns ("","") when the filter is valid.
func validateCalendarEventFilter(filter map[string]any, calCap CalendarsCapability, loc *time.Location) (errType, errMsg string) {
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
			if et, em := validateCalendarEventFilter(cm, calCap, loc); et != "" {
				return et, em
			}
		}
		return "", ""
	}
	for k, v := range filter {
		if !calendarEventFilterConditions[k] {
			return "unsupportedFilter", "unknown filter condition: " + k
		}
		switch k {
		case "before", "after", "updatedBefore", "updatedAfter":
			if s, ok := v.(string); ok && s != "" {
				t, ok := parseLocalDateTimeBound(s, loc)
				if !ok {
					return MethodErrorInvalidArguments, fmt.Sprintf("invalid date format for %s: %s", k, s)
				}
				if calCap.MinDateTime != "" {
					if minT, okMin := parseLocalDateTimeBound(calCap.MinDateTime, time.UTC); okMin {
						if t.Before(minT) {
							return MethodErrorInvalidArguments, fmt.Sprintf("%s date (%s) is earlier than minDateTime (%s)", k, s, calCap.MinDateTime)
						}
					}
				}
				if calCap.MaxDateTime != "" {
					if maxT, okMax := parseLocalDateTimeBound(calCap.MaxDateTime, time.UTC); okMax {
						if t.After(maxT) {
							return MethodErrorInvalidArguments, fmt.Sprintf("%s date (%s) is later than maxDateTime (%s)", k, s, calCap.MaxDateTime)
						}
					}
				}
			}
		}
	}
	return "", ""
}

func validateExpandDuration(filter map[string]any, calCap CalendarsCapability, loc *time.Location) string {
	if calCap.MaxExpandedQueryDuration == "" {
		return ""
	}
	maxDur, ok := ParseISODuration(calCap.MaxExpandedQueryDuration)
	if !ok || maxDur <= 0 {
		return ""
	}
	beforeStr, afterStr := extractFilterBounds(filter)
	if beforeStr == "" || afterStr == "" {
		return ""
	}
	beforeT, okB := parseLocalDateTimeBound(beforeStr, loc)
	afterT, okA := parseLocalDateTimeBound(afterStr, loc)
	if !okB || !okA {
		return ""
	}
	diff := beforeT.Sub(afterT)
	if diff > maxDur {
		return fmt.Sprintf("duration between before (%s) and after (%s) is %v, which exceeds maxExpandedQueryDuration (%s)", beforeStr, afterStr, diff, calCap.MaxExpandedQueryDuration)
	}
	return ""
}

func extractFilterBounds(filter map[string]any) (before, after string) {
	if filter == nil {
		return "", ""
	}
	if b, ok := filter["before"].(string); ok {
		before = b
	}
	if a, ok := filter["after"].(string); ok {
		after = a
	}
	if conds, ok := filter["conditions"].([]any); ok {
		for _, c := range conds {
			if cm, ok := c.(map[string]any); ok {
				cb, ca := extractFilterBounds(cm)
				if before == "" {
					before = cb
				}
				if after == "" {
					after = ca
				}
			}
		}
	}
	return before, after
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

func sanitizeEventPatch(m map[string]any) map[string]any {
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

	// 3. summary -> title (only if summary was passed and title wasn't)
	if sum, okS := cleaned["summary"].(string); okS && sum != "" {
		if title, ok := cleaned["title"].(string); !ok || title == "" {
			cleaned["title"] = sum
		}
	}

	// 4. location string -> locations map
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

	// 5. recurrenceRule -> recurrenceRules
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

func validateCalendarEventMap(m map[string]any, calCap CalendarsCapability) error {
	if rawCids, hasCids := m["calendarIds"]; hasCids {
		if cidsMap, ok := rawCids.(map[string]any); ok && len(cidsMap) == 0 {
			return jmapcore.SetError{
				Type:        "invalidProperties",
				Description: "Event has to belong to at least one calendar.",
				Properties:  []string{"calendarIds"},
			}
		}
	}
	for k, v := range m {
		baseKey := strings.TrimPrefix(k, "/")
		if strings.Contains(baseKey, "/") {
			baseKey = strings.Split(baseKey, "/")[0]
		}
		if !validCalendarEventProperties[baseKey] {
			return jmapcore.SetError{
				Type:        "invalidProperties",
				Description: "unknown property: " + k,
				Properties:  []string{k},
			}
		}
		switch baseKey {
		case "start":
			if s, ok := v.(string); ok && s != "" {
				t, okT := parseLocalDateTimeBound(s, time.UTC)
				if !okT {
					return jmapcore.SetError{Type: "invalidProperties", Description: "invalid start date format: " + s, Properties: []string{k}}
				}
				if calCap.MinDateTime != "" {
					if minT, okMin := parseLocalDateTimeBound(calCap.MinDateTime, time.UTC); okMin && t.Before(minT) {
						return jmapcore.SetError{Type: "invalidProperties", Description: fmt.Sprintf("start date (%s) is earlier than minDateTime (%s)", s, calCap.MinDateTime), Properties: []string{k}}
					}
				}
				if calCap.MaxDateTime != "" {
					if maxT, okMax := parseLocalDateTimeBound(calCap.MaxDateTime, time.UTC); okMax && t.After(maxT) {
						return jmapcore.SetError{Type: "invalidProperties", Description: fmt.Sprintf("start date (%s) is later than maxDateTime (%s)", s, calCap.MaxDateTime), Properties: []string{k}}
					}
				}
			}
		case "recurrenceRules":
			if rules, ok := v.([]any); ok {
				for _, r := range rules {
					if rm, ok := r.(map[string]any); ok {
						if until, ok := rm["until"].(string); ok && until != "" {
							t, okT := parseLocalDateTimeBound(until, time.UTC)
							if okT {
								if calCap.MinDateTime != "" {
									if minT, okMin := parseLocalDateTimeBound(calCap.MinDateTime, time.UTC); okMin && t.Before(minT) {
										return jmapcore.SetError{Type: "invalidProperties", Description: fmt.Sprintf("recurrence rule until date (%s) is earlier than minDateTime (%s)", until, calCap.MinDateTime), Properties: []string{k}}
									}
								}
								if calCap.MaxDateTime != "" {
									if maxT, okMax := parseLocalDateTimeBound(calCap.MaxDateTime, time.UTC); okMax && t.After(maxT) {
										return jmapcore.SetError{Type: "invalidProperties", Description: fmt.Sprintf("recurrence rule until date (%s) is later than maxDateTime (%s)", until, calCap.MaxDateTime), Properties: []string{k}}
									}
								}
							}
						}
					}
				}
			}
		case "recurrenceOverrides":
			if overrides, ok := v.(map[string]any); ok {
				for recID := range overrides {
					t, okT := parseLocalDateTimeBound(recID, time.UTC)
					if okT {
						if calCap.MinDateTime != "" {
							if minT, okMin := parseLocalDateTimeBound(calCap.MinDateTime, time.UTC); okMin && t.Before(minT) {
								return jmapcore.SetError{Type: "invalidProperties", Description: fmt.Sprintf("recurrence override date (%s) is earlier than minDateTime (%s)", recID, calCap.MinDateTime), Properties: []string{k}}
							}
						}
						if calCap.MaxDateTime != "" {
							if maxT, okMax := parseLocalDateTimeBound(calCap.MaxDateTime, time.UTC); okMax && t.After(maxT) {
								return jmapcore.SetError{Type: "invalidProperties", Description: fmt.Sprintf("recurrence override date (%s) is later than maxDateTime (%s)", recID, calCap.MaxDateTime), Properties: []string{k}}
							}
						}
					}
				}
			}
		case "status":
			// "status" is an Event property (RFC 8984 Section 4.4.2); its only valid values
			// are confirmed/tentative/cancelled. JSCalendar Tasks track state via "progress"
			// (Section 5.2.5), not "status", so the Task states must not be accepted here.
			if s, ok := v.(string); ok && s != "" {
				switch strings.ToLower(s) {
				case "confirmed", "tentative", "cancelled", "canceled":
				default:
					return jmapcore.SetError{Type: "invalidProperties", Description: "invalid status value: " + s, Properties: []string{k}}
				}
			}
		case "privacy":
			if s, ok := v.(string); ok && s != "" {
				switch strings.ToLower(s) {
				case "public", "private", "secret", "confidential":
				default:
					return jmapcore.SetError{Type: "invalidProperties", Description: "invalid privacy value: " + s, Properties: []string{k}}
				}
			}
		case "freeBusyStatus":
			if s, ok := v.(string); ok && s != "" {
				switch strings.ToLower(s) {
				case "free", "busy", "tentative", "opaque", "transparent":
				default:
					return jmapcore.SetError{Type: "invalidProperties", Description: "invalid freeBusyStatus value: " + s, Properties: []string{k}}
				}
			}
		case "progress":
			// JSCalendar Task progress (RFC 8984 Section 5.2.5).
			if s, ok := v.(string); ok && s != "" {
				switch strings.ToLower(s) {
				case "needs-action", "in-process", "completed", "failed", "pending", "cancelled", "canceled":
				default:
					return jmapcore.SetError{Type: "invalidProperties", Description: "invalid progress value: " + s, Properties: []string{k}}
				}
			}
		}
	}
	return nil
}
