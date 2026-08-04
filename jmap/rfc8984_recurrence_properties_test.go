package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_RecurrencePropertiesRoundTrip tests recurrenceId, excludedRecurrenceRules, recurrenceOverrides, and excluded properties per RFC 8984 §4.3.
func TestRFC8984_RecurrencePropertiesRoundTrip(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create recurring event with recurrenceOverrides, excluded, and excludedRecurrenceRules
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title": "Weekly Sprint Planning",
					"start": "2026-08-03T10:00:00Z",
					"recurrenceRules": []any{
						map[string]any{
							"frequency": "weekly",
						},
					},
					"excludedRecurrenceRules": []any{
						map[string]any{
							"frequency": "monthly",
							"byMonth":   []string{"12"},
						},
					},
					"recurrenceOverrides": map[string]any{
						"2026-08-10T10:00:00Z": map[string]any{
							"title": "Weekly Planning (Rescheduled Room B)",
						},
					},
					"excluded": map[string]any{
						"2026-08-17T10:00:00Z": true,
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

	// 2. Retrieve event and verify recurrence properties
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{evID},
		}, "call-2"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	evData := list[0].(map[string]any)

	exRules, ok := evData["excludedRecurrenceRules"].([]any)
	if !ok || len(exRules) != 1 {
		t.Errorf("expected 1 excludedRecurrenceRule, got %+v", evData["excludedRecurrenceRules"])
	}

	overrides, ok := evData["recurrenceOverrides"].(map[string]any)
	if !ok || overrides["2026-08-10T10:00:00Z"] == nil {
		t.Errorf("expected recurrenceOverrides entry for 2026-08-10T10:00:00Z, got %+v", evData["recurrenceOverrides"])
	}

	exMap, ok := evData["excluded"].(map[string]any)
	if !ok || exMap["2026-08-17T10:00:00Z"] != true {
		t.Errorf("expected excluded entry for 2026-08-17T10:00:00Z, got %+v", evData["excluded"])
	}

	// 3. Patch recurrenceId and recurrenceIdTimeZone
	updateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				evID: map[string]any{
					"recurrenceId":         "2026-08-10T10:00:00Z",
					"recurrenceIdTimeZone": "America/New_York",
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

	if evData2["recurrenceId"] != "2026-08-10T10:00:00Z" {
		t.Errorf("expected recurrenceId '2026-08-10T10:00:00Z', got %v", evData2["recurrenceId"])
	}
	if evData2["recurrenceIdTimeZone"] != "America/New_York" {
		t.Errorf("expected recurrenceIdTimeZone 'America/New_York', got %v", evData2["recurrenceIdTimeZone"])
	}
}
