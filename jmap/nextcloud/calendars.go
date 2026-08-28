package nextcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// CalendarsBackend implements jmap.CalendarsBackend backed by Nextcloud CalDAV via github.com/emersion/go-webdav/caldav.
type CalendarsBackend struct {
	client      *Client
	mu          sync.RWMutex
	broadcaster *jmap.Broadcaster

	calStates          map[string]int
	eventStates        map[string]int
	identityStates     map[string]int
	notificationStates map[string]int

	calsCache          map[string][]*jmap.Calendar
	calsCacheTime      map[string]time.Time
	eventsCache        map[string]map[jmap.Id]*jmap.CalendarEvent
	eventsCacheTime    map[string]time.Time
	identitiesCache    map[string]map[jmap.Id]*jmap.ParticipantIdentity
	notificationsCache map[string]map[jmap.Id]*jmap.CalendarEventNotification
}

var _ jmap.CalendarsBackend = (*CalendarsBackend)(nil)

// NewCalendarsBackend initializes a new Nextcloud-backed CalendarsBackend.
func NewCalendarsBackend(client *Client) *CalendarsBackend {
	return &CalendarsBackend{
		client:             client,
		calStates:          make(map[string]int),
		eventStates:        make(map[string]int),
		identityStates:     make(map[string]int),
		notificationStates: make(map[string]int),
		calsCache:          make(map[string][]*jmap.Calendar),
		calsCacheTime:      make(map[string]time.Time),
		eventsCache:        make(map[string]map[jmap.Id]*jmap.CalendarEvent),
		eventsCacheTime:    make(map[string]time.Time),
		identitiesCache:    make(map[string]map[jmap.Id]*jmap.ParticipantIdentity),
		notificationsCache: make(map[string]map[jmap.Id]*jmap.CalendarEventNotification),
	}
}

func (b *CalendarsBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *CalendarsBackend) emitStateChange(accountID, typeName, newState string) {
	b.mu.RLock()
	bc := b.broadcaster
	b.mu.RUnlock()
	if bc != nil {
		bc.PublishStateChange(accountID, typeName, newState)
	}
}

func (b *CalendarsBackend) user(ctx context.Context) string {
	u, _ := b.client.getUserAndPass(ctx)
	return u
}

// CalendarState
func (b *CalendarsBackend) CalendarState(ctx context.Context) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.user(ctx)
	st, ok := b.calStates[u]
	if !ok {
		st = 1
		b.calStates[u] = st
	}
	return strconv.Itoa(st)
}

func (b *CalendarsBackend) CalendarChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMoreChanges bool) {
	cur := b.CalendarState(ctx)
	if sinceState == cur {
		return nil, nil, nil, cur, false
	}
	cals, _ := b.GetAllCalendars(ctx)
	for _, c := range cals {
		created = append(created, c.ID)
	}
	return created, nil, nil, cur, false
}

func (b *CalendarsBackend) GetAllCalendars(ctx context.Context) ([]*jmap.Calendar, error) {
	list, _, err := b.GetCalendars(ctx, nil)
	return list, err
}

func filterCalendars(list []*jmap.Calendar, ids []jmap.Id) ([]*jmap.Calendar, []jmap.Id, error) {
	if len(ids) == 0 {
		return list, []jmap.Id{}, nil
	}
	idMap := make(map[jmap.Id]bool, len(ids))
	for _, id := range ids {
		idMap[id] = true
	}
	var filtered []*jmap.Calendar
	foundMap := make(map[jmap.Id]bool, len(list))
	for _, c := range list {
		if idMap[c.ID] {
			filtered = append(filtered, c)
			foundMap[c.ID] = true
		}
	}
	var notFound []jmap.Id
	for _, id := range ids {
		if !foundMap[id] {
			notFound = append(notFound, id)
		}
	}
	if notFound == nil {
		notFound = []jmap.Id{}
	}
	return filtered, notFound, nil
}

