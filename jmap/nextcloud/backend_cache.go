package nextcloud

import (
	"sync"
	"time"

	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
)

// AGENTS.md §1 Architectural Invariant: Stateless Proxy & Non-Authoritative Caches.
// All calendar and event caching in backendCache is strictly non-authoritative with
// bounded TTL. Any entry is safely evictable and is refreshed from upstream CalDAV
// on miss or expiration. Constructors never seed authoritative user data.

const defaultBackendCacheTTL = 5 * time.Minute
const defaultMaxCacheEntries = 500

// backendCache encapsulates calendar and event storage caching.
type backendCache interface {
	GetCals(user string) ([]*jmapcalendar.Calendar, bool)
	SetCals(user string, cals []*jmapcalendar.Calendar)
	AppendCal(user string, cal *jmapcalendar.Calendar)
	DeleteCal(user string, calID jmapcore.Id)
	ClearCals(user string)
	HasCals(user string) bool

	GetEvent(user string, id jmapcore.Id) (*jmapcalendar.CalendarEvent, bool)
	GetEvents(user string) (map[jmapcore.Id]*jmapcalendar.CalendarEvent, bool)
	GetCalIDsForEvent(user string, id jmapcore.Id) []jmapcore.Id
	SetEvents(user string, events map[jmapcore.Id]*jmapcalendar.CalendarEvent)
	StoreEvents(user string, events map[jmapcore.Id]*jmapcalendar.CalendarEvent)
	StoreEvent(user string, event *jmapcalendar.CalendarEvent)
	DeleteEvent(user string, id jmapcore.Id)
}

// dummyBackendCache always returns empty/not found and drops updates.
type dummyBackendCache struct{}

func (d *dummyBackendCache) GetCals(user string) ([]*jmapcalendar.Calendar, bool) { return nil, false }
func (d *dummyBackendCache) SetCals(user string, cals []*jmapcalendar.Calendar)   {}
func (d *dummyBackendCache) AppendCal(user string, cal *jmapcalendar.Calendar)    {}
func (d *dummyBackendCache) DeleteCal(user string, calID jmapcore.Id)             {}
func (d *dummyBackendCache) ClearCals(user string)                                {}
func (d *dummyBackendCache) HasCals(user string) bool                             { return false }

func (d *dummyBackendCache) GetEvent(user string, id jmapcore.Id) (*jmapcalendar.CalendarEvent, bool) {
	return nil, false
}
func (d *dummyBackendCache) GetEvents(user string) (map[jmapcore.Id]*jmapcalendar.CalendarEvent, bool) {
	return nil, false
}
func (d *dummyBackendCache) GetCalIDsForEvent(user string, id jmapcore.Id) []jmapcore.Id {
	return nil
}
func (d *dummyBackendCache) SetEvents(user string, events map[jmapcore.Id]*jmapcalendar.CalendarEvent) {}
func (d *dummyBackendCache) StoreEvents(user string, events map[jmapcore.Id]*jmapcalendar.CalendarEvent) {
}
func (d *dummyBackendCache) StoreEvent(user string, event *jmapcalendar.CalendarEvent) {}
func (d *dummyBackendCache) DeleteEvent(user string, id jmapcore.Id)                    {}

// memBackendCache holds in-memory maps for calendars and events with bounded TTL.
type memBackendCache struct {
	mu          sync.RWMutex
	ttl         time.Duration
	maxEntries  int
	calsCache   map[string][]*jmapcalendar.Calendar
	calsTime    map[string]time.Time
	eventsCache map[string]map[jmapcore.Id]*jmapcalendar.CalendarEvent
	eventsTime  map[string]time.Time
}

func newMemBackendCache() *memBackendCache {
	return &memBackendCache{
		ttl:         defaultBackendCacheTTL,
		maxEntries:  defaultMaxCacheEntries,
		calsCache:   make(map[string][]*jmapcalendar.Calendar),
		calsTime:    make(map[string]time.Time),
		eventsCache: make(map[string]map[jmapcore.Id]*jmapcalendar.CalendarEvent),
		eventsTime:  make(map[string]time.Time),
	}
}

func (m *memBackendCache) SetTTL(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ttl = d
}

func (m *memBackendCache) GetCals(user string) ([]*jmapcalendar.Calendar, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if exp, ok := m.calsTime[user]; !ok || time.Now().After(exp) {
		return nil, false
	}
	cals, ok := m.calsCache[user]
	return cals, ok && cals != nil
}

