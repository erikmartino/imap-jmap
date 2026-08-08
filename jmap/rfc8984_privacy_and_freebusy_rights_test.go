package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8984_PrivacyOwnerSeesFullData verifies that the Principal that owns the calendar sees
// the full event data for both "private" and "secret" events. Per draft-ietf-jmap-calendars-27
// Section 4.2.10 the privacy property only restricts what NON-owner sharees see: "private"
// returns a reduced property set and "secret" makes the server behave as though the event does
// not exist — for users OTHER THAN the owner. CalendarEvent/get runs against the caller's own
// account, i.e. the owner, so it must never censor.
func TestRFC8984_PrivacyOwnerSeesFullData(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "4.2.10", spectest.MUST,
		"privacy=secret: the server behaves as though the event does not exist for users other than the owner; the owner still sees it.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "4.2.10", spectest.MUST,
		"privacy=private: only non-owner sharees get the reduced property set; the owner sees full data.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title":       "Doctor Appointment",
					"description": "Confidential medical review",
					"privacy":     "private",
					"start":       "2026-08-12T10:00:00Z",
				},
				"c2": map[string]any{
					"title":       "Secret Project Meeting",
					"description": "Unannounced launch",
					"privacy":     "secret",
					"start":       "2026-08-12T14:00:00Z",
				},
			},
		}, "call-1"},
	}

	resp := postJMAP(t, ts.URL, using, createReq)
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil || created["c2"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", resp.MethodResponses[0].Args)
	}

	privID := created["c1"].(map[string]any)["id"].(string)
	secID := created["c2"].(map[string]any)["id"].(string)

	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{privID, secID},
		}, "call-2"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	notFound, _ := getResp.MethodResponses[0].Args["notFound"].([]any)

	// The owner MUST see BOTH events in full — none in notFound.
	if len(notFound) != 0 {
		t.Errorf("owner must see private and secret events; got notFound=%+v", notFound)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 events (owner sees all), got %d: %+v", len(list), list)
	}

	byID := map[string]map[string]any{}
	for _, item := range list {
		m := item.(map[string]any)
		byID[m["id"].(string)] = m
	}

	priv := byID[privID]
	if priv == nil {
		t.Fatalf("private event %s missing from owner's list", privID)
	}
	if priv["title"] != "Doctor Appointment" || priv["description"] != "Confidential medical review" {
		t.Errorf("owner must see full private event data, got title=%v desc=%v", priv["title"], priv["description"])
	}
	if priv["privacy"] != "private" {
		t.Errorf("expected privacy=private preserved, got %v", priv["privacy"])
	}

	sec := byID[secID]
	if sec == nil {
		t.Fatalf("secret event %s missing from owner's list (must be visible to owner)", secID)
	}
	if sec["title"] != "Secret Project Meeting" {
		t.Errorf("owner must see full secret event data, got title=%v", sec["title"])
	}
}

// TestRFC8984_HideAttendeesRoundTrip verifies the hideAttendees property (owner-only participant
// visibility, draft-ietf-jmap-calendars-27 Section 4.4.5) round-trips through set/get.
func TestRFC8984_HideAttendeesRoundTrip(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title":         "Team Sync",
					"start":         "2026-08-12T10:00:00Z",
					"hideAttendees": true,
				},
			},
		}, "call-1"},
	}
	resp := postJMAP(t, ts.URL, using, createReq)
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil {
		t.Fatalf("create failed: %+v", resp.MethodResponses[0].Args)
	}
	evID := created["c1"].(map[string]any)["id"].(string)

	getResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{evID},
		}, "call-2"},
	})
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 event, got %d", len(list))
	}
	if got := list[0].(map[string]any)["hideAttendees"]; got != true {
		t.Errorf("expected hideAttendees=true, got %v", got)
	}
}