func (b *CalendarsBackend) GetCalendars(ctx context.Context, ids []jmap.Id) ([]*jmap.Calendar, []jmap.Id, error) {
	u := b.user(ctx)

	b.mu.RLock()
	cachedCals, hasCached := b.calsCache[u]
	cacheAge := time.Since(b.calsCacheTime[u])
	b.mu.RUnlock()

	if hasCached && len(cachedCals) > 0 && cacheAge < 10*time.Second {
		return filterCalendars(cachedCals, ids)
	}

	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	homeSet := "calendars/" + u + "/"
	calList, err := calClient.FindCalendars(ctx, homeSet)
	if err != nil {
		// Fallback default personal calendar
		defaultCal := &jmap.Calendar{
			ID:        jmap.Id("personal"),
			Name:      "Personal Calendar",
			IsVisible: true,
			IsDefault: true,
			SortOrder: 0,
			MyRights:  jmap.FullCalendarRights(),
		}
		b.mu.Lock()
		b.calsCache[u] = []*jmap.Calendar{defaultCal}
		b.calsCacheTime[u] = time.Now()
		b.mu.Unlock()
		return filterCalendars([]*jmap.Calendar{defaultCal}, ids)
	}

	var list []*jmap.Calendar
	for _, c := range calList {
		calID := path.Base(strings.TrimRight(c.Path, "/"))
		if calID == "inbox" || calID == "outbox" || calID == "trashbin" {
			continue
		}

		name := c.Name
		if name == "" || strings.EqualFold(name, "Personal") {
			name = "Personal Calendar"
		}

		isDefault := calID == "personal" || strings.EqualFold(name, "Personal") || strings.EqualFold(name, "Personal Calendar")
		list = append(list, &jmap.Calendar{
			ID:        jmap.Id(calID),
			Name:      name,
			IsVisible: true,
			IsDefault: isDefault,
			SortOrder: 0,
			MyRights:  jmap.FullCalendarRights(),
		})
	}

	if len(list) == 0 {
		list = append(list, &jmap.Calendar{
			ID:        jmap.Id("personal"),
			Name:      "Personal Calendar",
			IsVisible: true,
			IsDefault: true,
			SortOrder: 0,
			MyRights:  jmap.FullCalendarRights(),
		})
	}

	b.mu.Lock()
	b.calsCache[u] = list
	b.calsCacheTime[u] = time.Now()
	b.mu.Unlock()

	return filterCalendars(list, ids)
}

func (b *CalendarsBackend) CreateCalendar(ctx context.Context, cal *jmap.Calendar) (*jmap.Calendar, error) {
	if cal == nil {
		return nil, fmt.Errorf("calendar is nil")
	}
	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, err
	}

	if cal.ID == "" {
		cal.ID = jmap.Id(fmt.Sprintf("cal-%d", time.Now().UnixNano()))
	}

	calPath := fmt.Sprintf("calendars/%s/%s/", u, cal.ID)
	_ = calClient.Mkdir(ctx, calPath)

	b.mu.Lock()
	if b.calsCache[u] != nil {
		b.calsCache[u] = append(b.calsCache[u], cal)
	}
	b.calsCacheTime[u] = time.Now()
	b.calStates[u]++
	st := strconv.Itoa(b.calStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	return cal, nil
}

func (b *CalendarsBackend) UpdateCalendar(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Calendar, error) {
	u := b.user(ctx)
	b.mu.Lock()
	b.calsCacheTime[u] = time.Time{}
	b.calStates[u]++
	st := strconv.Itoa(b.calStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)

	cals, _, _ := b.GetCalendars(ctx, []jmap.Id{id})
	if len(cals) > 0 {
		return cals[0], nil
	}
	name, _ := patch["name"].(string)
	return &jmap.Calendar{ID: id, Name: name, MyRights: jmap.FullCalendarRights()}, nil
}

func (b *CalendarsBackend) DeleteCalendar(ctx context.Context, id jmap.Id) (bool, error) {
	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return false, err
	}

	calPath := fmt.Sprintf("calendars/%s/%s/", u, id)
	_ = calClient.RemoveAll(ctx, calPath)

	b.mu.Lock()
	if b.calsCache[u] != nil {
		var filtered []*jmap.Calendar
		for _, c := range b.calsCache[u] {
			if c.ID != id {
				filtered = append(filtered, c)
			}
		}
		b.calsCache[u] = filtered
	}
	b.calsCacheTime[u] = time.Now()
	b.calStates[u]++
	st := strconv.Itoa(b.calStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	return true, nil
}

func (b *CalendarsBackend) SetDefaultCalendar(ctx context.Context, id jmap.Id) error {
	return nil
}

func (b *CalendarsBackend) CalendarHasEvents(ctx context.Context, id jmap.Id) (bool, error) {
	events, _, err := b.GetCalendarEvents(ctx, nil)
	if err != nil {
		return false, err
	}
	for _, ev := range events {
		if ev.CalendarIDs[id] {
			return true, nil
		}
	}
	return false, nil
}

