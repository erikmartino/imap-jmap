package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teambition/rrule-go"

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
	accountID := jmap.AccountIDForSubject("user@example.com")
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

	// Every fresh account gets exactly one default ParticipantIdentity (SHOULD per
	// draft-ietf-jmap-calendars Section 3), representing the account owner.
	defaultIdentity := &jmap.ParticipantIdentity{
		ID:              "identity-default",
		Name:            "Primary User",
		CalendarAddress: "mailto:user@example.com",
		SendTo:          map[string]string{"imip": "mailto:user@example.com"},
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
		accountID := jmap.AccountIDForSubject("user@example.com")
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

func matchEventText(ev *jmap.CalendarEvent, q string) bool {
	if containsFold(ev.Title, q) || containsFold(ev.Description, q) {
		return true
	}
	for _, loc := range ev.Locations {
		if loc != nil && (containsFold(loc.Name, q) || containsFold(loc.Description, q)) {
			return true
		}
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
	if filter == nil {
		return true
	}
	// FilterOperator (RFC 8620 Section 5.5): AND/OR/NOT over nested conditions.
	if opVal, ok := filter["operator"]; ok {
		op, _ := opVal.(string)
		var conds []map[string]any
		if raw, ok := filter["conditions"].([]any); ok {
			for _, c := range raw {
				if cm, ok := c.(map[string]any); ok {
					conds = append(conds, cm)
				}
			}
		} else if raw, ok := filter["conditions"].([]map[string]any); ok {
			conds = raw
		}
		switch strings.ToUpper(op) {
		case "AND":
			for _, c := range conds {
				if !MatchCalendarEvent(ev, c) {
					return false
				}
			}
			return true
		case "OR":
			for _, c := range conds {
				if MatchCalendarEvent(ev, c) {
					return true
				}
			}
			return false
		case "NOT":
			for _, c := range conds {
				if MatchCalendarEvent(ev, c) {
					return false
				}
			}
			return true
		}
	}
	for k, v := range filter {
		switch k {
		case "inCalendar":
			calID, ok := v.(string)
			if !ok || !ev.CalendarIDs[jmap.Id(calID)] {
				return false
			}
		case "inCalendars":
			raw, ok := v.([]any)
			if !ok {
				return false
			}
			matched := false
			for _, item := range raw {
				if calID, ok := item.(string); ok && ev.CalendarIDs[jmap.Id(calID)] {
					matched = true
					break
				}
			}
			if !matched {
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
			matchedLoc := false
			for _, loc := range ev.Locations {
				if loc != nil && (containsFold(loc.Name, s) || containsFold(loc.Description, s)) {
					matchedLoc = true
					break
				}
			}
			if !matchedLoc {
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
		case "owner":
			s, _ := v.(string)
			matchedOwner := false
			for _, p := range ev.Participants {
				if p != nil && (p.Role == "owner" || (p.Roles != nil && p.Roles["owner"])) {
					if containsFold(p.Email, s) || containsFold(p.Name, s) {
						matchedOwner = true
						break
					}
				}
			}
			if !matchedOwner {
				return false
			}
		case "attendee":
			s, _ := v.(string)
			matchedAttendee := false
			for _, p := range ev.Participants {
				if p != nil && (containsFold(p.Email, s) || containsFold(p.Name, s)) {
					matchedAttendee = true
					break
				}
			}
			if !matchedAttendee {
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

// parseFloatingDateTime parses a JSCalendar LocalDateTime ("2026-08-01T10:00:00", RFC 8984
// Section 1.4.5) or date-only value, treated as UTC for expansion purposes.
func parseFloatingDateTime(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
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

// RecurrenceInstance defines an expanded instance of a recurring event per RFC 8984.
type RecurrenceInstance struct {
	RecurrenceID string
	Start        time.Time
	End          time.Time
}

// jsWeekdayToRRule maps a JSCalendar NDay day code ("mo".."su") to an rrule.Weekday,
// applying the optional nth-occurrence qualifier (RFC 8984 Section 4.3.3).
func jsWeekdayToRRule(nd *jmap.NDay) (rrule.Weekday, bool) {
	if nd == nil {
		return rrule.Weekday{}, false
	}
	var wd rrule.Weekday
	switch strings.ToLower(nd.Day) {
	case "mo":
		wd = rrule.MO
	case "tu":
		wd = rrule.TU
	case "we":
		wd = rrule.WE
	case "th":
		wd = rrule.TH
	case "fr":
		wd = rrule.FR
	case "sa":
		wd = rrule.SA
	case "su":
		wd = rrule.SU
	default:
		return rrule.Weekday{}, false
	}
	if nd.Nth != 0 {
		wd = wd.Nth(nd.Nth)
	}
	return wd, true
}

// jsFrequencyToRRule maps a JSCalendar frequency to an rrule.Frequency.
func jsFrequencyToRRule(freq string) (rrule.Frequency, bool) {
	switch strings.ToLower(freq) {
	case "yearly":
		return rrule.YEARLY, true
	case "monthly":
		return rrule.MONTHLY, true
	case "weekly":
		return rrule.WEEKLY, true
	case "daily":
		return rrule.DAILY, true
	case "hourly":
		return rrule.HOURLY, true
	case "minutely":
		return rrule.MINUTELY, true
	case "secondly":
		return rrule.SECONDLY, true
	}
	return 0, false
}

// jsMonthToRRule converts a JSCalendar "byMonth" token (e.g. "1", "5", or leap-month
// forms like "5L") to a 1-based month number. The leap-month suffix is ignored for
// Gregorian expansion.
func jsMonthToRRule(m string) (int, bool) {
	m = strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(m)), "L")
	if m == "" {
		return 0, false
	}
	n := 0
	for _, c := range m {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > 12 {
		return 0, false
	}
	return n, true
}

// buildRRuleOption converts one JSCalendar RecurrenceRule to an rrule.ROption anchored at
// dtstart. Every RFC 8984 Section 4.3.3 "byX" part, plus interval/count/until/firstDayOfWeek/
// bySetPosition, is mapped through. Returns ok=false when the frequency is invalid.
func buildRRuleOption(rule *jmap.JSCalendarRecurrenceRule, dtstart time.Time) (rrule.ROption, bool) {
	if rule == nil {
		return rrule.ROption{}, false
	}
	freq, ok := jsFrequencyToRRule(rule.Frequency)
	if !ok {
		return rrule.ROption{}, false
	}
	opt := rrule.ROption{Freq: freq, Dtstart: dtstart}

	opt.Interval = int(rule.Interval)
	if opt.Interval == 0 {
		opt.Interval = 1
	}
	if rule.Count > 0 {
		opt.Count = int(rule.Count)
	}
	if rule.Until != "" {
		if u, ok := parseRFC3339(rule.Until); ok {
			opt.Until = u
		} else if u, ok := parseFloatingDateTime(rule.Until); ok {
			opt.Until = u
		}
	}
	if rule.FirstDayOfWeek != "" {
		if wd, ok := jsWeekdayToRRule(&jmap.NDay{Day: rule.FirstDayOfWeek}); ok {
			opt.Wkst = wd
		}
	}
	for _, nd := range rule.ByDay {
		if wd, ok := jsWeekdayToRRule(nd); ok {
			opt.Byweekday = append(opt.Byweekday, wd)
		}
	}
	opt.Bymonthday = append(opt.Bymonthday, rule.ByMonthDay...)
	opt.Byyearday = append(opt.Byyearday, rule.ByYearDay...)
	opt.Byweekno = append(opt.Byweekno, rule.ByWeekNo...)
	opt.Bysetpos = append(opt.Bysetpos, rule.BySetPosition...)
	for _, m := range rule.ByMonth {
		if n, ok := jsMonthToRRule(m); ok {
			opt.Bymonth = append(opt.Bymonth, n)
		}
	}
	for _, h := range rule.ByHour {
		opt.Byhour = append(opt.Byhour, int(h))
	}
	for _, mn := range rule.ByMinute {
		opt.Byminute = append(opt.Byminute, int(mn))
	}
	for _, s := range rule.BySecond {
		opt.Bysecond = append(opt.Bysecond, int(s))
	}
	return opt, true
}

// overrideStart returns the effective start time carried by a recurrenceOverrides patch,
// which may relocate the instance via a "start" property.
func overrideStart(recID string, patch map[string]any) (time.Time, bool) {
	if patch != nil {
		if s, ok := patch["start"].(string); ok && s != "" {
			if t, ok := parseRFC3339(s); ok {
				return t, true
			}
			if t, ok := parseFloatingDateTime(s); ok {
				return t, true
			}
		}
	}
	if t, ok := parseRFC3339(recID); ok {
		return t, true
	}
	if t, ok := parseFloatingDateTime(recID); ok {
		return t, true
	}
	return time.Time{}, false
}

// ExpandRecurrenceInstances expands an event's recurrenceRules over [start, horizon],
// honouring every RFC 8984 Section 4.3 mechanism: byX parts, bySetPosition, interval,
// count/until, excludedRecurrenceRules (as EXRULEs), the legacy per-instance "excluded"
// map, and recurrenceOverrides (relocated instances via "start", and instances removed
// with "excluded":true). Instances are de-duplicated and returned in chronological order.
func ExpandRecurrenceInstances(ev *jmap.CalendarEvent, horizon time.Time) []RecurrenceInstance {
	start, ok := parseRFC3339(ev.Start)
	if !ok {
		if start, ok = parseFloatingDateTime(ev.Start); !ok {
			return nil
		}
	}

	duration := time.Duration(0)
	if ev.Duration != "" {
		if d, ok := parseISODuration(ev.Duration); ok {
			duration = d
		}
	}

	masterInst := RecurrenceInstance{
		RecurrenceID: ev.Start,
		Start:        start,
		End:          start.Add(duration),
	}

	hasRules := false
	for _, r := range ev.RecurrenceRules {
		if r != nil {
			hasRules = true
			break
		}
	}
	if !hasRules && len(ev.RecurrenceOverrides) == 0 {
		return []RecurrenceInstance{masterInst}
	}

	// Bound the expansion window so unbounded rules terminate.
	if horizon.IsZero() {
		horizon = start.AddDate(5, 0, 0)
	}

	// starts collects the recurrence-id start times of generated instances, keyed by the
	// canonical RFC3339 recurrence id so overrides and exclusions can be matched.
	starts := make(map[string]time.Time)

	if hasRules {
		set := &rrule.Set{}
		set.DTStart(start)
		for _, rule := range ev.RecurrenceRules {
			opt, ok := buildRRuleOption(rule, start)
			if !ok {
				continue
			}
			rr, err := rrule.NewRRule(opt)
			if err != nil {
				continue
			}
			set.RRule(rr)
		}
		// This rrule-go version has no EXRULE; materialize excludedRecurrenceRules
		// (RFC 8984 Section 4.3.4) into EXDATEs over the expansion window.
		for _, rule := range ev.ExcludedRecurrenceRules {
			opt, ok := buildRRuleOption(rule, start)
			if !ok {
				continue
			}
			rr, err := rrule.NewRRule(opt)
			if err != nil {
				continue
			}
			for i, t := range rr.Between(start.Add(-time.Second), horizon, true) {
				if i >= 5000 {
					break
				}
				set.ExDate(t)
			}
		}
		// Between is inclusive; cap at a large occurrence count as a safety valve.
		occ := set.Between(start.Add(-time.Second), horizon, true)
		for i, t := range occ {
			if i >= 5000 {
				break
			}
			starts[t.UTC().Format(time.RFC3339)] = t
		}
	} else {
		starts[start.UTC().Format(time.RFC3339)] = start
	}

	// recurrenceOverrides may add instances that the rule would not generate.
	for recID := range ev.RecurrenceOverrides {
		if _, present := starts[recID]; present {
			continue
		}
		if t, ok := overrideStart(recID, ev.RecurrenceOverrides[recID]); ok {
			starts[t.UTC().Format(time.RFC3339)] = t
		}
	}

	results := make([]RecurrenceInstance, 0, len(starts))
	for recID, t := range starts {
		// Legacy per-instance exclusion map (RFC 8984 Section 4.3.6).
		if ev.Excluded != nil && ev.Excluded[recID] {
			continue
		}
		instStart := t
		instDur := duration
		if ov, ok := ev.RecurrenceOverrides[recID]; ok {
			if excluded, _ := ov["excluded"].(bool); excluded {
				continue
			}
			if s, ok := overrideStart(recID, ov); ok {
				instStart = s
			}
			if ds, ok := ov["duration"].(string); ok && ds != "" {
				if d, ok := parseISODuration(ds); ok {
					instDur = d
				}
			}
		}
		results = append(results, RecurrenceInstance{
			RecurrenceID: recID,
			Start:        instStart,
			End:          instStart.Add(instDur),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if !results[i].Start.Equal(results[j].Start) {
			return results[i].Start.Before(results[j].Start)
		}
		return results[i].RecurrenceID < results[j].RecurrenceID
	})

	if len(results) == 0 {
		return []RecurrenceInstance{masterInst}
	}
	return results
}

// eventEndsAfter matches when the event (or any recurrence) ends after the date.
func eventEndsAfter(ev *jmap.CalendarEvent, date string) bool {
	ref, ok := parseRFC3339(date)
	if !ok {
		return false
	}
	instances := ExpandRecurrenceInstances(ev, ref.AddDate(1, 0, 0))
	for _, inst := range instances {
		if inst.End.After(ref) {
			return true
		}
	}
	return false
}

// eventStartsBefore matches when the event (or any recurrence) starts before the date.
func eventStartsBefore(ev *jmap.CalendarEvent, date string) bool {
	ref, ok := parseRFC3339(date)
	if !ok {
		return false
	}
	instances := ExpandRecurrenceInstances(ev, ref)
	for _, inst := range instances {
		if inst.Start.Before(ref) {
			return true
		}
	}
	return false
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

func sortCalendarEvents(events []*jmap.CalendarEvent, comparators []jmap.Comparator) {
	sort.SliceStable(events, func(i, j int) bool {
		for _, comp := range comparators {
			var cmp int
			switch comp.Property {
			case "start":
				cmp = strings.Compare(events[i].Start, events[j].Start)
			case "uid":
				cmp = strings.Compare(events[i].UID, events[j].UID)
			case "recurrenceId":
				cmp = strings.Compare(events[i].RecurrenceID, events[j].RecurrenceID)
			case "created":
				cmp = strings.Compare(events[i].Created, events[j].Created)
			case "updated":
				cmp = strings.Compare(events[i].Updated, events[j].Updated)
			case "title":
				cmp = strings.Compare(strings.ToLower(events[i].Title), strings.ToLower(events[j].Title))
			}
			if cmp != 0 {
				if comp.IsAscending {
					return cmp < 0
				}
				return cmp > 0
			}
		}
		if events[i].Start != events[j].Start {
			return events[i].Start < events[j].Start
		}
		return events[i].ID < events[j].ID
	})
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
func (b *MemoryCalendarsBackend) ParticipantIdentityState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.identityState.State()
}

// ParticipantIdentityChanges returns created/updated/destroyed ParticipantIdentities since the given state.
func (b *MemoryCalendarsBackend) ParticipantIdentityChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.identityState.Changes(sinceState)
}

func (b *MemoryCalendarsBackend) GetParticipantIdentities(ctx context.Context, ids []jmap.Id) ([]*jmap.ParticipantIdentity, []jmap.Id, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.ParticipantIdentity
	var notFound []jmap.Id
	if len(ids) == 0 {
		for _, pi := range us.identities {
			list = append(list, pi)
		}
		return list, nil, nil
	}
	for _, id := range ids {
		if pi, ok := us.identities[id]; ok {
			list = append(list, pi)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryCalendarsBackend) GetAllParticipantIdentities(ctx context.Context) ([]*jmap.ParticipantIdentity, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.ParticipantIdentity
	for _, pi := range us.identities {
		list = append(list, pi)
	}
	return list, nil
}

func (b *MemoryCalendarsBackend) CreateParticipantIdentity(ctx context.Context, pi *jmap.ParticipantIdentity) (*jmap.ParticipantIdentity, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if pi.ID == "" {
		b.nextID++
		pi.ID = jmap.Id(fmt.Sprintf("pi-%d", b.nextID))
	}
	if pi.SendTo == nil {
		pi.SendTo = make(map[string]string)
	}
	if pi.IsDefault {
		for _, other := range us.identities {
			other.IsDefault = false
		}
	}
	us.identities[pi.ID] = pi
	b.recordChange(ctx, us.identityState, pi.ID, "create", "ParticipantIdentity")
	return pi, nil
}

func (b *MemoryCalendarsBackend) UpdateParticipantIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.ParticipantIdentity, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	pi, ok := us.identities[id]
	if !ok {
		return nil, fmt.Errorf("participant identity not found: %s", id)
	}
	if name, ok := patch["name"].(string); ok {
		pi.Name = name
	}
	if calAddr, ok := patch["calendarAddress"].(string); ok {
		pi.CalendarAddress = calAddr
	}
	if sendTo, ok := patch["sendTo"].(map[string]any); ok {
		converted := make(map[string]string, len(sendTo))
		for k, v := range sendTo {
			if s, ok := v.(string); ok {
				converted[k] = s
			}
		}
		pi.SendTo = converted
	}
	if isDefault, ok := patch["isDefault"].(bool); ok && isDefault {
		for _, other := range us.identities {
			if other.ID != pi.ID {
				other.IsDefault = false
			}
		}
		pi.IsDefault = true
	}
	b.recordChange(ctx, us.identityState, id, "update", "ParticipantIdentity")
	return pi, nil
}

func (b *MemoryCalendarsBackend) DeleteParticipantIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	pi, ok := us.identities[id]
	if !ok {
		return false, nil
	}
	wasDefault := pi.IsDefault
	delete(us.identities, id)
	// Keep the "SHOULD be exactly one default" invariant: if the default identity was
	// destroyed, promote another identity to be the default.
	if wasDefault {
		for _, other := range us.identities {
			other.IsDefault = true
			b.recordChange(ctx, us.identityState, other.ID, "update", "ParticipantIdentity")
			break
		}
	}
	b.recordChange(ctx, us.identityState, id, "destroy", "ParticipantIdentity")
	return true, nil
}

func (b *MemoryCalendarsBackend) SetDefaultParticipantIdentity(ctx context.Context, id jmap.Id) error {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.identities[id]; !ok {
		return fmt.Errorf("participant identity not found: %s", id)
	}
	for _, pi := range us.identities {
		want := pi.ID == id
		if pi.IsDefault != want {
			pi.IsDefault = want
			b.recordChange(ctx, us.identityState, pi.ID, "update", "ParticipantIdentity")
		}
	}
	return nil
}

// --- CalendarEventNotifications (draft-ietf-jmap-calendars Section 7) ---

// CalendarEventNotificationState returns the current change state token for notifications.
func (b *MemoryCalendarsBackend) CalendarEventNotificationState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.notificationState.State()
}

// CalendarEventNotificationChanges returns created/updated/destroyed notifications since the given state.
func (b *MemoryCalendarsBackend) CalendarEventNotificationChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.notificationState.Changes(sinceState)
}

func (b *MemoryCalendarsBackend) GetCalendarEventNotifications(ctx context.Context, ids []jmap.Id) ([]*jmap.CalendarEventNotification, []jmap.Id, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEventNotification
	var notFound []jmap.Id
	if len(ids) == 0 {
		for _, n := range us.notifications {
			list = append(list, n)
		}
		return list, nil, nil
	}
	for _, id := range ids {
		if n, ok := us.notifications[id]; ok {
			list = append(list, n)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryCalendarsBackend) GetAllCalendarEventNotifications(ctx context.Context) ([]*jmap.CalendarEventNotification, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEventNotification
	for _, n := range us.notifications {
		list = append(list, n)
	}
	return list, nil
}

func (b *MemoryCalendarsBackend) CreateCalendarEventNotification(ctx context.Context, n *jmap.CalendarEventNotification) (*jmap.CalendarEventNotification, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if n.ID == "" {
		b.nextID++
		n.ID = jmap.Id(fmt.Sprintf("not-%d", b.nextID))
	}
	if n.Created == "" {
		n.Created = time.Now().UTC().Format(time.RFC3339)
	}
	us.notifications[n.ID] = n
	us.notificationSeq[n.ID] = b.nextID
	b.recordChange(ctx, us.notificationState, n.ID, "create", "CalendarEventNotification")
	return n, nil
}

func (b *MemoryCalendarsBackend) DeleteCalendarEventNotification(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.notifications[id]; !ok {
		return false, nil
	}
	delete(us.notifications, id)
	delete(us.notificationSeq, id)
	b.recordChange(ctx, us.notificationState, id, "destroy", "CalendarEventNotification")
	return true, nil
}

// MatchCalendarEventNotification reports whether a notification satisfies a
// CalendarEventNotification filter condition per draft-ietf-jmap-calendars Section 7.6.1.
func MatchCalendarEventNotification(n *jmap.CalendarEventNotification, filter map[string]any) bool {
	for k, v := range filter {
		switch k {
		case "after":
			s, _ := v.(string)
			if s != "" && n.Created < s {
				return false
			}
		case "before":
			s, _ := v.(string)
			if s != "" && n.Created >= s {
				return false
			}
		case "type":
			s, _ := v.(string)
			if n.Type != s {
				return false
			}
		case "calendarEventIds":
			raw, _ := v.([]any)
			matched := false
			for _, item := range raw {
				if idStr, ok := item.(string); ok && jmap.Id(idStr) == n.CalendarEventID {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	}
	return true
}

func (b *MemoryCalendarsBackend) QueryCalendarEventNotifications(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var matched []*jmap.CalendarEventNotification
	for _, n := range us.notifications {
		if MatchCalendarEventNotification(n, filter) {
			matched = append(matched, n)
		}
	}

	// The "created" property is the only supported sort per Section 7.6.2; the default
	// order is newest first so clients always see the latest change at the top. When
	// created timestamps tie (second precision), the monotonic notification sequence
	// breaks the tie so the default order remains deterministic.
	sort.SliceStable(matched, func(i, j int) bool {
		for _, comp := range comparators {
			if comp.Property != "created" {
				continue
			}
			cmp := strings.Compare(matched[i].Created, matched[j].Created)
			if cmp != 0 {
				if comp.IsAscending {
					return cmp < 0
				}
				return cmp > 0
			}
		}
		if matched[i].Created != matched[j].Created {
			return matched[i].Created > matched[j].Created
		}
		return us.notificationSeq[matched[i].ID] > us.notificationSeq[matched[j].ID]
	})

	resultIDs := make([]jmap.Id, 0, len(matched))
	for _, n := range matched {
		resultIDs = append(resultIDs, n.ID)
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
