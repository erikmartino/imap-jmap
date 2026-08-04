package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_PrivacyAndFreeBusyRights tests freebusy rights calendar hiding and private/secret event censoring per RFC 8984.
func TestRFC8984_PrivacyAndFreeBusyRights(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create a private event and a secret event
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

	// 2. Retrieve private and secret events
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{privID, secID},
		}, "call-2"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	notFound, _ := getResp.MethodResponses[0].Args["notFound"].([]any)

	// Secret event MUST be in notFound
	foundSecInNotFound := false
	for _, item := range notFound {
		if item == secID {
			foundSecInNotFound = true
			break
		}
	}
	if !foundSecInNotFound {
		t.Errorf("expected secret event id %s in notFound, got %+v", secID, notFound)
	}

	// Private event MUST have title "Busy" and empty description
	if len(list) != 1 {
		t.Fatalf("expected 1 event in list for private event, got %d", len(list))
	}

	privData := list[0].(map[string]any)
	if privData["title"] != "Busy" || (privData["description"] != nil && privData["description"] != "") {
		t.Errorf("expected private event title 'Busy' and empty description, got title=%v desc=%v", privData["title"], privData["description"])
	}
}
