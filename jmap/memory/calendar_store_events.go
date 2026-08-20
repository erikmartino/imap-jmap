package memory

import (
	"context"
	"fmt"
	"imap-jmap/jmap"
	"strings"
	"time"
)

func (b *MemoryCalendarsBackend) CalendarEventState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.eventState.State()
}

// CalendarEventChanges returns created/updated/destroyed CalendarEvents since the given state per RFC 8620 Section 5.2.
func (b *MemoryCalendarsBackend) CalendarEventChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.eventState.Changes(sinceState)
}

// recordChange records a mutation on the given tracker and publishes the new state token
// to push subscribers.

func (b *MemoryCalendarsBackend) GetCalendarEvents(ctx context.Context, ids []jmap.Id) ([]*jmap.CalendarEvent, []jmap.Id, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEvent
	var notFound []jmap.Id

	if len(ids) == 0 {
		for _, ev := range us.events {
			list = append(list, ev)
		}
		return list, nil, nil
	}

	for _, id := range ids {
		if ev, ok := us.events[id]; ok {
			list = append(list, ev)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryCalendarsBackend) GetAllCalendarEvents(ctx context.Context) ([]*jmap.CalendarEvent, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEvent
	for _, ev := range us.events {
		list = append(list, ev)
	}
	return list, nil
}

func (b *MemoryCalendarsBackend) CreateCalendarEvent(ctx context.Context, ev *jmap.CalendarEvent) (*jmap.CalendarEvent, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if ev.ID == "" {
		b.nextID++
		ev.ID = jmap.Id(fmt.Sprintf("evt-%d", b.nextID))
	}
	if ev.UID == "" {
		ev.UID = fmt.Sprintf("uid-%s", string(ev.ID))
	}
	if ev.Type == "" {
		ev.Type = "Event"
	}
	if len(ev.CalendarIDs) == 0 {
		// Dev convenience: an event with no explicit calendarIds lands in the default
		// calendar. When calendarIds ARE given, every referenced calendar MUST exist and
		// map to true, or the create is rejected (data-integrity: never point an event at a
		// dangling calendar).
		ev.CalendarIDs = map[jmap.Id]bool{"cal-default": true}
	} else {
		for calID, member := range ev.CalendarIDs {
			if !member {
				return nil, fmt.Errorf("calendarIds values must be true: %s", calID)
			}
			if _, ok := us.calendars[calID]; !ok {
				return nil, fmt.Errorf("calendarIds references unknown calendar: %s", calID)
			}
		}
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	if ev.Created == "" {
		ev.Created = nowStr
	}
	ev.Updated = nowStr

	us.events[ev.ID] = ev
	b.recordChange(ctx, us.eventState, ev.ID, "create", "CalendarEvent")
	return ev, nil
}

// setCalendarEventField applies a single RFC 8984 CalendarEvent patch path.
func setCalendarEventField(ev *jmap.CalendarEvent, path string, val any) {
	if strings.Contains(path, "/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 3 {
			switch parts[0] {
			case "participants":
				partKey := parts[1]
				field := parts[2]
				if ev.Participants == nil {
					ev.Participants = make(map[string]*jmap.JSCalendarParticipant)
				}
				p, ok := ev.Participants[partKey]
				if !ok || p == nil {
					p = &jmap.JSCalendarParticipant{Email: partKey}
					ev.Participants[partKey] = p
				}
				if valStr, ok := val.(string); ok {
					if field == "participationStatus" || field == "status" {
						p.ParticipationStatus = valStr
						p.Status = valStr
					} else if field == "role" {
						p.Role = valStr
						if p.Roles == nil {
							p.Roles = make(map[string]bool)
						}
						p.Roles[valStr] = true
					} else if field == "name" {
						p.Name = valStr
					}
				}
			case "locations":
				locKey := parts[1]
				field := parts[2]
				if ev.Locations == nil {
					ev.Locations = make(map[string]*jmap.JSCalendarLocation)
				}
				loc, ok := ev.Locations[locKey]
				if !ok || loc == nil {
					loc = &jmap.JSCalendarLocation{}
					ev.Locations[locKey] = loc
				}
				if valStr, ok := val.(string); ok {
					if field == "name" {
						loc.Name = valStr
					} else if field == "description" {
						loc.Description = valStr
					} else if field == "rel" {
						loc.Rel = valStr
					}
				}
			}
		}
		return
	}
	switch path {
	case "@type", "type":
		if s, ok := val.(string); ok && s != "" {
			ev.Type = s
		}
	case "title":
		if s, ok := val.(string); ok {
			ev.Title = s
		}
	case "description":
		if val == nil {
			ev.Description = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.Description = s
		}
	case "start":
		if s, ok := val.(string); ok {
			ev.Start = s
		}
	case "duration":
		if val == nil {
			ev.Duration = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.Duration = s
		}
	case "timeZone":
		if val == nil {
			ev.TimeZone = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.TimeZone = s
		}
	case "locations":
		if val == nil {
			ev.Locations = nil
			return
		}
		var m map[string]*jmap.JSCalendarLocation
		if err := decodeJSONField(val, &m); err == nil {
			ev.Locations = m
		}
	case "location":
		if val == nil {
			ev.Locations = nil
			return
		}
		var loc jmap.JSCalendarLocation
		if err := decodeJSONField(val, &loc); err == nil {
			if ev.Locations == nil {
				ev.Locations = make(map[string]*jmap.JSCalendarLocation)
			}
			ev.Locations["loc-1"] = &loc
		}
	case "status":
		if s, ok := val.(string); ok {
			ev.Status = s
		}
	case "freeBusyStatus":
		if val == nil {
			ev.FreeBusyStatus = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.FreeBusyStatus = s
		}
	case "privacy":
		if val == nil {
			ev.Privacy = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.Privacy = s
		}
	case "hideAttendees":
		if val == nil {
			ev.HideAttendees = false
			return
		}
		if b, ok := val.(bool); ok {
			ev.HideAttendees = b
		}
	case "participants":
		if val == nil {
			ev.Participants = nil
			return
		}
		var m map[string]*jmap.JSCalendarParticipant
		if err := decodeJSONField(val, &m); err == nil {
			ev.Participants = m
		}
	case "recurrenceRules":
		if val == nil {
			ev.RecurrenceRules = nil
			return
		}
		var rules []*jmap.JSCalendarRecurrenceRule
		if err := decodeJSONField(val, &rules); err == nil {
			ev.RecurrenceRules = rules
		}
	case "alerts":
		if val == nil {
			ev.Alerts = nil
			return
		}
		var m map[string]*jmap.JSCalendarAlert
		if err := decodeJSONField(val, &m); err == nil {
			ev.Alerts = m
		}
	case "keywords":
		if val == nil {
			ev.Keywords = nil
			return
		}
		var m map[string]bool
		if err := decodeJSONField(val, &m); err == nil {
			ev.Keywords = m
		}
	case "relatedTo":
		if val == nil {
			ev.RelatedTo = nil
			return
		}
		var m map[string]*jmap.JSCalendarRelation
		if err := decodeJSONField(val, &m); err == nil {
			ev.RelatedTo = m
		}
	case "prodId":
		if val == nil {
			ev.ProdID = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.ProdID = s
		}
	case "sequence":
		if val == nil {
			ev.Sequence = 0
			return
		}
		if f, ok := val.(float64); ok {
			ev.Sequence = uint32(f)
		}
	case "method":
		if val == nil {
			ev.Method = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.Method = s
		}
	case "descriptionContentType":
		if val == nil {
			ev.DescriptionContentType = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.DescriptionContentType = s
		}
	case "showWithoutTime":
		if val == nil {
			ev.ShowWithoutTime = false
			return
		}
		if b, ok := val.(bool); ok {
			ev.ShowWithoutTime = b
		}
	case "locale":
		if val == nil {
			ev.Locale = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.Locale = s
		}
	case "categories":
		if val == nil {
			ev.Categories = nil
			return
		}
		var m map[string]bool
		if err := decodeJSONField(val, &m); err == nil {
			ev.Categories = m
		}
	case "color":
		if val == nil {
			ev.Color = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.Color = s
		}
	case "priority":
		if val == nil {
			ev.Priority = 0
			return
		}
		if f, ok := val.(float64); ok {
			ev.Priority = uint32(f)
		}
	case "replyTo":
		if val == nil {
			ev.ReplyTo = nil
			return
		}
		var m map[string]string
		if err := decodeJSONField(val, &m); err == nil {
			ev.ReplyTo = m
		}
	case "sentBy":
		if val == nil {
			ev.SentBy = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.SentBy = s
		}
	case "requestStatus":
		if val == nil {
			ev.RequestStatus = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.RequestStatus = s
		}
	case "useDefaultAlerts":
		if val == nil {
			ev.UseDefaultAlerts = false
			return
		}
		if b, ok := val.(bool); ok {
			ev.UseDefaultAlerts = b
		}
	case "localizations":
		if val == nil {
			ev.Localizations = nil
			return
		}
		var m map[string]map[string]any
		if err := decodeJSONField(val, &m); err == nil {
			ev.Localizations = m
		}
	case "timeZones":
		if val == nil {
			ev.TimeZones = nil
			return
		}
		var m map[string]*jmap.JSCalendarTimeZone
		if err := decodeJSONField(val, &m); err == nil {
			ev.TimeZones = m
		}
	case "recurrenceId":
		if val == nil {
			ev.RecurrenceID = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.RecurrenceID = s
		}
	case "recurrenceIdTimeZone":
		if val == nil {
			ev.RecurrenceIDTimeZone = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.RecurrenceIDTimeZone = s
		}
	case "excludedRecurrenceRules":
		if val == nil {
			ev.ExcludedRecurrenceRules = nil
			return
		}
		var rules []*jmap.JSCalendarRecurrenceRule
		if err := decodeJSONField(val, &rules); err == nil {
			ev.ExcludedRecurrenceRules = rules
		}
	case "recurrenceOverrides":
		if val == nil {
			ev.RecurrenceOverrides = nil
			return
		}
		var m map[string]map[string]any
		if err := decodeJSONField(val, &m); err == nil {
			ev.RecurrenceOverrides = m
		}
	case "excluded":
		if val == nil {
			ev.Excluded = nil
			return
		}
		var m map[string]bool
		if err := decodeJSONField(val, &m); err == nil {
			ev.Excluded = m
		}
	case "due":
		if val == nil {
			ev.Due = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.Due = s
		}
	case "estimatedDuration":
		if val == nil {
			ev.EstimatedDuration = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.EstimatedDuration = s
		}
	case "percentComplete":
		if val == nil {
			ev.PercentComplete = 0
			return
		}
		if f, ok := val.(float64); ok {
			ev.PercentComplete = uint32(f)
		}
	case "progress":
		if val == nil {
			ev.Progress = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.Progress = s
		}
	case "progressUpdated":
		if val == nil {
			ev.ProgressUpdated = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.ProgressUpdated = s
		}
	case "entries":
		if val == nil {
			ev.Entries = nil
			return
		}
		var m map[string]map[string]any
		if err := decodeJSONField(val, &m); err == nil {
			ev.Entries = m
		}
	case "source":
		if val == nil {
			ev.Source = ""
			return
		}
		if s, ok := val.(string); ok {
			ev.Source = s
		}
	case "uid":
		if val == nil {
			return
		}
		if s, ok := val.(string); ok && s != "" {
			ev.UID = s
		}
	}
}

func (b *MemoryCalendarsBackend) UpdateCalendarEvent(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.CalendarEvent, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	ev, ok := us.events[id]
	if !ok {
		return nil, fmt.Errorf("calendar event not found: %s", id)
	}

	for path, val := range patch {
		setCalendarEventField(ev, path, val)
	}
	ev.Updated = time.Now().UTC().Format(time.RFC3339)
	b.recordChange(ctx, us.eventState, id, "update", "CalendarEvent")
	return ev, nil
}

func (b *MemoryCalendarsBackend) DeleteCalendarEvent(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.events[id]; !ok {
		return false, nil
	}
	delete(us.events, id)
	b.recordChange(ctx, us.eventState, id, "destroy", "CalendarEvent")
	return true, nil
}

func (b *MemoryCalendarsBackend) QueryCalendarEvents(ctx context.Context, filter map[string]any, sort []jmap.Comparator, position int, limit *uint64, expandRecurrences bool) ([]jmap.Id, int, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var matched []*jmap.CalendarEvent
	for _, ev := range us.events {
		if MatchCalendarEvent(ev, filter) {
			matched = append(matched, ev)
		}
	}

	sortCalendarEvents(matched, sort)

	var resultIDs []jmap.Id
	if expandRecurrences {
		horizon := time.Now().AddDate(2, 0, 0)
		for _, ev := range matched {
			if len(ev.RecurrenceRules) > 0 {
				instances := ExpandRecurrenceInstances(ev, horizon)
				for _, inst := range instances {
					resultIDs = append(resultIDs, jmap.Id(fmt.Sprintf("%s#%s", string(ev.ID), inst.RecurrenceID)))
				}
			} else {
				resultIDs = append(resultIDs, ev.ID)
			}
		}
	} else {
		for _, ev := range matched {
			resultIDs = append(resultIDs, ev.ID)
		}
	}

	total := len(resultIDs)
	position = jmap.NormalizePosition(position, total)
	if position >= total {
		return []jmap.Id{}, total, nil
	}

	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}

	ids := make([]jmap.Id, 0, end-position)
	for i := position; i < end; i++ {
		ids = append(ids, resultIDs[i])
	}
	return ids, total, nil
}

// --- ParticipantIdentities (draft-ietf-jmap-calendars Section 3) ---

// ParticipantIdentityState returns the current change state token for ParticipantIdentities.
