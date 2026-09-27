package nextcloud

import (
	"testing"
	"time"

	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcontacts"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapfilenode"
	"imap-jmap/jmap/jmapprincipals"
)

// AGENTS.md §1 Invariant Guard: Nextcloud backend constructors MUST NEVER seed authoritative user data.
func TestNextcloudConstructorsZeroAuthoritativeData(t *testing.T) {
	client := NewClient("http://127.0.0.1:8080")

	// 1. CalendarsBackend
	calBackend := NewCalendarsBackend(client)
	if len(calBackend.identitiesCache) != 0 {
		t.Errorf("expected identitiesCache to be empty, got %d", len(calBackend.identitiesCache))
	}
	if len(calBackend.notificationsCache) != 0 {
		t.Errorf("expected notificationsCache to be empty, got %d", len(calBackend.notificationsCache))
	}
	if len(calBackend.shareNotificationsCache) != 0 {
		t.Errorf("expected shareNotificationsCache to be empty, got %d", len(calBackend.shareNotificationsCache))
	}
	if len(calBackend.defaultCalendars) != 0 {
		t.Errorf("expected defaultCalendars to be empty, got %d", len(calBackend.defaultCalendars))
	}
	if len(calBackend.calProps) != 0 {
		t.Errorf("expected calProps to be empty, got %d", len(calBackend.calProps))
	}
	if len(calBackend.userCalOverrides) != 0 {
		t.Errorf("expected userCalOverrides to be empty, got %d", len(calBackend.userCalOverrides))
	}

	// 2. ContactsBackend
	contactsBackend := NewContactsBackend(client)
	if len(contactsBackend.cardsCache) != 0 {
		t.Errorf("expected cardsCache to be empty, got %d", len(contactsBackend.cardsCache))
	}
	if len(contactsBackend.cardPaths) != 0 {
		t.Errorf("expected cardPaths to be empty, got %d", len(contactsBackend.cardPaths))
	}
	if len(contactsBackend.abPaths) != 0 {
		t.Errorf("expected abPaths to be empty, got %d", len(contactsBackend.abPaths))
	}

	// 3. FileNodeBackend
	fileNodeBackend := NewFileNodeBackend(client)
	if len(fileNodeBackend.nodesCache) != 0 {
		t.Errorf("expected nodesCache to be empty, got %d", len(fileNodeBackend.nodesCache))
	}
	if len(fileNodeBackend.movedIDs) != 0 {
		t.Errorf("expected movedIDs to be empty, got %d", len(fileNodeBackend.movedIDs))
	}
	if len(fileNodeBackend.destroyedIDs) != 0 {
		t.Errorf("expected destroyedIDs to be empty, got %d", len(fileNodeBackend.destroyedIDs))
	}

	// 4. PrincipalsBackend
	principalsBackend := NewPrincipalsBackend(client, nil)
	if len(principalsBackend.principalsCache) != 0 {
		t.Errorf("expected principalsCache to be empty, got %d", len(principalsBackend.principalsCache))
	}
	if len(principalsBackend.directoryCache) != 0 {
		t.Errorf("expected directoryCache to be empty, got %d", len(principalsBackend.directoryCache))
	}
}

// AGENTS.md §1 Invariant Guard: all in-memory caches must support eviction on demand.
func TestNextcloudCacheInvalidationAndEviction(t *testing.T) {
	client := NewClient("http://127.0.0.1:8080")
	user := "user@example.com"

	// 1. CalendarsBackend InvalidateCache
	calBackend := NewCalendarsBackend(client)
	calBackend.identitiesCache[user] = make(map[jmapcore.Id]*jmapcalendar.ParticipantIdentity)
	calBackend.notificationsCache[user] = make(map[jmapcore.Id]*jmapcalendar.CalendarEventNotification)
	calBackend.shareNotificationsCache[user] = make(map[jmapcore.Id]*jmapcalendar.ShareNotification)
	calBackend.calProps[user] = make(map[jmapcore.Id]*jmapcalendar.Calendar)
	calBackend.InvalidateCache(user)
	if _, ok := calBackend.identitiesCache[user]; ok {
		t.Errorf("expected user identities to be evicted")
	}
	if _, ok := calBackend.notificationsCache[user]; ok {
		t.Errorf("expected user notifications to be evicted")
	}
	if _, ok := calBackend.shareNotificationsCache[user]; ok {
		t.Errorf("expected user shareNotifications to be evicted")
	}
	if _, ok := calBackend.calProps[user]; ok {
		t.Errorf("expected user calProps to be evicted")
	}

	// 2. ContactsBackend InvalidateCache
	contactsBackend := NewContactsBackend(client)
	contactsBackend.cardsCache[user] = make(map[jmapcore.Id]*jmapcontacts.Card)
	contactsBackend.cardPaths[user] = make(map[jmapcore.Id]string)
	contactsBackend.InvalidateCache(user)
	if _, ok := contactsBackend.cardsCache[user]; ok {
		t.Errorf("expected user cardsCache to be evicted")
	}
	if _, ok := contactsBackend.cardPaths[user]; ok {
		t.Errorf("expected user cardPaths to be evicted")
	}

	// 3. FileNodeBackend InvalidateCache
	fileNodeBackend := NewFileNodeBackend(client)
	fileNodeBackend.nodesCache[user] = make(map[jmapcore.Id]*jmapfilenode.FileNode)
	fileNodeBackend.InvalidateCache(user)
	if _, ok := fileNodeBackend.nodesCache[user]; ok {
		t.Errorf("expected user nodesCache to be evicted")
	}

	// 4. PrincipalsBackend InvalidateCache
	principalsBackend := NewPrincipalsBackend(client, nil)
	principalsBackend.directoryCache[user] = &principalDirectory{
		syncedAt:   time.Now(),
		principals: make(map[jmapcore.Id]*jmapprincipals.Principal),
	}
	principalsBackend.InvalidateCache(user)
	if _, ok := principalsBackend.directoryCache[user]; ok {
		t.Errorf("expected user directoryCache to be evicted")
	}
}

// AGENTS.md §1 Invariant Guard: memBackendCache expires after TTL.
func TestMemBackendCacheTTLExpiration(t *testing.T) {
	c := newMemBackendCache()
	c.SetTTL(50 * time.Millisecond)

	user := "testuser"
	c.SetCals(user, []*jmapcalendar.Calendar{{ID: "cal-1"}})
	c.StoreEvent(user, &jmapcalendar.CalendarEvent{ID: "ev-1"})

	// Immediately accessible
	if cals, ok := c.GetCals(user); !ok || len(cals) != 1 {
		t.Fatalf("expected cals before TTL expiration, got %v, %v", cals, ok)
	}
	if ev, ok := c.GetEvent(user, "ev-1"); !ok || ev == nil {
		t.Fatalf("expected event before TTL expiration, got %v, %v", ev, ok)
	}

	// Sleep until TTL expires
	time.Sleep(60 * time.Millisecond)

	if cals, ok := c.GetCals(user); ok || cals != nil {
		t.Fatalf("expected cals to be expired after TTL, got %v, %v", cals, ok)
	}
	if ev, ok := c.GetEvent(user, "ev-1"); ok || ev != nil {
		t.Fatalf("expected event to be expired after TTL, got %v, %v", ev, ok)
	}
}
