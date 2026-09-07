package nextcloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"

	"imap-jmap/jmap"
)

// CalendarsBackend implements jmap.CalendarsBackend backed by Nextcloud CalDAV via github.com/emersion/go-webdav/caldav.
type CalendarsBackend struct {
	client      *Client
	mu          sync.RWMutex
	trackersMu  sync.Mutex
	broadcaster *jmap.Broadcaster

	calTrackers          map[string]*jmap.ChangeTracker
	eventTrackers        map[string]*jmap.ChangeTracker
	identityTrackers     map[string]*jmap.ChangeTracker
	notificationTrackers map[string]*jmap.ChangeTracker

	calsFingerprint    map[string]string
	eventsFingerprint  map[string]string

	calsCache          map[string][]*jmap.Calendar
	calPaths           map[string]map[jmap.Id]string
	homeSets           map[string]string
	calsCacheTime      map[string]time.Time
	eventsCache        map[string]map[jmap.Id]*jmap.CalendarEvent
	eventsCacheTime    map[string]time.Time
	identitiesCache    map[string]map[jmap.Id]*jmap.ParticipantIdentity
	notificationsCache map[string]map[jmap.Id]*jmap.CalendarEventNotification
	defaultCalendars   map[string]jmap.Id
	calProps           map[string]map[jmap.Id]*jmap.Calendar
	notifSeq           map[string]map[jmap.Id]uint64
	nextNotifSeq       uint64
}

var _ jmap.CalendarsBackend = (*CalendarsBackend)(nil)

// NewCalendarsBackend initializes a new Nextcloud-backed CalendarsBackend.
func NewCalendarsBackend(client *Client) *CalendarsBackend {
	return &CalendarsBackend{
		client:               client,
		calTrackers:          make(map[string]*jmap.ChangeTracker),
		eventTrackers:        make(map[string]*jmap.ChangeTracker),
		identityTrackers:     make(map[string]*jmap.ChangeTracker),
		notificationTrackers: make(map[string]*jmap.ChangeTracker),
		calsFingerprint:      make(map[string]string),
		eventsFingerprint:    make(map[string]string),
		calsCache:            make(map[string][]*jmap.Calendar),
		calPaths:             make(map[string]map[jmap.Id]string),
		homeSets:             make(map[string]string),
		calsCacheTime:        make(map[string]time.Time),
		eventsCache:          make(map[string]map[jmap.Id]*jmap.CalendarEvent),
		eventsCacheTime:      make(map[string]time.Time),
		identitiesCache:      make(map[string]map[jmap.Id]*jmap.ParticipantIdentity),
		notificationsCache:   make(map[string]map[jmap.Id]*jmap.CalendarEventNotification),
		defaultCalendars:     make(map[string]jmap.Id),
		calProps:             make(map[string]map[jmap.Id]*jmap.Calendar),
		notifSeq:             make(map[string]map[jmap.Id]uint64),
	}
}

func (b *CalendarsBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *CalendarsBackend) emitStateChange(u, typeName, newState string) {
	bc := b.broadcaster
	if bc != nil {
		accountID := jmap.AccountIDForSubject(u)
		bc.PublishStateChange(accountID, typeName, newState)
		if accountID != u {
			bc.PublishStateChange(u, typeName, newState)
		}
	}
}

func eventFingerprint(ev *jmap.CalendarEvent) string {
	if ev == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%v", ev.ID, ev.Title, ev.Start, ev.Duration, ev.Updated, ev.Description, ev.Sequence, ev.CalendarIDs)
}

