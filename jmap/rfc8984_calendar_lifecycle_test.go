package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_CalendarLifecycleAndRights tests calendar rights, isDefault rejection, onDestroyRemoveEvents, and onSuccessSetIsDefault.
func TestRFC8984_CalendarLifecycleAndRights(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create calendar with direct isDefault: true -> MUST reject with invalidProperties
	createInvalidReq := []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"name":      "Direct Default Cal",
					"isDefault": true,
				},
			},
		}, "call-1"},
	}

	resp1 := postJMAP(t, ts.URL, using, createInvalidReq)
	notCreated, ok := resp1.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok || notCreated["c1"] == nil {
		t.Fatalf("expected notCreated entry for direct isDefault set, got %+v", resp1.MethodResponses[0].Args)
	}

	// 2. Create calendar using onSuccessSetIsDefault arg
	createValidReq := []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c2": map[string]any{
					"name": "Team Work Calendar",
				},
			},
			"onSuccessSetIsDefault": "#c2",
		}, "call-2"},
	}

	resp2 := postJMAP(t, ts.URL, using, createValidReq)
	created, ok := resp2.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c2"] == nil {
		t.Fatalf("Calendar/set create failed: %+v", resp2.MethodResponses[0].Args)
	}

	calID := created["c2"].(map[string]any)["id"].(string)

	// Verify calendar is default and has full spec myRights
	getReq := []any{
		[]any{"Calendar/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{calID},
		}, "call-3"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	calData := list[0].(map[string]any)

	if calData["isDefault"] != true {
		t.Errorf("expected isDefault true via onSuccessSetIsDefault, got %v", calData["isDefault"])
	}

	myRights, ok := calData["myRights"].(map[string]any)
	if !ok || myRights["mayWriteAll"] != true || myRights["mayWriteOwn"] != true || myRights["mayRSVP"] != true {
		t.Errorf("expected spec myRights with mayWriteAll, mayWriteOwn, mayRSVP, got %+v", calData["myRights"])
	}

	// 3. Add event to calendar calID
	addEvReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{
					"title":       "Event in Cal",
					"calendarIds": map[string]any{calID: true},
				},
			},
		}, "call-4"},
	}

	postJMAP(t, ts.URL, using, addEvReq)

	// 4. Attempt destroy calendar without onDestroyRemoveEvents -> MUST fail with calendarHasEvents
	destroyReq := []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{calID},
		}, "call-5"},
	}

	destroyResp := postJMAP(t, ts.URL, using, destroyReq)
	notDestroyed, ok := destroyResp.MethodResponses[0].Args["notDestroyed"].(map[string]any)
	if !ok || notDestroyed[calID] == nil {
		t.Fatalf("expected notDestroyed entry for calendar with events, got %+v", destroyResp.MethodResponses[0].Args)
	}

	errCal, _ := notDestroyed[calID].(map[string]any)
	if errCal["type"] != "calendarHasEvents" {
		t.Errorf("expected type calendarHasEvents, got %v", errCal["type"])
	}

	// 5. Destroy calendar WITH onDestroyRemoveEvents: true (and set cal-default as default) -> MUST succeed
	destroyReq2 := []any{
		[]any{"Calendar/set", map[string]any{
			"accountId":              "primary",
			"destroy":                []string{calID},
			"onDestroyRemoveEvents": true,
			"onSuccessSetIsDefault": "cal-default",
		}, "call-6"},
	}

	destroyResp2 := postJMAP(t, ts.URL, using, destroyReq2)
	destroyed, ok := destroyResp2.MethodResponses[0].Args["destroyed"].([]any)
	if !ok || len(destroyed) != 1 {
		t.Fatalf("expected 1 destroyed calendar, got %+v", destroyResp2.MethodResponses[0].Args)
	}
}