// CalendarEventState
func (b *CalendarsBackend) CalendarEventState(ctx context.Context) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.user(ctx)
	st, ok := b.eventStates[u]
	if !ok {
		st = 1
		b.eventStates[u] = st
	}
	return strconv.Itoa(st)
}

func (b *CalendarsBackend) CalendarEventChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMoreChanges bool) {
	cur := b.CalendarEventState(ctx)
	if sinceState == cur {
		return nil, nil, nil, cur, false
	}
	events, _ := b.GetAllCalendarEvents(ctx)
	for _, ev := range events {
		created = append(created, ev.ID)
	}
	return created, nil, nil, cur, false
}

func (b *CalendarsBackend) GetAllCalendarEvents(ctx context.Context) ([]*jmap.CalendarEvent, error) {
	evs, _, err := b.GetCalendarEvents(ctx, nil)
	return evs, err
}

func (b *CalendarsBackend) buildEventResponseFromCache(u string, ids []jmap.Id) ([]*jmap.CalendarEvent, []jmap.Id) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEvent
	var notFound []jmap.Id
	if len(ids) == 0 {
		for _, ev := range b.eventsCache[u] {
			list = append(list, ev)
		}
	} else {
		for _, id := range ids {
			if ev, ok := b.eventsCache[u][id]; ok {
				list = append(list, ev)
			} else if strings.Contains(string(id), "#") {
				parts := strings.SplitN(string(id), "#", 2)
				masterID := jmap.Id(parts[0])
				recID := parts[1]
				if master, okMaster := b.eventsCache[u][masterID]; okMaster {
					if master.Excluded != nil && master.Excluded[recID] {
						notFound = append(notFound, id)
						continue
					}
					if override, okOv := master.RecurrenceOverrides[recID]; okOv {
						if ex, _ := override["excluded"].(bool); ex {
							notFound = append(notFound, id)
							continue
						}
					}
					inst := *master
					inst.ID = id
					inst.Start = recID
					inst.RecurrenceID = recID
					inst.RecurrenceRules = nil
					inst.ExcludedRecurrenceRules = nil
					if override, okOv := master.RecurrenceOverrides[recID]; okOv {
						_ = applyEventPatch(&inst, override)
					}
					list = append(list, &inst)
				} else {
					notFound = append(notFound, id)
				}
			} else {
				notFound = append(notFound, id)
			}
		}
	}
	if notFound == nil {
		notFound = []jmap.Id{}
	}
	return list, notFound
}

func (b *CalendarsBackend) GetCalendarEvents(ctx context.Context, ids []jmap.Id) ([]*jmap.CalendarEvent, []jmap.Id, error) {
	u := b.user(ctx)

	b.mu.RLock()
	cache := b.eventsCache[u]
	cacheAge := time.Since(b.eventsCacheTime[u])
	b.mu.RUnlock()

	// Check if all requested IDs can be satisfied immediately from cache
	canServeFromCache := false
	if cache != nil {
		if len(ids) == 0 && cacheAge < 5*time.Second {
			canServeFromCache = true
		} else if len(ids) > 0 {
			canServeFromCache = true
			for _, id := range ids {
				masterID := id
				if strings.Contains(string(id), "#") {
					parts := strings.SplitN(string(id), "#", 2)
					masterID = jmap.Id(parts[0])
				}
				if _, ok := cache[masterID]; !ok {
					canServeFromCache = false
					break
				}
			}
		}
	}

	if canServeFromCache {
		list, notFound := b.buildEventResponseFromCache(u, ids)
		return list, notFound, nil
	}

	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	b.mu.Lock()
	if b.eventsCache[u] == nil {
		b.eventsCache[u] = make(map[jmap.Id]*jmap.CalendarEvent)
	}
	b.mu.Unlock()

	cals, _, _ := b.GetCalendars(ctx, nil)

	for _, cal := range cals {
		calPath := fmt.Sprintf("calendars/%s/%s/", u, cal.ID)
		fis, fErr := calClient.ReadDir(ctx, calPath, false)
		if fErr != nil {
			continue
		}

		for _, fi := range fis {
			name := path.Base(fi.Path)
			if !strings.HasSuffix(name, ".ics") {
				continue
			}

			rawID := strings.TrimSuffix(name, ".ics")
			evID := jmap.Id(rawID)

			itemPath := calPath + name
			calObj, gErr := calClient.GetCalendarObject(ctx, itemPath)
			if gErr != nil || calObj == nil || calObj.Data == nil {
				continue
			}

			var buf bytes.Buffer
			_ = ical.NewEncoder(&buf).Encode(calObj.Data)

			parsedList, pErr := jmap.ParseICalendar(buf.Bytes())
			if pErr == nil && len(parsedList) > 0 {
				ev := parsedList[0]
				ev.ID = evID
				if ev.CalendarIDs == nil {
					ev.CalendarIDs = make(map[jmap.Id]bool)
				}
				ev.CalendarIDs[cal.ID] = true

				b.mu.Lock()
				b.eventsCache[u][evID] = ev
				b.mu.Unlock()
			}
		}
	}

	b.mu.Lock()
	b.eventsCacheTime[u] = time.Now()
	b.mu.Unlock()

	list, notFound := b.buildEventResponseFromCache(u, ids)
	return list, notFound, nil
}

