package memory

import (
	"context"
	"fmt"
	"sync"

	"imap-jmap/jmap"
)

// MemoryCalendarsBackend provides an in-memory implementation of jmap.CalendarsBackend for JMAP Calendars & JSCalendar (RFC 8984).
type userCalendarStore struct {
	calendars         map[jmap.Id]*jmap.Calendar
	events            map[jmap.Id]*jmap.CalendarEvent
	calState          *changeTracker
	eventState        *changeTracker
	identities        map[jmap.Id]*jmap.ParticipantIdentity
	identityState     *changeTracker
	notifications     map[jmap.Id]*jmap.CalendarEventNotification
	notificationState *changeTracker
	notificationSeq   map[jmap.Id]uint64
}

type MemoryCalendarsBackend struct {
	mu          sync.RWMutex
	users       map[string]*userCalendarStore
	nextID      uint64
	broadcaster *jmap.Broadcaster
}

func (b *MemoryCalendarsBackend) getStoreLocked(ctx context.Context) *userCalendarStore {
	accountID, _ := jmap.AccountIDFromContext(ctx)

	us, ok := b.users[accountID]
	if !ok {
		us = newMemoryUserCalendarStore(accountID)
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

func newMemoryUserCalendarStore(accountID string) *userCalendarStore {
	us := &userCalendarStore{
		calendars:         make(map[jmap.Id]*jmap.Calendar),
		events:            make(map[jmap.Id]*jmap.CalendarEvent),
		calState:          newChangeTracker(1000),
		eventState:        newChangeTracker(1000),
		identities:        make(map[jmap.Id]*jmap.ParticipantIdentity),
		identityState:     newChangeTracker(1000),
		notifications:     make(map[jmap.Id]*jmap.CalendarEventNotification),
		notificationState: newChangeTracker(1000),
		notificationSeq:   make(map[jmap.Id]uint64),
	}

	defaultCal := &jmap.Calendar{
		ID:        "cal-default",
		Name:      "Personal Calendar",
		SortOrder: 0,
		IsDefault: true,
		IsVisible: true,
		MyRights:  jmap.FullCalendarRights(),
	}
	us.calendars[defaultCal.ID] = defaultCal

	userEmail, _ := jmap.SubjectForAccountID(accountID)
	if userEmail == "" {
		userEmail = accountID
	}

	// Every fresh account gets exactly one default ParticipantIdentity (SHOULD per
	// draft-ietf-jmap-calendars Section 3), representing the account owner.
	defaultIdentity := &jmap.ParticipantIdentity{
		ID:              "identity-default",
		Name:            userEmail,
		CalendarAddress: "mailto:" + userEmail,
		SendTo:          map[string]string{"imip": "mailto:" + userEmail},
		IsDefault:       true,
	}
	us.identities[defaultIdentity.ID] = defaultIdentity

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

func (b *MemoryCalendarsBackend) recordChange(ctx context.Context, tracker *changeTracker, id jmap.Id, action string, typeName string) string {
	newState := tracker.record(id, action)
	if b.broadcaster != nil {
		accountID, _ := jmap.AccountIDFromContext(ctx)
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
	if cal.MyRights.MayReadFreeBusy == false && cal.MyRights.MayReadItems == false && cal.MyRights.MayWriteAll == false {
		cal.MyRights = jmap.FullCalendarRights()
	}
	cal.MyRights.EnforceInvariants()

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
	if isSubscribed, ok := patch["isSubscribed"].(bool); ok {
		cal.IsSubscribed = isSubscribed
	}
	if inc, ok := patch["includeInAvailability"].(string); ok {
		cal.IncludeInAvailability = inc
	}
	if tz, ok := patch["timeZone"].(string); ok {
		cal.TimeZone = tz
	}
	if isDefault, ok := patch["isDefault"].(bool); ok && isDefault {
		for _, other := range us.calendars {
			if other.ID != cal.ID {
				other.IsDefault = false
			}
		}
		cal.IsDefault = true
	}
	cal.MyRights.EnforceInvariants()

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

func (b *MemoryCalendarsBackend) SetDefaultCalendar(ctx context.Context, id jmap.Id) error {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	target, ok := us.calendars[id]
	if !ok {
		return fmt.Errorf("calendar not found: %s", id)
	}

	for _, cal := range us.calendars {
		if cal.ID == id {
			cal.IsDefault = true
		} else {
			cal.IsDefault = false
		}
	}
	b.recordChange(ctx, us.calState, target.ID, "update", "Calendar")
	return nil
}

func (b *MemoryCalendarsBackend) CalendarHasEvents(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	for _, ev := range us.events {
		if ev.CalendarIDs != nil && ev.CalendarIDs[id] {
			return true, nil
		}
	}
	return false, nil
}
