package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestEmailSubmissionSetDestroyTests tests EmailSubmission/set create, destroy, and error paths per RFC 8621 Section 7.3.
func TestEmailSubmissionSetDestroyTests(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Seed an email
	em, err := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Submission Destroy Test",
	})
	if err != nil {
		t.Fatalf("Failed to create email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}

	// 2. Create an EmailSubmission
	calls1 := []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"emailId":    string(em.ID),
					"identityId": "id-primary",
				},
			},
		}, "c1"},
	}
	res1 := postJMAP(t, ts.URL, using, calls1)
	var subID string
	if len(res1.MethodResponses) > 0 {
		args := res1.MethodResponses[0].Args
		if created, ok := args["created"].(map[string]any); ok {
			if subMap, ok := created["sub1"].(map[string]any); ok {
				subID, _ = subMap["id"].(string)
			}
		}
	}
	if subID == "" {
		t.Fatalf("Failed to create EmailSubmission")
	}

	// 3. Destroy the submission & attempt to update (must produce notUpdated invalidProperties and destroyed)
	calls2 := []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{"sendAt": "2030-01-01T00:00:00Z"},
			},
			"destroy": []any{subID, "non-existent-sub"},
		}, "c2"},
	}
	res2 := postJMAP(t, ts.URL, using, calls2)
	if len(res2.MethodResponses) == 0 {
		t.Fatalf("Empty response for EmailSubmission/set destroy")
	}
	args2 := res2.MethodResponses[0].Args
	destroyed, _ := args2["destroyed"].([]any)
	notDestroyed, _ := args2["notDestroyed"].(map[string]any)
	notUpdated, _ := args2["notUpdated"].(map[string]any)

	if len(destroyed) != 1 || destroyed[0] != subID {
		t.Errorf("Expected destroyed=[%q], got %v", subID, destroyed)
	}
	if notDestroyed["non-existent-sub"] == nil {
		t.Errorf("Expected notDestroyed for 'non-existent-sub', got %v", notDestroyed)
	}
	if notUpdated[subID] == nil {
		t.Errorf("Expected notUpdated for %q (submissions immutable), got %v", subID, notUpdated)
	}
}

// TestGetMethods_IdsNullAllRecords verifies Thread/get, EmailSubmission/get, Blob/get return all records when ids: null.
func TestGetMethods_IdsNullAllRecords(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed items
	em1, _ := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Item 1",
	})
	_, _ = srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Item 2",
	})
	_, _ = srv.MailBackend.CreateSubmission(context.Background(), &jmap.EmailSubmission{
		EmailID:    em1.ID,
		IdentityID: "id1",
	})
	_, _ = srv.BlobBackend.PutBlob(context.Background(), "primary", "text/plain", []byte("hello blob"))

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI, jmap.BlobCapabilityURI}
	calls := []any{
		[]any{"Thread/get", map[string]any{"accountId": "primary", "ids": nil}, "c1"},
		[]any{"EmailSubmission/get", map[string]any{"accountId": "primary", "ids": nil}, "c2"},
		[]any{"Blob/get", map[string]any{"accountId": "primary", "ids": nil}, "c3"},
	}

	res := postJMAP(t, ts.URL, using, calls)
	if len(res.MethodResponses) != 3 {
		t.Fatalf("Expected 3 responses, got %d", len(res.MethodResponses))
	}

	// Thread/get list must contain all items (at least 2 seeded in test plus default items)
	tList, _ := res.MethodResponses[0].Args["list"].([]any)
	if len(tList) < 2 {
		t.Errorf("Thread/get with ids:null expected at least 2 items, got %d", len(tList))
	}

	// EmailSubmission/get list must contain 1 item
	sList, _ := res.MethodResponses[1].Args["list"].([]any)
	if len(sList) != 1 {
		t.Errorf("EmailSubmission/get with ids:null expected 1 item, got %d", len(sList))
	}

	// Blob/get list must contain 1 item
	bList, _ := res.MethodResponses[2].Args["list"].([]any)
	if len(bList) != 1 {
		t.Errorf("Blob/get with ids:null expected 1 item, got %d", len(bList))
	}
}

