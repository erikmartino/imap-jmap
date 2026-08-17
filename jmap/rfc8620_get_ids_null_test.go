package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestGetMethods_IdsNullAllRecords verifies that Foo/get with ids:null returns all records
// per RFC 8620 Section 5.1 ("If null, then all records of the data type are returned"),
// covering Thread/get, EmailSubmission/get, and Blob/get.
func TestGetMethods_IdsNullAllRecords(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed items
	em1, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Item 1",
	})
	_, _ = srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Item 2",
	})
	_, _ = srv.MailBackend.CreateSubmission(seedCtx(), &jmap.EmailSubmission{
		EmailID:    em1.ID,
		IdentityID: "id1",
	})
	_, _ = srv.BlobBackend.PutBlob(context.Background(), jmap.AccountIDForSubject(testUsername), "text/plain", []byte("hello blob"))

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
