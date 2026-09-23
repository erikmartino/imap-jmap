package nextcloud

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapprincipals"
	"imap-jmap/jmap/jmappush"
)

// CalendarsBackend implements jmapcalendar.CalendarsBackend backed by Nextcloud.
type CalendarsBackend struct {
	client      *Client
	mu          sync.RWMutex
	trackersMu  sync.Mutex
	broadcaster *jmappush.Broadcaster

	calTrackers          map[string]*jmappush.ChangeTracker
	eventTrackers        map[string]*jmappush.ChangeTracker
	identityTrackers     map[string]*jmappush.ChangeTracker
	notificationTrackers map[string]*jmappush.ChangeTracker

	calsFingerprint   map[string]string
	eventsFingerprint map[string]string

	cache                     backendCache
	identitiesCache           map[string]map[jmapcore.Id]*jmapcalendar.ParticipantIdentity
	notificationsCache        map[string]map[jmapcore.Id]*jmapcalendar.CalendarEventNotification
	defaultCalendars          map[string]jmapcore.Id
	calProps                  map[string]map[jmapcore.Id]*jmapcalendar.Calendar
	notifSeq                  map[string]map[jmapcore.Id]uint64
	nextNotifSeq              uint64
	allowedAddresses          map[string]map[string]bool
	shareNotificationsCache   map[string]map[jmapcore.Id]*jmapcalendar.ShareNotification
	shareNotificationTrackers map[string]*jmappush.ChangeTracker
	userCalOverrides          map[string]map[jmapcore.Id]*jmapcalendar.Calendar
	principalsBackend         jmapprincipals.PrincipalsBackend
}

var _ jmapcalendar.CalendarsBackend = (*CalendarsBackend)(nil)

// NewCalendarsBackend initializes a new Nextcloud-backed CalendarsBackend.
func NewCalendarsBackend(client *Client) *CalendarsBackend {
	var c backendCache = &dummyBackendCache{}
	if !isCacheDisabledEnv() && client != nil && !client.IsCacheDisabled() {
		c = newMemBackendCache()
	}

	return &CalendarsBackend{
		client:                    client,
		calTrackers:               make(map[string]*jmappush.ChangeTracker),
		eventTrackers:             make(map[string]*jmappush.ChangeTracker),
		identityTrackers:          make(map[string]*jmappush.ChangeTracker),
		notificationTrackers:      make(map[string]*jmappush.ChangeTracker),
		calsFingerprint:           make(map[string]string),
		eventsFingerprint:         make(map[string]string),
		cache:                     c,
		identitiesCache:           make(map[string]map[jmapcore.Id]*jmapcalendar.ParticipantIdentity),
		notificationsCache:        make(map[string]map[jmapcore.Id]*jmapcalendar.CalendarEventNotification),
		defaultCalendars:          make(map[string]jmapcore.Id),
		calProps:                  make(map[string]map[jmapcore.Id]*jmapcalendar.Calendar),
		notifSeq:                  make(map[string]map[jmapcore.Id]uint64),
		allowedAddresses:          make(map[string]map[string]bool),
		shareNotificationsCache:   make(map[string]map[jmapcore.Id]*jmapcalendar.ShareNotification),
		shareNotificationTrackers: make(map[string]*jmappush.ChangeTracker),
		userCalOverrides:          make(map[string]map[jmapcore.Id]*jmapcalendar.Calendar),
	}
}

// SetCacheDisabled toggles cache between dummy (disabled) and memory.
func (b *CalendarsBackend) SetCacheDisabled(disabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if disabled {
		b.cache = &dummyBackendCache{}
	} else {
		b.cache = newMemBackendCache()
	}
}

func (b *CalendarsBackend) SetBroadcaster(bc *jmappush.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *CalendarsBackend) emitStateChange(u, typeName, newState string) {
	bc := b.broadcaster
	if bc != nil {
		accountID := jmapauth.AccountIDForSubject(u)
		bc.PublishStateChange(accountID, typeName, newState)
		if accountID != u {
			bc.PublishStateChange(u, typeName, newState)
		}
	}
}

func eventFingerprint(ev *jmapcalendar.CalendarEvent) string {
	if ev == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%v", ev.ID, ev.Title, ev.Start, ev.Duration, ev.Updated, ev.Description, ev.Sequence, ev.CalendarIDs)
}

func eventsMapFingerprint(m map[jmapcore.Id]*jmapcalendar.CalendarEvent) string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		fmt.Fprintf(h, "%s:%s;", id, eventFingerprint(m[jmapcore.Id(id)]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func calsListFingerprint(cals []*jmapcalendar.Calendar) string {
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

func (b *CalendarsBackend) getCalTracker(u string) *jmappush.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.calTrackers[u] == nil {
		b.calTrackers[u] = jmappush.NewChangeTracker(1000)
	}
	return b.calTrackers[u]
}

func (b *CalendarsBackend) getEventTracker(u string) *jmappush.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.eventTrackers[u] == nil {
		b.eventTrackers[u] = jmappush.NewChangeTracker(1000)
	}
	return b.eventTrackers[u]
}

func (b *CalendarsBackend) getIdentityTracker(u string) *jmappush.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.identityTrackers[u] == nil {
		b.identityTrackers[u] = jmappush.NewChangeTracker(1000)
	}
	return b.identityTrackers[u]
}

func (b *CalendarsBackend) getNotificationTracker(u string) *jmappush.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.notificationTrackers[u] == nil {
		b.notificationTrackers[u] = jmappush.NewChangeTracker(1000)
	}
	return b.notificationTrackers[u]
}

func (b *CalendarsBackend) getShareNotificationTracker(u string) *jmappush.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.shareNotificationTrackers[u] == nil {
		b.shareNotificationTrackers[u] = jmappush.NewChangeTracker(1000)
	}
	return b.shareNotificationTrackers[u]
}

func (b *CalendarsBackend) SetPrincipalsBackend(pb jmapprincipals.PrincipalsBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.principalsBackend = pb
}

func (b *CalendarsBackend) lookupUserNameLocked(subj string) string {
	pb := b.principalsBackend
	if pb != nil {
		principals, err := pb.GetAllPrincipals(context.Background())
		if err == nil {
			for _, p := range principals {
				if p != nil && (strings.EqualFold(p.Email, subj) || strings.EqualFold(p.CalendarAddress, "mailto:"+subj)) {
					if p.Name != "" {
						return p.Name
					}
				}
			}
		}
	}
	return subj
}

func (b *CalendarsBackend) lookupUserName(subj string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lookupUserNameLocked(subj)
}

func (b *CalendarsBackend) CanAccessSharedAccount(principalAccountID, targetAccountID string) (allowed bool, accountKnown bool) {
	if principalAccountID == targetAccountID {
		return true, true
	}
	targetUser, ok := jmapauth.SubjectForAccountID(targetAccountID)
	if !ok || targetUser == "" {
		targetUser = targetAccountID
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	known := b.calProps[targetUser] != nil || b.cache.HasCals(targetUser) || b.defaultCalendars[targetUser] != "" || b.allowedAddresses[targetUser] != nil
	if !known && b.principalsBackend != nil {
		if principals, err := b.principalsBackend.GetAllPrincipals(context.Background()); err == nil {
			for _, p := range principals {
				if p != nil && (p.AccountIDs[targetAccountID] || string(p.ID) == targetAccountID || strings.EqualFold(p.Email, targetUser) || strings.EqualFold(p.CalendarAddress, "mailto:"+targetUser)) {
					known = true
					break
				}
			}
		}
	}

	for _, cp := range b.calProps[targetUser] {
		if cp != nil && cp.ShareWith != nil && cp.ShareWith[principalAccountID] != nil {
			return true, known
		}
	}
	if cals, ok := b.cache.GetCals(targetUser); ok {
		for _, cal := range cals {
			if cal != nil && cal.ShareWith != nil && cal.ShareWith[principalAccountID] != nil {
				return true, known
			}
		}
	}

	return false, known
}

// CalendarState
func (b *CalendarsBackend) CalendarState(ctx context.Context) string {
	return b.getCalTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) CalendarChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool) {
	return b.getCalTracker(b.user(ctx)).Changes(sinceState)
}

func (b *CalendarsBackend) GetAllCalendars(ctx context.Context) ([]*jmapcalendar.Calendar, error) {
	list, _, err := b.GetCalendars(ctx, nil)
	return list, err
}

func filterCalendars(list []*jmapcalendar.Calendar, ids []jmapcore.Id) ([]*jmapcalendar.Calendar, []jmapcore.Id, error) {
	if ids == nil {
		return list, []jmapcore.Id{}, nil
	}
	if len(ids) == 0 {
		return []*jmapcalendar.Calendar{}, []jmapcore.Id{}, nil
	}
	idMap := make(map[jmapcore.Id]bool, len(ids))
	for _, id := range ids {
		idMap[id] = true
	}
	var filtered []*jmapcalendar.Calendar
	foundMap := make(map[jmapcore.Id]bool, len(list))
	for _, c := range list {
		if idMap[c.ID] || (idMap["cal-default"] && c.IsDefault) {
			filtered = append(filtered, c)
			foundMap[c.ID] = true
			if c.IsDefault {
				foundMap["cal-default"] = true
			}
		}
	}
	var notFound []jmapcore.Id
	for _, id := range ids {
		if !foundMap[id] {
			notFound = append(notFound, id)
		}
	}
	if notFound == nil {
		notFound = []jmapcore.Id{}
	}
	return filtered, notFound, nil
}

type reqCacheCalsKey struct {
	user string
}

type reqCacheEventsKey struct {
	user string
}

// eventWindowKey identifies a CalendarEvent retrieval window within a single JMAP
// request. start/end are Unix nanoseconds and are 0 when that side is open-ended.
type eventWindowKey struct {
	start, end int64
}

