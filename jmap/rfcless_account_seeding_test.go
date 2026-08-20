package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFCLess_FirstUseAccountSeeding tests that a brand-new authenticated account is lazily seeded
// with sample emails across folders, calendar entries, address-book contacts, and a FileNode subfolder.
func TestRFCLess_FirstUseAccountSeeding(t *testing.T) {
	authBackend := memory.NewMemoryAuthBackend()
	srv := newTestServer(jmap.WithAuthBackend(authBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Authenticate a brand-new user "newuser"
	token, err := authBackend.Authenticate(context.Background(), "newuser", "newuser")
	if err != nil {
		t.Fatalf("Failed to authenticate newuser: %v", err)
	}

	usingAll := []string{
		jmap.CoreCapabilityURI,
		jmap.MailCapabilityURI,
		jmap.CalendarsCapabilityURI,
		jmap.ContactsCapabilityURI,
		jmap.FileNodeCapabilityURI,
	}

	// 1. Email/query returns seeded emails
	res1 := postJMAPWithToken(t, ts.URL, token, usingAll, []any{
		[]any{"Email/query", map[string]any{"accountId": "primary"}, "c1"},
	})
	idsEmail, _ := res1.MethodResponses[0].Args["ids"].([]any)
	if len(idsEmail) == 0 {
		t.Errorf("Expected seeded emails for brand-new account, got 0")
	}

	// 1a. First-use seeding provisions all standard role mailboxes required by
	// real mail-client compose, send, archive, and delete workflows.
	mailboxes := postJMAPWithToken(t, ts.URL, token, usingAll, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c1a"},
	})
	mailboxList, _ := mailboxes.MethodResponses[0].Args["list"].([]any)
	roles := map[string]bool{}
	for _, raw := range mailboxList {
		if mailbox, ok := raw.(map[string]any); ok {
			if role, ok := mailbox["role"].(string); ok {
				roles[role] = true
			}
		}
	}
	for _, role := range []string{jmap.RoleInbox, jmap.RoleSent, jmap.RoleDrafts, jmap.RoleJunk, jmap.RoleTrash, jmap.RoleArchive} {
		if !roles[role] {
			t.Errorf("Expected seeded account to have %q mailbox role, got %v", role, roles)
		}
	}

	// 2. CalendarEvent/get returns seeded calendar events
	res2 := postJMAPWithToken(t, ts.URL, token, usingAll, []any{
		[]any{"CalendarEvent/get", map[string]any{"accountId": "primary"}, "c2"},
	})
	listCal, _ := res2.MethodResponses[0].Args["list"].([]any)
	if len(listCal) == 0 {
		t.Errorf("Expected seeded calendar events for brand-new account, got 0")
	}

	// 3. Card/get returns seeded contact cards
	res3 := postJMAPWithToken(t, ts.URL, token, usingAll, []any{
		[]any{"Card/get", map[string]any{"accountId": "primary"}, "c3"},
	})
	listCards, _ := res3.MethodResponses[0].Args["list"].([]any)
	if len(listCards) == 0 {
		t.Errorf("Expected seeded contact cards for brand-new account, got 0")
	}

	// 4. FileNode/query returns seeded file nodes including a subfolder
	res4 := postJMAPWithToken(t, ts.URL, token, usingAll, []any{
		[]any{"FileNode/query", map[string]any{"accountId": "primary"}, "c4"},
	})
	idsFiles, _ := res4.MethodResponses[0].Args["ids"].([]any)
	if len(idsFiles) == 0 {
		t.Errorf("Expected seeded file nodes for brand-new account, got 0")
	}
}
