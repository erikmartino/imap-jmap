package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8621_Section4_3_EmailSetUpdateKeywords tests Email/set update patch for keywords per RFC 8621 Section 4.3.
func TestRFC8621_Section4_3_EmailSetUpdateKeywords(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Update email-1: remove $unread / set $seen
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/set", map[string]any{
				"accountId": "primary",
				"update": map[string]any{
					"email-1": map[string]any{
						"keywords/$unread": nil,
						"keywords/$seen":   true,
					},
				},
			}, "c1"},
			[]any{"Mailbox/get", map[string]any{
				"accountId": "primary",
				"ids":       []string{"mb-inbox"},
			}, "c2"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	if len(jmapResp.MethodResponses) < 2 {
		t.Fatalf("Expected 2 method responses, got %d", len(jmapResp.MethodResponses))
	}

	setResp := jmapResp.MethodResponses[0]
	if setResp.Name != "Email/set" {
		t.Fatalf("Expected method response 'Email/set', got %q", setResp.Name)
	}

	updated, ok := setResp.Args["updated"].(map[string]any)
	if !ok || updated["email-1"] == nil {
		t.Errorf("Expected email-1 in updated map, got %v", setResp.Args["updated"])
	}

	// Verify mailbox unread counter was updated
	mbResp := jmapResp.MethodResponses[1]
	listRaw, ok := mbResp.Args["list"].([]any)
	if !ok || len(listRaw) != 1 {
		t.Fatalf("Expected 1 mailbox in list, got %v", mbResp.Args["list"])
	}

	mbMap, ok := listRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("Invalid mailbox object")
	}

	unreadEmails, _ := mbMap["unreadEmails"].(float64)
	if unreadEmails != 1 {
		t.Errorf("Expected 1 unread email in inbox after marking email-1 as read, got %v", unreadEmails)
	}
}
