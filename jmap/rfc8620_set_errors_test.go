package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8620_Section5_3_SetErrors_StateMismatchAllHandlers verifies that an invalid ifInState token causes a stateMismatch error across all /set handlers per RFC 8620 Section 5.3.
func TestRFC8620_Section5_3_SetErrors_StateMismatchAllHandlers(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST,
		"If supplied, the string must match the current state;")

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
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST,
		"If an id given cannot be found, the update or destroy MUST be")

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
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST,
		"If an id given cannot be found, the update or destroy MUST be")

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

// TestRFC8620_Section5_3_BatchPartialSuccessAndNotCreatedUpdatedDestroyed verifies RFC 8620 Section 5.3:
// "If an individual create, update, or destroy fails... it MUST be added to the
// notCreated/notUpdated/notDestroyed property of the response, and the server MUST continue
// to the next create/update/destroy"
func TestRFC8620_Section5_3_BatchPartialSuccessAndNotCreatedUpdatedDestroyed(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST,
		"MUST be added to the notCreated/notUpdated/notDestroyed property of")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST,
		"the response, and the server MUST continue to the next create/update/")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Create mailboxes first to use for update and destroy
	r1 := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mb1": map[string]any{"name": "ValidMB1"},
				"mb2": map[string]any{"name": "ValidMB2"},
			},
		}, "setup"},
	})
	created, _ := r1.MethodResponses[0].Args["created"].(map[string]any)
	id1 := created["mb1"].(map[string]any)["id"].(string)
	id2 := created["mb2"].(map[string]any)["id"].(string)

	// 2. Perform batch /set with mixed valid and invalid operations:
	// - create: one valid, one invalid (empty name)
	// - update: one valid (rename id1), one invalid (non-existent id)
	// - destroy: one valid (destroy id2), one invalid (non-existent id)
	r2 := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"validCreate":   map[string]any{"name": "BatchCreated"},
				"invalidCreate": map[string]any{"name": ""},
			},
			"update": map[string]any{
				id1:              map[string]any{"name": "BatchRenamed"},
				"non-existent-u": map[string]any{"name": "Fail"},
			},
			"destroy": []any{id2, "non-existent-d"},
		}, "batch"},
	})

	args := r2.MethodResponses[0].Args

	// Verify create: valid succeeded in created, invalid reported in notCreated
	createdMap, _ := args["created"].(map[string]any)
	if createdMap["validCreate"] == nil {
		t.Errorf("expected validCreate in created, got %v", createdMap)
	}
	notCreatedMap, _ := args["notCreated"].(map[string]any)
	if notCreatedMap["invalidCreate"] == nil {
		t.Errorf("expected invalidCreate in notCreated, got %v", notCreatedMap)
	}

	// Verify update: valid succeeded in updated, invalid reported in notUpdated
	updatedMap, _ := args["updated"].(map[string]any)
	if _, ok := updatedMap[id1]; !ok {
		t.Errorf("expected %s in updated, got %v", id1, updatedMap)
	}
	notUpdatedMap, _ := args["notUpdated"].(map[string]any)
	if notUpdatedMap["non-existent-u"] == nil {
		t.Errorf("expected non-existent-u in notUpdated, got %v", notUpdatedMap)
	}

	// Verify destroy: valid succeeded in destroyed, invalid reported in notDestroyed
	destroyedList, _ := args["destroyed"].([]any)
	foundDestroyed := false
	for _, d := range destroyedList {
		if d == id2 {
			foundDestroyed = true
		}
	}
	if !foundDestroyed {
		t.Errorf("expected %s in destroyed list, got %v", id2, destroyedList)
	}
	notDestroyedMap, _ := args["notDestroyed"].(map[string]any)
	if notDestroyedMap["non-existent-d"] == nil {
		t.Errorf("expected non-existent-d in notDestroyed, got %v", notDestroyedMap)
	}
}