// TestMailboxQuery_HasAnyRole_IsSubscribed tests Mailbox/query hasAnyRole and isSubscribed filters.
func TestMailboxQuery_HasAnyRole_IsSubscribed(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed a custom mailbox without a role and not subscribed
	mbCustom, _ := srv.MailBackend.CreateMailbox(context.Background(), &jmap.Mailbox{
		Name:         "CustomUnsubscribedNoRole",
		Role:         nil,
		IsSubscribed: false,
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}
	calls := []any{
		[]any{"Mailbox/query", map[string]any{"accountId": "primary", "filter": map[string]any{"hasAnyRole": false}}, "c1"},
		[]any{"Mailbox/query", map[string]any{"accountId": "primary", "filter": map[string]any{"isSubscribed": false}}, "c2"},
		[]any{"Mailbox/query", map[string]any{"accountId": "primary", "filter": map[string]any{"name": "CustomUnsubscribedNoRole", "hasAnyRole": false}}, "c3"},
	}

	res := postJMAP(t, ts.URL, using, calls)
	if len(res.MethodResponses) != 3 {
		t.Fatalf("Expected 3 responses, got %d", len(res.MethodResponses))
	}

	ids1, _ := res.MethodResponses[0].Args["ids"].([]any)
	foundCustom1 := false
	for _, id := range ids1 {
		if id == string(mbCustom.ID) {
			foundCustom1 = true
		}
	}
	if !foundCustom1 {
		t.Errorf("hasAnyRole:false expected to include %s, got %v", mbCustom.ID, ids1)
	}

	ids2, _ := res.MethodResponses[1].Args["ids"].([]any)
	foundCustom2 := false
	for _, id := range ids2 {
		if id == string(mbCustom.ID) {
			foundCustom2 = true
		}
	}
	if !foundCustom2 {
		t.Errorf("isSubscribed:false expected to include %s, got %v", mbCustom.ID, ids2)
	}

	ids3, _ := res.MethodResponses[2].Args["ids"].([]any)
	if len(ids3) != 1 || ids3[0] != string(mbCustom.ID) {
		t.Errorf("name+hasAnyRole:false expected [%s], got %v", mbCustom.ID, ids3)
	}
}

// TestQuotaQuery_Filter tests Quota/query filtering by name, scope, and resourceType.
func TestQuotaQuery_Filter(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.QuotaCapabilityURI}
	calls := []any{
		[]any{"Quota/query", map[string]any{"accountId": "primary", "filter": map[string]any{"resourceType": "octets"}}, "c1"},
		[]any{"Quota/query", map[string]any{"accountId": "primary", "filter": map[string]any{"resourceType": "non-existent"}}, "c2"},
	}

	res := postJMAP(t, ts.URL, using, calls)
	if len(res.MethodResponses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(res.MethodResponses))
	}

	ids1, _ := res.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 {
		t.Errorf("Quota/query resourceType:octets expected 1 item, got %d", len(ids1))
	}

	ids2, _ := res.MethodResponses[1].Args["ids"].([]any)
	if len(ids2) != 0 {
		t.Errorf("Quota/query resourceType:non-existent expected 0 items, got %d", len(ids2))
	}
}

// TestCalendarEventQuery_FilterProperties tests CalendarEvent/query uid, updatedBefore, and updatedAfter.
func TestCalendarEventQuery_FilterProperties(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed calendar event
	ev, err := srv.CalendarsBackend.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		CalendarIDs: map[jmap.Id]bool{"cal-1": true},
		Title:       "Meeting",
		UID:         "unique-uid-123",
		Updated:     "2026-06-01T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("Failed to create CalendarEvent: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}
	calls := []any{
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary", "filter": map[string]any{"uid": "unique-uid-123"}}, "c1"},
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary", "filter": map[string]any{"uid": "wrong-uid"}}, "c2"},
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary", "filter": map[string]any{"updatedAfter": "2026-01-01T00:00:00Z"}}, "c3"},
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary", "filter": map[string]any{"updatedBefore": "2026-01-01T00:00:00Z"}}, "c4"},
	}

	res := postJMAP(t, ts.URL, using, calls)
	if len(res.MethodResponses) != 4 {
		t.Fatalf("Expected 4 responses, got %d", len(res.MethodResponses))
	}

	ids1, _ := res.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 || ids1[0] != string(ev.ID) {
		t.Errorf("uid match expected [%s], got %v", ev.ID, ids1)
	}

	ids2, _ := res.MethodResponses[1].Args["ids"].([]any)
	if len(ids2) != 0 {
		t.Errorf("uid mismatch expected [], got %v", ids2)
	}

	ids3, _ := res.MethodResponses[2].Args["ids"].([]any)
	if len(ids3) != 1 || ids3[0] != string(ev.ID) {
		t.Errorf("updatedAfter match expected [%s], got %v", ev.ID, ids3)
	}

	ids4, _ := res.MethodResponses[3].Args["ids"].([]any)
	if len(ids4) != 0 {
		t.Errorf("updatedBefore mismatch expected [], got %v", ids4)
	}
}
