package jmap

import (
	"context"
	"encoding/json"
	"strings"
)

// RegisterCalendarHandlers registers JMAP for Calendars & JSCalendar method handlers into MethodRegistry.
func RegisterCalendarHandlers(r *MethodRegistry, backend CalendarsBackend, mailBackend MailBackend, principalsBackend PrincipalsBackend, blobBackend BlobBackend, resolver AccountResolver) {
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
				if err := validateCalendarMap(calMap); err != nil {
					notCreated[creationID] = err
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
				if err := validateCalendarMap(patch); err != nil {
					notUpdated[string(resolvedID)] = err
					continue
				}
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
			return SetError{Type: "invalidProperties", Description: "unknown property: " + k, Properties: []string{k}}
		}
	}
	return nil
}