func eventWindowKeyFor(start, end time.Time) eventWindowKey {
	k := eventWindowKey{}
	if !start.IsZero() {
		k.start = start.UnixNano()
	}
	if !end.IsZero() {
		k.end = end.UnixNano()
	}
	return k
}

// eventWindowMaps holds the per-request CalendarEvent maps keyed by retrieval window.
// A single key (the user) is stored in the RequestCache so that mutations can
// invalidate every window at once.
type eventWindowMaps struct {
	mu      sync.Mutex
	windows map[eventWindowKey]map[jmapcore.Id]*jmapcalendar.CalendarEvent
}

func loadRequestEvents(rc *jmapcore.RequestCache, u string, key eventWindowKey) (map[jmapcore.Id]*jmapcalendar.CalendarEvent, bool) {
	if rc == nil {
		return nil, false
	}
	raw, ok := rc.Load(reqCacheEventsKey{user: u})
	if !ok {
		return nil, false
	}
	maps, ok := raw.(*eventWindowMaps)
	if !ok || maps == nil {
		return nil, false
	}
	maps.mu.Lock()
	defer maps.mu.Unlock()
	evs, ok := maps.windows[key]
	return evs, ok
}

func storeRequestEvents(rc *jmapcore.RequestCache, u string, key eventWindowKey, evs map[jmapcore.Id]*jmapcalendar.CalendarEvent) {
	if rc == nil {
		return
	}
	raw, _ := rc.Load(reqCacheEventsKey{user: u})
	maps, _ := raw.(*eventWindowMaps)
	if maps == nil {
		maps = &eventWindowMaps{windows: make(map[eventWindowKey]map[jmapcore.Id]*jmapcalendar.CalendarEvent)}
		rc.Store(reqCacheEventsKey{user: u}, maps)
	}
	maps.mu.Lock()
	maps.windows[key] = evs
	maps.mu.Unlock()
}

// lookupRequestEvents searches every window already fetched during this JMAP request
// for the master events backing ids. It lets a CalendarEvent/query followed by a
// CalendarEvent/get in the same request reuse the query's bounded fetch instead of
// falling back to a full collection scan. It reports ok only when every requested
// master id was found in some window.
func lookupRequestEvents(rc *jmapcore.RequestCache, u string, ids []jmapcore.Id) (map[jmapcore.Id]*jmapcalendar.CalendarEvent, bool) {
	if rc == nil || len(ids) == 0 {
		return nil, false
	}
	raw, ok := rc.Load(reqCacheEventsKey{user: u})
	if !ok {
		return nil, false
	}
	maps, ok := raw.(*eventWindowMaps)
	if !ok || maps == nil {
		return nil, false
	}

	want := make(map[jmapcore.Id]bool, len(ids))
	for _, id := range ids {
		baseID := id
		if strings.Contains(string(id), "#") {
			baseID = jmapcore.Id(strings.SplitN(string(id), "#", 2)[0])
		}
		want[baseID] = true
	}

	maps.mu.Lock()
	defer maps.mu.Unlock()
	found := make(map[jmapcore.Id]*jmapcalendar.CalendarEvent, len(want))
	for _, window := range maps.windows {
		for id := range want {
			if found[id] == nil {
				if ev := window[id]; ev != nil {
					found[id] = ev
				}
			}
		}
	}
	if len(found) != len(want) {
		return nil, false
	}
	return found, true
}

// eventTimeWindow derives the half-open UTC range [start, end) that a
// CalendarEvent/query filter constrains. Per draft-ietf-jmap-calendars-27
// Section 5.11.1, "after" bounds the event's end (range start) and "before"
// bounds the event's start (range end). Only a top-level FilterCondition or an
// AND FilterOperator can be represented by a single range; OR/NOT trees return
// an open range because their union cannot be pushed down as one time-range.
func eventTimeWindow(filter map[string]any, loc *time.Location) (start, end time.Time) {
	if filter == nil {
		return time.Time{}, time.Time{}
	}
	if opRaw, ok := filter["operator"]; ok {
		op, _ := opRaw.(string)
		if !strings.EqualFold(op, "AND") {
			return time.Time{}, time.Time{}
		}
		conds, _ := filter["conditions"].([]any)
		for _, c := range conds {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			s, e := eventTimeWindow(cm, loc)
			if !s.IsZero() && (start.IsZero() || s.After(start)) {
				start = s
			}
			if !e.IsZero() && (end.IsZero() || e.Before(end)) {
				end = e
			}
		}
		return start, end
	}
	if afterStr, ok := filter["after"].(string); ok && afterStr != "" {
		if t, ok := jmapcalendar.ParseLocalDateTimeBound(afterStr, loc); ok {
			start = t
		}
	}
	if beforeStr, ok := filter["before"].(string); ok && beforeStr != "" {
		if t, ok := jmapcalendar.ParseLocalDateTimeBound(beforeStr, loc); ok {
			end = t
		}
	}
	return start, end
}

func (b *CalendarsBackend) GetCalendars(ctx context.Context, ids []jmapcore.Id) ([]*jmapcalendar.Calendar, []jmapcore.Id, error) {
	u := b.user(ctx)
	callerAccountID, hasCaller := jmapauth.PrincipalAccountIDFromContext(ctx)
	targetAccountID, _ := jmapauth.AccountIDFromContext(ctx)
	isSharedCaller := hasCaller && callerAccountID != "" && callerAccountID != targetAccountID

	rc := jmapcore.RequestCacheFrom(ctx)
	if rc != nil && !isSharedCaller {
		if cached, ok := rc.Load(reqCacheCalsKey{user: u}); ok {
			if cachedList, okList := cached.([]*jmapcalendar.Calendar); okList {
				return filterCalendars(cachedList, ids)
			}
		}
	}

	calList, u, err := b.client.ListCalendars(ctx)
	if err != nil {
		return nil, nil, err
	}

	b.mu.RLock()
	defID := b.defaultCalendars[u]
	b.mu.RUnlock()

	var list []*jmapcalendar.Calendar
	for _, c := range calList {
		cid := jmapcore.Id(c.ID)
		isDefault := c.IsDefault
		if defID != "" {
			isDefault = (cid == defID)
		}
		incAvail := "all"
		b.mu.RLock()
		if b.calProps[u] != nil && b.calProps[u][cid] != nil && b.calProps[u][cid].IncludeInAvailability != "" {
			incAvail = b.calProps[u][cid].IncludeInAvailability
		}
		b.mu.RUnlock()
		var desc *string
		if c.Description != "" {
			desc = &c.Description
		}
		cal := &jmapcalendar.Calendar{
			ID:                    cid,
			Name:                  c.Name,
			Description:           desc,
			IsVisible:             true,
			IsDefault:             isDefault,
			SortOrder:             0,
			IncludeInAvailability: incAvail,
			MyRights:              jmapcalendar.FullCalendarRights(),
		}
		b.mu.RLock()
		if b.calProps[u] != nil && b.calProps[u][cid] != nil {
			cp := b.calProps[u][cid]
			if cp.Name != "" {
				cal.Name = cp.Name
			}
			cal.Description = cp.Description
			cal.Color = cp.Color
			cal.TimeZone = cp.TimeZone
			cal.SortOrder = cp.SortOrder
			cal.IsSubscribed = cp.IsSubscribed
			cal.IsVisible = cp.IsVisible
			if cp.IncludeInAvailability != "" {
				cal.IncludeInAvailability = cp.IncludeInAvailability
			}
			cal.DefaultAlertsWithTime = cp.DefaultAlertsWithTime
			cal.DefaultAlertsWithoutTime = cp.DefaultAlertsWithoutTime
			if cp.ShareWith != nil {
				cal.ShareWith = cp.ShareWith
			}
		}
		b.mu.RUnlock()
		list = append(list, cal)
	}

	hasDefault := false
	for _, c := range list {
		if c.IsDefault {
			hasDefault = true
			break
		}
	}
	if !hasDefault && len(list) > 0 {
		list[0].IsDefault = true
	}

	b.mu.Lock()
	newFp := calsListFingerprint(list)
	oldFp := b.calsFingerprint[u]
	b.calsFingerprint[u] = newFp
	b.cache.SetCals(u, list)
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

	callerUser := ""
	if hasCaller && callerAccountID != "" {
		if s, ok := jmapauth.SubjectForAccountID(callerAccountID); ok && s != "" {
			callerUser = s
		} else {
			callerUser = callerAccountID
		}
	}

	if isSharedCaller {
		var sharedList []*jmapcalendar.Calendar
		for _, cal := range list {
			if cal != nil && cal.ShareWith != nil && cal.ShareWith[callerAccountID] != nil {
				calCopy := *cal
				calCopy.MyRights = *cal.ShareWith[callerAccountID]
				if !calCopy.MyRights.MayShare {
					calCopy.ShareWith = nil
				}
				b.mu.RLock()
				if b.userCalOverrides[callerUser] != nil && b.userCalOverrides[callerUser][calCopy.ID] != nil {
					ov := b.userCalOverrides[callerUser][calCopy.ID]
					if ov.Name != "" {
						calCopy.Name = ov.Name
					}
					if ov.Description != nil {
						calCopy.Description = ov.Description
					}
				}
				b.mu.RUnlock()
				sharedList = append(sharedList, &calCopy)
			}
		}
		return filterCalendars(sharedList, ids)
	}

	for _, cal := range list {
		if cal != nil && cal.ShareWith == nil {
			cal.ShareWith = make(map[string]*jmapcalendar.CalendarRights)
		}
	}

	if rc != nil && !isSharedCaller {
		rc.Store(reqCacheCalsKey{user: u}, list)
	}

	return filterCalendars(list, ids)
}