func (b *CalendarsBackend) CreateCalendarEvent(ctx context.Context, event *jmap.CalendarEvent) (*jmap.CalendarEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}
	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, err
	}

	if event.ID == "" {
		event.ID = jmap.Id(fmt.Sprintf("event-%d", time.Now().UnixNano()))
	}
	if event.UID == "" {
		event.UID = string(event.ID)
	}

	// Ensure user's calendars are discovered and initialized in Nextcloud
	cals, _, _ := b.GetCalendars(ctx, nil)

	calID := ""
	if len(event.CalendarIDs) > 0 {
		for cid := range event.CalendarIDs {
			if cid != "cal-default" && cid != "" {
				calID = string(cid)
				break
			}
		}
	}
	if calID == "" {
		if len(cals) > 0 {
			calID = string(cals[0].ID)
		} else {
			calID = "personal"
		}
		if event.CalendarIDs == nil {
			event.CalendarIDs = make(map[jmap.Id]bool)
		}
		delete(event.CalendarIDs, "cal-default")
		event.CalendarIDs[jmap.Id(calID)] = true
	}

	calPath := fmt.Sprintf("calendars/%s/%s/", u, calID)
	_ = calClient.Mkdir(ctx, calPath)

	icsStr := jmap.EncodeCalDAVEvent(event)
	dec := ical.NewDecoder(strings.NewReader(icsStr))
	calObj, decErr := dec.Decode()
	if decErr != nil {
		return nil, fmt.Errorf("failed to decode icalendar: %w", decErr)
	}

	eventPath := fmt.Sprintf("calendars/%s/%s/%s.ics", u, calID, event.ID)
	_, putErr := calClient.PutCalendarObject(ctx, eventPath, calObj)
	if putErr != nil {
		return nil, fmt.Errorf("failed to put calendar object via caldav client: %w", putErr)
	}

	b.mu.Lock()
	if b.eventsCache[u] == nil {
		b.eventsCache[u] = make(map[jmap.Id]*jmap.CalendarEvent)
	}
	b.eventsCache[u][event.ID] = event
	b.eventsCacheTime[u] = time.Now()
	b.eventStates[u]++
	st := strconv.Itoa(b.eventStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEvent", st)
	return event, nil
}

func setNestedMapValue(m map[string]any, parts []string, val any) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		if val == nil {
			delete(m, parts[0])
		} else {
			m[parts[0]] = val
		}
		return
	}
	key := parts[0]
	sub, ok := m[key].(map[string]any)
	if !ok {
		if val == nil {
			return
		}
		sub = make(map[string]any)
		m[key] = sub
	}
	setNestedMapValue(sub, parts[1:], val)
}

