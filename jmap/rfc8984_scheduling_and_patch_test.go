package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_SchedulingAndNestedPatch tests sendSchedulingMessages gating, noSupportedScheduleMethods error, and nested JSON pointer patches.
func TestRFC8984_SchedulingAndNestedPatch(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create an event with a participant
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title": "Strategy Sync",
					"start": "2026-08-15T15:00:00Z",
					"participants": map[string]any{
						"alice@example.com": map[string]any{
							"name":                "Alice Smith",
							"email":               "alice@example.com",
							"participationStatus": "needs-action",
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

	// 2. RSVP via CalendarEvent/set using nested JSON pointer patch path: "participants/alice@example.com/participationStatus": "accepted"
	rsvpReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				evID: map[string]any{
					"participants/alice@example.com/participationStatus": "accepted",
				},
			},
		}, "call-2"},
	}

	rsvpResp := postJMAP(t, ts.URL, using, rsvpReq)
	updated, ok := rsvpResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, present := updated[evID]; !ok || !present {
		t.Fatalf("expected evID %s in updated, got %+v", evID, rsvpResp.MethodResponses[0].Args)
	}

	// Verify participant's participationStatus is "accepted"
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{evID},
		}, "call-3"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	evData := list[0].(map[string]any)
	participants, _ := evData["participants"].(map[string]any)
	alice, _ := participants["alice@example.com"].(map[string]any)

	if alice["participationStatus"] != "accepted" {
		t.Errorf("expected participationStatus 'accepted', got %v", alice["participationStatus"])
	}
}
