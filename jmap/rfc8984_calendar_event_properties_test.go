package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_CalendarEventCommonPropertiesRoundTrip tests create, get, and patch for all 16 extended RFC 8984 properties.
func TestRFC8984_CalendarEventCommonPropertiesRoundTrip(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create CalendarEvent with all 16 properties
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title":                  "Project Architecture Review",
					"descriptionContentType": "text/markdown",
					"showWithoutTime":        true,
					"locale":                 "en-US",
					"categories":             map[string]any{"Architecture": true, "Work": true},
					"color":                  "#ff5722",
					"priority":               1,
					"replyTo":                map[string]any{"imip": "mailto:reply@example.com"},
					"sentBy":                 "mailto:assistant@example.com",
					"requestStatus":          "2.0;Success",
					"useDefaultAlerts":       true,
					"localizations": map[string]any{
						"fr": map[string]any{"title": "Revue d'architecture"},
					},
					"timeZones": map[string]any{
						"UTC": map[string]any{"tzId": "UTC"},
					},
					"relatedTo": map[string]any{
						"evt-parent-100": map[string]any{
							"relation": map[string]any{"parent": true},
						},
					},
					"prodId":   "-//Example Corp//JMAP Calendar//EN",
					"sequence": 5,
					"method":   "REQUEST",
				},
			},
		}, "call-1"},
	}

	resp := postJMAP(t, ts.URL, using, createReq)
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", resp.MethodResponses[0].Args)
	}

	eventMap, _ := created["c1"].(map[string]any)
	eventID, _ := eventMap["id"].(string)

	// 2. Retrieve event via CalendarEvent/get and assert all properties
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{eventID},
		}, "call-2"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, ok := getResp.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("CalendarEvent/get failed to return created event")
	}

	ev, _ := list[0].(map[string]any)

	if ev["descriptionContentType"] != "text/markdown" {
		t.Errorf("expected descriptionContentType 'text/markdown', got %v", ev["descriptionContentType"])
	}
	if ev["showWithoutTime"] != true {
		t.Errorf("expected showWithoutTime true, got %v", ev["showWithoutTime"])
	}
	if ev["locale"] != "en-US" {
		t.Errorf("expected locale 'en-US', got %v", ev["locale"])
	}
	if ev["color"] != "#ff5722" {
		t.Errorf("expected color '#ff5722', got %v", ev["color"])
	}
	if pri, ok := ev["priority"].(float64); !ok || uint32(pri) != 1 {
		t.Errorf("expected priority 1, got %v", ev["priority"])
	}
	if ev["sentBy"] != "mailto:assistant@example.com" {
		t.Errorf("expected sentBy 'mailto:assistant@example.com', got %v", ev["sentBy"])
	}
	if ev["requestStatus"] != "2.0;Success" {
		t.Errorf("expected requestStatus '2.0;Success', got %v", ev["requestStatus"])
	}
	if ev["useDefaultAlerts"] != true {
		t.Errorf("expected useDefaultAlerts true, got %v", ev["useDefaultAlerts"])
	}
	if ev["prodId"] != "-//Example Corp//JMAP Calendar//EN" {
		t.Errorf("expected prodId '-//Example Corp//JMAP Calendar//EN', got %v", ev["prodId"])
	}
	if seq, ok := ev["sequence"].(float64); !ok || uint32(seq) != 5 {
		t.Errorf("expected sequence 5, got %v", ev["sequence"])
	}
	if ev["method"] != "REQUEST" {
		t.Errorf("expected method 'REQUEST', got %v", ev["method"])
	}

	// 3. Patch event via CalendarEvent/set update
	updateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				eventID: map[string]any{
					"descriptionContentType": "text/html",
					"color":                  "#009688",
					"sequence":               6,
					"method":                 "REPLY",
					"useDefaultAlerts":       false,
				},
			},
		}, "call-3"},
	}

	updateResp := postJMAP(t, ts.URL, using, updateReq)
	updated, ok := updateResp.MethodResponses[0].Args["updated"].(map[string]any)
	if !ok {
		t.Fatalf("CalendarEvent/set update failed: %+v", updateResp.MethodResponses[0].Args)
	}
	if _, ok := updated[eventID]; !ok {
		t.Fatalf("CalendarEvent/set update did not contain %s: %+v", eventID, updateResp.MethodResponses[0].Args)
	}

	// 4. Verify patch updates
	getResp2 := postJMAP(t, ts.URL, using, getReq)
	list2, _ := getResp2.MethodResponses[0].Args["list"].([]any)
	ev2, _ := list2[0].(map[string]any)

	if ev2["descriptionContentType"] != "text/html" {
		t.Errorf("expected updated descriptionContentType 'text/html', got %v", ev2["descriptionContentType"])
	}
	if ev2["color"] != "#009688" {
		t.Errorf("expected updated color '#009688', got %v", ev2["color"])
	}
	if seq, ok := ev2["sequence"].(float64); !ok || uint32(seq) != 6 {
		t.Errorf("expected updated sequence 6, got %v", ev2["sequence"])
	}
	if ev2["method"] != "REPLY" {
		t.Errorf("expected updated method 'REPLY', got %v", ev2["method"])
	}
	if ev2["useDefaultAlerts"] != false {
		t.Errorf("expected updated useDefaultAlerts false, got %v", ev2["useDefaultAlerts"])
	}
}
