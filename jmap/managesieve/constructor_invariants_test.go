package managesieve

import (
	"testing"

	"imap-jmap/jmap/jmapcore"
)

// AGENTS.md §1 Invariant Guard: ManageSieve constructor MUST NEVER seed authoritative user data.
func TestManageSieveConstructorZeroAuthoritativeData(t *testing.T) {
	b := NewBackend("127.0.0.1:4190")

	if len(b.movedIDs) != 0 {
		t.Errorf("expected movedIDs to be empty, got %d", len(b.movedIDs))
	}
	if len(b.trackers) != 0 {
		t.Errorf("expected trackers to be empty, got %d", len(b.trackers))
	}
}

// AGENTS.md §1 Invariant Guard: ManageSieve transient redirects support invalidation on demand.
func TestManageSieveCacheInvalidation(t *testing.T) {
	b := NewBackend("127.0.0.1:4190")
	user := "user@example.com"

	b.mu.Lock()
	b.movedIDs[user] = map[jmapcore.Id]jmapcore.Id{
		"old-id": "new-id",
	}
	b.mu.Unlock()

	b.InvalidateCache(user)

	b.mu.RLock()
	if _, ok := b.movedIDs[user]; ok {
		t.Errorf("expected movedIDs for %q to be evicted", user)
	}
	b.mu.RUnlock()
}