func eventsMapFingerprint(m map[jmap.Id]*jmap.CalendarEvent) string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		fmt.Fprintf(h, "%s:%s;", id, eventFingerprint(m[jmap.Id(id)]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func calsListFingerprint(cals []*jmap.Calendar) string {
	h := sha256.New()
	for _, c := range cals {
		if c != nil {
			fmt.Fprintf(h, "%s:%s:%t;", c.ID, c.Name, c.IsVisible)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (b *CalendarsBackend) user(ctx context.Context) string {
	u, _ := b.client.getUserAndPass(ctx)
	return u
}

func (b *CalendarsBackend) getCalTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.calTrackers[u] == nil {
		b.calTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.calTrackers[u]
}

func (b *CalendarsBackend) getEventTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.eventTrackers[u] == nil {
		b.eventTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.eventTrackers[u]
}

func (b *CalendarsBackend) getIdentityTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.identityTrackers[u] == nil {
		b.identityTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.identityTrackers[u]
}

func (b *CalendarsBackend) getNotificationTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.notificationTrackers[u] == nil {
		b.notificationTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.notificationTrackers[u]
}

// CalendarState
func (b *CalendarsBackend) CalendarState(ctx context.Context) string {
	return b.getCalTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) CalendarChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMoreChanges bool) {
	return b.getCalTracker(b.user(ctx)).Changes(sinceState)
}

func (b *CalendarsBackend) GetAllCalendars(ctx context.Context) ([]*jmap.Calendar, error) {
	list, _, err := b.GetCalendars(ctx, nil)
	return list, err
}

func filterCalendars(list []*jmap.Calendar, ids []jmap.Id) ([]*jmap.Calendar, []jmap.Id, error) {
	if ids == nil {
		return list, []jmap.Id{}, nil
	}
	if len(ids) == 0 {
		return []*jmap.Calendar{}, []jmap.Id{}, nil
	}
	idMap := make(map[jmap.Id]bool, len(ids))
	for _, id := range ids {
		idMap[id] = true
	}
	var filtered []*jmap.Calendar
	foundMap := make(map[jmap.Id]bool, len(list))
	for _, c := range list {
		if idMap[c.ID] || (idMap["cal-default"] && c.IsDefault) {
			filtered = append(filtered, c)
			foundMap[c.ID] = true
			if c.IsDefault {
				foundMap["cal-default"] = true
			}
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

func (b *CalendarsBackend) getCalendarHomeSet(ctx context.Context, calClient *caldav.Client, u string) string {
	b.mu.RLock()
	if hs, ok := b.homeSets[u]; ok && hs != "" {
		b.mu.RUnlock()
		return hs
	}
	b.mu.RUnlock()

	principal, err := calClient.FindCurrentUserPrincipal(ctx)
	if err == nil && principal != "" {
		homeSet, err := calClient.FindCalendarHomeSet(ctx, principal)
		if err == nil && homeSet != "" {
			b.mu.Lock()
			b.homeSets[u] = homeSet
			b.mu.Unlock()
			return homeSet
		}
	}
	defaultHS := "calendars/" + u + "/"
	b.mu.Lock()
	b.homeSets[u] = defaultHS
	b.mu.Unlock()
	return defaultHS
}

func (b *CalendarsBackend) getCalPath(u string, cid jmap.Id, homeSet string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.calPaths[u] != nil {
		if p, ok := b.calPaths[u][cid]; ok && p != "" {
			return p
		}
	}
	if homeSet != "" {
		return strings.TrimRight(homeSet, "/") + "/" + string(cid) + "/"
	}
	return "calendars/" + u + "/" + string(cid) + "/"
}

func (b *CalendarsBackend) GetCalendars(ctx context.Context, ids []jmap.Id) ([]*jmap.Calendar, []jmap.Id, error) {
	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)
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
		if b.calPaths[u] == nil {
			b.calPaths[u] = make(map[jmap.Id]string)
		}
		b.calPaths[u]["personal"] = strings.TrimRight(homeSet, "/") + "/personal/"
		b.calsCache[u] = []*jmap.Calendar{defaultCal}
		b.calsCacheTime[u] = time.Now()
		b.mu.Unlock()
		return filterCalendars([]*jmap.Calendar{defaultCal}, ids)
	}

	b.mu.RLock()
	defID := b.defaultCalendars[u]
	b.mu.RUnlock()

	var list []*jmap.Calendar
	pathMap := make(map[jmap.Id]string)
	for _, c := range calList {
		calID := path.Base(strings.TrimRight(c.Path, "/"))
		if calID == "inbox" || calID == "outbox" || calID == "trashbin" {
			continue
		}

		name := c.Name
		if name == "" || strings.EqualFold(name, "Personal") {
			name = "Personal Calendar"
		}

		cid := jmap.Id(calID)
		isDefault := false
		if defID != "" {
			isDefault = (cid == defID)
		} else {
			isDefault = (calID == "personal" || strings.EqualFold(name, "Personal") || strings.EqualFold(name, "Personal Calendar"))
		}
		pathMap[cid] = c.Path
		incAvail := "all"
		b.mu.RLock()
		if b.calProps[u] != nil && b.calProps[u][cid] != nil && b.calProps[u][cid].IncludeInAvailability != "" {
			incAvail = b.calProps[u][cid].IncludeInAvailability
		}
		b.mu.RUnlock()
		list = append(list, &jmap.Calendar{
			ID:                    cid,
			Name:                  name,
			IsVisible:             true,
			IsDefault:             isDefault,
			SortOrder:             0,
			IncludeInAvailability: incAvail,
			MyRights:              jmap.FullCalendarRights(),
		})
	}

	if len(list) == 0 {
		cid := jmap.Id("personal")
		pathMap[cid] = strings.TrimRight(homeSet, "/") + "/personal/"
		incAvail := "all"
		b.mu.RLock()
		if b.calProps[u] != nil && b.calProps[u][cid] != nil && b.calProps[u][cid].IncludeInAvailability != "" {
			incAvail = b.calProps[u][cid].IncludeInAvailability
		}
		b.mu.RUnlock()
		list = append(list, &jmap.Calendar{
			ID:                    cid,
			Name:                  "Personal Calendar",
			IsVisible:             true,
			IsDefault:             true,
			SortOrder:             0,
			IncludeInAvailability: incAvail,
			MyRights:              jmap.FullCalendarRights(),
		})
	}

	b.mu.Lock()
	if b.calPaths[u] == nil {
		b.calPaths[u] = make(map[jmap.Id]string)
	}
	for k, v := range pathMap {
		b.calPaths[u][k] = v
	}
	newFp := calsListFingerprint(list)
	oldFp := b.calsFingerprint[u]
	b.calsFingerprint[u] = newFp
	b.calsCache[u] = list
	b.calsCacheTime[u] = time.Now()
	needEmit := false
	var st string
	if oldFp != "" && oldFp != newFp {
		st = b.getCalTracker(u).Record("external-sync", "update")
		needEmit = true
	}
	b.mu.Unlock()

	if needEmit {
		b.emitStateChange(u, "Calendar", st)
	}

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
	if cal.IncludeInAvailability == "" {
		cal.IncludeInAvailability = "all"
	}

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)
	calPath := strings.TrimRight(homeSet, "/") + "/" + string(cal.ID) + "/"
	_ = calClient.Mkdir(ctx, calPath)

	b.mu.Lock()
	if b.calPaths[u] == nil {
		b.calPaths[u] = make(map[jmap.Id]string)
	}
	b.calPaths[u][cal.ID] = calPath
	if b.calProps[u] == nil {
		b.calProps[u] = make(map[jmap.Id]*jmap.Calendar)
	}
	calCopy := *cal
	b.calProps[u][cal.ID] = &calCopy
	if b.calsCache[u] != nil {
		b.calsCache[u] = append(b.calsCache[u], cal)
	}
	b.calsCacheTime[u] = time.Now()
	st := b.getCalTracker(u).Record(cal.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	return cal, nil
}

func (b *CalendarsBackend) UpdateCalendar(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Calendar, error) {
	cals, notFound, err := b.GetCalendars(ctx, []jmap.Id{id})
	if err != nil {
		return nil, err
	}
	if len(notFound) > 0 || len(cals) == 0 {
		return nil, jmap.ErrNotFound
	}

	u := b.user(ctx)
	b.mu.Lock()
	if b.calProps[u] == nil {
		b.calProps[u] = make(map[jmap.Id]*jmap.Calendar)
	}
	cp := b.calProps[u][id]
	if cp == nil {
		cp = &jmap.Calendar{ID: id, IncludeInAvailability: "all"}
		b.calProps[u][id] = cp
	}
	if inc, ok := patch["includeInAvailability"].(string); ok {
		cp.IncludeInAvailability = inc
	}
	b.calsCacheTime[u] = time.Time{}
	st := b.getCalTracker(u).Record(id, "update")
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)

	cals, _, _ = b.GetCalendars(ctx, []jmap.Id{id})
	if len(cals) > 0 {
		return cals[0], nil
	}
	name, _ := patch["name"].(string)
	return &jmap.Calendar{ID: id, Name: name, IncludeInAvailability: cp.IncludeInAvailability, MyRights: jmap.FullCalendarRights()}, nil
}

func (b *CalendarsBackend) DeleteCalendar(ctx context.Context, id jmap.Id) (bool, error) {
	cals, notFound, err := b.GetCalendars(ctx, []jmap.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(cals) == 0 {
		return false, nil
	}

	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return false, err
	}

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)
	calPath := b.getCalPath(u, id, homeSet)
	_ = calClient.RemoveAll(ctx, calPath)

	b.mu.Lock()
	if b.calPaths[u] != nil {
		delete(b.calPaths[u], id)
	}
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
	st := b.getCalTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	return true, nil
}

func (b *CalendarsBackend) SetDefaultCalendar(ctx context.Context, id jmap.Id) error {
	u := b.user(ctx)
	b.mu.Lock()
	if b.defaultCalendars == nil {
		b.defaultCalendars = make(map[string]jmap.Id)
	}
	b.defaultCalendars[u] = id
	b.calsCacheTime[u] = time.Time{}
	b.mu.Unlock()
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
	return b.getEventTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) CalendarEventChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMoreChanges bool) {
	return b.getEventTracker(b.user(ctx)).Changes(sinceState)
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
	if ids == nil {
		for _, ev := range b.eventsCache[u] {
			list = append(list, ev)
		}
	} else if len(ids) == 0 {
		list = []*jmap.CalendarEvent{}
		notFound = []jmap.Id{}
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
	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	cals, _, _ := b.GetCalendars(ctx, nil)
	homeSet := b.getCalendarHomeSet(ctx, calClient, u)

	type calResult struct {
		calID jmap.Id
		objs  []caldav.CalendarObject
	}
	resChan := make(chan calResult, len(cals))
	var wg sync.WaitGroup

	for _, cal := range cals {
		wg.Add(1)
		go func(cal *jmap.Calendar) {
			defer wg.Done()
			calPath := b.getCalPath(u, cal.ID, homeSet)
			objs, qErr := calClient.QueryCalendar(ctx, calPath, &caldav.CalendarQuery{
				CompFilter: caldav.CompFilter{
					Name: "VCALENDAR",
				},
			})
			if qErr == nil {
				resChan <- calResult{calID: cal.ID, objs: objs}
			}
		}(cal)
	}
	wg.Wait()
	close(resChan)

	freshMap := make(map[jmap.Id]*jmap.CalendarEvent)
	for res := range resChan {
		for _, calObj := range res.objs {
			if calObj.Data == nil {
				continue
			}
			name := path.Base(calObj.Path)
			rawID := strings.TrimSuffix(name, ".ics")
			evID := jmap.Id(rawID)

			var buf bytes.Buffer
			_ = ical.NewEncoder(&buf).Encode(calObj.Data)

			parsedList, pErr := jmap.ParseICalendar(buf.Bytes())
			if pErr == nil && len(parsedList) > 0 {
				ev := parsedList[0]
				ev.ID = evID
				if ev.CalendarIDs == nil {
					ev.CalendarIDs = make(map[jmap.Id]bool)
				}
				ev.CalendarIDs[res.calID] = true
				freshMap[evID] = ev
			}
		}
	}

	b.mu.Lock()
	newFp := eventsMapFingerprint(freshMap)
	oldFp := b.eventsFingerprint[u]
	b.eventsFingerprint[u] = newFp
	b.eventsCache[u] = freshMap
	b.eventsCacheTime[u] = time.Now()
	needEmit := false
	var st string
	if oldFp != "" && oldFp != newFp {
		st = b.getEventTracker(u).Record("external-sync", "update")
		needEmit = true
	}
	b.mu.Unlock()

	if needEmit {
		b.emitStateChange(u, "CalendarEvent", st)
	}

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
	if event.TimeZone == "" {
		event.TimeZone = "Etc/UTC"
	}
	if event.UTCStart == "" {
		event.UTCStart = jmap.ComputeUTCStart(event.Start, event.TimeZone)
	}
	if event.UTCEnd == "" {
		event.UTCEnd = jmap.ComputeUTCEnd(event.Start, event.Duration, event.TimeZone)
	}

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)

	if len(event.CalendarIDs) > 0 {
		cals, _, err := b.GetCalendars(ctx, nil)
		if err == nil {
			calMap := make(map[jmap.Id]bool, len(cals))
			for _, c := range cals {
				calMap[c.ID] = true
			}
			for cid := range event.CalendarIDs {
				if cid != "cal-default" && !calMap[cid] {
					return nil, jmap.SetError{Type: "notFound", Description: fmt.Sprintf("calendar %s not found", cid)}
				}
			}
		}
	}

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
		calID = "personal"
		if event.CalendarIDs == nil {
			event.CalendarIDs = make(map[jmap.Id]bool)
		}
		delete(event.CalendarIDs, "cal-default")
		event.CalendarIDs[jmap.Id(calID)] = true
	}

	calPath := b.getCalPath(u, jmap.Id(calID), homeSet)

	calObj := jmap.CalendarEventToICalendar(event, "", "", "", "")
	eventPath := strings.TrimRight(calPath, "/") + "/" + string(event.ID) + ".ics"
	_, putErr := calClient.PutCalendarObject(ctx, eventPath, calObj)
	if putErr != nil {
		return nil, fmt.Errorf("failed to put calendar object via caldav client: %w", putErr)
	}

	b.mu.Lock()
	if b.eventsCache[u] == nil {
		b.eventsCache[u] = make(map[jmap.Id]*jmap.CalendarEvent)
	}
	action := "create"
	if _, exists := b.eventsCache[u][event.ID]; exists {
		action = "update"
	}
	b.eventsCache[u][event.ID] = event
	b.eventsCacheTime[u] = time.Now()
	st := b.getEventTracker(u).Record(event.ID, action)
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
	if (key == "calendarIds" || key == "mailboxIds" || key == "keywords" || key == "addressBookIds") && len(parts) == 2 {
		sub, ok := m[key].(map[string]any)
		if !ok {
			if val == nil || val == false {
				return
			}
			sub = make(map[string]any)
			m[key] = sub
		}
		if val == nil || val == false {
			delete(sub, parts[1])
		} else {
			sub[parts[1]] = val
		}
		return
	}
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
		if len(parts) == 3 && parts[0] == "participants" {
			if parts[2] == "participationStatus" {
				setNestedMapValue(m, []string{"participants", parts[1], "status"}, val)
			} else if parts[2] == "status" {
				setNestedMapValue(m, []string{"participants", parts[1], "participationStatus"}, val)
			}
		}
	}

	rawUpdated, err := json.Marshal(m)
	if err != nil {
		return err
	}

	origID := ev.ID
	origUID := ev.UID
	origType := ev.Type
	origCalIDs := ev.CalendarIDs
	var updatedEv jmap.CalendarEvent
	if err := json.Unmarshal(rawUpdated, &updatedEv); err != nil {
		return err
	}
	updatedEv.ID = origID
	if updatedEv.UID == "" {
		updatedEv.UID = origUID
	}
	if updatedEv.Type == "" {
		updatedEv.Type = origType
	}

	for cid, isSet := range updatedEv.CalendarIDs {
		if !isSet {
			delete(updatedEv.CalendarIDs, cid)
		}
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

	if locVal, hasLoc := patch["location"]; hasLoc {
		if locVal == nil {
			updatedEv.Locations = nil
		} else if locStr, ok := locVal.(string); ok {
			updatedEv.Locations = map[string]*jmap.JSCalendarLocation{
				"loc-1": {
					Type: "Location",
					Name: locStr,
				},
			}
		} else if locBytes, err := json.Marshal(locVal); err == nil {
			var loc jmap.JSCalendarLocation
			if err := json.Unmarshal(locBytes, &loc); err == nil {
				if loc.Type == "" {
					loc.Type = "Location"
				}
				updatedEv.Locations = map[string]*jmap.JSCalendarLocation{
					"loc-1": &loc,
				}
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
	updatedEv.UTCStart = jmap.ComputeUTCStart(updatedEv.Start, updatedEv.TimeZone)
	updatedEv.UTCEnd = jmap.ComputeUTCEnd(updatedEv.Start, updatedEv.Duration, updatedEv.TimeZone)

	for _, p := range updatedEv.Participants {
		if p != nil {
			if p.ParticipationStatus != "" && (p.Status == "" || p.Status != p.ParticipationStatus) {
				p.Status = p.ParticipationStatus
			} else if p.Status != "" && p.ParticipationStatus == "" {
				p.ParticipationStatus = p.Status
			}
		}
	}

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

	u := b.user(ctx)
	b.mu.RLock()
	var ev *jmap.CalendarEvent
	if b.eventsCache[u] != nil {
		if cached, ok := b.eventsCache[u][id]; ok && cached != nil {
			evCopy := *cached
			ev = &evCopy
		}
	}
	b.mu.RUnlock()

	if ev == nil {
		events, notFound, err := b.GetCalendarEvents(ctx, []jmap.Id{id})
		if err != nil || len(notFound) > 0 || len(events) == 0 {
			return nil, fmt.Errorf("event %s not found: %w", id, jmap.ErrNotFound)
		}
		ev = events[0]
	}

	oldCalID := ""
	for cid, isSet := range ev.CalendarIDs {
		if isSet && cid != "" && cid != "cal-default" {
			oldCalID = string(cid)
			break
		}
	}
	if oldCalID == "" {
		for cid, isSet := range ev.CalendarIDs {
			if isSet && cid != "" {
				oldCalID = string(cid)
				break
			}
		}
	}

	hasSeq := false
	if _, ok := patch["sequence"]; ok {
		hasSeq = true
	}
	if err := applyEventPatch(ev, patch); err != nil {
		return nil, err
	}
	if !hasSeq {
		ev.Sequence++
	}
	ev.Updated = time.Now().UTC().Format(time.RFC3339)
	ev.UTCStart = jmap.ComputeUTCStart(ev.Start, ev.TimeZone)
	ev.UTCEnd = jmap.ComputeUTCEnd(ev.Start, ev.Duration, ev.TimeZone)

	newCalID := ""
	for cid, isSet := range ev.CalendarIDs {
		if isSet && cid != "" && cid != "cal-default" && string(cid) != oldCalID {
			newCalID = string(cid)
			break
		}
	}
	if newCalID == "" {
		for cid, isSet := range ev.CalendarIDs {
			if isSet && cid != "" && cid != "cal-default" {
				newCalID = string(cid)
				break
			}
		}
	}
	if newCalID == "" {
		for cid, isSet := range ev.CalendarIDs {
			if isSet && cid != "" {
				newCalID = string(cid)
				break
			}
		}
	}
	if oldCalID != "" && newCalID != "" && oldCalID != newCalID {
		calClient, u, cErr := b.client.CalDAV(ctx)
		if cErr == nil {
			homeSet := b.getCalendarHomeSet(ctx, calClient, u)
			oldPath := strings.TrimRight(b.getCalPath(u, jmap.Id(oldCalID), homeSet), "/") + "/" + string(id) + ".ics"
			_ = calClient.RemoveAll(ctx, oldPath)
		}
		delete(ev.CalendarIDs, jmap.Id(oldCalID))
		ev.CalendarIDs[jmap.Id(newCalID)] = true
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

	events, notFound, err := b.GetCalendarEvents(ctx, []jmap.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(events) == 0 {
		return false, nil
	}

	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return false, err
	}

	calID := "personal"
	b.mu.RLock()
	if b.eventsCache[u] != nil {
		if ev, ok := b.eventsCache[u][id]; ok && ev != nil {
			for cid := range ev.CalendarIDs {
				calID = string(cid)
				break
			}
		}
	}
	b.mu.RUnlock()

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)
	calPath := b.getCalPath(u, jmap.Id(calID), homeSet)
	eventPath := strings.TrimRight(calPath, "/") + "/" + string(id) + ".ics"
	_ = calClient.RemoveAll(ctx, eventPath)

	b.mu.Lock()
	if b.eventsCache[u] != nil {
		delete(b.eventsCache[u], id)
	}
	b.eventsCacheTime[u] = time.Now()
	st := b.getEventTracker(u).Record(id, "destroy")
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
		if jmap.MatchCalendarEvent(ev, filter) {
			matched = append(matched, ev)
		}
	}

	jmap.SortCalendarEvents(matched, sortCriteria)

	var resultIDs []jmap.Id
	if expandRecurrences {
		horizon := time.Now().AddDate(2, 0, 0)
		for _, ev := range matched {
			if len(ev.RecurrenceRules) > 0 {
				instances := jmap.ExpandRecurrenceInstances(ev, horizon)
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
	return b.getIdentityTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) ParticipantIdentityChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return b.getIdentityTracker(b.user(ctx)).Changes(sinceState)
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
		defaultID := jmap.Id("identity-default")
		b.identitiesCache[u][defaultID] = &jmap.ParticipantIdentity{
			ID:              defaultID,
			Name:            u,
			CalendarAddress: "mailto:" + u,
			SendTo:          map[string]string{"imip": "mailto:" + u},
			IsDefault:       true,
		}
	}

	var list []*jmap.ParticipantIdentity
	var notFound []jmap.Id
	if ids == nil {
		for _, id := range b.identitiesCache[u] {
			list = append(list, id)
		}
	} else {
		for _, id := range ids {
			if pi, ok := b.identitiesCache[u][id]; ok {
				list = append(list, pi)
			} else {
				notFound = append(notFound, id)
			}
		}
	}
	return list, notFound, nil
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
	st := b.getIdentityTracker(u).Record(identity.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "ParticipantIdentity", st)
	return identity, nil
}

func (b *CalendarsBackend) UpdateParticipantIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.ParticipantIdentity, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if pi, ok := b.identitiesCache[u][id]; ok {
		if n, ok := patch["name"].(string); ok {
			pi.Name = n
		}
		if ca, ok := patch["calendarAddress"].(string); ok {
			pi.CalendarAddress = ca
		}
		if st, ok := patch["sendTo"].(map[string]any); ok {
			pi.SendTo = make(map[string]string)
			for k, v := range st {
				if s, ok := v.(string); ok {
					pi.SendTo[k] = s
				}
			}
		}
		st := b.getIdentityTracker(u).Record(id, "update")
		b.mu.Unlock()
		b.emitStateChange(u, "ParticipantIdentity", st)
		return pi, nil
	}
	b.mu.Unlock()
	return nil, fmt.Errorf("participant identity not found")
}

func (b *CalendarsBackend) DeleteParticipantIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if _, ok := b.identitiesCache[u][id]; !ok {
		b.mu.Unlock()
		return false, fmt.Errorf("participant identity not found")
	}
	delete(b.identitiesCache[u], id)
	st := b.getIdentityTracker(u).Record(id, "destroy")
	b.mu.Unlock()
	b.emitStateChange(u, "ParticipantIdentity", st)
	return true, nil
}

func (b *CalendarsBackend) SetDefaultParticipantIdentity(ctx context.Context, id jmap.Id) error {
	u := b.user(ctx)
	b.mu.Lock()
	if _, ok := b.identitiesCache[u][id]; ok {
		for otherID, other := range b.identitiesCache[u] {
			wasDefault := other.IsDefault
			other.IsDefault = (otherID == id)
			if wasDefault != other.IsDefault {
				b.getIdentityTracker(u).Record(otherID, "update")
			}
		}
		st := b.getIdentityTracker(u).State()
		b.mu.Unlock()
		b.emitStateChange(u, "ParticipantIdentity", st)
		return nil
	}
	b.mu.Unlock()
	return fmt.Errorf("participant identity not found")
}

// CalendarEventNotifications
func (b *CalendarsBackend) CalendarEventNotificationState(ctx context.Context) string {
	return b.getNotificationTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) CalendarEventNotificationChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return b.getNotificationTracker(b.user(ctx)).Changes(sinceState)
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
	var notFound []jmap.Id
	if ids == nil {
		if b.notificationsCache[u] != nil {
			for _, n := range b.notificationsCache[u] {
				list = append(list, n)
			}
		}
	} else {
		for _, id := range ids {
			if b.notificationsCache[u] != nil {
				if n, ok := b.notificationsCache[u][id]; ok {
					list = append(list, n)
				} else {
					notFound = append(notFound, id)
				}
			} else {
				notFound = append(notFound, id)
			}
		}
	}
	return list, notFound, nil
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
	if notification.Created == "" {
		notification.Created = time.Now().UTC().Format(time.RFC3339)
	}
	b.notificationsCache[u][notification.ID] = notification
	b.nextNotifSeq++
	if b.notifSeq[u] == nil {
		b.notifSeq[u] = make(map[jmap.Id]uint64)
	}
	b.notifSeq[u][notification.ID] = b.nextNotifSeq
	st := b.getNotificationTracker(u).Record(notification.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEventNotification", st)
	return notification, nil
}

func (b *CalendarsBackend) DeleteCalendarEventNotification(ctx context.Context, id jmap.Id) (bool, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if b.notificationsCache[u] == nil {
		b.mu.Unlock()
		return false, nil
	}
	if _, ok := b.notificationsCache[u][id]; !ok {
		b.mu.Unlock()
		return false, nil
	}
	delete(b.notificationsCache[u], id)
	if b.notifSeq[u] != nil {
		delete(b.notifSeq[u], id)
	}
	st := b.getNotificationTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEventNotification", st)
	return true, nil
}

func (b *CalendarsBackend) QueryCalendarEventNotifications(ctx context.Context, filter map[string]any, sortCriteria []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	u := b.user(ctx)
	notifs, _, err := b.GetCalendarEventNotifications(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	var matched []*jmap.CalendarEventNotification
	for _, n := range notifs {
		if matchCalendarEventNotification(n, filter) {
			matched = append(matched, n)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		for _, comp := range sortCriteria {
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
		b.mu.RLock()
		seqs := b.notifSeq[u]
		b.mu.RUnlock()
		if seqs != nil {
			return seqs[matched[i].ID] > seqs[matched[j].ID]
		}
		return matched[i].ID > matched[j].ID
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
	return resultIDs[position:end], total, nil
}

func matchCalendarEventNotification(n *jmap.CalendarEventNotification, filter map[string]any) bool {
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
