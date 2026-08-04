package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_UIDAutoGenerateAndPersistence tests that created events always have a stable uid per RFC 8984 §4.1.2.
func TestRFC8984_UIDAutoGenerateAndPersistence(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create an event without specifying 'uid'
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title": "Event Without UID",
					"start": "2026-08-10T10:00:00Z",
				},
				"c2": map[string]any{
					"title": "Event With Explicit UID",
					"uid":   "custom-uid-12345",
					"start": "2026-08-11T10:00:00Z",
				},
			},
		}, "call-1"},
	}

	resp := postJMAP(t, ts.URL, using, createReq)
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil || created["c2"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", resp.MethodResponses[0].Args)
	}

	ev1ID := created["c1"].(map[string]any)["id"].(string)
	ev2ID := created["c2"].(map[string]any)["id"].(string)

	// 2. Retrieve events and verify uid properties
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{ev1ID, ev2ID},
		}, "call-2"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)

	var ev1Data, ev2Data map[string]any
	for _, item := range list {
		m := item.(map[string]any)
		if m["id"] == ev1ID {
			ev1Data = m
		} else if m["id"] == ev2ID {
			ev2Data = m
		}
	}

	if uid1, _ := ev1Data["uid"].(string); uid1 == "" {
		t.Errorf("expected auto-generated uid for ev1, got empty: %+v", ev1Data)
	}
	if uid2, _ := ev2Data["uid"].(string); uid2 != "custom-uid-12345" {
		t.Errorf("expected explicit uid 'custom-uid-12345', got %v", uid2)
	}
}
