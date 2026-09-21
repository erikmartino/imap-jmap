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

	// A later call (new request context) must reuse the cached discovery and only
	// re-list the calendars (a single PROPFIND).
	counter.reset()
	if _, _, err := client.ListCalendars(testContext()); err != nil {
		t.Fatalf("second ListCalendars failed: %v", err)
	}
	if second := counter.get(); second != 1 {
		t.Fatalf("expected only the calendar listing (1 request) on the warm call, got %d", second)
	}
}
