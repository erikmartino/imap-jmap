package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// MemoryCalendarsBackend provides an in-memory implementation of jmap.CalendarsBackend for JMAP Calendars & JSCalendar (RFC 8984).
type MemoryCalendarsBackend struct {
	mu         sync.RWMutex
	calendars  map[jmap.Id]*jmap.Calendar
	events     map[jmap.Id]*jmap.CalendarEvent
	calState   *changeTracker
	eventState *changeTracker
	nextID     uint64
}

// NewMemoryCalendarsBackend initializes a new MemoryCalendarsBackend with a default calendar.
func NewMemoryCalendarsBackend() *MemoryCalendarsBackend {
	b := &MemoryCalendarsBackend{
		calendars:  make(map[jmap.Id]*jmap.Calendar),
		events:     make(map[jmap.Id]*jmap.CalendarEvent),
		calState:   newChangeTracker(1000),
		eventState: newChangeTracker(1000),
		nextID:     1,
	}

	defaultCal := &jmap.Calendar{
		ID:        "cal-default",
		Name:      "Personal Calendar",
		SortOrder: 0,
		IsDefault: true,
		IsVisible: true,
		MyRights: jmap.CalendarRights{
			MayReadItems:  true,
			MayWriteItems: true,
			MayAdmin:      true,
			MayDelete:     false,
		},
	}
	b.calendars[defaultCal.ID] = defaultCal

	return b
}

// CalendarState returns the current change token for Calendars per RFC 8620.
func (b *MemoryCalendarsBackend) CalendarState(ctx context.Context) string {
	return b.calState.State()
}

// CalendarChanges returns created/updated/destroyed Calendars since the given state per RFC 8620 Section 5.2.
func (b *MemoryCalendarsBackend) CalendarChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	return b.calState.Changes(sinceState)
}

// CalendarEventState returns the current change token for CalendarEvents per RFC 8620.
func (b *MemoryCalendarsBackend) CalendarEventState(ctx context.Context) string {
	return b.eventState.State()
}

// CalendarEventChanges returns created/updated/destroyed CalendarEvents since the given state per RFC 8620 Section 5.2.
func (b *MemoryCalendarsBackend) CalendarEventChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	return b.eventState.Changes(sinceState)
}

