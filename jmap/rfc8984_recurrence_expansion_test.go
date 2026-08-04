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
