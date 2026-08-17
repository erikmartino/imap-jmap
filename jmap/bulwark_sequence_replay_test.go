package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/spectest"
)

// TestRFCLess_BulwarkWebmailSequenceReplay tests the exact JMAP request sequence
// issued by Bulwark Webmail upon user login:
// 1. Mailbox/get to retrieve system mailboxes (Inbox ID: "mb-inbox")
// 2. Email/query with filter inMailbox: "mb-inbox" to fetch Inbox message IDs
// 3. Email/get with result reference to fetch message details for those IDs
func TestRFCLess_BulwarkWebmailSequenceReplay(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.5", "MUST", "Email/query returns emails matching the inMailbox filter")
	spectest.Require(t, "RFC8621", "4.2", "MUST", "Email/get returns message details for queried email IDs")

	authBackend := memory.NewMemoryAuthBackend()
	srv := newTestServer(jmap.WithAuthBackend(authBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Authenticate user "bulwark-user@example.com"
	token, err := authBackend.Authenticate(context.Background(), "bulwark-user@example.com", "bulwark-user@example.com")
	if err != nil {
		t.Fatalf("Failed to authenticate user: %v", err)
	}

	usingAll := []string{
		jmap.CoreCapabilityURI,
		jmap.MailCapabilityURI,
	}

	// 1. Bulwark Step 1: Mailbox/get
	res1 := postJMAPWithToken(t, ts.URL, token, usingAll, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "m0"},
	})
	if len(res1.MethodResponses) != 1 || res1.MethodResponses[0].Name != "Mailbox/get" {
		t.Fatalf("Expected Mailbox/get response, got %#v", res1)
	}

	mList, _ := res1.MethodResponses[0].Args["list"].([]any)
	var inboxID string
	for _, item := range mList {
		if mbMap, ok := item.(map[string]any); ok {
			if mbMap["role"] == "inbox" {
				inboxID, _ = mbMap["id"].(string)
				break
			}
		}
	}
	if inboxID == "" {
		t.Fatalf("Expected Inbox mailbox with role 'inbox' to be returned")
	}

	// 2. Bulwark Step 2 & 3 in single batch call:
	// Email/query with filter {"inMailbox": inboxID} followed by Email/get using Result Reference "#c1"
	res2 := postJMAPWithToken(t, ts.URL, token, usingAll, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": inboxID},
			"sort":      []any{map[string]any{"property": "receivedAt", "isAscending": false}},
		}, "c1"},
		[]any{"Email/get", map[string]any{
			"accountId": "primary",
			"#ids": map[string]any{
				"resultOf": "c1",
				"name":     "Email/query",
				"path":     "/ids",
			},
			"properties": []any{"id", "threadId", "mailboxIds", "subject", "from", "to", "receivedAt", "preview", "keywords", "hasAttachment"},
		}, "c2"},
	})

	if len(res2.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses in batch, got %d", len(res2.MethodResponses))
	}

	// Assert Email/query returned non-empty IDs
	queryResp := res2.MethodResponses[0]
	if queryResp.Name != "Email/query" {
		t.Fatalf("Expected Email/query response, got %s", queryResp.Name)
	}
	ids, _ := queryResp.Args["ids"].([]any)
	if len(ids) == 0 {
		t.Errorf("Email/query for Inbox emails returned 0 IDs; expected seeded messages")
	}

	// Assert Email/get returned email list matching the queried IDs
	getResp := res2.MethodResponses[1]
	if getResp.Name != "Email/get" {
		t.Fatalf("Expected Email/get response, got %s", getResp.Name)
	}
	emailList, _ := getResp.Args["list"].([]any)
	if len(emailList) != len(ids) {
		t.Errorf("Expected %d emails in Email/get list, got %d", len(ids), len(emailList))
	}
}