func (b *MemoryCalendarsBackend) GetCalendars(ctx context.Context, ids []jmap.Id) ([]*jmap.Calendar, []jmap.Id, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.Calendar
	var notFound []jmap.Id

	if len(ids) == 0 {
		for _, cal := range b.calendars {
			list = append(list, cal)
		}
		return list, nil, nil
	}

	for _, id := range ids {
		if cal, ok := b.calendars[id]; ok {
			list = append(list, cal)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryCalendarsBackend) GetAllCalendars(ctx context.Context) ([]*jmap.Calendar, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.Calendar
	for _, cal := range b.calendars {
		list = append(list, cal)
	}
	return list, nil
}

func (b *MemoryCalendarsBackend) CreateCalendar(ctx context.Context, cal *jmap.Calendar) (*jmap.Calendar, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if cal.ID == "" {
		b.nextID++
		cal.ID = jmap.Id(fmt.Sprintf("cal-%d", b.nextID))
	}
	cal.MyRights = jmap.CalendarRights{
		MayReadItems:  true,
		MayWriteItems: true,
		MayAdmin:      true,
		MayDelete:     true,
	}
	if cal.IsDefault {
		for _, other := range b.calendars {
			other.IsDefault = false
		}
	}
	b.calendars[cal.ID] = cal
	b.calState.record(cal.ID, "create")
	return cal, nil
}

func (b *MemoryCalendarsBackend) UpdateCalendar(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Calendar, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	cal, ok := b.calendars[id]
	if !ok {
		return nil, fmt.Errorf("calendar not found: %s", id)
	}

	if name, ok := patch["name"].(string); ok && name != "" {
		cal.Name = name
	}
	if desc, ok := patch["description"].(string); ok {
		cal.Description = &desc
	} else if _, present := patch["description"]; present {
		cal.Description = nil
	}
	if color, ok := patch["color"].(string); ok {
		cal.Color = &color
	} else if _, present := patch["color"]; present {
		cal.Color = nil
	}
	if sortOrder, ok := patch["sortOrder"].(float64); ok {
		cal.SortOrder = uint64(sortOrder)
	}
	if isVisible, ok := patch["isVisible"].(bool); ok {
		cal.IsVisible = isVisible
	}
	if isDefault, ok := patch["isDefault"].(bool); ok && isDefault {
		for _, other := range b.calendars {
			if other.ID != cal.ID {
				other.IsDefault = false
			}
		}
		cal.IsDefault = true
	}

	b.calState.record(id, "update")
	return cal, nil
}

func (b *MemoryCalendarsBackend) DeleteCalendar(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	cal, ok := b.calendars[id]
	if !ok {
		return false, nil
	}
	if cal.IsDefault {
		return false, fmt.Errorf("cannot delete default calendar")
	}

	delete(b.calendars, id)
	b.calState.record(id, "destroy")
	return true, nil
}

func (b *MemoryCalendarsBackend) GetCalendarEvents(ctx context.Context, ids []jmap.Id) ([]*jmap.CalendarEvent, []jmap.Id, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEvent
	var notFound []jmap.Id

	if len(ids) == 0 {
		for _, ev := range b.events {
			list = append(list, ev)
		}
		return list, nil, nil
	}

	for _, id := range ids {
		if ev, ok := b.events[id]; ok {
			list = append(list, ev)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryCalendarsBackend) GetAllCalendarEvents(ctx context.Context) ([]*jmap.CalendarEvent, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEvent
	for _, ev := range b.events {
		list = append(list, ev)
	}
	return list, nil
}

func (b *MemoryCalendarsBackend) CreateCalendarEvent(ctx context.Context, ev *jmap.CalendarEvent) (*jmap.CalendarEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ev.ID == "" {
		b.nextID++
		ev.ID = jmap.Id(fmt.Sprintf("evt-%d", b.nextID))
	}
	ev.Type = "Event"
	if len(ev.CalendarIDs) == 0 {
		ev.CalendarIDs = map[jmap.Id]bool{"cal-default": true}
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	if ev.Created == "" {
		ev.Created = nowStr
	}
	ev.Updated = nowStr

	b.events[ev.ID] = ev
	b.eventState.record(ev.ID, "create")
	return ev, nil
}

// setCalendarEventField applies a single RFC 8984 CalendarEvent patch path.
func setCalendarEventField(ev *jmap.CalendarEvent, path string, val any) {
	switch path {
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
	case "location":
		if val == nil {
			ev.Location = nil
			return
		}
		var loc jmap.JSCalendarLocation
		if err := decodeJSONField(val, &loc); err == nil {
			ev.Location = &loc
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
	}
}

func (b *MemoryCalendarsBackend) UpdateCalendarEvent(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.CalendarEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ev, ok := b.events[id]
	if !ok {
		return nil, fmt.Errorf("calendar event not found: %s", id)
	}

	for path, val := range patch {
		setCalendarEventField(ev, path, val)
	}
	ev.Updated = time.Now().UTC().Format(time.RFC3339)
	b.eventState.record(id, "update")
	return ev, nil
}

func (b *MemoryCalendarsBackend) DeleteCalendarEvent(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.events[id]; !ok {
		return false, nil
	}
	delete(b.events, id)
	b.eventState.record(id, "destroy")
	return true, nil
}

func matchEventText(ev *jmap.CalendarEvent, q string) bool {
	if containsFold(ev.Title, q) || containsFold(ev.Description, q) {
		return true
	}
	if ev.Location != nil && (containsFold(ev.Location.Name, q) || containsFold(ev.Location.Description, q)) {
		return true
	}
	for _, p := range ev.Participants {
		if p != nil && (containsFold(p.Name, q) || containsFold(p.Email, q)) {
			return true
		}
	}
	return false
}

// MatchCalendarEvent reports whether an event satisfies a CalendarEvent filter
// condition per RFC 8984 / draft-ietf-jmap-calendars Section 5.11.
func MatchCalendarEvent(ev *jmap.CalendarEvent, filter map[string]any) bool {
	for k, v := range filter {
		switch k {
		case "inCalendar":
			calID, ok := v.(string)
			if !ok || !ev.CalendarIDs[jmap.Id(calID)] {
				return false
			}
		case "title":
			s, _ := v.(string)
			if !containsFold(ev.Title, s) {
				return false
			}
		case "description":
			s, _ := v.(string)
			if !containsFold(ev.Description, s) {
				return false
			}
		case "location":
			s, _ := v.(string)
			if ev.Location == nil || !(containsFold(ev.Location.Name, s) || containsFold(ev.Location.Description, s)) {
				return false
			}
		case "text":
			s, _ := v.(string)
			if !matchEventText(ev, s) {
				return false
			}
		case "after":
			s, _ := v.(string)
			if !eventEndsAfter(ev, s) {
				return false
			}
		case "before":
			s, _ := v.(string)
			if !eventStartsBefore(ev, s) {
				return false
			}
		}
	}
	return true
}

// parseRFC3339 parses an RFC 3339 string used for Date-type comparisons.
func parseRFC3339(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// eventEndTime returns the event end time computed as start + duration,
// or start when no duration is present per RFC 8984.
func eventEndTime(ev *jmap.CalendarEvent) (time.Time, bool) {
	start, ok := parseRFC3339(ev.Start)
	if !ok {
		return time.Time{}, false
	}
	if ev.Duration == "" {
		return start, true
	}
	d, ok := parseISODuration(ev.Duration)
	if !ok {
		return start, true
	}
	return start.Add(d), true
}

// eventEndsAfter matches when the event (or any recurrence) ends after the date.
func eventEndsAfter(ev *jmap.CalendarEvent, date string) bool {
	ref, ok := parseRFC3339(date)
	if !ok {
		return false
	}
	end, ok := eventEndTime(ev)
	if !ok {
		return false
	}
	return end.After(ref)
}

// eventStartsBefore matches when the event starts before the date.
func eventStartsBefore(ev *jmap.CalendarEvent, date string) bool {
	ref, ok := parseRFC3339(date)
	if !ok {
		return false
	}
	start, ok := parseRFC3339(ev.Start)
	if !ok {
		return false
	}
	return start.Before(ref)
}

// parseISODuration converts an ISO 8601 duration (e.g. "PT1H30M", "P1D") to
// time.Duration. Returns ok=false for unsupported or empty input.
func parseISODuration(raw string) (time.Duration, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || s[0] != 'P' {
		return 0, false
	}
	s = s[1:]
	var total time.Duration
	var value uint64
	inTime := false

	flush := func(unit time.Duration) bool {
		total += time.Duration(value) * unit
		value = 0
		return true
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			value = value*10 + uint64(c-'0')
		case c == 'T':
			inTime = true
		case c == 'W' && !inTime:
			flush(7 * 24 * time.Hour)
		case c == 'D' && !inTime:
			flush(24 * time.Hour)
		case c == 'H' && inTime:
			flush(time.Hour)
		case c == 'M' && inTime:
			flush(time.Minute)
		case c == 'S' && inTime:
			flush(time.Second)
		default:
			return 0, false
		}
	}
	return total, true
}

func (b *MemoryCalendarsBackend) QueryCalendarEvents(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var matched []*jmap.CalendarEvent
	for _, ev := range b.events {
		if MatchCalendarEvent(ev, filter) {
			matched = append(matched, ev)
		}
	}

	total := len(matched)
	if position < 0 {
		position = 0
	}
	if position >= total {
		return []jmap.Id{}, total, nil
	}

	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}

	ids := make([]jmap.Id, 0, end-position)
	for i := position; i < end; i++ {
		ids = append(ids, matched[i].ID)
	}
	return ids, total, nil
}
