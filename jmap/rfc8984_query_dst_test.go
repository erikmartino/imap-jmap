package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8984_QueryDSTTransitionBounds verifies that CalendarEvent/query before/after
// filter conditions are DST-correct across daylight saving transitions in a non-UTC timeZone
// per draft-ietf-jmap-calendars-27 Section 5.11.1.
func TestRFC8984_QueryDSTTransitionBounds(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.11.1", spectest.SHOULD,
		"before/after are DST-correct in a non-UTC timeZone.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// In America/New_York:
	// Spring forward transition: Sunday, March 8, 2026.
	// Clocks jump forward at 02:00:00 EST (07:00 UTC) to 03:00:00 EDT (07:00 UTC).
	// - Event Pre-Spring: starts 2026-03-08T01:15:00 EST (06:15 UTC), ends 01:45 EST (06:45 UTC).
	// - Event Post-Spring: starts 2026-03-08T03:15:00 EDT (07:15 UTC), ends 03:45 EDT (07:45 UTC).
	//
	// Fall back transition: Sunday, November 1, 2026.
	// Clocks jump back at 02:00:00 EDT (06:00 UTC) to 01:00:00 EST (06:00 UTC).
	// - Event Pre-Fall: starts 2026-11-01T00:30:00 EDT (04:30 UTC), ends 01:00 EDT (05:00 UTC).
	// - Event Post-Fall: starts 2026-11-01T03:30:00 EST (08:30 UTC), ends 04:00 EST (09:00 UTC).

	createResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"preSpring": map[string]any{
					"title":    "Pre-Spring Event",
					"start":    "2026-03-08T01:15:00",
					"duration": "PT30M",
					"timeZone": "America/New_York",
				},
				"postSpring": map[string]any{
					"title":    "Post-Spring Event",
					"start":    "2026-03-08T03:15:00",
					"duration": "PT30M",
					"timeZone": "America/New_York",
				},
				"preFall": map[string]any{
					"title":    "Pre-Fall Event",
					"start":    "2026-11-01T00:30:00",
					"duration": "PT30M",
					"timeZone": "America/New_York",
				},
				"postFall": map[string]any{
					"title":    "Post-Fall Event",
					"start":    "2026-11-01T03:30:00",
					"duration": "PT30M",
					"timeZone": "America/New_York",
				},
			},
		}, "s1"},
	})
	created, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || len(created) != 4 {
		t.Fatalf("create failed: %+v", createResp.MethodResponses[0].Args)
	}
	preSpringID := created["preSpring"].(map[string]any)["id"].(string)
	postSpringID := created["postSpring"].(map[string]any)["id"].(string)
	preFallID := created["preFall"].(map[string]any)["id"].(string)
	postFallID := created["postFall"].(map[string]any)["id"].(string)

	queryNY := func(filter map[string]any) []string {
		resp := postJMAP(t, ts.URL, using, []any{
			[]any{"CalendarEvent/query", map[string]any{
				"accountId": "primary",
				"timeZone":  "America/New_York",
				"filter":    filter,
			}, "q"},
		})
		if resp.MethodResponses[0].Name != "CalendarEvent/query" {
			t.Fatalf("query failed: %+v", resp.MethodResponses[0].Args)
		}
		rawIDs, _ := resp.MethodResponses[0].Args["ids"].([]any)
		var out []string
		for _, id := range rawIDs {
			out = append(out, id.(string))
		}
		return out
	}

	hasID := func(list []string, target string) bool {
		for _, id := range list {
			if id == target {
				return true
			}
		}
		return false
	}

	// Spring tests:
	// 1. Window [03:00, 04:00] in America/New_York MUST match postSpring and exclude preSpring.
	springAfter := queryNY(map[string]any{
		"after":  "2026-03-08T03:00:00",
		"before": "2026-03-08T04:00:00",
	})
	if !hasID(springAfter, postSpringID) {
		t.Errorf("expected postSpringID in [03:00, 04:00], got %+v", springAfter)
	}
	if hasID(springAfter, preSpringID) {
		t.Errorf("preSpringID should NOT be in [03:00, 04:00], got %+v", springAfter)
	}

	// 2. Window [01:00, 03:00] in America/New_York (up to the DST spring jump) MUST match preSpring and exclude postSpring.
	springBefore := queryNY(map[string]any{
		"after":  "2026-03-08T01:00:00",
		"before": "2026-03-08T03:00:00",
	})
	if !hasID(springBefore, preSpringID) {
		t.Errorf("expected preSpringID in [01:00, 03:00], got %+v", springBefore)
	}
	if hasID(springBefore, postSpringID) {
		t.Errorf("postSpringID should NOT be in [01:00, 03:00], got %+v", springBefore)
	}

	// Fall tests:
	// 3. Window [03:00, 04:00] in America/New_York MUST match postFall and exclude preFall.
	fallAfter := queryNY(map[string]any{
		"after":  "2026-11-01T03:00:00",
		"before": "2026-11-01T04:00:00",
	})
	if !hasID(fallAfter, postFallID) {
		t.Errorf("expected postFallID in [03:00, 04:00], got %+v", fallAfter)
	}
	if hasID(fallAfter, preFallID) {
		t.Errorf("preFallID should NOT be in [03:00, 04:00], got %+v", fallAfter)
	}

	// 4. Window [00:00, 01:00] in America/New_York MUST match preFall and exclude postFall.
	fallBefore := queryNY(map[string]any{
		"after":  "2026-11-01T00:00:00",
		"before": "2026-11-01T01:00:00",
	})
	if !hasID(fallBefore, preFallID) {
		t.Errorf("expected preFallID in [00:00, 01:00], got %+v", fallBefore)
	}
	if hasID(fallBefore, postFallID) {
		t.Errorf("postFallID should NOT be in [00:00, 01:00], got %+v", fallBefore)
	}
}
