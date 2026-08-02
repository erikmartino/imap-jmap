package jmap

import (
	"context"
	"encoding/json"
)

// RegisterCalendarHandlers registers JMAP for Calendars & JSCalendar method handlers into MethodRegistry.
func RegisterCalendarHandlers(r *MethodRegistry, backend CalendarsBackend) {
	if backend == nil {
		return
	}
	r.Register("Calendar/get", handleCalendarGet(backend))
	r.Register("Calendar/changes", handleCalendarChanges(backend))
	r.Register("Calendar/set", handleCalendarSet(backend))

	r.Register("CalendarEvent/get", handleCalendarEventGet(backend))
	r.Register("CalendarEvent/changes", handleCalendarEventChanges(backend))
	r.Register("CalendarEvent/set", handleCalendarEventSet(backend))
	r.Register("CalendarEvent/query", handleCalendarEventQuery(backend))
	r.Register("CalendarEvent/copy", handleCalendarEventCopy(backend))
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

func handleCalendarEventSet(backend CalendarsBackend) MethodHandler {
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
					_ = updatedEv
					updated[idStr] = nil
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					okDel, err := backend.DeleteCalendarEvent(ctx, Id(idStr))
					if err != nil || !okDel {
						notDestroyed[idStr] = SetError{Type: "notFound", Description: "calendar event not found"}
					} else {
						destroyed = append(destroyed, Id(idStr))
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
