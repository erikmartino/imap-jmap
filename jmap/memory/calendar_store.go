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
	mu        sync.RWMutex
	calendars map[jmap.Id]*jmap.Calendar
	events    map[jmap.Id]*jmap.CalendarEvent
	nextID    uint64
}

// NewMemoryCalendarsBackend initializes a new MemoryCalendarsBackend with a default calendar.
func NewMemoryCalendarsBackend() *MemoryCalendarsBackend {
	b := &MemoryCalendarsBackend{
		calendars: make(map[jmap.Id]*jmap.Calendar),
		events:    make(map[jmap.Id]*jmap.CalendarEvent),
		nextID:    1,
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
	b.calendars[cal.ID] = cal
	return cal, nil
}

func (b *MemoryCalendarsBackend) UpdateCalendar(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Calendar, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	cal, ok := b.calendars[id]
	if !ok {
		return nil, fmt.Errorf("calendar not found: %s", id)
	}

	if name, ok := patch["name"].(string); ok {
		cal.Name = name
	}
	if desc, ok := patch["description"].(string); ok {
		cal.Description = &desc
	}
	if color, ok := patch["color"].(string); ok {
		cal.Color = &color
	}
	if isVisible, ok := patch["isVisible"].(bool); ok {
		cal.IsVisible = isVisible
	}

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
	return ev, nil
}

func (b *MemoryCalendarsBackend) UpdateCalendarEvent(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.CalendarEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ev, ok := b.events[id]
	if !ok {
		return nil, fmt.Errorf("calendar event not found: %s", id)
	}

	if title, ok := patch["title"].(string); ok {
		ev.Title = title
	}
	if desc, ok := patch["description"].(string); ok {
		ev.Description = desc
	}
	if start, ok := patch["start"].(string); ok {
		ev.Start = start
	}
	if dur, ok := patch["duration"].(string); ok {
		ev.Duration = dur
	}
	if status, ok := patch["status"].(string); ok {
		ev.Status = status
	}
	if fb, ok := patch["freeBusyStatus"].(string); ok {
		ev.FreeBusyStatus = fb
	}

	ev.Updated = time.Now().UTC().Format(time.RFC3339)
	return ev, nil
}

func (b *MemoryCalendarsBackend) DeleteCalendarEvent(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.events[id]; !ok {
		return false, nil
	}
	delete(b.events, id)
	return true, nil
}

func (b *MemoryCalendarsBackend) QueryCalendarEvents(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var matched []*jmap.CalendarEvent

	inCalendarID, _ := filter["inCalendar"].(string)
	titleFilter, _ := filter["title"].(string)

	for _, ev := range b.events {
		if inCalendarID != "" && !ev.CalendarIDs[jmap.Id(inCalendarID)] {
			continue
		}
		if titleFilter != "" && !strings.Contains(strings.ToLower(ev.Title), strings.ToLower(titleFilter)) {
			continue
		}
		matched = append(matched, ev)
	}

	total := len(matched)
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
