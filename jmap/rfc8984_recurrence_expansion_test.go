package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_RecurrenceExpansionQuery tests querying recurring events with after/before filter over expanded instances.
func TestRFC8984_RecurrenceExpansionQuery(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// Create a weekly event starting Aug 1, 2026
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title":    "Weekly Sync Aug-Sept",
					"start":    "2026-08-01T10:00:00Z",
					"duration": "PT1H",
					"recurrenceRules": []any{
						map[string]any{
							"frequency": "weekly",
							"count":     10,
						},
					},
				},
			},
		}, "call-1"},
	}

	resp := postJMAP(t, ts.URL, using, createReq)
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", resp.MethodResponses[0].Args)
	}

	evID := created["c1"].(map[string]any)["id"].(string)

	// Query after 2026-09-01T00:00:00Z (master start was 2026-08-01, but instances occur in Sept!)
	queryReq := []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"after":  "2026-09-01T00:00:00Z",
				"before": "2026-09-30T23:59:59Z",
			},
		}, "call-2"},
	}

	queryResp := postJMAP(t, ts.URL, using, queryReq)
	ids, ok := queryResp.MethodResponses[0].Args["ids"].([]any)
	if !ok || len(ids) == 0 {
		t.Fatalf("expected recurring event id in query results for Sept time window, got %+v", queryResp.MethodResponses[0].Args)
	}

	found := false
	for _, id := range ids {
		if id == evID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected event %s in query results, got %+v", evID, ids)
	}
}

// expandCount creates an event and returns the number of expanded occurrence ids from
// CalendarEvent/query with expandRecurrences:true.
func expandCount(t *testing.T, tsURL string, create map[string]any) int {
	t.Helper()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}
	resp := postJMAP(t, tsURL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"c1": create},
		}, "s"},
	})
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil {
		t.Fatalf("create failed: %+v", resp.MethodResponses[0].Args)
	}
	q := postJMAP(t, tsURL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId":         "primary",
			"expandRecurrences": true,
		}, "q"},
	})
	ids, _ := q.MethodResponses[0].Args["ids"].([]any)
	return len(ids)
}

// TestRFC8984_RecurrenceByDay verifies weekly byDay expansion (RFC 8984 Section 4.3.3).
func TestRFC8984_RecurrenceByDay(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Mon 2026-08-03; weekly on Mon/Wed/Fri, 6 occurrences.
	n := expandCount(t, ts.URL, map[string]any{
		"title": "MWF standup",
		"start": "2026-08-03T09:00:00Z",
		"recurrenceRules": []any{map[string]any{
			"frequency": "weekly",
			"byDay":     []any{"mo", "we", "fr"},
			"count":     6,
		}},
	})
	if n != 6 {
		t.Errorf("expected 6 byDay occurrences, got %d", n)
	}
}

// TestRFC8984_RecurrenceBySetPosition verifies bySetPosition (last weekday of month).
func TestRFC8984_RecurrenceBySetPosition(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	n := expandCount(t, ts.URL, map[string]any{
		"title": "Last weekday",
		"start": "2026-08-31T09:00:00Z", // Mon Aug 31 = last weekday of Aug
		"recurrenceRules": []any{map[string]any{
			"frequency":     "monthly",
			"byDay":         []any{"mo", "tu", "we", "th", "fr"},
			"bySetPosition": []any{-1},
			"count":         3,
		}},
	})
	if n != 3 {
		t.Errorf("expected 3 bySetPosition occurrences, got %d", n)
	}
}

// TestRFC8984_RecurrenceOverrideExcluded verifies recurrenceOverrides with excluded:true
// removes an instance (RFC 8984 Section 4.3.5).
func TestRFC8984_RecurrenceOverrideExcluded(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	n := expandCount(t, ts.URL, map[string]any{
		"title":    "Weekly with skip",
		"start":    "2026-08-03T09:00:00Z",
		"duration": "PT1H",
		"recurrenceRules": []any{map[string]any{
			"frequency": "weekly",
			"count":     4,
		}},
		"recurrenceOverrides": map[string]any{
			"2026-08-17T09:00:00Z": map[string]any{"excluded": true},
		},
	})
	if n != 3 {
		t.Errorf("expected 3 occurrences after excluding one override, got %d", n)
	}
}

// TestRFC8984_ExcludedRecurrenceRules verifies excludedRecurrenceRules subtract instances
// (RFC 8984 Section 4.3.4).
func TestRFC8984_ExcludedRecurrenceRules(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Daily for 10 days, minus every other day → 5 remain.
	n := expandCount(t, ts.URL, map[string]any{
		"title": "Daily minus alternates",
		"start": "2026-08-03T09:00:00Z",
		"recurrenceRules": []any{map[string]any{
			"frequency": "daily",
			"count":     10,
		}},
		"excludedRecurrenceRules": []any{map[string]any{
			"frequency": "daily",
			"interval":  2,
			"count":     10,
		}},
	})
	if n != 5 {
		t.Errorf("expected 5 occurrences after excluded rule, got %d", n)
	}
}
