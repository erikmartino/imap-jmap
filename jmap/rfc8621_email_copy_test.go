package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8621_Section4_6_EmailCopyRoundTrip tests Email/copy round-trip per RFC 8621 Section 4.6.
func TestRFC8621_Section4_6_EmailCopyRoundTrip(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed source email
	srcEM, err := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Original Email for Copy",
		Keywords:   map[string]bool{"$seen": true},
	})
	if err != nil {
		t.Fatalf("Failed to seed source email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	srcAccount := jmap.AccountIDForSubject(testUsername)
	destAccount := jmap.AccountIDForSubject(testUsername + "-secondary")

	// 1. Copy source email to a new destination mailbox with keywords override
	calls1 := []any{
		[]any{"Email/copy", map[string]any{
			"fromAccountId": srcAccount,
			"accountId":     destAccount,
			"create": map[string]any{
				"copy1": map[string]any{
					"id":         string(srcEM.ID),
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"keywords":   map[string]bool{"$flagged": true},
				},
			},
		}, "c1"},
	}

	res1 := postJMAP(t, ts.URL, using, calls1)
	if len(res1.MethodResponses) == 0 {
		t.Fatalf("Empty response for Email/copy")
	}
	created, _ := res1.MethodResponses[0].Args["created"].(map[string]any)
	copyObj, ok := created["copy1"].(map[string]any)
	if !ok {
		t.Fatalf("Failed to copy Email: %v", res1.MethodResponses[0].Args)
	}
	newID, _ := copyObj["id"].(string)
	if newID == "" || newID == string(srcEM.ID) {
		t.Errorf("Copied Email ID should be new and distinct from source ID %s, got %q", srcEM.ID, newID)
	}

	// 2. Email/copy missing source ID -> notCreated with type notFound
	calls2 := []any{
		[]any{"Email/copy", map[string]any{
			"fromAccountId": srcAccount,
			"accountId":     destAccount,
			"create": map[string]any{
				"copyBad": map[string]any{
					"id":         "non-existent-source-id-999",
					"mailboxIds": map[string]bool{"mb-inbox": true},
				},
			},
		}, "c2"},
	}
	res2 := postJMAP(t, ts.URL, using, calls2)
	notCreated, _ := res2.MethodResponses[0].Args["notCreated"].(map[string]any)
	errObj, ok := notCreated["copyBad"].(map[string]any)
	if !ok {
		t.Fatalf("Expected copyBad in notCreated")
	}
	errType, _ := errObj["type"].(string)
	if errType != "notFound" {
		t.Errorf("Expected notFound in notCreated for missing source, got %q", errType)
	}
}
