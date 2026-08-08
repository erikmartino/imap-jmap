package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_QueryFloatingLocalDateTimeBounds covers CalendarEvent/query "after"/"before"
// filter bounds expressed as JSCalendar LocalDateTime values — i.e. floating, with NO "Z" or
// offset. Per draft-ietf-jmap-calendars-27 Section 5.11.1 both properties are LocalDateTime
// (RFC 8984 Section 1.4.5), interpreted in the query "timeZone" argument (default Etc/UTC,
// Section 5.11). A real client (Bulwark's month grid) sends exactly this form, e.g.
// {after:"2026-07-27T00:00:00", before:"2026-09-06T23:59:59", timeZone:"Europe/Copenhagen"}.
//
// Regression: the matcher previously parsed the bounds with an RFC3339 parser that rejects
// floating values, so every floating-bounds range query returned nothing and events never
// appeared in the calendar view. Earlier tests only exercised "Z"-suffixed (UTCDate) bounds,
// so the spec's LocalDateTime type went uncovered.
func TestRFC8984_QueryFloatingLocalDateTimeBounds(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	title := "Floating Bounds Event"
	create := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e": map[string]any{
					"title":    title,
					"start":    "2026-08-08T18:00:00", // floating LocalDateTime
					"duration": "PT1H",
					"timeZone": "Europe/Copenhagen",
				},
			},
		}, "s"},
	})
	created, _ := create.MethodResponses[0].Args["created"].(map[string]any)
	if created["e"] == nil {
		t.Fatalf("create failed: %+v", create.MethodResponses[0].Args)
	}
	id := created["e"].(map[string]any)["id"].(string)

	// Positive: floating bounds spanning the event, with a non-UTC timeZone argument.
	queryIDs := func(filter map[string]any, extra map[string]any) []any {
		args := map[string]any{"accountId": "primary", "filter": filter}
		for k, v := range extra {
			args[k] = v
		}
		resp := postJMAP(t, ts.URL, using, []any{[]any{"CalendarEvent/query", args, "q"}})
		if resp.MethodResponses[0].Name == "error" {
			t.Fatalf("query error: %+v", resp.MethodResponses[0].Args)
		}
		ids, _ := resp.MethodResponses[0].Args["ids"].([]any)
		return ids
	}

	got := queryIDs(
		map[string]any{"after": "2026-07-27T00:00:00", "before": "2026-09-06T23:59:59"},
		map[string]any{"timeZone": "Europe/Copenhagen"},
	)
	if len(got) != 1 || got[0] != id {
		t.Errorf("floating bounds + timeZone should match the event, got %+v", got)
	}

	// Positive: floating bounds with the default (Etc/UTC) timeZone — no timeZone argument.
	got = queryIDs(map[string]any{"after": "2026-08-01T00:00:00", "before": "2026-08-31T00:00:00"}, nil)
	if len(got) != 1 || got[0] != id {
		t.Errorf("floating bounds (default UTC) should match the event, got %+v", got)
	}

	// Negative: "before" earlier than the event start excludes it (start must be before bound).
	if got = queryIDs(map[string]any{"before": "2026-08-01T00:00:00"}, nil); len(got) != 0 {
		t.Errorf("event starting 2026-08-08 must not match before=2026-08-01, got %+v", got)
	}

	// Negative: "after" later than the event end excludes it (end must be after bound).
	if got = queryIDs(map[string]any{"after": "2026-09-01T00:00:00"}, nil); len(got) != 0 {
		t.Errorf("event ending 2026-08-08 must not match after=2026-09-01, got %+v", got)
	}
}
