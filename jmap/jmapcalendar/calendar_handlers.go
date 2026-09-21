package jmapcalendar

import (
	"context"
	"encoding/json"
	"strings"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcopy"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmapprincipals"
)

// RegisterCalendarHandlers registers JMAP for Calendars & JSCalendar method handlers into MethodRegistry.
func RegisterCalendarHandlers(r *jmaphandler.MethodRegistry, backend CalendarsBackend, mailBackend jmapmail.MailBackend, principalsBackend jmapprincipals.PrincipalsBackend, blobBackend jmapblob.BlobBackend, resolver jmapauth.AccountResolver) {
	if backend == nil {
		return
	}
	r.Register("Calendar/get", handleCalendarGet(backend))
	r.Register("Calendar/changes", handleCalendarChanges(backend))
	r.Register("Calendar/set", handleCalendarSet(backend))
	r.Register("Calendar/copy", handleCalendarCopy(backend))

	r.Register("CalendarEvent/get", handleCalendarEventGet(backend))
	r.Register("CalendarEvent/changes", handleCalendarEventChanges(backend))
	r.Register("CalendarEvent/set", handleCalendarEventSet(backend, mailBackend, principalsBackend, resolver))
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

	// ShareNotification (RFC 9670)
	r.Register("ShareNotification/get", handleShareNotificationGet(backend))
	r.Register("ShareNotification/changes", handleShareNotificationChanges(backend))
	r.Register("ShareNotification/set", handleShareNotificationSet(backend))
	r.Register("ShareNotification/query", handleShareNotificationQuery(backend))
	r.Register("ShareNotification/queryChanges", handleShareNotificationQueryChanges(backend))
}

