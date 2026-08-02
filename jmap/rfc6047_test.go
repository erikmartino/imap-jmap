package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC6047_AutoSendInvitationAndCancellation tests RFC 6047 iMIP email binding for invitation request & cancellation.
func TestRFC6047_AutoSendInvitationAndCancellation(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	mailBackend := memory.NewMemoryBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend), jmap.WithMailBackend(mailBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Issue CalendarEvent/set create with attendee -> verifies RFC 6047 iMIP email dispatch
	createReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{
				"CalendarEvent/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"ev6047": map[string]any{
							"title": "RFC 6047 iMIP Sync",
							"start": "2026-09-01T10:00:00Z",
							"participants": map[string]any{
								"external6047@example.com": map[string]any{
									"name":   "External Client",
									"email":  "external6047@example.com",
									"status": "needs-action",
								},
							},
						},
					},
				},
				"c1",
			},
		},
	}

	bodyBytes, _ := json.Marshal(createReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST set failed: %v", err)
	}
	defer resp.Body.Close()

	var setResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&setResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	createdMap := setResp.MethodResponses[0].Args["created"].(map[string]any)
	createdEv := createdMap["ev6047"].(map[string]any)
	evID := createdEv["id"].(string)

	// Verify iMIP REQUEST email created in MailBackend
	emails, err := mailBackend.GetAllEmails(nil)
	if err != nil || len(emails) == 0 {
		t.Fatalf("Expected RFC 6047 iMIP email in MailBackend, got 0")
	}

	foundInvite := false
	for _, em := range emails {
		if strings.Contains(em.Subject, "RFC 6047 iMIP Sync") {
			foundInvite = true
			if len(em.TextBody) == 0 || em.TextBody[0].Type != "text/calendar; method=REQUEST" {
				t.Errorf("Expected Content-Type 'text/calendar; method=REQUEST', got %v", em.TextBody)
			}
		}
	}
	if !foundInvite {
		t.Errorf("Expected iMIP invitation email with subject containing 'RFC 6047 iMIP Sync'")
	}

	// 2. Issue CalendarEvent/set destroy -> verifies RFC 6047 iMIP cancellation email dispatch
	destroyReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{
				"CalendarEvent/set",
				map[string]any{
					"accountId": "primary",
					"destroy":   []string{evID},
				},
				"c2",
			},
		},
	}

	bodyBytesDestroy, _ := json.Marshal(destroyReq)
	respDestroy, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytesDestroy))
	if err != nil {
		t.Fatalf("JMAP POST destroy failed: %v", err)
	}
	defer respDestroy.Body.Close()

	emailsAfterDestroy, _ := mailBackend.GetAllEmails(nil)
	foundCancel := false
	for _, em := range emailsAfterDestroy {
		if strings.Contains(em.Subject, "Cancelled: RFC 6047 iMIP Sync") {
			foundCancel = true
			if len(em.TextBody) == 0 || em.TextBody[0].Type != "text/calendar; method=CANCEL" {
				t.Errorf("Expected Content-Type 'text/calendar; method=CANCEL', got %v", em.TextBody)
			}
		}
	}
	if !foundCancel {
		t.Errorf("Expected iMIP cancellation email with subject 'Cancelled: RFC 6047 iMIP Sync'")
	}
}