func applyEventPatch(ev *jmap.CalendarEvent, patch map[string]any) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if m == nil {
		m = make(map[string]any)
	}

	for path, val := range patch {
		cleanPath := strings.TrimPrefix(path, "/")
		parts := strings.Split(cleanPath, "/")
		setNestedMapValue(m, parts, val)
	}

	rawUpdated, err := json.Marshal(m)
	if err != nil {
		return err
	}

	origID := ev.ID
	origUID := ev.UID
	origCalIDs := ev.CalendarIDs
	var updatedEv jmap.CalendarEvent
	if err := json.Unmarshal(rawUpdated, &updatedEv); err != nil {
		return err
	}
	updatedEv.ID = origID
	if updatedEv.UID == "" {
		updatedEv.UID = origUID
	}

	if cid, ok := m["calendarId"].(string); ok && cid != "" {
		if updatedEv.CalendarIDs == nil {
			updatedEv.CalendarIDs = make(map[jmap.Id]bool)
		}
		updatedEv.CalendarIDs[jmap.Id(cid)] = true
	}

	if len(updatedEv.CalendarIDs) == 0 {
		if _, hasCalIds := m["calendarIds"]; !hasCalIds {
			if _, hasCalId := m["calendarId"]; !hasCalId {
				updatedEv.CalendarIDs = origCalIDs
			}
		}
	}

	if allDay, ok := m["allDay"].(bool); ok {
		updatedEv.ShowWithoutTime = allDay
	}
	if sum, ok := m["summary"].(string); ok && sum != "" && updatedEv.Title == "" {
		updatedEv.Title = sum
	}
	if end, ok := m["end"].(string); ok && end != "" {
		if updatedEv.Start != "" {
			updatedEv.Duration = jmap.IcalDurationBetween(updatedEv.Start, end)
		}
	}
	if rrule, hasRrule := m["recurrenceRule"]; hasRrule {
		if rrule == nil {
			updatedEv.RecurrenceRules = nil
			updatedEv.RecurrenceRule = nil
		} else if rruleBytes, err := json.Marshal(rrule); err == nil {
			var r jmap.JSCalendarRecurrenceRule
			if err := json.Unmarshal(rruleBytes, &r); err == nil {
				updatedEv.RecurrenceRule = &r
				updatedEv.RecurrenceRules = []*jmap.JSCalendarRecurrenceRule{&r}
			}
		}
	}
	if exrule, hasExrule := m["excludedRecurrenceRule"]; hasExrule {
		if exrule == nil {
			updatedEv.ExcludedRecurrenceRules = nil
			updatedEv.ExcludedRecurrenceRule = nil
		} else if exruleBytes, err := json.Marshal(exrule); err == nil {
			var r jmap.JSCalendarRecurrenceRule
			if err := json.Unmarshal(exruleBytes, &r); err == nil {
				updatedEv.ExcludedRecurrenceRule = &r
				updatedEv.ExcludedRecurrenceRules = []*jmap.JSCalendarRecurrenceRule{&r}
			}
		}
	}
	if len(updatedEv.RecurrenceRules) > 0 && updatedEv.RecurrenceRule == nil {
		updatedEv.RecurrenceRule = updatedEv.RecurrenceRules[0]
	}
	if len(updatedEv.ExcludedRecurrenceRules) > 0 && updatedEv.ExcludedRecurrenceRule == nil {
		updatedEv.ExcludedRecurrenceRule = updatedEv.ExcludedRecurrenceRules[0]
	}
	if updatedEv.TimeZone == "" {
		updatedEv.TimeZone = "Etc/UTC"
	}
	updatedEv.Start = strings.TrimSuffix(updatedEv.Start, "Z")

	*ev = updatedEv
	return nil
}

func (b *CalendarsBackend) UpdateCalendarEvent(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.CalendarEvent, error) {
	if strings.Contains(string(id), "#") {
		parts := strings.SplitN(string(id), "#", 2)
		masterID := jmap.Id(parts[0])
		recID := parts[1]
		masters, _, err := b.GetCalendarEvents(ctx, []jmap.Id{masterID})
		if err != nil || len(masters) == 0 {
			return nil, fmt.Errorf("master event %s not found", masterID)
		}
		master := masters[0]
		if master.RecurrenceOverrides == nil {
			master.RecurrenceOverrides = make(map[string]map[string]any)
		}
		overridePatch, exists := master.RecurrenceOverrides[recID]
		if !exists || overridePatch == nil {
			overridePatch = make(map[string]any)
		}
		for k, v := range patch {
			overridePatch[strings.TrimPrefix(k, "/")] = v
		}
		master.RecurrenceOverrides[recID] = overridePatch
		master.Sequence++
		master.Updated = time.Now().UTC().Format(time.RFC3339)
		return b.CreateCalendarEvent(ctx, master)
	}

	events, _, err := b.GetCalendarEvents(ctx, []jmap.Id{id})
	if err != nil || len(events) == 0 {
		return nil, fmt.Errorf("event %s not found", id)
	}
	ev := events[0]
	oldCalID := ""
	for cid := range ev.CalendarIDs {
		oldCalID = string(cid)
		break
	}

	if err := applyEventPatch(ev, patch); err != nil {
		return nil, err
	}
	ev.Sequence++
	ev.Updated = time.Now().UTC().Format(time.RFC3339)

	newCalID := ""
	for cid := range ev.CalendarIDs {
		newCalID = string(cid)
		break
	}
	if oldCalID != "" && newCalID != "" && oldCalID != newCalID {
		calClient, u, cErr := b.client.CalDAV(ctx)
		if cErr == nil {
			oldPath := fmt.Sprintf("calendars/%s/%s/%s.ics", u, oldCalID, id)
			_ = calClient.RemoveAll(ctx, oldPath)
		}
	}

	return b.CreateCalendarEvent(ctx, ev)
}