func (m *memBackendCache) SetCals(user string, cals []*jmapcalendar.Calendar) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calsCache) >= m.maxEntries {
		// Evict oldest or clear to enforce bounded size
		m.calsCache = make(map[string][]*jmapcalendar.Calendar)
		m.calsTime = make(map[string]time.Time)
	}
	m.calsCache[user] = cals
	m.calsTime[user] = time.Now().Add(m.ttl)
}

func (m *memBackendCache) AppendCal(user string, cal *jmapcalendar.Calendar) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calsCache[user] != nil {
		m.calsCache[user] = append(m.calsCache[user], cal)
	}
}

func (m *memBackendCache) DeleteCal(user string, calID jmapcore.Id) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calsCache[user] != nil {
		var filtered []*jmapcalendar.Calendar
		for _, c := range m.calsCache[user] {
			if c != nil && c.ID != calID {
				filtered = append(filtered, c)
			}
		}
		m.calsCache[user] = filtered
	}
}

func (m *memBackendCache) ClearCals(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.calsCache, user)
}

func (m *memBackendCache) HasCals(user string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.calsCache[user] != nil
}

func (m *memBackendCache) GetEvent(user string, id jmapcore.Id) (*jmapcalendar.CalendarEvent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if exp, ok := m.eventsTime[user]; !ok || time.Now().After(exp) {
		return nil, false
	}
	if m.eventsCache[user] == nil {
		return nil, false
	}
	ev, ok := m.eventsCache[user][id]
	return ev, ok && ev != nil
}

func (m *memBackendCache) GetEvents(user string) (map[jmapcore.Id]*jmapcalendar.CalendarEvent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if exp, ok := m.eventsTime[user]; !ok || time.Now().After(exp) {
		return nil, false
	}
	evs, ok := m.eventsCache[user]
	return evs, ok && evs != nil
}

func (m *memBackendCache) GetCalIDsForEvent(user string, id jmapcore.Id) []jmapcore.Id {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if exp, ok := m.eventsTime[user]; !ok || time.Now().After(exp) {
		return nil
	}
	if m.eventsCache[user] == nil {
		return nil
	}
	ev := m.eventsCache[user][id]
	if ev == nil || len(ev.CalendarIDs) == 0 {
		return nil
	}
	var res []jmapcore.Id
	for cid, isSet := range ev.CalendarIDs {
		if isSet && cid != "" && cid != "cal-default" {
			res = append(res, cid)
		}
	}
	return res
}

func (m *memBackendCache) SetEvents(user string, events map[jmapcore.Id]*jmapcalendar.CalendarEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.eventsCache) >= m.maxEntries {
		m.eventsCache = make(map[string]map[jmapcore.Id]*jmapcalendar.CalendarEvent)
		m.eventsTime = make(map[string]time.Time)
	}
	m.eventsCache[user] = events
	m.eventsTime[user] = time.Now().Add(m.ttl)
}

func (m *memBackendCache) StoreEvents(user string, events map[jmapcore.Id]*jmapcalendar.CalendarEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.eventsCache) >= m.maxEntries {
		m.eventsCache = make(map[string]map[jmapcore.Id]*jmapcalendar.CalendarEvent)
		m.eventsTime = make(map[string]time.Time)
	}
	if m.eventsCache[user] == nil {
		m.eventsCache[user] = make(map[jmapcore.Id]*jmapcalendar.CalendarEvent)
	}
	for k, v := range events {
		m.eventsCache[user][k] = v
	}
	m.eventsTime[user] = time.Now().Add(m.ttl)
}

func (m *memBackendCache) StoreEvent(user string, event *jmapcalendar.CalendarEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.eventsCache) >= m.maxEntries {
		m.eventsCache = make(map[string]map[jmapcore.Id]*jmapcalendar.CalendarEvent)
		m.eventsTime = make(map[string]time.Time)
	}
	if m.eventsCache[user] == nil {
		m.eventsCache[user] = make(map[jmapcore.Id]*jmapcalendar.CalendarEvent)
	}
	m.eventsCache[user][event.ID] = event
	m.eventsTime[user] = time.Now().Add(m.ttl)
}

func (m *memBackendCache) DeleteEvent(user string, id jmapcore.Id) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.eventsCache[user] != nil {
		delete(m.eventsCache[user], id)
	}
}
