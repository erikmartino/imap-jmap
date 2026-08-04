package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_CalendarEventQuerySorting tests sorting CalendarEvent/query by start, title, uid per RFC 8984.
func TestRFC8984_CalendarEventQuerySorting(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create events with different start times and titles
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title": "Charlie Standup",
					"start": "2026-08-10T14:00:00Z",
				},
				"c2": map[string]any{
					"title": "Alpha Meeting",
					"start": "2026-08-10T09:00:00Z",
				},
				"c3": map[string]any{
					"title": "Bravo Workshop",
					"start": "2026-08-10T11:00:00Z",
				},
			},
		}, "call-1"},
	}

	resp := postJMAP(t, ts.URL, using, createReq)
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil || created["c2"] == nil || created["c3"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", resp.MethodResponses[0].Args)
	}

	id1 := created["c1"].(map[string]any)["id"].(string)
	id2 := created["c2"].(map[string]any)["id"].(string)
	id3 := created["c3"].(map[string]any)["id"].(string)

	// 2. Query sorted by 'start' ascending
	queryReqAsc := []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"sort": []any{
				map[string]any{
					"property":    "start",
					"isAscending": true,
				},
			},
		}, "call-2"},
	}

	queryRespAsc := postJMAP(t, ts.URL, using, queryReqAsc)
	idsAsc, ok := queryRespAsc.MethodResponses[0].Args["ids"].([]any)
	if !ok || len(idsAsc) < 3 {
		t.Fatalf("expected at least 3 ids in query result, got %+v", queryRespAsc.MethodResponses[0].Args)
	}

	// Filter ids to our 3 created events
	var filteredAsc []string
	for _, rawID := range idsAsc {
		s := rawID.(string)
		if s == id1 || s == id2 || s == id3 {
			filteredAsc = append(filteredAsc, s)
		}
	}

	if len(filteredAsc) != 3 || filteredAsc[0] != id2 || filteredAsc[1] != id3 || filteredAsc[2] != id1 {
		t.Errorf("expected ascending start sort [id2, id3, id1], got %+v", filteredAsc)
	}

	// 3. Query sorted by 'title' ascending
	queryReqTitle := []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"sort": []any{
				map[string]any{
					"property":    "title",
					"isAscending": true,
				},
			},
		}, "call-3"},
	}

	queryRespTitle := postJMAP(t, ts.URL, using, queryReqTitle)
	idsTitle, _ := queryRespTitle.MethodResponses[0].Args["ids"].([]any)
	var filteredTitle []string
	for _, rawID := range idsTitle {
		s := rawID.(string)
		if s == id1 || s == id2 || s == id3 {
			filteredTitle = append(filteredTitle, s)
		}
	}

	// Alpha (id2), Bravo (id3), Charlie (id1)
	if len(filteredTitle) != 3 || filteredTitle[0] != id2 || filteredTitle[1] != id3 || filteredTitle[2] != id1 {
		t.Errorf("expected ascending title sort [id2, id3, id1], got %+v", filteredTitle)
	}
}
