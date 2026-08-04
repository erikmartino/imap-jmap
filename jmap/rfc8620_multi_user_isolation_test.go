package jmap_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC8620_Section3_6_MultiUserIsolation tests that different authenticated users have completely isolated data in memory backends per RFC 8620.
func TestRFC8620_Section3_6_MultiUserIsolation(t *testing.T) {
	authBackend := memory.NewMemoryAuthBackend()
	srv := newTestServer(jmap.WithAuthBackend(authBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Authenticate User A ("userA") and User B ("userB")
	tokenA, errA := authBackend.Authenticate(context.Background(), "userA", "userA")
	if errA != nil {
		t.Fatalf("Failed to authenticate userA: %v", errA)
	}

	tokenB, errB := authBackend.Authenticate(context.Background(), "userB", "userB")
	if errB != nil {
		t.Fatalf("Failed to authenticate userB: %v", errB)
	}

	usingMail := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. User A creates a private mailbox "UserA Private Folder"
	resA := postJMAPWithToken(t, ts.URL, tokenA, usingMail, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mbA": map[string]any{
					"name": "UserA Private Folder",
				},
			},
		}, "cA1"},
	})
	createdA, _ := resA.MethodResponses[0].Args["created"].(map[string]any)
	mbAObj, ok := createdA["mbA"].(map[string]any)
	if !ok {
		t.Fatalf("User A failed to create mailbox: %v", resA.MethodResponses[0].Args)
	}
	mbAID, _ := mbAObj["id"].(string)

	// 2. User B queries mailboxes -> "UserA Private Folder" MUST NOT be returned for User B
	resB1 := postJMAPWithToken(t, ts.URL, tokenB, usingMail, []any{
		[]any{"Mailbox/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{mbAID},
		}, "cB1"},
	})
	notFoundB, _ := resB1.MethodResponses[0].Args["notFound"].([]any)
	foundMissing := false
	for _, id := range notFoundB {
		if id == mbAID {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("Security/Isolation breach! User B should get %q in notFound, got args: %v", mbAID, resB1.MethodResponses[0].Args)
	}

	// 3. User B attempts to delete User A's mailbox -> MUST fail with notFound in notDestroyed
	resB2 := postJMAPWithToken(t, ts.URL, tokenB, usingMail, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{mbAID},
		}, "cB2"},
	})
	notDestroyedB, _ := resB2.MethodResponses[0].Args["notDestroyed"].(map[string]any)
	errObjB, ok := notDestroyedB[mbAID].(map[string]any)
	if !ok {
		t.Fatalf("Security/Isolation breach! User B destroy of User A's mailbox should fail with notDestroyed")
	}
	errTypeB, _ := errObjB["type"].(string)
	if errTypeB != "notFound" {
		t.Errorf("Expected notFound type for cross-user destroy attempt, got %q", errTypeB)
	}

	// 4. Verify User A can still fetch their private mailbox
	resA2 := postJMAPWithToken(t, ts.URL, tokenA, usingMail, []any{
		[]any{"Mailbox/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{mbAID},
		}, "cA2"},
	})
	listA, _ := resA2.MethodResponses[0].Args["list"].([]any)
	if len(listA) != 1 {
		t.Errorf("User A expected 1 mailbox, got %d", len(listA))
	}
}

func postJMAPWithToken(t *testing.T, url, token string, using []string, methodCalls []any) jmap.Response {
	reqBody := map[string]any{
		"using":       using,
		"methodCalls": methodCalls,
	}
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req, err := http.NewRequest("POST", url+"/jmap", strings.NewReader(string(reqJSON)))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected HTTP 200 OK, got %d", resp.StatusCode)
	}

	var jmapRes jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapRes); err != nil {
		t.Fatalf("Failed to decode JMAP response: %v", err)
	}
	return jmapRes
}