func (b *CalendarsBackend) DeleteCalendarEvent(ctx context.Context, id jmap.Id) (bool, error) {
	if strings.Contains(string(id), "#") {
		parts := strings.SplitN(string(id), "#", 2)
		masterID := jmap.Id(parts[0])
		recID := parts[1]
		masters, _, err := b.GetCalendarEvents(ctx, []jmap.Id{masterID})
		if err == nil && len(masters) > 0 {
			master := masters[0]
			if master.RecurrenceOverrides == nil {
				master.RecurrenceOverrides = make(map[string]map[string]any)
			}
			if master.Excluded == nil {
				master.Excluded = make(map[string]bool)
			}
			master.Excluded[recID] = true
			master.RecurrenceOverrides[recID] = map[string]any{"excluded": true}
			master.Sequence++
			master.Updated = time.Now().UTC().Format(time.RFC3339)
			_, _ = b.CreateCalendarEvent(ctx, master)
			return true, nil
		}
		return false, nil
	}

	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return false, err
	}

	events, _, _ := b.GetCalendarEvents(ctx, []jmap.Id{id})
	calID := "personal"
	if len(events) > 0 {
		for cid := range events[0].CalendarIDs {
			calID = string(cid)
			break
		}
	}

	eventPath := fmt.Sprintf("calendars/%s/%s/%s.ics", u, calID, id)
	_ = calClient.RemoveAll(ctx, eventPath)

	b.mu.Lock()
	if b.eventsCache[u] != nil {
		delete(b.eventsCache[u], id)
	}
	b.eventsCacheTime[u] = time.Now()
	b.eventStates[u]++
	st := strconv.Itoa(b.eventStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEvent", st)
	return true, nil
}

