package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestCalendarEvent_UpdateWithLeadingSlashJSONPointers verifies that CalendarEvent/set update
// correctly processes RFC 6901 JSON pointer paths with leading slashes (/title, /description, /calendarIds/..., etc.).
func TestCalendarEvent_UpdateWithLeadingSlashJSONPointers(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create a work calendar
	calReq := []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c_work": map[string]any{
					"name": "Work",
				},
			},
		}, "cal-call"},
	}
	calResp := postJMAP(t, ts.URL, using, calReq)
	workCalID := calResp.MethodResponses[0].Args["created"].(map[string]any)["c_work"].(map[string]any)["id"].(string)

	// 2. Create a calendar event
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title":       "Original Title",
					"description": "Original Description",
					"start":       "2026-09-01T10:00:00",
					"duration":    "PT1H",
					"timeZone":    "Europe/Berlin",
					"calendarIds": map[string]any{"cal-default": true},
				},
			},
		}, "call-1"},
	}

	createResp := postJMAP(t, ts.URL, using, createReq)
	created, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", createResp.MethodResponses[0].Args)
	}
	eventMap := created["c1"].(map[string]any)
	eventID := eventMap["id"].(string)

	// 3. Patch using leading slash JSON Pointers (/title, /description, /calendarIds/<workCalID>, /utcStart, /utcEnd)
	updateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				eventID: map[string]any{
					"/title":                        "Updated Title with Slash",
					"/description":                  "Updated Description with Slash",
					"/calendarIds/" + workCalID:     true,
					"/calendarIds/cal-default":     false,
					"/utcStart":                     "2026-09-01T08:00:00Z",
					"/utcEnd":                       "2026-09-01T09:00:00Z",
					"/isDraft":                      false,
				},
			},
		}, "call-2"},
	}

	updateResp := postJMAP(t, ts.URL, using, updateReq)
	notUpdated, _ := updateResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if len(notUpdated) > 0 {
		t.Fatalf("CalendarEvent/set update failed with notUpdated: %+v", notUpdated)
	}
	updated, ok := updateResp.MethodResponses[0].Args["updated"].(map[string]any)
	if !ok {
		t.Fatalf("expected updated map in response: %+v", updateResp.MethodResponses[0].Args)
	}
	if _, exists := updated[eventID]; !exists {
		t.Fatalf("expected %s in updated map: %+v", eventID, updateResp.MethodResponses[0].Args)
	}

	// 4. Fetch event and assert updated fields
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{eventID},
		}, "call-3"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, ok := getResp.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("expected 1 event in list, got: %+v", getResp.MethodResponses[0].Args)
	}
	ev := list[0].(map[string]any)
	if ev["title"] != "Updated Title with Slash" {
		t.Errorf("expected title 'Updated Title with Slash', got %v", ev["title"])
	}
	if ev["description"] != "Updated Description with Slash" {
		t.Errorf("expected description 'Updated Description with Slash', got %v", ev["description"])
	}
	calIDs, _ := ev["calendarIds"].(map[string]any)
	if calIDs == nil || calIDs[workCalID] != true || calIDs["cal-default"] == true {
		t.Errorf("expected calendarIds to have %s=true and cal-default removed/false, got: %+v", workCalID, calIDs)
	}
}

