package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestMailboxQuery_HasAnyRole_IsSubscribed tests the Mailbox/query hasAnyRole and isSubscribed
// FilterCondition properties per RFC 8621 Section 2.3.
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
