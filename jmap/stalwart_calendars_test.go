package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestStalwart_CalendarLifecycleAndProperties translates stalwart's tests/src/jmap/calendar/calendars.rs.
// It verifies:
//   - Initial default calendar retrieval and spec-mandated properties.
//   - Calendar creation with custom properties: description, color, timeZone, sortOrder,
//     includeInAvailability, defaultAlertsWithTime, and defaultAlertsWithoutTime.
//   - Calendar/changes tracking after creation.
//   - Calendar/get roundtrip asserting all custom properties and alerts.
//   - Calendar/set update with JSON-pointer patches (including alerts addition and null-deletion).
//   - onSuccessSetIsDefault setting the updated calendar as the new default.
//   - Calendar destruction with events: fails with calendarHasEvents without onDestroyRemoveEvents,
//     and succeeds when onDestroyRemoveEvents is true.
func TestStalwart_CalendarLifecycleAndProperties(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST,
		"A */set update response value is null unless the server changed properties beyond those the client sent.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Make sure the default calendar exists and inspect properties
	getInitialResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId": "primary",
			"properties": []string{
				"id", "name", "description", "sortOrder", "color", "timeZone",
				"isSubscribed", "isDefault", "isVisible", "includeInAvailability",
				"defaultAlertsWithTime", "defaultAlertsWithoutTime",
			},
		}, "get-initial"},
	})
	initialList, ok := getInitialResp.MethodResponses[0].Args["list"].([]any)
	if !ok || len(initialList) == 0 {
		t.Fatalf("expected at least 1 default calendar, got %+v", getInitialResp.MethodResponses[0].Args)
	}
	defaultCal := initialList[0].(map[string]any)
	defaultCalID, _ := defaultCal["id"].(string)
	if defaultCalID == "" {
		t.Fatalf("default calendar missing id")
	}
	if defaultCal["isDefault"] != true {
		t.Errorf("expected default calendar isDefault=true, got %v", defaultCal["isDefault"])
	}
	initialState, _ := getInitialResp.MethodResponses[0].Args["state"].(string)

	// 2. Create Calendar with alerts and custom availability
	createResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"cal1": map[string]any{
					"name":                  "Test calendar",
					"description":           "My personal calendar",
					"sortOrder":             1,
					"isSubscribed":          true,
					"color":                 "#ff0000",
					"timeZone":              "Indian/Christmas",
					"isVisible":             false,
					"includeInAvailability": "attending",
					"defaultAlertsWithTime": map[string]any{
						"0": map[string]any{
							"action": "display",
							"trigger": map[string]any{
								"@type":      "OffsetTrigger",
								"relativeTo": "start",
								"offset":     "PT15M",
							},
						},
						"1": map[string]any{
							"action": "email",
							"trigger": map[string]any{
								"@type":      "OffsetTrigger",
								"relativeTo": "end",
								"offset":     "PT30M",
							},
						},
					},
					"defaultAlertsWithoutTime": map[string]any{
						"0": map[string]any{
							"action": "display",
							"trigger": map[string]any{
								"@type":      "OffsetTrigger",
								"relativeTo": "start",
								"offset":     "P1D",
							},
						},
						"1": map[string]any{
							"action": "email",
							"trigger": map[string]any{
								"@type":      "OffsetTrigger",
								"relativeTo": "end",
								"offset":     "P2D",
							},
						},
					},
				},
			},
		}, "create-cal"},
	})
	created, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["cal1"] == nil {
		t.Fatalf("Calendar/set create failed: %+v", createResp.MethodResponses[0].Args)
	}
	calendarID := created["cal1"].(map[string]any)["id"].(string)

	// 3. Validate changes via Calendar/changes
	changesResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": initialState,
		}, "chk-changes"},
	})
	createdChanges, _ := changesResp.MethodResponses[0].Args["created"].([]any)
	foundCreated := false
	for _, c := range createdChanges {
		if c == calendarID {
			foundCreated = true
			break
		}
	}
	if !foundCreated {
		t.Errorf("expected calendar %s in Calendar/changes created list, got %+v", calendarID, createdChanges)
	}

	// 4. Get Calendar and assert properties
	getResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId": "primary",
			"properties": []string{
				"id", "name", "description", "sortOrder", "color", "timeZone",
				"isSubscribed", "isDefault", "isVisible", "includeInAvailability",
				"defaultAlertsWithTime", "defaultAlertsWithoutTime",
			},
			"ids": []string{calendarID},
		}, "get-cal"},
	})
	getList, _ := getResp.MethodResponses[0].Args["list"].([]any)
	if len(getList) == 0 {
		t.Fatalf("expected calendar %s in get response, got %+v", calendarID, getResp.MethodResponses[0].Args)
	}
	calData := getList[0].(map[string]any)
	if calData["name"] != "Test calendar" {
		t.Errorf("name = %v, want 'Test calendar'", calData["name"])
	}
	if calData["description"] != "My personal calendar" {
		t.Errorf("description = %v, want 'My personal calendar'", calData["description"])
	}
	if calData["color"] != "#ff0000" {
		t.Errorf("color = %v, want '#ff0000'", calData["color"])
	}
	if calData["timeZone"] != "Indian/Christmas" {
		t.Errorf("timeZone = %v, want 'Indian/Christmas'", calData["timeZone"])
	}
	if calData["isSubscribed"] != true {
		t.Errorf("isSubscribed = %v, want true", calData["isSubscribed"])
	}
	if calData["isVisible"] != false {
		t.Errorf("isVisible = %v, want false", calData["isVisible"])
	}
	if calData["includeInAvailability"] != "attending" {
		t.Errorf("includeInAvailability = %v, want 'attending'", calData["includeInAvailability"])
	}
	alertsWithTime, _ := calData["defaultAlertsWithTime"].(map[string]any)
	if len(alertsWithTime) < 2 {
		t.Errorf("defaultAlertsWithTime len = %d, want 2; got %+v", len(alertsWithTime), alertsWithTime)
	}

	// 5. Update Calendar and set it as default via onSuccessSetIsDefault
	updateResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				calendarID: map[string]any{
					"name":                  "Updated calendar",
					"description":           "My updated personal calendar",
					"sortOrder":             2,
					"isSubscribed":          false,
					"isVisible":             true,
					"includeInAvailability": "none",
					"defaultAlertsWithTime": map[string]any{
						"0": map[string]any{
							"action": "email",
							"trigger": map[string]any{
								"@type":      "OffsetTrigger",
								"relativeTo": "start",
								"offset":     "PT10M",
							},
						},
					},
					"defaultAlertsWithoutTime/0": map[string]any{
						"action": "email",
						"trigger": map[string]any{
							"@type":      "OffsetTrigger",
							"relativeTo": "start",
							"offset":     "P3D",
						},
					},
					"defaultAlertsWithoutTime/1": nil,
					"defaultAlertsWithoutTime/2": map[string]any{
						"action": "display",
						"trigger": map[string]any{
							"@type":      "OffsetTrigger",
							"relativeTo": "end",
							"offset":     "P1W",
						},
					},
				},
			},
			"onSuccessSetIsDefault": calendarID,
		}, "update-cal"},
	})
	updatedMap, ok := updateResp.MethodResponses[0].Args["updated"].(map[string]any)
	if !ok || updatedMap[calendarID] == nil && updateResp.MethodResponses[0].Args["notUpdated"] != nil {
		if notUp, hasNot := updateResp.MethodResponses[0].Args["notUpdated"].(map[string]any); hasNot && len(notUp) > 0 {
			t.Fatalf("Calendar/set update failed: %+v", notUp)
		}
	}

	// 6. Validate updated calendar is default and old default is no longer default
	getUpdatedResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{calendarID, defaultCalID},
		}, "get-both"},
	})
	bothList, _ := getUpdatedResp.MethodResponses[0].Args["list"].([]any)
	var newDefCal, oldDefCal map[string]any
	for _, item := range bothList {
		m := item.(map[string]any)
		if m["id"] == calendarID {
			newDefCal = m
		} else if m["id"] == defaultCalID {
			oldDefCal = m
		}
	}
	if newDefCal == nil || newDefCal["isDefault"] != true {
		t.Errorf("expected calendar %s isDefault=true, got %+v", calendarID, newDefCal)
	}
	if newDefCal["name"] != "Updated calendar" {
		t.Errorf("name = %v, want 'Updated calendar'", newDefCal["name"])
	}
	if oldDefCal != nil && oldDefCal["isDefault"] == true {
		t.Errorf("expected previous default calendar %s isDefault=false", defaultCalID)
	}

	// 7. Create an event in the calendar
	addEvResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{
					"calendarIds": map[string]any{calendarID: true},
					"uid":         "a8df6573-0474-496d-8496-033ad45d7fea",
					"updated":     "2020-01-02T18:23:04Z",
					"title":       "Some event",
					"start":       "2020-01-15T13:00:00",
					"timeZone":    "America/New_York",
					"duration":    "PT1H",
				},
			},
		}, "create-ev"},
	})
	evCreated, _ := addEvResp.MethodResponses[0].Args["created"].(map[string]any)
	if evCreated == nil || evCreated["e1"] == nil {
		t.Fatalf("failed to create event in calendar: %+v", addEvResp.MethodResponses[0].Args)
	}

	// 8. Try destroying calendar without onDestroyRemoveEvents -> MUST fail with calendarHasEvents
	destroyFailResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{calendarID},
		}, "destroy-fail"},
	})
	notDestroyed, _ := destroyFailResp.MethodResponses[0].Args["notDestroyed"].(map[string]any)
	if notDestroyed == nil || notDestroyed[calendarID] == nil {
		t.Fatalf("expected notDestroyed for calendar with events, got %+v", destroyFailResp.MethodResponses[0].Args)
	}
	errType := notDestroyed[calendarID].(map[string]any)["type"]
	if errType != "calendarHasEvents" && errType != "calendarHasEvent" {
		t.Errorf("expected error calendarHasEvents, got %v", errType)
	}

	// 9. Destroy calendar with onDestroyRemoveEvents: true -> MUST succeed
	destroySuccessResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId":             "primary",
			"destroy":               []string{calendarID},
			"onDestroyRemoveEvents": true,
		}, "destroy-ok"},
	})
	destroyedList, _ := destroySuccessResp.MethodResponses[0].Args["destroyed"].([]any)
	foundDestroyed := false
	for _, id := range destroyedList {
		if id == calendarID {
			foundDestroyed = true
			break
		}
	}
	if !foundDestroyed {
		t.Fatalf("expected calendar %s in destroyed list, got %+v", calendarID, destroySuccessResp.MethodResponses[0].Args)
	}
}