// TestCalendarEvent_RecurrenceInstanceUpdateAndDestroy verifies editing and destroying
// an individual recurrence instance identified by masterID#recurrenceId.
func TestCalendarEvent_RecurrenceInstanceUpdateAndDestroy(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create a recurring event
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"rec1": map[string]any{
					"title":       "Weekly Standup",
					"start":       "2026-09-01T09:00:00",
					"duration":    "PT30M",
					"timeZone":    "UTC",
					"calendarIds": map[string]any{"cal-default": true},
					"recurrenceRules": []any{
						map[string]any{
							"frequency": "weekly",
							"interval":  1,
							"count":     5,
						},
					},
				},
			},
		}, "call-1"},
	}

	createResp := postJMAP(t, ts.URL, using, createReq)
	created, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["rec1"] == nil {
		t.Fatalf("create failed: %+v", createResp.MethodResponses[0].Args)
	}
	masterID := created["rec1"].(map[string]any)["id"].(string)
	instanceID := masterID + "#2026-09-08T09:00:00"

	// 2. Update specific recurrence instance
	updateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				instanceID: map[string]any{
					"/title":    "Weekly Standup (Special Topic)",
					"/duration": "PT1H",
				},
			},
		}, "call-2"},
	}

	updateResp := postJMAP(t, ts.URL, using, updateReq)
	notUpdated, _ := updateResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if len(notUpdated) > 0 {
		t.Fatalf("instance update failed: %+v", notUpdated)
	}

	// 3. Query expanded recurrences and verify instance has updated title
	queryReq := []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId":         "primary",
			"expandRecurrences": true,
		}, "call-3"},
	}
	queryResp := postJMAP(t, ts.URL, using, queryReq)
	ids, _ := queryResp.MethodResponses[0].Args["ids"].([]any)
	if len(ids) == 0 {
		t.Fatalf("expected expanded instances, got: %+v", queryResp.MethodResponses[0].Args)
	}

	// 4. Fetch the instance
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{instanceID},
		}, "call-4"},
	}
	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 instance in list, got: %+v", getResp.MethodResponses[0].Args)
	}
	inst := list[0].(map[string]any)
	if inst["title"] != "Weekly Standup (Special Topic)" {
		t.Errorf("expected title 'Weekly Standup (Special Topic)', got %v", inst["title"])
	}

	// 5. Delete specific recurrence instance
	delReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{instanceID},
		}, "call-5"},
	}
	delResp := postJMAP(t, ts.URL, using, delReq)
	destroyed, _ := delResp.MethodResponses[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != instanceID {
		t.Fatalf("expected instance to be destroyed, got: %+v", delResp.MethodResponses[0].Args)
	}
}

// TestCalendarEvent_UTCTimezoneDefaultAndDrag verifies that when an event without an explicit
// timezone is created and subsequently dragged/updated, timeZone defaults to Etc/UTC and utcStart
// matches the start instant exactly without arbitrary timezone shifts.
func TestCalendarEvent_UTCTimezoneDefaultAndDrag(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create event with floating/unspecified timeZone
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{
					"title":       "Team Standup",
					"start":       "2026-09-01T14:00:00",
					"duration":    "PT1H",
					"calendarIds": map[string]any{"cal-default": true},
				},
			},
		}, "call-1"},
	}

	createResp := postJMAP(t, ts.URL, using, createReq)
	created := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if created["e1"] == nil {
		t.Fatalf("create failed: %+v", createResp.MethodResponses[0].Args)
	}
	evID := created["e1"].(map[string]any)["id"].(string)

	// 2. Fetch event: verify timeZone defaults to Etc/UTC and utcStart is 14:00:00Z
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{evID},
		}, "call-2"},
	}
	getResp := postJMAP(t, ts.URL, using, getReq)
	ev := getResp.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if ev["timeZone"] != "Etc/UTC" {
		t.Errorf("expected timeZone Etc/UTC, got %v", ev["timeZone"])
	}
	if ev["utcStart"] != "2026-09-01T14:00:00Z" {
		t.Errorf("expected utcStart 2026-09-01T14:00:00Z, got %v", ev["utcStart"])
	}
	if ev["utcEnd"] != "2026-09-01T15:00:00Z" {
		t.Errorf("expected utcEnd 2026-09-01T15:00:00Z, got %v", ev["utcEnd"])
	}

	// 3. Drag event to 16:00:00 (simulate Bulwark update without timezone parameter)
	updateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				evID: map[string]any{
					"start": "2026-09-01T16:00:00",
				},
			},
		}, "call-3"},
	}
	updateResp := postJMAP(t, ts.URL, using, updateReq)
	notUpdated, _ := updateResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if len(notUpdated) > 0 {
		t.Fatalf("drag update failed: %+v", notUpdated)
	}

	// 4. Fetch updated event: verify start is 16:00:00 and utcStart is 16:00:00Z (no offset shift)
	getResp2 := postJMAP(t, ts.URL, using, getReq)
	ev2 := getResp2.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if ev2["start"] != "2026-09-01T16:00:00" {
		t.Errorf("expected start 2026-09-01T16:00:00, got %v", ev2["start"])
	}
	if ev2["utcStart"] != "2026-09-01T16:00:00Z" {
		t.Errorf("expected utcStart 2026-09-01T16:00:00Z, got %v", ev2["utcStart"])
	}
	if ev2["utcEnd"] != "2026-09-01T17:00:00Z" {
		t.Errorf("expected utcEnd 2026-09-01T17:00:00Z, got %v", ev2["utcEnd"])
	}
}

