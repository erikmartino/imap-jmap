package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_CalendarEventLocationsMapRoundTrip tests locations map (§4.2.5) round-trip and backward-compatibility fallback.
func TestRFC8984_CalendarEventLocationsMapRoundTrip(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create event with locations map per RFC 8984 §4.2.5
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title": "Conf Call",
					"locations": map[string]any{
						"loc-main": map[string]any{
							"name":        "HQ Building 1",
							"description": "Room 402",
							"rel":         "start",
						},
						"loc-overflow": map[string]any{
							"name": "Overflow Room B",
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

	// 2. Retrieve event and verify locations map
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{evID},
		}, "call-2"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	evData := list[0].(map[string]any)

	locsMap, ok := evData["locations"].(map[string]any)
	if !ok || len(locsMap) != 2 {
		t.Fatalf("expected locations map with 2 entries, got %+v", evData["locations"])
	}

	locMain, _ := locsMap["loc-main"].(map[string]any)
	if locMain["name"] != "HQ Building 1" || locMain["description"] != "Room 402" {
		t.Errorf("unexpected loc-main data: %+v", locMain)
	}

	// 3. Test singular 'location' fallback patch
	updateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				evID: map[string]any{
					"location": map[string]any{
						"name": "New Singular Location",
					},
				},
			},
		}, "call-3"},
	}

	updateResp := postJMAP(t, ts.URL, using, updateReq)
	updated, _ := updateResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := updated[evID]; !ok {
		t.Fatalf("CalendarEvent/set update failed: %+v", updateResp.MethodResponses[0].Args)
	}

	getResp2 := postJMAP(t, ts.URL, using, getReq)
	list2, _ := getResp2.MethodResponses[0].Args["list"].([]any)
	evData2 := list2[0].(map[string]any)

	locsMap2, _ := evData2["locations"].(map[string]any)
	if locsMap2["loc-1"] == nil {
		t.Fatalf("expected fallback loc-1 in locations map, got %+v", locsMap2)
	}
}