func (b *CalendarsBackend) CreateCalendar(ctx context.Context, cal *jmapcalendar.Calendar) (*jmapcalendar.Calendar, error) {
	if cal == nil {
		return nil, fmt.Errorf("calendar is nil")
	}
	u := b.user(ctx)

	if rc := jmapcore.RequestCacheFrom(ctx); rc != nil {
		rc.Delete(reqCacheCalsKey{user: u})
	}

	if cal.ID == "" {
		cal.ID = jmapcore.Id(fmt.Sprintf("cal-%d", time.Now().UnixNano()))
	}
	if cal.IncludeInAvailability == "" {
		cal.IncludeInAvailability = "all"
	}
	if cal.ShareWith == nil {
		cal.ShareWith = make(map[string]*jmapcalendar.CalendarRights)
	}
	cal.MyRights = jmapcalendar.FullCalendarRights()

	_ = b.client.CreateCalendar(ctx, string(cal.ID))

	b.mu.Lock()
	if b.calProps[u] == nil {
		b.calProps[u] = make(map[jmapcore.Id]*jmapcalendar.Calendar)
	}
	calCopy := *cal
	b.calProps[u][cal.ID] = &calCopy
	b.cache.AppendCal(u, cal)
	st := b.getCalTracker(u).Record(cal.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	return cal, nil
}

func (b *CalendarsBackend) UpdateCalendar(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapcalendar.Calendar, error) {
	cals, notFound, err := b.GetCalendars(ctx, []jmapcore.Id{id})
	if err != nil {
		return nil, err
	}
	if len(notFound) > 0 || len(cals) == 0 {
		return nil, jmapcore.ErrNotFound
	}

	callerAccountID, hasCaller := jmapauth.PrincipalAccountIDFromContext(ctx)
	targetAccountID, _ := jmapauth.AccountIDFromContext(ctx)
	isSharedCaller := hasCaller && callerAccountID != "" && callerAccountID != targetAccountID

	callerUser := ""
	if hasCaller && callerAccountID != "" {
		if s, ok := jmapauth.SubjectForAccountID(callerAccountID); ok && s != "" {
			callerUser = s
		} else {
			callerUser = callerAccountID
		}
	}

	if isSharedCaller {
		b.mu.Lock()
		if b.userCalOverrides[callerUser] == nil {
			b.userCalOverrides[callerUser] = make(map[jmapcore.Id]*jmapcalendar.Calendar)
		}
		ov := b.userCalOverrides[callerUser][id]
		if ov == nil {
			ov = &jmapcalendar.Calendar{ID: id}
			b.userCalOverrides[callerUser][id] = ov
		}
		if nameVal, hasName := patch["name"]; hasName {
			if s, ok := nameVal.(string); ok {
				ov.Name = s
			}
		}
		if descVal, hasDesc := patch["description"]; hasDesc {
			if s, ok := descVal.(string); ok {
				ov.Description = &s
			} else if descVal == nil {
				ov.Description = nil
			}
		}
		b.mu.Unlock()
		cals, _, _ = b.GetCalendars(ctx, []jmapcore.Id{id})
		if len(cals) > 0 {
			return cals[0], nil
		}
		return &jmapcalendar.Calendar{ID: id, Name: ov.Name, Description: ov.Description}, nil
	}

	u := b.user(ctx)
	b.mu.Lock()
	if b.calProps[u] == nil {
		b.calProps[u] = make(map[jmapcore.Id]*jmapcalendar.Calendar)
	}
	cp := b.calProps[u][id]
	if cp == nil {
		cpCopy := *cals[0]
		cp = &cpCopy
		b.calProps[u][id] = cp
	}

	oldShareWith := make(map[string]*jmapcalendar.CalendarRights)
	if cp.ShareWith != nil {
		for k, v := range cp.ShareWith {
			if v != nil {
				rCopy := *v
				oldShareWith[k] = &rCopy
			}
		}
	}

	_ = applyCalendarPatch(cp, patch)

	if cp.ShareWith != nil {
		for k, v := range cp.ShareWith {
			if v == nil {
				delete(cp.ShareWith, k)
			}
		}
	}

	allPrincipals := make(map[string]bool)
	for p := range oldShareWith {
		allPrincipals[p] = true
	}
	if cp.ShareWith != nil {
		for p := range cp.ShareWith {
			allPrincipals[p] = true
		}
	}

	type shareChange struct {
		pUser string
		pID   string
		oldR  *jmapcalendar.CalendarRights
		newR  *jmapcalendar.CalendarRights
	}
	var changes []shareChange

	for pID := range allPrincipals {
		oldR := oldShareWith[pID]
		var newR *jmapcalendar.CalendarRights
		if cp.ShareWith != nil {
			newR = cp.ShareWith[pID]
		}
		if !equalCalendarRights(oldR, newR) {
			pUser := pID
			if s, ok := jmapauth.SubjectForAccountID(pID); ok && s != "" {
				pUser = s
			}
			notifOldR := &jmapcalendar.CalendarRights{}
			if oldR != nil {
				rCopy := *oldR
				notifOldR = &rCopy
			}
			notifNewR := &jmapcalendar.CalendarRights{}
			if newR != nil {
				rCopy := *newR
				notifNewR = &rCopy
			}
			changes = append(changes, shareChange{
				pUser: pUser,
				pID:   pID,
				oldR:  notifOldR,
				newR:  notifNewR,
			})
		}
	}

	b.cache.ClearCals(u)
	st := b.getCalTracker(u).Record(id, "update")

	var shareNotifs []struct {
		user  string
		state string
	}

	for _, ch := range changes {
		notifID := jmapcore.Id(fmt.Sprintf("sn-%d-%s", time.Now().UnixNano(), ch.pID))
		ownerSubj := u
		ownerName := b.lookupUserNameLocked(ownerSubj)
		notif := &jmapcalendar.ShareNotification{
			ID:      notifID,
			Created: time.Now().UTC().Format(time.RFC3339),
			ChangedBy: jmapcalendar.ShareNotificationPerson{
				PrincipalID: targetAccountID,
				Name:        ownerName,
				Email:       ownerSubj,
			},
			ObjectType:      "Calendar",
			ObjectAccountID: targetAccountID,
			ObjectID:        id,
			OldRights:       ch.oldR,
			NewRights:       ch.newR,
			Name:            nil,
		}
		if b.shareNotificationsCache[ch.pUser] == nil {
			b.shareNotificationsCache[ch.pUser] = make(map[jmapcore.Id]*jmapcalendar.ShareNotification)
		}
		b.shareNotificationsCache[ch.pUser][notifID] = notif
		stNotif := b.getShareNotificationTracker(ch.pUser).Record(notifID, "create")
		shareNotifs = append(shareNotifs, struct {
			user  string
			state string
		}{user: ch.pUser, state: stNotif})
	}
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	for _, sn := range shareNotifs {
		b.emitStateChange(sn.user, "ShareNotification", sn.state)
	}

	if rc := jmapcore.RequestCacheFrom(ctx); rc != nil {
		rc.Delete(reqCacheCalsKey{user: u})
	}

	cals, _, _ = b.GetCalendars(ctx, []jmapcore.Id{id})
	if len(cals) > 0 {
		return cals[0], nil
	}
	name, _ := patch["name"].(string)
	return &jmapcalendar.Calendar{ID: id, Name: name, IncludeInAvailability: cp.IncludeInAvailability, MyRights: jmapcalendar.FullCalendarRights()}, nil
}

func (b *CalendarsBackend) DeleteCalendar(ctx context.Context, id jmapcore.Id) (bool, error) {
	cals, notFound, err := b.GetCalendars(ctx, []jmapcore.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(cals) == 0 {
		return false, nil
	}

	u := b.user(ctx)
	if rc := jmapcore.RequestCacheFrom(ctx); rc != nil {
		rc.Delete(reqCacheCalsKey{user: u})
	}
	_ = b.client.DeleteCalendar(ctx, string(id))

	b.cache.DeleteCal(u, id)
	b.mu.Lock()
	st := b.getCalTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	return true, nil
}

func (b *CalendarsBackend) SetDefaultCalendar(ctx context.Context, id jmapcore.Id) error {
	u := b.user(ctx)
	b.mu.Lock()
	if b.defaultCalendars == nil {
		b.defaultCalendars = make(map[string]jmapcore.Id)
	}
	b.defaultCalendars[u] = id
	b.cache.ClearCals(u)
	b.mu.Unlock()
	return nil
}

func (b *CalendarsBackend) CalendarHasEvents(ctx context.Context, id jmapcore.Id) (bool, error) {
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

func encodeSyncState(tokens map[string]string) string {
	if len(tokens) == 0 {
		return "sync-v1:empty"
	}
	data, err := json.Marshal(tokens)
	if err != nil {
		return "sync-v1:empty"
	}
	return "sync-v1:" + base64.RawURLEncoding.EncodeToString(data)
}

func decodeSyncState(state string) (map[string]string, error) {
	if !strings.HasPrefix(state, "sync-v1:") {
		return nil, errors.New("not a sync-v1 state")
	}
	raw := strings.TrimPrefix(state, "sync-v1:")
	if raw == "empty" || raw == "" {
		return make(map[string]string), nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var tokens map[string]string
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

// CalendarEventState derives an opaque JMAP state token from CalDAV collection sync-tokens/CTags.
func (b *CalendarsBackend) CalendarEventState(ctx context.Context) string {
	u := b.user(ctx)
	statuses, err := b.client.GetCalendarSyncStatuses(ctx)
	if err != nil || len(statuses) == 0 {
		return b.getEventTracker(u).State()
	}

	tokens := make(map[string]string, len(statuses))
	for calID, st := range statuses {
		tok := st.SyncToken
		if tok == "" {
			tok = st.CTag
		}
		if tok != "" {
			tokens[calID] = tok
		}
	}
	if len(tokens) == 0 {
		return b.getEventTracker(u).State()
	}
	return encodeSyncState(tokens)
}

// CalendarEventChanges resolves changes since sinceState by leveraging CalDAV collection sync-tokens/CTags
// and RFC 6578 sync-collection REPORT to retrieve only modified/deleted entities.
func (b *CalendarsBackend) CalendarEventChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool) {
	u := b.user(ctx)
	if !strings.HasPrefix(sinceState, "sync-v1:") {
		return b.getEventTracker(u).Changes(sinceState)
	}

	oldTokens, err := decodeSyncState(sinceState)
	if err != nil {
		return nil, nil, nil, "", false
	}

	statuses, err := b.client.GetCalendarSyncStatuses(ctx)
	if err != nil {
		return nil, nil, nil, "", false
	}

	newTokens := make(map[string]string, len(statuses))
	for calID, st := range statuses {
		tok := st.SyncToken
		if tok == "" {
			tok = st.CTag
		}
		if tok != "" {
			newTokens[calID] = tok
		}
	}
	newState = encodeSyncState(newTokens)

	allMatch := true
	if len(oldTokens) != len(newTokens) {
		allMatch = false
	} else {
		for calID, oldTok := range oldTokens {
			if newTokens[calID] != oldTok {
				allMatch = false
				break
			}
		}
	}

	if allMatch {
		return []jmapcore.Id{}, []jmapcore.Id{}, []jmapcore.Id{}, newState, false
	}

	var createdList, updatedList, destroyedList []jmapcore.Id
	seenCreated := make(map[jmapcore.Id]bool)
	seenUpdated := make(map[jmapcore.Id]bool)
	seenDestroyed := make(map[jmapcore.Id]bool)

	for calID, newTok := range newTokens {
		oldTok, hadCal := oldTokens[calID]
		if !hadCal {
			// The calendar is new to this state. Discover its resources from CalDAV
			// ETags (a body-less PROPFIND) rather than downloading every event.
			etags, err := b.client.ListCalendarObjectETags(ctx, calID)
			if err == nil {
				for eventID := range etags {
					id := jmapcore.Id(eventID)
					if !seenCreated[id] {
						seenCreated[id] = true
						createdList = append(createdList, id)
					}
				}
			}
			continue
		}

		if oldTok == newTok {
			continue
		}

		res, err := b.client.SyncCalendarCollection(ctx, calID, oldTok)
		if err != nil {
			return nil, nil, nil, "", false
		}

		for _, ch := range res.Changes {
			id := jmapcore.Id(ch.EventID)
			if ch.Deleted {
				delete(seenCreated, id)
				delete(seenUpdated, id)
				if !seenDestroyed[id] {
					seenDestroyed[id] = true
					destroyedList = append(destroyedList, id)
				}
			} else {
				isCreate := ch.Created || b.getEventTracker(u).ActionForID(id) == "create"
				if isCreate {
					if !seenDestroyed[id] && !seenCreated[id] {
						seenCreated[id] = true
						createdList = append(createdList, id)
					}
				} else {
					if !seenDestroyed[id] && !seenUpdated[id] && !seenCreated[id] {
						seenUpdated[id] = true
						updatedList = append(updatedList, id)
					}
				}
			}
		}
	}

	if createdList == nil {
		createdList = []jmapcore.Id{}
	}
	if updatedList == nil {
		updatedList = []jmapcore.Id{}
	}
	if destroyedList == nil {
		destroyedList = []jmapcore.Id{}
	}

	return createdList, updatedList, destroyedList, newState, false
}

func (b *CalendarsBackend) GetAllCalendarEvents(ctx context.Context) ([]*jmapcalendar.CalendarEvent, error) {
	evs, _, err := b.GetCalendarEvents(ctx, nil)
	return evs, err
}

func (b *CalendarsBackend) buildEventResponse(eventsMap map[jmapcore.Id]*jmapcalendar.CalendarEvent, ids []jmapcore.Id) ([]*jmapcalendar.CalendarEvent, []jmapcore.Id) {
	var list []*jmapcalendar.CalendarEvent
	var notFound []jmapcore.Id
	if ids == nil {
		for _, ev := range eventsMap {
			list = append(list, ev)
		}
	} else if len(ids) == 0 {
		list = []*jmapcalendar.CalendarEvent{}
		notFound = []jmapcore.Id{}
	} else {
		for _, id := range ids {
			if ev, ok := eventsMap[id]; ok {
				list = append(list, ev)
			} else if strings.Contains(string(id), "#") {
				parts := strings.SplitN(string(id), "#", 2)
				masterID := jmapcore.Id(parts[0])
				recID := parts[1]
				if master, okMaster := eventsMap[masterID]; okMaster {
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
					masterIDCopy := masterID
					inst.BaseEventID = &masterIDCopy
					inst.RecurrenceRules = nil
					inst.ExcludedRecurrenceRules = nil
					inst.RecurrenceOverrides = nil
					if len(master.RecurrenceRules) == 0 && len(master.RecurrenceOverrides) == 0 {
						inst.Start = master.Start
						inst.RecurrenceID = ""
					} else {
						inst.Start = recID
						inst.RecurrenceID = recID
						if override, okOv := master.RecurrenceOverrides[recID]; okOv {
							_ = applyEventPatch(&inst, override)
							inst.RecurrenceID = recID
						}
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
		notFound = []jmapcore.Id{}
	}
	return list, notFound
}

// applyCachedEventFields overlays the per-user fields kept in the backend cache (draft
// state, time zone, keywords, recurrence overrides, participants) onto a freshly parsed
// event. The CalDAV resource is the source of truth for event data; these fields are
// proxy-local metadata that CalDAV does not carry.
func (b *CalendarsBackend) applyCachedEventFields(u string, ev *jmapcalendar.CalendarEvent) {
	cached, okCached := b.cache.GetEvent(u, ev.ID)
	if !okCached || cached == nil {
		return
	}
	ev.IsDraft = cached.IsDraft
	ev.MayInviteSelf = cached.MayInviteSelf
	ev.MayInviteOthers = cached.MayInviteOthers
	ev.HideAttendees = cached.HideAttendees
	ev.UseDefaultAlerts = cached.UseDefaultAlerts
	if cached.TimeZone != "" {
		ev.TimeZone = cached.TimeZone
	}
	if ev.OrganizerCalendarAddress == "" && cached.OrganizerCalendarAddress != "" {
		ev.OrganizerCalendarAddress = cached.OrganizerCalendarAddress
	}
	if len(cached.CalendarIDs) > 0 {
		if ev.CalendarIDs == nil {
			ev.CalendarIDs = make(map[jmapcore.Id]bool)
		}
		for cid, isSet := range cached.CalendarIDs {
			if isSet {
				ev.CalendarIDs[cid] = true
			}
		}
	}
	if len(ev.RecurrenceOverrides) == 0 && len(cached.RecurrenceOverrides) > 0 {
		ev.RecurrenceOverrides = cached.RecurrenceOverrides
	}
	if len(ev.Excluded) == 0 && len(cached.Excluded) > 0 {
		ev.Excluded = cached.Excluded
	}
	if len(cached.Participants) > 0 {
		ev.Participants = cached.Participants
	}
	if len(cached.Keywords) > 0 && len(ev.Keywords) == 0 {
		ev.Keywords = cached.Keywords
	}
}

func (b *CalendarsBackend) fetchEventsForCalendars(ctx context.Context, cals []*jmapcalendar.Calendar, start, end time.Time) (map[jmapcore.Id]*jmapcalendar.CalendarEvent, error) {
	u := b.user(ctx)
	windowed := !start.IsZero() || !end.IsZero()
	type calResult struct {
		calID jmapcore.Id
		objs  []*CalendarObjectInfo
	}
	const maxConcurrentQueries = 10
	sem := make(chan struct{}, maxConcurrentQueries)
	resChan := make(chan calResult, len(cals))
	var wg sync.WaitGroup

	for _, cal := range cals {
		wg.Add(1)
		sem <- struct{}{}
		go func(cal *jmapcalendar.Calendar) {
			defer func() { <-sem }()
			defer wg.Done()
			var objs []*CalendarObjectInfo
			var qErr error
			if windowed {
				objs, qErr = b.client.QueryCalendarObjectsInRange(ctx, string(cal.ID), start, end)
			} else {
				objs, qErr = b.client.QueryCalendarObjects(ctx, string(cal.ID))
			}
			if qErr == nil {
				resChan <- calResult{calID: cal.ID, objs: objs}
			}
		}(cal)
	}
	wg.Wait()
	close(resChan)

	freshMap := make(map[jmapcore.Id]*jmapcalendar.CalendarEvent)
	for res := range resChan {
		for _, calObj := range res.objs {
			if calObj.Data == nil || calObj.ID == "" {
				continue
			}
			evID := jmapcore.Id(calObj.ID)

			parsedList, pErr := jmapcalendar.CalendarEventsFromICalendar(calObj.Data)
			if pErr == nil && len(parsedList) > 0 {
				ev := parsedList[0]
				ev.ID = evID
				if existing, ok := freshMap[evID]; ok {
					if existing.CalendarIDs == nil {
						existing.CalendarIDs = make(map[jmapcore.Id]bool)
					}
					existing.CalendarIDs[res.calID] = true
					continue
				}
				if ev.CalendarIDs == nil {
					ev.CalendarIDs = make(map[jmapcore.Id]bool)
				}
				ev.CalendarIDs[res.calID] = true
				b.applyCachedEventFields(u, ev)
				freshMap[evID] = ev
			}
		}
	}
	return freshMap, nil
}

func (b *CalendarsBackend) GetCalendarEvents(ctx context.Context, ids []jmapcore.Id) ([]*jmapcalendar.CalendarEvent, []jmapcore.Id, error) {
	return b.getCalendarEventsWindowed(ctx, ids, time.Time{}, time.Time{}, nil)
}

// getCalendarEventsWindowed retrieves CalendarEvents, optionally constrained to the
// half-open time window [start, end). A bounded window is pushed down to CalDAV as a
// time-range filter so the upstream server returns only resources overlapping the
// window instead of the entire calendar collection. When cals is nil the calendar
// collection list is discovered; callers that already have it pass it in to avoid a
// redundant CalDAV discovery round-trip.
func (b *CalendarsBackend) getCalendarEventsWindowed(ctx context.Context, ids []jmapcore.Id, start, end time.Time, cals []*jmapcalendar.Calendar) ([]*jmapcalendar.CalendarEvent, []jmapcore.Id, error) {
	u := b.user(ctx)
	windowed := !start.IsZero() || !end.IsZero()
	windowKey := eventWindowKeyFor(start, end)

	rc := jmapcore.RequestCacheFrom(ctx)
	if evMap, ok := loadRequestEvents(rc, u, windowKey); ok && evMap != nil {
		list, notFound := b.buildEventResponse(evMap, ids)
		return list, notFound, nil
	}

	// A CalendarEvent/get with explicit ids often follows a bounded
	// CalendarEvent/query in the same request. Reuse any events already fetched
	// into other windows rather than re-scanning the whole collection.
	if ids != nil && len(ids) > 0 {
		if evMap, ok := lookupRequestEvents(rc, u, ids); ok {
			storeRequestEvents(rc, u, windowKey, evMap)
			list, notFound := b.buildEventResponse(evMap, ids)
			return list, notFound, nil
		}
	}

	if cals == nil {
		cals, _, _ = b.GetCalendars(ctx, nil)
	}

	// Targeted retrieval: when specific IDs are requested, fetch them with one
	// calendar-multiget REPORT per calendar instead of one GET per id per calendar.
	// The latter is O(ids x calendars) round-trips and is catastrophic when the
	// upstream CalDAV server is slow.
	if ids != nil && len(ids) > 0 {
		masterSet := make(map[jmapcore.Id]bool)
		for _, id := range ids {
			baseID := id
			if strings.Contains(string(id), "#") {
				baseID = jmapcore.Id(strings.SplitN(string(id), "#", 2)[0])
			}
			masterSet[baseID] = true
		}
		masterIDs := make([]string, 0, len(masterSet))
		for id := range masterSet {
			masterIDs = append(masterIDs, string(id))
		}

		foundObjs := make(map[jmapcore.Id]*CalendarObjectInfo, len(masterSet))
		foundCals := make(map[jmapcore.Id]map[jmapcore.Id]bool, len(masterSet))
		for _, cal := range cals {
			objs, qErr := b.client.GetCalendarObjects(ctx, string(cal.ID), masterIDs)
			if qErr != nil {
				continue
			}
			for name, obj := range objs {
				mID := jmapcore.Id(name)
				if foundObjs[mID] == nil {
					foundObjs[mID] = obj
				}
				if foundCals[mID] == nil {
					foundCals[mID] = make(map[jmapcore.Id]bool)
				}
				foundCals[mID][cal.ID] = true
			}
		}

		if len(foundObjs) == len(masterSet) {
			targetedEvents := make(map[jmapcore.Id]*jmapcalendar.CalendarEvent, len(foundObjs))
			for mID, obj := range foundObjs {
				if obj.Data == nil {
					continue
				}
				parsedList, pErr := jmapcalendar.CalendarEventsFromICalendar(obj.Data)
				if pErr != nil || len(parsedList) == 0 {
					continue
				}
				ev := parsedList[0]
				ev.ID = mID
				if ev.CalendarIDs == nil {
					ev.CalendarIDs = make(map[jmapcore.Id]bool)
				}
				for cid := range foundCals[mID] {
					ev.CalendarIDs[cid] = true
				}
				b.applyCachedEventFields(u, ev)
				targetedEvents[mID] = ev
			}
			if len(targetedEvents) == len(masterSet) {
				b.cache.StoreEvents(u, targetedEvents)
				storeRequestEvents(rc, u, windowKey, targetedEvents)
				list, notFound := b.buildEventResponse(targetedEvents, ids)
				return list, notFound, nil
			}
		}
	}

	freshMap, err := b.fetchEventsForCalendars(ctx, cals, start, end)
	if err != nil {
		return nil, nil, err
	}

	if windowed {
		// A bounded window is a partial view: merge it into the backend cache for
		// targeted lookups, but never treat it as the full event set and never derive
		// external-change fingerprints or state changes from it.
		b.cache.StoreEvents(u, freshMap)
	} else {
		b.mu.Lock()
		newFp := eventsMapFingerprint(freshMap)
		oldFp := b.eventsFingerprint[u]
		b.eventsFingerprint[u] = newFp
		b.cache.SetEvents(u, freshMap)
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
	}

	storeRequestEvents(rc, u, windowKey, freshMap)

	list, notFound := b.buildEventResponse(freshMap, ids)
	return list, notFound, nil
}

func (b *CalendarsBackend) CreateCalendarEvent(ctx context.Context, event *jmapcalendar.CalendarEvent) (*jmapcalendar.CalendarEvent, error) {
	return b.putCalendarEvent(ctx, event, false)
}

func (b *CalendarsBackend) putCalendarEvent(ctx context.Context, event *jmapcalendar.CalendarEvent, isUpdate bool) (*jmapcalendar.CalendarEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}
	u := b.user(ctx)
	if rc := jmapcore.RequestCacheFrom(ctx); rc != nil {
		rc.Delete(reqCacheEventsKey{user: u})
	}
	if event.ID == "" {
		// The JMAP id is the CalDAV resource name; use a ".ics" resource for events we
		// create ourselves (RFC 4791 Section 5.3.1 permits, but does not require, it).
		event.ID = jmapcore.Id(fmt.Sprintf("event-%d.ics", time.Now().UnixNano()))
	}
	if event.UID == "" {
		event.UID = strings.TrimSuffix(string(event.ID), ".ics")
	}
	if event.TimeZone == "" {
		event.TimeZone = "Etc/UTC"
	}
	if event.UTCStart == "" {
		event.UTCStart = jmapcalendar.ComputeUTCStart(event.Start, event.TimeZone)
	}
	if event.UTCEnd == "" {
		event.UTCEnd = jmapcalendar.ComputeUTCEnd(event.Start, event.Duration, event.TimeZone)
	}

	cals, _, _ := b.GetCalendars(ctx, nil)
	calMap := make(map[jmapcore.Id]bool, len(cals))
	var defaultID jmapcore.Id
	for _, c := range cals {
		calMap[c.ID] = true
		if c.IsDefault && defaultID == "" {
			defaultID = c.ID
		}
	}
	if defaultID == "" && len(cals) > 0 {
		defaultID = cals[0].ID
	}

	if len(event.CalendarIDs) > 0 {
		for cid := range event.CalendarIDs {
			if cid == "cal-default" {
				delete(event.CalendarIDs, "cal-default")
				if defaultID != "" {
					event.CalendarIDs[defaultID] = true
				}
			} else if len(calMap) > 0 && !calMap[cid] {
				return nil, jmapcore.SetError{Type: "notFound", Description: fmt.Sprintf("calendar %s not found", cid)}
			}
		}
	}

	// METHOD is an iTIP transport property, not part of the stored JSCalendar
	// object; RFC 4791 Section 4.1 forbids it in a calendar object resource.
	event.Method = ""
	calObj := jmapcalendar.CalendarEventToICalendar(event, "", "", "", "")
	written := false
	for cid, isSet := range event.CalendarIDs {
		if isSet && cid != "" {
			putErr := b.client.PutCalendarObject(ctx, string(cid), string(event.ID), calObj)
			if putErr != nil {
				return nil, fmt.Errorf("failed to put calendar object via nextcloud backend: %w", putErr)
			}
			written = true
		}
	}
	if !written {
		if defaultID == "" {
			return nil, jmapcore.SetError{Type: "notFound", Description: "no calendar available to create event"}
		}
		putErr := b.client.PutCalendarObject(ctx, string(defaultID), string(event.ID), calObj)
		if putErr != nil {
			return nil, fmt.Errorf("failed to put calendar object via nextcloud backend: %w", putErr)
		}
		if event.CalendarIDs == nil {
			event.CalendarIDs = make(map[jmapcore.Id]bool)
		}
		event.CalendarIDs[defaultID] = true
	}

	b.cache.StoreEvent(u, event)
	action := "create"
	if isUpdate {
		action = "update"
	}
	b.mu.Lock()
	st := b.getEventTracker(u).Record(event.ID, action)
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEvent", st)
	b.scheduleEventAlerts(u, event)
	return event, nil
}

func parseAlertOffset(alert *jmapcalendar.JSCalendarAlert) time.Duration {
	if alert == nil || alert.Trigger == nil {
		return 0
	}
	var offsetStr string
	switch t := alert.Trigger.(type) {
	case string:
		offsetStr = t
	case jmapcalendar.OffsetTrigger:
		offsetStr = t.Offset
	case *jmapcalendar.OffsetTrigger:
		offsetStr = t.Offset
	case map[string]any:
		if s, ok := t["offset"].(string); ok {
			offsetStr = s
		}
	}
	if offsetStr == "" {
		return 0
	}
	neg := false
	if strings.HasPrefix(offsetStr, "-") {
		neg = true
		offsetStr = strings.TrimPrefix(offsetStr, "-")
	} else if strings.HasPrefix(offsetStr, "+") {
		offsetStr = strings.TrimPrefix(offsetStr, "+")
	}

	prop := ical.NewProp(ical.PropDuration)
	prop.Value = offsetStr
	dur, err := prop.Duration()
	if err != nil {
		return 0
	}
	if neg {
		dur = -dur
	}
	return dur
}

func parseEventTime(startStr, tzName string) time.Time {
	loc := time.UTC
	if tzName != "" && tzName != "Etc/UTC" {
		if l, err := time.LoadLocation(tzName); err == nil {
			loc = l
		}
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, startStr, loc); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (b *CalendarsBackend) scheduleEventAlerts(user string, ev *jmapcalendar.CalendarEvent) {
	b.mu.RLock()
	bc := b.broadcaster
	b.mu.RUnlock()
	if bc == nil || ev == nil || len(ev.Alerts) == 0 || ev.Start == "" {
		return
	}
	accountID := jmapauth.AccountIDForSubject(user)
	startTime := parseEventTime(ev.Start, ev.TimeZone)
	if startTime.IsZero() {
		return
	}
	now := time.Now()
	for alertID, alert := range ev.Alerts {
		if alert == nil {
			continue
		}
		offsetDur := parseAlertOffset(alert)
		triggerTime := startTime.Add(offsetDur)
		delay := triggerTime.Sub(now)
		if delay < 0 {
			continue
		}
		aID := alertID
		evID := string(ev.ID)
		evUID := ev.UID
		time.AfterFunc(delay, func() {
			calAlert := &jmappush.CalendarAlert{
				Type:            "CalendarAlert",
				AccountID:       accountID,
				CalendarEventID: evID,
				UID:             evUID,
				RecurrenceID:    nil,
				AlertID:         aID,
			}
			bc.PublishCalendarAlert(calAlert)
		})
	}
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
	if len(parts) == 2 {
		sub, ok := m[key].(map[string]any)
		if !ok {
			if val == nil {
				return
			}
			sub = make(map[string]any)
			m[key] = sub
		}
		if val == nil || (val == false && (key == "calendarIds" || key == "mailboxIds" || key == "keywords" || key == "addressBookIds" || key == "roles")) {
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

func applyCalendarPatch(cal *jmapcalendar.Calendar, patch map[string]any) error {
	raw, err := json.Marshal(cal)
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
	origID := cal.ID
	origRights := cal.MyRights
	var updatedCal jmapcalendar.Calendar
	if err := json.Unmarshal(rawUpdated, &updatedCal); err != nil {
		return err
	}
	updatedCal.ID = origID
	updatedCal.MyRights = origRights
	*cal = updatedCal
	return nil
}

func applyEventPatch(ev *jmapcalendar.CalendarEvent, patch map[string]any) error {
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
	var updatedEv jmapcalendar.CalendarEvent
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
			updatedEv.CalendarIDs = make(map[jmapcore.Id]bool)
		}
		updatedEv.CalendarIDs[jmapcore.Id(cid)] = true
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
			updatedEv.Locations = map[string]*jmapcalendar.JSCalendarLocation{
				"loc-1": {
					Type: "Location",
					Name: locStr,
				},
			}
		} else if locBytes, err := json.Marshal(locVal); err == nil {
			var loc jmapcalendar.JSCalendarLocation
			if err := json.Unmarshal(locBytes, &loc); err == nil {
				if loc.Type == "" {
					loc.Type = "Location"
				}
				updatedEv.Locations = map[string]*jmapcalendar.JSCalendarLocation{
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
			updatedEv.Duration = jmapcalendar.IcalDurationBetween(updatedEv.Start, end)
		}
	}
	if rrule, hasRrule := m["recurrenceRule"]; hasRrule {
		if rrule == nil {
			updatedEv.RecurrenceRules = nil
			updatedEv.RecurrenceRule = nil
		} else if rruleBytes, err := json.Marshal(rrule); err == nil {
			var r jmapcalendar.JSCalendarRecurrenceRule
			if err := json.Unmarshal(rruleBytes, &r); err == nil {
				updatedEv.RecurrenceRule = &r
				updatedEv.RecurrenceRules = []*jmapcalendar.JSCalendarRecurrenceRule{&r}
			}
		}
	}
	if exrule, hasExrule := m["excludedRecurrenceRule"]; hasExrule {
		if exrule == nil {
			updatedEv.ExcludedRecurrenceRules = nil
			updatedEv.ExcludedRecurrenceRule = nil
		} else if exruleBytes, err := json.Marshal(exrule); err == nil {
			var r jmapcalendar.JSCalendarRecurrenceRule
			if err := json.Unmarshal(exruleBytes, &r); err == nil {
				updatedEv.ExcludedRecurrenceRule = &r
				updatedEv.ExcludedRecurrenceRules = []*jmapcalendar.JSCalendarRecurrenceRule{&r}
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
	if utcStartStr, ok := patch["utcStart"].(string); ok && utcStartStr != "" {
		if t, err := time.Parse(time.RFC3339, utcStartStr); err == nil {
			loc := time.UTC
			if updatedEv.TimeZone != "" && updatedEv.TimeZone != "floating" {
				loc = jmapcalendar.LoadLocation(updatedEv.TimeZone)
			}
			updatedEv.Start = t.In(loc).Format("2006-01-02T15:04:05")
			updatedEv.UTCStart = t.UTC().Format(time.RFC3339)
		}
	}
	if utcEndStr, ok := patch["utcEnd"].(string); ok && utcEndStr != "" {
		if updatedEv.UTCStart != "" {
			if dur := jmapcalendar.IcalDurationBetween(updatedEv.UTCStart, utcEndStr); dur != "" {
				updatedEv.Duration = dur
			}
		}
		updatedEv.UTCEnd = utcEndStr
	}
	updatedEv.Start = strings.TrimSuffix(updatedEv.Start, "Z")
	updatedEv.UTCStart = jmapcalendar.ComputeUTCStart(updatedEv.Start, updatedEv.TimeZone)
	updatedEv.UTCEnd = jmapcalendar.ComputeUTCEnd(updatedEv.Start, updatedEv.Duration, updatedEv.TimeZone)

	for _, p := range updatedEv.Participants {
		if p != nil {
			if p.ParticipationStatus != "" && (p.Status == "" || p.Status != p.ParticipationStatus) {
				p.Status = p.ParticipationStatus
			} else if p.Status != "" && p.ParticipationStatus == "" {
				p.ParticipationStatus = p.Status
			}
		}
	}

	if updatedEv.OrganizerCalendarAddress == "" && len(updatedEv.Participants) > 0 {
		for _, p := range updatedEv.Participants {
			if p != nil && (p.Role == "owner" || (p.Roles != nil && p.Roles["owner"])) {
				updatedEv.OrganizerCalendarAddress = p.CalendarAddress
				break
			}
		}
		if updatedEv.OrganizerCalendarAddress == "" {
			for _, p := range updatedEv.Participants {
				if p != nil && p.CalendarAddress != "" {
					updatedEv.OrganizerCalendarAddress = p.CalendarAddress
					break
				}
			}
		}
	}

	*ev = updatedEv
	return nil
}

func (b *CalendarsBackend) UpdateCalendarEvent(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapcalendar.CalendarEvent, error) {
	if strings.Contains(string(id), "#") {
		parts := strings.SplitN(string(id), "#", 2)
		masterID := jmapcore.Id(parts[0])
		recID := parts[1]
		masters, _, err := b.GetCalendarEvents(ctx, []jmapcore.Id{masterID})
		if err != nil || len(masters) == 0 {
			return nil, jmapcore.SetError{Type: "notFound", Description: fmt.Sprintf("event %s not found", id)}
		}
		master := masters[0]
		if len(master.RecurrenceRules) == 0 && len(master.RecurrenceOverrides) == 0 {
			return b.UpdateCalendarEvent(ctx, masterID, patch)
		}
		if master.Excluded != nil && master.Excluded[recID] {
			return nil, jmapcore.SetError{Type: "notFound", Description: fmt.Sprintf("instance %s not found", id)}
		}
		validOccurrence := false
		if _, ok := master.RecurrenceOverrides[recID]; ok {
			validOccurrence = true
		} else {
			instances := jmapcalendar.ExpandRecurrenceInstances(master, time.Time{})
			for _, inst := range instances {
				if inst.RecurrenceID == recID || strings.TrimSuffix(inst.RecurrenceID, "Z") == strings.TrimSuffix(recID, "Z") {
					validOccurrence = true
					break
				}
			}
		}
		if !validOccurrence {
			return nil, jmapcore.SetError{Type: "notFound", Description: fmt.Sprintf("instance %s not found", id)}
		}

		if master.RecurrenceOverrides == nil {
			master.RecurrenceOverrides = make(map[string]map[string]any)
		}
		overridePatch, exists := master.RecurrenceOverrides[recID]
		if !exists || overridePatch == nil {
			overridePatch = make(map[string]any)
			overridePatch["start"] = recID
			overridePatch["duration"] = master.Duration
			if len(master.Locations) > 0 {
				locMap := make(map[string]any)
				for k, v := range master.Locations {
					locBytes, _ := json.Marshal(v)
					var m any
					_ = json.Unmarshal(locBytes, &m)
					locMap[k] = m
				}
				overridePatch["locations"] = locMap
			}
		}
		for k, v := range patch {
			cleanKey := strings.TrimPrefix(k, "/")
			firstPart := strings.Split(cleanKey, "/")[0]
			if firstPart == "@type" || firstPart == "uid" || firstPart == "recurrenceRule" ||
				firstPart == "recurrenceRules" || firstPart == "privacy" || firstPart == "participants" {
				continue
			}
			parts := strings.Split(cleanKey, "/")
			setNestedMapValue(overridePatch, parts, v)
		}
		master.RecurrenceOverrides[recID] = overridePatch
		master.Sequence++
		master.Updated = time.Now().UTC().Format(time.RFC3339)
		return b.putCalendarEvent(ctx, master, true)
	}

	u := b.user(ctx)
	var ev *jmapcalendar.CalendarEvent
	if cached, ok := b.cache.GetEvent(u, id); ok && cached != nil {
		evCopy := *cached
		ev = &evCopy
	}

	if ev == nil {
		events, notFound, err := b.GetCalendarEvents(ctx, []jmapcore.Id{id})
		if err != nil || len(notFound) > 0 || len(events) == 0 {
			return nil, fmt.Errorf("event %s not found: %w", id, jmapcore.ErrNotFound)
		}
		ev = events[0]
	}

	oldCalendarIDs := make(map[jmapcore.Id]bool)
	for cid, isSet := range ev.CalendarIDs {
		if isSet && cid != "" && cid != "cal-default" {
			oldCalendarIDs[cid] = true
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
	ev.UTCStart = jmapcalendar.ComputeUTCStart(ev.Start, ev.TimeZone)
	ev.UTCEnd = jmapcalendar.ComputeUTCEnd(ev.Start, ev.Duration, ev.TimeZone)

	for oldCID := range oldCalendarIDs {
		if !ev.CalendarIDs[oldCID] {
			_ = b.client.DeleteCalendarObject(ctx, string(oldCID), string(id))
		}
	}

	return b.putCalendarEvent(ctx, ev, true)
}

func (b *CalendarsBackend) DeleteCalendarEvent(ctx context.Context, id jmapcore.Id) (bool, error) {
	if strings.Contains(string(id), "#") {
		parts := strings.SplitN(string(id), "#", 2)
		masterID := jmapcore.Id(parts[0])
		recID := parts[1]
		masters, _, err := b.GetCalendarEvents(ctx, []jmapcore.Id{masterID})
		if err == nil && len(masters) > 0 {
			master := masters[0]
			if len(master.RecurrenceRules) == 0 && len(master.RecurrenceOverrides) == 0 {
				return b.DeleteCalendarEvent(ctx, masterID)
			}
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
			_, _ = b.putCalendarEvent(ctx, master, true)
			return true, nil
		}
		return false, nil
	}

	events, notFound, err := b.GetCalendarEvents(ctx, []jmapcore.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(events) == 0 {
		return false, nil
	}
	targetEv := events[0]

	u := b.user(ctx)
	if len(targetEv.CalendarIDs) > 0 {
		for cid, isSet := range targetEv.CalendarIDs {
			if isSet && cid != "" && cid != "cal-default" {
				_ = b.client.DeleteCalendarObject(ctx, string(cid), string(id))
			}
		}
	} else {
		defaultID := jmapcore.Id("")
		if cals, _, err := b.GetCalendars(ctx, nil); err == nil {
			for _, c := range cals {
				if c.IsDefault {
					defaultID = c.ID
					break
				}
			}
			if defaultID == "" && len(cals) > 0 {
				defaultID = cals[0].ID
			}
		}
		if defaultID != "" {
			_ = b.client.DeleteCalendarObject(ctx, string(defaultID), string(id))
		}
	}

	b.cache.DeleteEvent(u, id)
	b.mu.Lock()
	st := b.getEventTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	if rc := jmapcore.RequestCacheFrom(ctx); rc != nil {
		rc.Delete(reqCacheEventsKey{user: u})
	}

	b.emitStateChange(u, "CalendarEvent", st)
	return true, nil
}

func (b *CalendarsBackend) QueryCalendarEvents(ctx context.Context, filter map[string]any, sortCriteria []jmapcore.Comparator, position int, limit *uint64, expandRecurrences bool) ([]jmapcore.Id, int, error) {
	cals, _, err := b.GetCalendars(ctx, nil)
	if err != nil {
		return nil, 0, err
	}

	targetCals := cals
	if inCals, ok := filter["inCalendars"].([]any); ok && len(inCals) > 0 {
		calSet := make(map[string]bool)
		for _, ic := range inCals {
			if s, okS := ic.(string); okS {
				calSet[s] = true
			}
		}
		if len(calSet) > 0 {
			var filtered []*jmapcalendar.Calendar
			for _, c := range cals {
				if calSet[string(c.ID)] {
					filtered = append(filtered, c)
				}
			}
			targetCals = filtered
		}
	} else if inCal, ok := filter["inCalendar"].(string); ok && inCal != "" {
		var filtered []*jmapcalendar.Calendar
		for _, c := range cals {
			if string(c.ID) == inCal {
				filtered = append(filtered, c)
			}
		}
		targetCals = filtered
	}

	if len(targetCals) == 0 {
		return []jmapcore.Id{}, 0, nil
	}

	loc := time.UTC
	if tz, ok := filter["__timeZone"].(string); ok && tz != "" {
		loc = jmapcalendar.LoadLocation(tz)
	}

	// Push the filter's after/before window down to CalDAV as a time-range so the
	// backend only transfers events overlapping the requested range.
	windowStart, windowEnd := eventTimeWindow(filter, loc)

	var events []*jmapcalendar.CalendarEvent
	if len(targetCals) == len(cals) {
		events, _, err = b.getCalendarEventsWindowed(ctx, nil, windowStart, windowEnd, cals)
		if err != nil {
			return nil, 0, err
		}
	} else {
		freshMap, fErr := b.fetchEventsForCalendars(ctx, targetCals, windowStart, windowEnd)
		if fErr != nil {
			return nil, 0, fErr
		}
		events, _ = b.buildEventResponse(freshMap, nil)
	}

	var resultIDs []jmapcore.Id
	if expandRecurrences {
		horizon := time.Now().AddDate(2, 0, 0)
		if beforeStr, _ := filter["before"].(string); beforeStr != "" {
			if bt, ok := jmapcalendar.ParseLocalDateTimeBound(beforeStr, loc); ok {
				if bt.After(horizon) {
					horizon = bt.AddDate(0, 0, 1)
				}
			}
		}

		var expandedList []*jmapcalendar.CalendarEvent
		for _, ev := range events {
			hasRules := len(ev.RecurrenceRules) > 0 || len(ev.RecurrenceOverrides) > 0
			if hasRules {
				instances := jmapcalendar.ExpandRecurrenceInstances(ev, horizon)
				for _, inst := range instances {
					instEv := *ev
					instEv.ID = jmapcore.Id(fmt.Sprintf("%s#%s", string(ev.ID), inst.RecurrenceID))
					evIDCopy := ev.ID
					instEv.BaseEventID = &evIDCopy
					instEv.RecurrenceRules = nil
					instEv.ExcludedRecurrenceRules = nil
					instEv.RecurrenceOverrides = nil
					instEv.RecurrenceID = inst.RecurrenceID
					if ov, ok := ev.RecurrenceOverrides[inst.RecurrenceID]; ok {
						_ = applyEventPatch(&instEv, ov)
						instEv.RecurrenceID = inst.RecurrenceID
					} else {
						if strings.HasSuffix(ev.Start, "Z") {
							instEv.Start = inst.Start.UTC().Format(time.RFC3339)
						} else {
							instLoc := loc
							if ev.TimeZone != "" && ev.TimeZone != "floating" {
								instLoc = jmapcalendar.LoadLocation(ev.TimeZone)
							}
							instEv.Start = inst.Start.In(instLoc).Format("2006-01-02T15:04:05")
						}
					}
					instEv.UTCStart = jmapcalendar.ComputeUTCStart(instEv.Start, instEv.TimeZone)
					instEv.UTCEnd = jmapcalendar.ComputeUTCEnd(instEv.Start, instEv.Duration, instEv.TimeZone)

					if jmapcalendar.MatchCalendarEvent(&instEv, filter) {
						expandedList = append(expandedList, &instEv)
					}
				}
			} else {
				instEv := *ev
				instEv.ID = jmapcore.Id(fmt.Sprintf("%s#%s", string(ev.ID), ev.Start))
				evIDCopy := ev.ID
				instEv.BaseEventID = &evIDCopy
				if jmapcalendar.MatchCalendarEvent(&instEv, filter) {
					expandedList = append(expandedList, &instEv)
				}
			}
		}

		jmapcalendar.SortCalendarEvents(expandedList, sortCriteria)

		for _, ev := range expandedList {
			resultIDs = append(resultIDs, ev.ID)
		}
	} else {
		var matched []*jmapcalendar.CalendarEvent
		for _, ev := range events {
			if jmapcalendar.MatchCalendarEvent(ev, filter) {
				matched = append(matched, ev)
			}
		}
		jmapcalendar.SortCalendarEvents(matched, sortCriteria)
		for _, ev := range matched {
			resultIDs = append(resultIDs, ev.ID)
		}
	}

	total := len(resultIDs)
	position = jmapcore.NormalizePosition(position, total)
	if position >= total {
		return []jmapcore.Id{}, total, nil
	}

	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}

	ids := make([]jmapcore.Id, 0, end-position)
	for i := position; i < end; i++ {
		ids = append(ids, resultIDs[i])
	}

	return ids, total, nil
}

// ParticipantIdentities
func (b *CalendarsBackend) ParticipantIdentityState(ctx context.Context) string {
	return b.getIdentityTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) ParticipantIdentityChanges(ctx context.Context, sinceState string) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	return b.getIdentityTracker(b.user(ctx)).Changes(sinceState)
}

func (b *CalendarsBackend) GetAllParticipantIdentities(ctx context.Context) ([]*jmapcalendar.ParticipantIdentity, error) {
	list, _, err := b.GetParticipantIdentities(ctx, nil)
	return list, err
}

func (b *CalendarsBackend) GetParticipantIdentities(ctx context.Context, ids []jmapcore.Id) ([]*jmapcalendar.ParticipantIdentity, []jmapcore.Id, error) {
	u := b.user(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.identitiesCache[u] == nil {
		b.identitiesCache[u] = make(map[jmapcore.Id]*jmapcalendar.ParticipantIdentity)
		defaultID := jmapcore.Id("identity-default")
		b.identitiesCache[u][defaultID] = &jmapcalendar.ParticipantIdentity{
			ID:              defaultID,
			Name:            u,
			CalendarAddress: "mailto:" + u,
			SendTo:          map[string]string{"imip": "mailto:" + u},
			IsDefault:       true,
		}
	}

	var list []*jmapcalendar.ParticipantIdentity
	var notFound []jmapcore.Id
	if ids == nil {
		for _, id := range b.identitiesCache[u] {
			list = append(list, id)
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i].ID < list[j].ID
		})
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

func (b *CalendarsBackend) SetAllowedAddresses(user string, addrs []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.allowedAddresses == nil {
		b.allowedAddresses = make(map[string]map[string]bool)
	}
	m := make(map[string]bool)
	for _, a := range addrs {
		cleaned := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), "mailto:")
		if cleaned != "" {
			m[cleaned] = true
		}
	}
	b.allowedAddresses[user] = m
}

func (b *CalendarsBackend) CreateParticipantIdentity(ctx context.Context, identity *jmapcalendar.ParticipantIdentity) (*jmapcalendar.ParticipantIdentity, error) {
	u := b.user(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.identitiesCache[u] == nil {
		b.identitiesCache[u] = make(map[jmapcore.Id]*jmapcalendar.ParticipantIdentity)
	}

	if identity.CalendarAddress != "" && !strings.HasPrefix(identity.CalendarAddress, "mailto:") {
		identity.CalendarAddress = "mailto:" + identity.CalendarAddress
	}

	if b.allowedAddresses != nil && len(b.allowedAddresses[u]) > 0 {
		cleaned := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(identity.CalendarAddress)), "mailto:")
		if !b.allowedAddresses[u][cleaned] {
			return nil, jmapcore.SetError{
				Type:        "invalidProperties",
				Description: "Calendar address not configured for this account.",
				Properties:  []string{"calendarAddress"},
			}
		}
	}

	for _, existing := range b.identitiesCache[u] {
		if strings.EqualFold(existing.CalendarAddress, identity.CalendarAddress) {
			return nil, jmapcore.SetError{
				Type:        "invalidProperties",
				Description: "Calendar address already in use.",
				Properties:  []string{"calendarAddress"},
			}
		}
	}

	if identity.ID == "" {
		allSingleLetters := true
		maxLetter := byte('a' - 1)
		for existingID := range b.identitiesCache[u] {
			str := string(existingID)
			if len(str) == 1 && str[0] >= 'a' && str[0] <= 'z' {
				if str[0] > maxLetter {
					maxLetter = str[0]
				}
			} else {
				allSingleLetters = false
				break
			}
		}
		if allSingleLetters && maxLetter >= 'a'-1 && maxLetter < 'z' {
			identity.ID = jmapcore.Id(string([]byte{maxLetter + 1}))
		} else {
			identity.ID = jmapcore.Id(fmt.Sprintf("pi-%d", time.Now().UnixNano()))
		}
	}
	b.identitiesCache[u][identity.ID] = identity
	st := b.getIdentityTracker(u).Record(identity.ID, "create")

	b.emitStateChange(u, "ParticipantIdentity", st)
	return identity, nil
}

func (b *CalendarsBackend) UpdateParticipantIdentity(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapcalendar.ParticipantIdentity, error) {
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

func (b *CalendarsBackend) DeleteParticipantIdentity(ctx context.Context, id jmapcore.Id) (bool, error) {
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

func (b *CalendarsBackend) SetDefaultParticipantIdentity(ctx context.Context, id jmapcore.Id) error {
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

func (b *CalendarsBackend) CalendarEventNotificationChanges(ctx context.Context, sinceState string) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	return b.getNotificationTracker(b.user(ctx)).Changes(sinceState)
}

func (b *CalendarsBackend) GetAllCalendarEventNotifications(ctx context.Context) ([]*jmapcalendar.CalendarEventNotification, error) {
	list, _, err := b.GetCalendarEventNotifications(ctx, nil)
	return list, err
}

func (b *CalendarsBackend) GetCalendarEventNotifications(ctx context.Context, ids []jmapcore.Id) ([]*jmapcalendar.CalendarEventNotification, []jmapcore.Id, error) {
	u := b.user(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()
	var list []*jmapcalendar.CalendarEventNotification
	var notFound []jmapcore.Id
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

func (b *CalendarsBackend) CreateCalendarEventNotification(ctx context.Context, notification *jmapcalendar.CalendarEventNotification) (*jmapcalendar.CalendarEventNotification, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if b.notificationsCache[u] == nil {
		b.notificationsCache[u] = make(map[jmapcore.Id]*jmapcalendar.CalendarEventNotification)
	}
	if notification.ID == "" {
		notification.ID = jmapcore.Id(fmt.Sprintf("notif-%d", time.Now().UnixNano()))
	}
	if notification.Created == "" {
		notification.Created = time.Now().UTC().Format(time.RFC3339)
	}
	b.notificationsCache[u][notification.ID] = notification
	b.nextNotifSeq++
	if b.notifSeq[u] == nil {
		b.notifSeq[u] = make(map[jmapcore.Id]uint64)
	}
	b.notifSeq[u][notification.ID] = b.nextNotifSeq
	st := b.getNotificationTracker(u).Record(notification.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEventNotification", st)
	return notification, nil
}

func (b *CalendarsBackend) DeleteCalendarEventNotification(ctx context.Context, id jmapcore.Id) (bool, error) {
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

func (b *CalendarsBackend) QueryCalendarEventNotifications(ctx context.Context, filter map[string]any, sortCriteria []jmapcore.Comparator, position int, limit *uint64) ([]jmapcore.Id, int, error) {
	u := b.user(ctx)
	notifs, _, err := b.GetCalendarEventNotifications(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	var matched []*jmapcalendar.CalendarEventNotification
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

	resultIDs := make([]jmapcore.Id, 0, len(matched))
	for _, n := range matched {
		resultIDs = append(resultIDs, n.ID)
	}
	total := len(resultIDs)
	position = jmapcore.NormalizePosition(position, total)
	if position >= total {
		return []jmapcore.Id{}, total, nil
	}
	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}
	return resultIDs[position:end], total, nil
}

func matchCalendarEventNotification(n *jmapcalendar.CalendarEventNotification, filter map[string]any) bool {
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
				if idStr, ok := item.(string); ok && jmapcore.Id(idStr) == n.CalendarEventID {
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

func equalCalendarRights(a, b *jmapcalendar.CalendarRights) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// ShareNotificationState implements ShareNotification state per RFC 9670.
func (b *CalendarsBackend) ShareNotificationState(ctx context.Context) string {
	return b.getShareNotificationTracker(b.user(ctx)).State()
}

// ShareNotificationChanges implements ShareNotification/changes per RFC 9670.
func (b *CalendarsBackend) ShareNotificationChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool) {
	return b.getShareNotificationTracker(b.user(ctx)).Changes(sinceState)
}

// GetShareNotifications retrieves specific ShareNotifications by ID.
func (b *CalendarsBackend) GetShareNotifications(ctx context.Context, ids []jmapcore.Id) (list []*jmapcalendar.ShareNotification, notFound []jmapcore.Id, err error) {
	u := b.user(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()

	var found []*jmapcalendar.ShareNotification
	var missing []jmapcore.Id

	for _, id := range ids {
		if b.shareNotificationsCache[u] != nil && b.shareNotificationsCache[u][id] != nil {
			found = append(found, b.shareNotificationsCache[u][id])
		} else {
			missing = append(missing, id)
		}
	}
	if missing == nil {
		missing = []jmapcore.Id{}
	}
	return found, missing, nil
}

// GetAllShareNotifications retrieves all ShareNotifications for the user.
func (b *CalendarsBackend) GetAllShareNotifications(ctx context.Context) ([]*jmapcalendar.ShareNotification, error) {
	u := b.user(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmapcalendar.ShareNotification
	if b.shareNotificationsCache[u] != nil {
		for _, n := range b.shareNotificationsCache[u] {
			if n != nil {
				list = append(list, n)
			}
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})
	return list, nil
}

// QueryShareNotifications implements ShareNotification/query (RFC 9670 Section 3.4).
func (b *CalendarsBackend) QueryShareNotifications(ctx context.Context, filter map[string]any, sortCriteria []jmapcore.Comparator, position int, limit *uint64) ([]jmapcore.Id, int, error) {
	notifs, err := b.GetAllShareNotifications(ctx)
	if err != nil {
		return nil, 0, err
	}
	var matched []*jmapcalendar.ShareNotification
	for _, n := range notifs {
		if matchShareNotification(n, filter) {
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
		return matched[i].ID > matched[j].ID
	})

	resultIDs := make([]jmapcore.Id, 0, len(matched))
	for _, n := range matched {
		resultIDs = append(resultIDs, n.ID)
	}
	total := len(resultIDs)
	position = jmapcore.NormalizePosition(position, total)
	if position >= total {
		return []jmapcore.Id{}, total, nil
	}
	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}
	return resultIDs[position:end], total, nil
}

// matchShareNotification evaluates the RFC 9670 Section 3.4.1 filter conditions
// ("after", "before", "objectType", "objectAccountId") and FilterOperator trees.
func matchShareNotification(n *jmapcalendar.ShareNotification, filter map[string]any) bool {
	if n == nil {
		return false
	}
	if match, isOp := jmapcalendar.EvalFilterOperator(filter, func(cond map[string]any) bool {
		return matchShareNotification(n, cond)
	}); isOp {
		return match
	}
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
		case "objectType":
			s, _ := v.(string)
			if n.ObjectType != s {
				return false
			}
		case "objectAccountId":
			s, _ := v.(string)
			if n.ObjectAccountID != s {
				return false
			}
		}
	}
	return true
}

// CreateShareNotification creates a new ShareNotification.
func (b *CalendarsBackend) CreateShareNotification(ctx context.Context, notification *jmapcalendar.ShareNotification) (*jmapcalendar.ShareNotification, error) {
	if notification == nil {
		return nil, fmt.Errorf("notification is nil")
	}
	u := b.user(ctx)
	b.mu.Lock()
	if b.shareNotificationsCache[u] == nil {
		b.shareNotificationsCache[u] = make(map[jmapcore.Id]*jmapcalendar.ShareNotification)
	}
	b.shareNotificationsCache[u][notification.ID] = notification
	st := b.getShareNotificationTracker(u).Record(notification.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "ShareNotification", st)
	return notification, nil
}

// DeleteShareNotification deletes a ShareNotification.
func (b *CalendarsBackend) DeleteShareNotification(ctx context.Context, id jmapcore.Id) (bool, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if b.shareNotificationsCache[u] != nil {
		delete(b.shareNotificationsCache[u], id)
	}
	st := b.getShareNotificationTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "ShareNotification", st)
	return true, nil
}
