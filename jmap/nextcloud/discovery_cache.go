package nextcloud

import "sync"

// discoveryCache encapsulates caching of Nextcloud/CalDAV discovery paths, principals, and collections.
type discoveryCache interface {
	GetPrincipal(user string) (string, bool)
	SetPrincipal(user, principal string)
	GetScheduleDefaultCal(user string) (string, bool)
	SetScheduleDefaultCal(user, calID string)
	GetHomeSet(user string) (string, bool)
	SetHomeSet(user, homeSet string)
	GetCalPath(user, calID string) (string, bool)
	SetCalPath(user, calID, calPath string)
	DeleteCal(user, calID string)
	Reset()
}

// dummyDiscoveryCache is a no-op cache that always returns empty and discards writes.
type dummyDiscoveryCache struct{}

func (d *dummyDiscoveryCache) GetPrincipal(user string) (string, bool)          { return "", false }
func (d *dummyDiscoveryCache) SetPrincipal(user, principal string)              {}
func (d *dummyDiscoveryCache) GetScheduleDefaultCal(user string) (string, bool) { return "", false }
func (d *dummyDiscoveryCache) SetScheduleDefaultCal(user, calID string)         {}
func (d *dummyDiscoveryCache) GetHomeSet(user string) (string, bool)            { return "", false }
func (d *dummyDiscoveryCache) SetHomeSet(user, homeSet string)                  {}
func (d *dummyDiscoveryCache) GetCalPath(user, calID string) (string, bool)     { return "", false }
func (d *dummyDiscoveryCache) SetCalPath(user, calID, calPath string)           {}
func (d *dummyDiscoveryCache) DeleteCal(user, calID string)                     {}
func (d *dummyDiscoveryCache) Reset()                                           {}

// memDiscoveryCache holds thread-safe in-memory maps for discovered paths and endpoints.
type memDiscoveryCache struct {
	mu                  sync.RWMutex
	principals          map[string]string
	scheduleDefaultCals map[string]string
	homeSets            map[string]string
	calPaths            map[string]map[string]string
}

func newMemDiscoveryCache() *memDiscoveryCache {
	return &memDiscoveryCache{
		principals:          make(map[string]string),
		scheduleDefaultCals: make(map[string]string),
		homeSets:            make(map[string]string),
		calPaths:            make(map[string]map[string]string),
	}
}

func (m *memDiscoveryCache) GetPrincipal(user string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.principals[user]
	return p, ok && p != ""
}

func (m *memDiscoveryCache) SetPrincipal(user, principal string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.principals[user] = principal
}

func (m *memDiscoveryCache) GetScheduleDefaultCal(user string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.scheduleDefaultCals[user]
	return c, ok && c != ""
}

func (m *memDiscoveryCache) SetScheduleDefaultCal(user, calID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scheduleDefaultCals[user] = calID
}

func (m *memDiscoveryCache) GetHomeSet(user string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.homeSets[user]
	return h, ok && h != ""
}

func (m *memDiscoveryCache) SetHomeSet(user, homeSet string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.homeSets[user] = homeSet
}

func (m *memDiscoveryCache) GetCalPath(user, calID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.calPaths[user] == nil {
		return "", false
	}
	p, ok := m.calPaths[user][calID]
	return p, ok && p != ""
}

func (m *memDiscoveryCache) SetCalPath(user, calID, calPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calPaths[user] == nil {
		m.calPaths[user] = make(map[string]string)
	}
	m.calPaths[user][calID] = calPath
}

func (m *memDiscoveryCache) DeleteCal(user, calID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calPaths[user] != nil {
		delete(m.calPaths[user], calID)
	}
	delete(m.scheduleDefaultCals, user)
}

func (m *memDiscoveryCache) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.principals = make(map[string]string)
	m.scheduleDefaultCals = make(map[string]string)
	m.homeSets = make(map[string]string)
	m.calPaths = make(map[string]map[string]string)
}
