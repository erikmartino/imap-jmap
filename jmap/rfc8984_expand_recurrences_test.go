package jmap_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_ExpandRecurrencesQueryArg tests expandRecurrences query argument returning per-occurrence IDs.
func TestRFC8984_ExpandRecurrencesQueryArg(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create a daily recurring event (3 instances)
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title": "Daily Standup",
					"start": "2026-08-01T09:00:00Z",
					"recurrenceRules": []any{
						map[string]any{
							"frequency": "daily",
							"count":     3,
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

	// 2. Query with expandRecurrences: true
	queryReq := []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId":          "primary",
			"expandRecurrences": true,
		}, "call-2"},
	}

	queryResp := postJMAP(t, ts.URL, using, queryReq)
	ids, ok := queryResp.MethodResponses[0].Args["ids"].([]any)
	if !ok {
		t.Fatalf("expected ids array, got %+v", queryResp.MethodResponses[0].Args)
	}

	// Filter ids for our created event occurrence IDs
	var occurrenceIDs []string
	for _, rawID := range ids {
		s := rawID.(string)
		if strings.HasPrefix(s, evID+"#") {
			occurrenceIDs = append(occurrenceIDs, s)
		}
	}

	if len(occurrenceIDs) != 3 {
		t.Fatalf("expected 3 per-occurrence IDs starting with %s#, got %+v", evID, occurrenceIDs)
	}
}