func handleCalendarGet(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*Calendar
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			if len(idsRaw) == 0 {
				list = []*Calendar{}
				notFound = []jmapcore.Id{}
				_ = backend.CalendarState(ctx)
			} else {
				ids := make([]jmapcore.Id, 0, len(idsRaw))
				for _, item := range idsRaw {
					if idStr, ok := item.(string); ok {
						ids = append(ids, jmapcore.Id(idStr))
					}
				}
				list, notFound, err = backend.GetCalendars(ctx, ids)
			}
		} else {
			list, err = backend.GetAllCalendars(ctx)
		}

		if err != nil || list == nil {
			list = []*Calendar{}
		}
		if notFound == nil {
			notFound = []jmapcore.Id{}
		}

		return "Calendar/get", map[string]any{
			"accountId": accountID,
			"state":     backend.CalendarState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleCalendarChanges(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		created, updated, destroyed, newState, hasMore := backend.CalendarChanges(ctx, sinceState)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
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

func handleCalendarSet(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.CalendarState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		created := make(map[string]*Calendar)
		updated := make(map[string]map[string]any)
		destroyed := make([]jmapcore.Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)
		creationRefs := newSetCreationRefs(ctx)

		onDestroyRemoveEvents, _ := args["onDestroyRemoveEvents"].(bool)

		callerAccountID, hasCaller := PrincipalAccountIDFromContext(ctx)
		targetAccountID, _ := AccountIDFromContext(ctx)
		isSharedCaller := hasCaller && callerAccountID != "" && callerAccountID != targetAccountID

		if createRaw, ok := args["create"].(map[string]any); ok {
			for creationID, calMapRaw := range createRaw {
				if isSharedCaller {
					notCreated[creationID] = jmapcore.SetError{
						Type:        "forbidden",
						Description: "Cannot create calendars in a shared account.",
					}
					continue
				}
				calMap, _ := calMapRaw.(map[string]any)
				if _, hasIsDefault := calMap["isDefault"]; hasIsDefault {
					notCreated[creationID] = jmapcore.SetError{
						Type:        "invalidProperties",
						Description: "isDefault is server-set and cannot be set directly",
						Properties:  []string{"isDefault"},
					}
					continue
				}
				if err := validateCalendarMap(calMap); err != nil {
					notCreated[creationID] = err
					continue
				}
				calBytes, _ := json.Marshal(calMap)
				var cal Calendar
				_ = json.Unmarshal(calBytes, &cal)

				createdCal, err := backend.CreateCalendar(ctx, &cal)
				if err != nil {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = createdCal
					recordCreationRefs(ctx, creationRefs, creationID, createdCal.ID)
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				patch, _ := patchRaw.(map[string]any)
				resolvedID := resolveCreationID(idStr, creationRefs)
				if isSharedCaller {
					cals, _, _ := backend.GetCalendars(ctx, []jmapcore.Id{jmapcore.Id(resolvedID)})
					if len(cals) == 0 || !cals[0].MyRights.MayWriteAll {
						notUpdated[string(resolvedID)] = jmapcore.SetError{
							Type:        "forbidden",
							Description: "You are not allowed to modify this calendar.",
						}
						continue
					}
				}
				if _, hasIsDefault := patch["isDefault"]; hasIsDefault {
					notUpdated[string(resolvedID)] = jmapcore.SetError{
						Type:        "invalidProperties",
						Description: "isDefault is server-set and cannot be set directly",
						Properties:  []string{"isDefault"},
					}
					continue
				}
				if err := validateCalendarMap(patch); err != nil {
					notUpdated[string(resolvedID)] = err
					continue
				}
				updatedCal, err := backend.UpdateCalendar(ctx, jmapcore.Id(resolvedID), resolvePatchCreationRefs(patch, creationRefs))
				if err != nil {
					notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: err.Error()}
				} else {
					_ = updatedCal
					updated[string(resolvedID)] = nil
				}
			}
		}

		if setDefaultRaw, ok := args["onSuccessSetIsDefault"].(string); ok && setDefaultRaw != "" {
			targetID := resolveCreationID(setDefaultRaw, creationRefs)
			_ = backend.SetDefaultCalendar(ctx, jmapcore.Id(targetID))
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					if isSharedCaller {
						cals, _, _ := backend.GetCalendars(ctx, []jmapcore.Id{jmapcore.Id(resolvedID)})
						if len(cals) == 0 || !cals[0].MyRights.MayDelete {
							notDestroyed[string(resolvedID)] = jmapcore.SetError{
								Type:        "forbidden",
								Description: "You are not allowed to delete this calendar.",
							}
							continue
						}
					}
					if !onDestroyRemoveEvents {
						hasEvs, _ := backend.CalendarHasEvents(ctx, jmapcore.Id(resolvedID))
						if hasEvs {
							notDestroyed[string(resolvedID)] = jmapcore.SetError{
								Type:        "calendarHasEvents",
								Description: "calendar contains events; use onDestroyRemoveEvents to delete",
							}
							continue
						}
					}
					okDel, err := backend.DeleteCalendar(ctx, jmapcore.Id(resolvedID))
					if err != nil || !okDel {
						notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: "calendar cannot be deleted"}
					} else {
						destroyed = append(destroyed, jmapcore.Id(resolvedID))
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

func handleCalendarCopy(backend CalendarsBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, fromAccountID := jmapcopy.ResolveCopyAccountIDs(args)
		srcCtx := SourceAccountContext(ctx, args)

		oldState, errInv := jmapcopy.ValidateCopyStates(ctx, srcCtx, args, backend.CalendarState, backend.CalendarState)
		if errInv != nil {
			return errInv.Name, errInv.Args
		}

		onSuccessDestroyOriginal, _ := args["onSuccessDestroyOriginal"].(bool)
		created := make(map[string]*Calendar)
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
				srcs, notFound, _ := backend.GetCalendars(srcCtx, []jmapcore.Id{jmapcore.Id(resolvedSrcID)})
				if len(srcs) == 0 || len(notFound) > 0 {
					notCreated[creationID] = jmapcore.SetError{Type: "notFound", Description: "source calendar not found: " + srcID}
					continue
				}

				merged := mergeCopyOverrides(srcs[0], m)
				calBytes, _ := json.Marshal(merged)
				var cal Calendar
				_ = json.Unmarshal(calBytes, &cal)
				cal.ID = ""

				newCal, err := backend.CreateCalendar(ctx, &cal)
				if err != nil {
					notCreated[creationID] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
				} else {
					created[creationID] = newCal
					recordCreationRefs(ctx, creationRefs, creationID, newCal.ID)
					destroyOriginals = append(destroyOriginals, jmapcore.Id(resolvedSrcID))
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
			"created":       nilIfEmpty(created),
			"notCreated":    nilIfEmpty(notCreated),
		}
	}
}

// validCalendarProperties are the settable/known Calendar properties (draft-ietf-jmap-calendars
// Section 2). id and myRights are server-set; unknown properties are rejected.
var validCalendarProperties = map[string]bool{
	"id": true, "name": true, "description": true, "color": true, "sortOrder": true,
	"isDefault": true, "isVisible": true, "isSubscribed": true, "includeInAvailability": true,
	"defaultAlertsWithTime": true, "defaultAlertsWithoutTime": true, "timeZone": true,
	"shareWith": true, "myRights": true,
}

// validateCalendarMap rejects unknown Calendar properties (including JSON-pointer patch paths)
// with invalidProperties, so Calendar/set never silently drops a misspelled property.
func validateCalendarMap(m map[string]any) error {
	for k := range m {
		baseKey := k
		if strings.Contains(k, "/") {
			baseKey = strings.Split(k, "/")[0]
		}
		if !validCalendarProperties[baseKey] {
			return jmapcore.SetError{Type: "invalidProperties", Description: "unknown property: " + k, Properties: []string{k}}
		}
	}
	return nil
}
