package imapsmtp

import (
	"testing"
	"time"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

// AGENTS.md §1 Invariant Guard: constructors MUST NEVER seed authoritative user data.
func TestIMAPSMTPConstructorZeroAuthoritativeData(t *testing.T) {
	b := New("localhost:993", "localhost:587")

	if len(b.activeAccounts) != 0 {
		t.Errorf("expected activeAccounts to be empty, got %d", len(b.activeAccounts))
	}
	if len(b.idleWatchers) != 0 {
		t.Errorf("expected idleWatchers to be empty, got %d", len(b.idleWatchers))
	}
	if len(b.submissions) != 0 {
		t.Errorf("expected submissions cache to be empty, got %d", len(b.submissions))
	}
	if len(b.identities) != 0 {
		t.Errorf("expected identities cache to be empty, got %d", len(b.identities))
	}
	if len(b.movedIDs) != 0 {
		t.Errorf("expected movedIDs to be empty, got %d", len(b.movedIDs))
	}
	if len(b.mailboxMovedIDs) != 0 {
		t.Errorf("expected mailboxMovedIDs to be empty, got %d", len(b.mailboxMovedIDs))
	}
	if len(b.mailboxParentOverrides) != 0 {
		t.Errorf("expected mailboxParentOverrides to be empty, got %d", len(b.mailboxParentOverrides))
	}
	if len(b.mailboxSortOrders) != 0 {
		t.Errorf("expected mailboxSortOrders to be empty, got %d", len(b.mailboxSortOrders))
	}
	if len(b.vacationResponses) != 0 {
		t.Errorf("expected vacationResponses to be empty, got %d", len(b.vacationResponses))
	}
	if len(b.pushSubscriptions) != 0 {
		t.Errorf("expected pushSubscriptions to be empty, got %d", len(b.pushSubscriptions))
	}
	if len(b.blobs) != 0 {
		t.Errorf("expected blobs to be empty, got %d", len(b.blobs))
	}
}

// AGENTS.md §1 Invariant Guard: all cache entries are evictable on demand.
func TestIMAPSMTPCacheInvalidationAndEviction(t *testing.T) {
	b := New("localhost:993", "localhost:587")
	accountID := "user@example.com"

	b.identitiesMu.Lock()
	b.identities[accountID] = make(map[jmapcore.Id]*jmapmail.Identity)
	b.identitiesTime[accountID] = time.Now().Add(5 * time.Minute)
	b.identitiesMu.Unlock()

	b.submissionsMu.Lock()
	b.submissions[accountID] = make(map[jmapcore.Id]*jmapmail.EmailSubmission)
	b.submissionsTime[accountID] = time.Now().Add(5 * time.Minute)
	b.submissionsMu.Unlock()

	b.InvalidateCache(accountID)

	b.identitiesMu.RLock()
	if _, ok := b.identities[accountID]; ok {
		t.Errorf("expected identities for %q to be evicted", accountID)
	}
	b.identitiesMu.RUnlock()

	b.submissionsMu.RLock()
	if _, ok := b.submissions[accountID]; ok {
		t.Errorf("expected submissions for %q to be evicted", accountID)
	}
	b.submissionsMu.RUnlock()
}
