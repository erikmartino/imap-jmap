package nextcloud_test

import (
	"net/http"
	"sync"
	"testing"

	"imap-jmap/jmap/nextcloud"
)

type countingRoundTripper struct {
	base  http.RoundTripper
	mu    sync.Mutex
	count int
}

func (c *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
	return c.base.RoundTrip(req)
}

func (c *countingRoundTripper) reset() {
	c.mu.Lock()
	c.count = 0
	c.mu.Unlock()
}

func (c *countingRoundTripper) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

// TestDiscoveryCachedAcrossRequests verifies that CalDAV discovery metadata (principal,
// home set, schedule-default, calendar paths) is cached in memory across requests, so a
// warm operation only re-lists the calendars instead of repeating discovery. This is
// metadata only: no calendar/event bodies are cached and nothing is persisted to disk.
func TestDiscoveryCachedAcrossRequests(t *testing.T) {
	_, client, cleanup := nextcloud.NewEmbeddedServer("user@example.com")
	defer cleanup()

	counter := &countingRoundTripper{base: client.HTTPClient.Transport}
	client.HTTPClient.Transport = counter

	ctx := testContext()

	// First call performs full discovery.
	counter.reset()
	if _, _, err := client.ListCalendars(ctx); err != nil {
		t.Fatalf("first ListCalendars failed: %v", err)
	}
	first := counter.get()
	if first < 2 {
		t.Fatalf("expected full discovery on the first call, got %d requests", first)
	}

	// A later call (new request context) is served entirely from the in-memory
	// metadata caches (discovery + calendar list), with no CalDAV round-trip.
	counter.reset()
	if _, _, err := client.ListCalendars(testContext()); err != nil {
		t.Fatalf("second ListCalendars failed: %v", err)
	}
	if second := counter.get(); second != 0 {
		t.Fatalf("expected the warm call to be fully cached (0 requests), got %d", second)
	}
}

// TestCalendarListCachedAcrossRequests verifies that the calendar collection list is
// reused across requests for a short TTL (so Calendar/get does not PROPFIND every time)
// while the CTag/sync-token state stays live, and that mutations invalidate the cache.
func TestCalendarListCachedAcrossRequests(t *testing.T) {
	_, client, cleanup := nextcloud.NewEmbeddedServer("user@example.com")
	defer cleanup()

	counter := &countingRoundTripper{base: client.HTTPClient.Transport}
	client.HTTPClient.Transport = counter
	ctx := testContext()

	counter.reset()
	if _, _, err := client.ListCalendars(ctx); err != nil {
		t.Fatalf("first ListCalendars failed: %v", err)
	}
	if counter.get() == 0 {
		t.Fatal("expected the first ListCalendars to fetch from CalDAV")
	}

	// A later request must be served from the metadata cache.
	counter.reset()
	if _, _, err := client.ListCalendars(ctx); err != nil {
		t.Fatalf("second ListCalendars failed: %v", err)
	}
	if n := counter.get(); n != 0 {
		t.Fatalf("expected cached calendar list (0 DAV requests), got %d", n)
	}

	// The CTag/sync-token state must NOT be served from the list cache.
	counter.reset()
	if _, err := client.GetCalendarSyncStatuses(ctx); err != nil {
		t.Fatalf("GetCalendarSyncStatuses failed: %v", err)
	}
	if counter.get() == 0 {
		t.Fatal("expected GetCalendarSyncStatuses to stay live (not cached)")
	}

	// Creating a calendar invalidates the cached list.
	counter.reset()
	if err := client.CreateCalendar(ctx, "newcal"); err != nil {
		t.Fatalf("CreateCalendar failed: %v", err)
	}
	counter.reset()
	list, _, err := client.ListCalendars(ctx)
	if err != nil {
		t.Fatalf("ListCalendars after create failed: %v", err)
	}
	if counter.get() == 0 {
		t.Fatal("expected ListCalendars to refetch after a calendar create")
	}
	found := false
	for _, c := range list {
		if c.ID == "newcal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected newcal in refreshed list, got %+v", list)
	}
}
