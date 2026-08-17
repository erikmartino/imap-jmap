package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8621_Section4_5_EmailFilterPropertiesPosNeg verifies positive and negative filter matching for Email properties per RFC 8621 Section 4.5.
func TestRFC8621_Section4_5_EmailFilterPropertiesPosNeg(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := seedCtx()

	// Seed email with CC and BCC
	e1, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Special Meeting Notice",
		From:       []jmap.EmailAddress{{Name: "Manager", Email: "manager@example.com"}},
		To:         []jmap.EmailAddress{{Name: "Team", Email: "team@example.com"}},
		CC:         []jmap.EmailAddress{{Name: "Audit", Email: "audit@example.com"}},
		BCC:        []jmap.EmailAddress{{Name: "Secret", Email: "secret@example.com"}},
		Keywords:   map[string]bool{"$draft": true},
	})
	e2, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Regular Discussion",
		From:       []jmap.EmailAddress{{Name: "User", Email: "user@example.com"}},
		To:         []jmap.EmailAddress{{Name: "Team", Email: "team@example.com"}},
		Keywords:   map[string]bool{"$seen": true},
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Positive filter by CC: "audit@example.com" -> returns e1
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"cc": "audit@example.com"},
		}, "c1"},
	})
	ids1, _ := res1.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 || ids1[0] != string(e1.ID) {
		t.Errorf("CC filter positive expected [%s], got %v", e1.ID, ids1)
	}

	// 2. Negative filter by CC: "non-existent@example.com" -> returns empty []
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"cc": "non-existent@example.com"},
		}, "c2"},
	})
	ids2, _ := res2.MethodResponses[0].Args["ids"].([]any)
	if len(ids2) != 0 {
		t.Errorf("CC filter negative expected empty [], got %v", ids2)
	}

	// 3. Positive filter by BCC: "secret@example.com" -> returns e1
	res3 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"bcc": "secret@example.com"},
		}, "c3"},
	})
	ids3, _ := res3.MethodResponses[0].Args["ids"].([]any)
	if len(ids3) != 1 || ids3[0] != string(e1.ID) {
		t.Errorf("BCC filter positive expected [%s], got %v", e1.ID, ids3)
	}

	// 4. Positive filter by notKeyword "$seen" -> returns e1 (since e2 has $seen)
	res4 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"notKeyword": "$seen", "subject": "Special Meeting Notice"},
		}, "c4"},
	})
	ids4, _ := res4.MethodResponses[0].Args["ids"].([]any)
	if len(ids4) != 1 || ids4[0] != string(e1.ID) {
		t.Errorf("notKeyword filter positive expected [%s], got %v", e1.ID, ids4)
	}

	_ = e2
}
