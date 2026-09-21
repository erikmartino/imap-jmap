package nextcloud_test

import (
	"context"
	"testing"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/nextcloud"
)

// TestEmbeddedCalendarRangeQueryPushdown verifies that a CalendarEvent/query
// after/before window is pushed down to the CalDAV backend as a server-side
// time-range, so only resources overlapping the window are transferred. This is
// the stateless speed-up: no local cache is required, the upstream call itself
// becomes selective.
func TestEmbeddedCalendarRangeQueryPushdown(t *testing.T) {
	client, calBackend, _, _, _, cleanup := nextcloud.NewEmbeddedBackend("user@example.com")
	defer cleanup()

	ctx := context.Background()
	ctx = jmapauth.ContextWithAccountID(ctx, "user@example.com")
	ctx = jmapauth.ContextWithSubject(ctx, "user@example.com")
	ctx = jmapauth.ContextWithCredentials(ctx, "user@example.com", "user@example.com")

	cals, err := calBackend.GetAllCalendars(ctx)
	if err != nil || len(cals) == 0 {
		t.Fatalf("GetAllCalendars failed: %v", err)
	}
	calID := cals[0].ID

	for _, start := range []string{
		"2026-01-15T10:00:00Z",
		"2026-06-15T10:00:00Z",
		"2026-12-15T10:00:00Z",
	} {
		ev := &jmapcalendar.CalendarEvent{
			Title:       "Event " + start,
			Start:       start,
			Duration:    "PT1H",
			CalendarIDs: map[jmapcore.Id]bool{calID: true},
		}
		if _, err := calBackend.CreateCalendarEvent(ctx, ev); err != nil {
			t.Fatalf("CreateCalendarEvent(%s) failed: %v", start, err)
		}
	}

	// Unbounded retrieval sees every event.
	all, err := client.QueryCalendarObjects(ctx, string(calID))
	if err != nil {
		t.Fatalf("QueryCalendarObjects failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 unbounded objects, got %d", len(all))
	}

	// The range query must let the backend drop the out-of-range resources.
	rangeStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	inRange, err := client.QueryCalendarObjectsInRange(ctx, string(calID), rangeStart, rangeEnd)
	if err != nil {
		t.Fatalf("QueryCalendarObjectsInRange failed: %v", err)
	}
	if len(inRange) != 1 {
		t.Fatalf("expected exactly 1 in-range object, got %d", len(inRange))
	}

	// CalendarEvent/query with the equivalent filter must return just that event.
	ids, total, err := calBackend.QueryCalendarEvents(ctx, map[string]any{
		"after":  "2026-06-01T00:00:00Z",
		"before": "2026-07-01T00:00:00Z",
	}, nil, 0, nil, false)
	if err != nil {
		t.Fatalf("QueryCalendarEvents failed: %v", err)
	}
	if total != 1 || len(ids) != 1 {
		t.Fatalf("expected 1 matching event, got total=%d ids=%v", total, ids)
	}
	events, notFound, err := calBackend.GetCalendarEvents(ctx, ids)
	if err != nil || len(events) != 1 || len(notFound) != 0 {
		t.Fatalf("GetCalendarEvents failed: err=%v events=%d notFound=%v", err, len(events), notFound)
	}
	if events[0].Start != "2026-06-15T10:00:00Z" {
		t.Fatalf("expected June event, got start %q", events[0].Start)
	}
}
