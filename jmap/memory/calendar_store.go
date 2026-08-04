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
type userCalendarStore struct {
	calendars  map[jmap.Id]*jmap.Calendar
	events     map[jmap.Id]*jmap.CalendarEvent
	calState   *changeTracker
	eventState *changeTracker
}

type MemoryCalendarsBackend struct {
	mu          sync.RWMutex
	users       map[string]*userCalendarStore
	nextID      uint64
	broadcaster *jmap.Broadcaster
}

func (b *MemoryCalendarsBackend) getStoreLocked(ctx context.Context) *userCalendarStore {
	accountID := "primary"
	if ctxID, ok := jmap.AccountIDFromContext(ctx); ok && ctxID != "" {
		accountID = ctxID
	}

	us, ok := b.users[accountID]
	if !ok {
		us = newMemoryUserCalendarStore()
		b.users[accountID] = us
	}
	return us
}

// Ensure MemoryCalendarsBackend implements jmap.CalendarsBackend interface.
var _ jmap.CalendarsBackend = (*MemoryCalendarsBackend)(nil)

// SetBroadcaster connects a Broadcaster so Calendar and CalendarEvent mutations emit
// RFC 8620 Section 7.1 StateChange push events.
func (b *MemoryCalendarsBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func newMemoryUserCalendarStore() *userCalendarStore {
	us := &userCalendarStore{
		calendars:  make(map[jmap.Id]*jmap.Calendar),
		events:     make(map[jmap.Id]*jmap.CalendarEvent),
		calState:   newChangeTracker(1000),
		eventState: newChangeTracker(1000),
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
	us.calendars[defaultCal.ID] = defaultCal

	return us
}

// NewMemoryCalendarsBackend initializes a new MemoryCalendarsBackend with a default calendar.
func NewMemoryCalendarsBackend() *MemoryCalendarsBackend {
	b := &MemoryCalendarsBackend{
		users:  make(map[string]*userCalendarStore),
		nextID: 1,
	}
	_ = b.getStoreLocked(context.Background())
	return b
}

// CalendarState returns the current change token for Calendars per RFC 8620.
func (b *MemoryCalendarsBackend) CalendarState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.calState.State()
}

// CalendarChanges returns created/updated/destroyed Calendars since the given state per RFC 8620 Section 5.2.
func (b *MemoryCalendarsBackend) CalendarChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.calState.Changes(sinceState)
}

// CalendarEventState returns the current change token for CalendarEvents per RFC 8620.
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
func (b *MemoryCalendarsBackend) recordChange(ctx context.Context, tracker *changeTracker, id jmap.Id, action string, typeName string) string {
	newState := tracker.record(id, action)
	if b.broadcaster != nil {
		accountID := "primary"
		if ctxID, ok := jmap.AccountIDFromContext(ctx); ok && ctxID != "" {
			accountID = ctxID
		}
		b.broadcaster.PublishStateChange(accountID, typeName, newState)
	}
	return newState
}

func (b *MemoryCalendarsBackend) GetCalendars(ctx context.Context, ids []jmap.Id) ([]*jmap.Calendar, []jmap.Id, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.Calendar
	var notFound []jmap.Id

	if len(ids) == 0 {
		for _, cal := range us.calendars {
			list = append(list, cal)
		}
		return list, nil, nil
	}

	for _, id := range ids {
		if cal, ok := us.calendars[id]; ok {
			list = append(list, cal)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryCalendarsBackend) GetAllCalendars(ctx context.Context) ([]*jmap.Calendar, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.Calendar
	for _, cal := range us.calendars {
		list = append(list, cal)
	}
	return list, nil
}

func (b *MemoryCalendarsBackend) CreateCalendar(ctx context.Context, cal *jmap.Calendar) (*jmap.Calendar, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
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
		for _, other := range us.calendars {
			other.IsDefault = false
		}
	}
	us.calendars[cal.ID] = cal
	b.recordChange(ctx, us.calState, cal.ID, "create", "Calendar")
	return cal, nil
}

func (b *MemoryCalendarsBackend) UpdateCalendar(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Calendar, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	cal, ok := us.calendars[id]
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
		for _, other := range us.calendars {
			if other.ID != cal.ID {
				other.IsDefault = false
			}
		}
		cal.IsDefault = true
	}

	b.recordChange(ctx, us.calState, id, "update", "Calendar")
	return cal, nil
}

func (b *MemoryCalendarsBackend) DeleteCalendar(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	cal, ok := us.calendars[id]
	if !ok {
		return false, nil
	}
	if cal.IsDefault {
		return false, fmt.Errorf("cannot delete default calendar")
	}

	delete(us.calendars, id)
	b.recordChange(ctx, us.calState, id, "destroy", "Calendar")
	return true, nil
}

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
	ev.Type = "Event"
	if len(ev.CalendarIDs) == 0 {
		ev.CalendarIDs = map[jmap.Id]bool{"cal-default": true}
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
		case "uid":
			s, _ := v.(string)
			if ev.UID != s {
				return false
			}
		case "updatedBefore":
			s, _ := v.(string)
			if s != "" && ev.Updated >= s {
				return false
			}
		case "updatedAfter":
			s, _ := v.(string)
			if s != "" && ev.Updated < s {
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
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var matched []*jmap.CalendarEvent
	for _, ev := range us.events {
		if MatchCalendarEvent(ev, filter) {
			matched = append(matched, ev)
		}
	}

	total := len(matched)
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
		ids = append(ids, matched[i].ID)
	}
	return ids, total, nil
}
