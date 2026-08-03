package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_Section5_3_SetErrors_StateMismatchAllHandlers verifies that an invalid ifInState token causes a stateMismatch error across all /set handlers per RFC 8620 Section 5.3.
func TestRFC8620_Section5_3_SetErrors_StateMismatchAllHandlers(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	invalidState := "invalid-state-token-999"

	setCases := []struct {
		method string
		using  []string
	}{
		{"Mailbox/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"Identity/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"Email/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"SieveScript/set", []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}},
		{"FileNode/set", []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI}},
		{"Calendar/set", []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}},
		{"CalendarEvent/set", []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}},
		{"AddressBook/set", []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}},
		{"Card/set", []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}},
		{"PushSubscription/set", []string{jmap.CoreCapabilityURI}},
	}

	for _, tc := range setCases {
		t.Run(tc.method+"_stateMismatch", func(t *testing.T) {
			res := postJMAP(t, ts.URL, tc.using, []any{
				[]any{tc.method, map[string]any{
					"accountId": "primary",
					"ifInState": invalidState,
				}, "c1"},
			})
			if len(res.MethodResponses) == 0 {
				t.Fatalf("No response for %s", tc.method)
			}
			mr := res.MethodResponses[0]
			if mr.Name != "error" {
				t.Errorf("Expected 'error' method response for %s stateMismatch, got %q", tc.method, mr.Name)
			}
			errType, _ := mr.Args["type"].(string)
			if errType != "stateMismatch" {
				t.Errorf("Expected error type 'stateMismatch' for %s, got %q", tc.method, errType)
			}
		})
	}
}

// TestRFC8620_Section5_3_SetErrors_NotDestroyedAllHandlers asserts that destroying a non-existent ID returns notDestroyed with type notFound per RFC 8620 Section 5.3.
func TestRFC8620_Section5_3_SetErrors_NotDestroyedAllHandlers(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	missingID := "non-existent-id-12345"

	setCases := []struct {
		method string
		using  []string
	}{
		{"Mailbox/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"Identity/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"Email/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"EmailSubmission/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}},
		{"SieveScript/set", []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}},
		{"FileNode/set", []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI}},
		{"Calendar/set", []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}},
		{"CalendarEvent/set", []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}},
		{"AddressBook/set", []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}},
		{"Card/set", []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}},
	}

	for _, tc := range setCases {
		t.Run(tc.method+"_notDestroyed", func(t *testing.T) {
			res := postJMAP(t, ts.URL, tc.using, []any{
				[]any{tc.method, map[string]any{
					"accountId": "primary",
					"destroy":   []any{missingID},
				}, "c1"},
			})
			if len(res.MethodResponses) == 0 {
				t.Fatalf("No response for %s", tc.method)
			}
			mr := res.MethodResponses[0]
			if mr.Name != tc.method {
				t.Fatalf("Expected response name %q, got %q", tc.method, mr.Name)
			}
			notDestroyed, _ := mr.Args["notDestroyed"].(map[string]any)
			errObj, ok := notDestroyed[missingID].(map[string]any)
			if !ok {
				t.Fatalf("%s notDestroyed did not contain entry for %q", tc.method, missingID)
			}
			errType, _ := errObj["type"].(string)
			if errType != "notFound" {
				t.Errorf("%s notDestroyed type expected 'notFound', got %q", tc.method, errType)
			}
		})
	}
}

// TestRFC8620_Section5_3_SetErrors_NotUpdatedAllHandlers asserts that updating a non-existent ID returns notUpdated with type notFound per RFC 8620 Section 5.3.
func TestRFC8620_Section5_3_SetErrors_NotUpdatedAllHandlers(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	missingID := "non-existent-id-99999"

	setCases := []struct {
		method string
		using  []string
		patch  map[string]any
	}{
		{"Mailbox/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, map[string]any{"name": "Renamed"}},
		{"Identity/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, map[string]any{"name": "Renamed"}},
		{"Email/set", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, map[string]any{"keywords/$seen": true}},
		{"SieveScript/set", []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}, map[string]any{"name": "Script"}},
		{"FileNode/set", []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI}, map[string]any{"name": "File"}},
		{"Calendar/set", []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}, map[string]any{"name": "Cal"}},
		{"CalendarEvent/set", []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}, map[string]any{"title": "Title"}},
		{"AddressBook/set", []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}, map[string]any{"name": "AB"}},
		{"Card/set", []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}, map[string]any{"fn": "Name"}},
	}

	for _, tc := range setCases {
		t.Run(tc.method+"_notUpdated", func(t *testing.T) {
			res := postJMAP(t, ts.URL, tc.using, []any{
				[]any{tc.method, map[string]any{
					"accountId": "primary",
					"update": map[string]any{
						missingID: tc.patch,
					},
				}, "c1"},
			})
			if len(res.MethodResponses) == 0 {
				t.Fatalf("No response for %s", tc.method)
			}
			mr := res.MethodResponses[0]
			if mr.Name != tc.method {
				t.Fatalf("Expected response name %q, got %q", tc.method, mr.Name)
			}
			notUpdated, _ := mr.Args["notUpdated"].(map[string]any)
			errObj, ok := notUpdated[missingID].(map[string]any)
			if !ok {
				t.Fatalf("%s notUpdated did not contain entry for %q", tc.method, missingID)
			}
			errType, _ := errObj["type"].(string)
			if errType != "notFound" {
				t.Errorf("%s notUpdated type expected 'notFound', got %q", tc.method, errType)
			}
		})
	}
	_ = context.Background()
}