func (b *CalendarsBackend) QueryCalendarEvents(ctx context.Context, filter map[string]any, sortCriteria []jmap.Comparator, position int, limit *uint64, expandRecurrences bool) ([]jmap.Id, int, error) {
	events, _, err := b.GetCalendarEvents(ctx, nil)
	if err != nil {
		return nil, 0, err
	}

	var matched []*jmap.CalendarEvent
	for _, ev := range events {
		if memory.MatchCalendarEvent(ev, filter) {
			matched = append(matched, ev)
		}
	}

	var resultIDs []jmap.Id
	if expandRecurrences {
		horizon := time.Now().AddDate(2, 0, 0)
		for _, ev := range matched {
			if len(ev.RecurrenceRules) > 0 {
				instances := memory.ExpandRecurrenceInstances(ev, horizon)
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

// ParticipantIdentities
func (b *CalendarsBackend) ParticipantIdentityState(ctx context.Context) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.user(ctx)
	st, ok := b.identityStates[u]
	if !ok {
		st = 1
		b.identityStates[u] = st
	}
	return strconv.Itoa(st)
}

func (b *CalendarsBackend) ParticipantIdentityChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	cur := b.ParticipantIdentityState(ctx)
	return nil, nil, nil, cur, false
}

func (b *CalendarsBackend) GetAllParticipantIdentities(ctx context.Context) ([]*jmap.ParticipantIdentity, error) {
	list, _, err := b.GetParticipantIdentities(ctx, nil)
	return list, err
}

func (b *CalendarsBackend) GetParticipantIdentities(ctx context.Context, ids []jmap.Id) ([]*jmap.ParticipantIdentity, []jmap.Id, error) {
	u := b.user(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.identitiesCache[u] == nil {
		b.identitiesCache[u] = make(map[jmap.Id]*jmap.ParticipantIdentity)
		defaultID := jmap.Id("pi-" + u)
		b.identitiesCache[u][defaultID] = &jmap.ParticipantIdentity{
			ID:              defaultID,
			Name:            u,
			CalendarAddress: "mailto:" + u,
			SendTo:          map[string]string{"imip": "mailto:" + u},
			IsDefault:       true,
		}
	}

	var list []*jmap.ParticipantIdentity
	for _, id := range b.identitiesCache[u] {
		list = append(list, id)
	}
	return list, nil, nil
}

func (b *CalendarsBackend) CreateParticipantIdentity(ctx context.Context, identity *jmap.ParticipantIdentity) (*jmap.ParticipantIdentity, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if b.identitiesCache[u] == nil {
		b.identitiesCache[u] = make(map[jmap.Id]*jmap.ParticipantIdentity)
	}
	if identity.ID == "" {
		identity.ID = jmap.Id(fmt.Sprintf("pi-%d", time.Now().UnixNano()))
	}
	b.identitiesCache[u][identity.ID] = identity
	b.identityStates[u]++
	st := strconv.Itoa(b.identityStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "ParticipantIdentity", st)
	return identity, nil
}

func (b *CalendarsBackend) UpdateParticipantIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.ParticipantIdentity, error) {
	u := b.user(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	if pi, ok := b.identitiesCache[u][id]; ok {
		if n, ok := patch["name"].(string); ok {
			pi.Name = n
		}
		return pi, nil
	}
	return nil, fmt.Errorf("participant identity not found")
}

func (b *CalendarsBackend) DeleteParticipantIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	u := b.user(ctx)
	b.mu.Lock()
	delete(b.identitiesCache[u], id)
	b.identityStates[u]++
	st := strconv.Itoa(b.identityStates[u])
	b.mu.Unlock()
	b.emitStateChange(u, "ParticipantIdentity", st)
	return true, nil
}

func (b *CalendarsBackend) SetDefaultParticipantIdentity(ctx context.Context, id jmap.Id) error {
	return nil
}

// CalendarEventNotifications
func (b *CalendarsBackend) CalendarEventNotificationState(ctx context.Context) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.user(ctx)
	st, ok := b.notificationStates[u]
	if !ok {
		st = 1
		b.notificationStates[u] = st
	}
	return strconv.Itoa(st)
}

func (b *CalendarsBackend) CalendarEventNotificationChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	cur := b.CalendarEventNotificationState(ctx)
	return nil, nil, nil, cur, false
}

func (b *CalendarsBackend) GetAllCalendarEventNotifications(ctx context.Context) ([]*jmap.CalendarEventNotification, error) {
	list, _, err := b.GetCalendarEventNotifications(ctx, nil)
	return list, err
}

func (b *CalendarsBackend) GetCalendarEventNotifications(ctx context.Context, ids []jmap.Id) ([]*jmap.CalendarEventNotification, []jmap.Id, error) {
	u := b.user(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()
	var list []*jmap.CalendarEventNotification
	if b.notificationsCache[u] != nil {
		for _, n := range b.notificationsCache[u] {
			list = append(list, n)
		}
	}
	return list, nil, nil
}

func (b *CalendarsBackend) CreateCalendarEventNotification(ctx context.Context, notification *jmap.CalendarEventNotification) (*jmap.CalendarEventNotification, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if b.notificationsCache[u] == nil {
		b.notificationsCache[u] = make(map[jmap.Id]*jmap.CalendarEventNotification)
	}
	if notification.ID == "" {
		notification.ID = jmap.Id(fmt.Sprintf("notif-%d", time.Now().UnixNano()))
	}
	b.notificationsCache[u][notification.ID] = notification
	b.notificationStates[u]++
	st := strconv.Itoa(b.notificationStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEventNotification", st)
	return notification, nil
}

func (b *CalendarsBackend) DeleteCalendarEventNotification(ctx context.Context, id jmap.Id) (bool, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if b.notificationsCache[u] != nil {
		delete(b.notificationsCache[u], id)
	}
	b.notificationStates[u]++
	st := strconv.Itoa(b.notificationStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEventNotification", st)
	return true, nil
}

func (b *CalendarsBackend) QueryCalendarEventNotifications(ctx context.Context, filter map[string]any, sortCriteria []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	notifs, _, err := b.GetCalendarEventNotifications(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	var ids []jmap.Id
	for _, n := range notifs {
		ids = append(ids, n.ID)
	}
	return ids, len(ids), nil
}
