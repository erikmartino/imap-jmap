package jmap_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
)

// TestRFC8620_Section7_1_EventSourceConnection tests GET /eventsource streaming connection per RFC 8620 Section 7.1.
func TestRFC8620_Section7_1_EventSourceConnection(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/eventsource?ping=1", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /eventsource failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 OK, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Errorf("Expected Content-Type text/event-stream, got %q", contentType)
	}
}

// TestRFC8620_Section7_1_StateChangeBroadcast tests StateChange event broadcast on data changes per RFC 8620 Section 7.1.
func TestRFC8620_Section7_1_StateChangeBroadcast(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Connect SSE listener
	req, err := http.NewRequest("GET", ts.URL+"/eventsource?types=Email&closeafter=state", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /eventsource failed: %v", err)
	}
	defer resp.Body.Close()

	// 2. Trigger state change via Email/set in background
	go func() {
		time.Sleep(100 * time.Millisecond)
		reqPayload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			"methodCalls": []any{
				[]any{"Email/set", map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"e1": map[string]any{
							"subject": "SSE Notification Test",
						},
					},
				}, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		_, _ = http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	}()

	// 3. Read SSE stream
	scanner := bufio.NewScanner(resp.Body)
	var eventLine, dataLine string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventLine = line
		} else if strings.HasPrefix(line, "data:") {
			dataLine = line
			break
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error reading SSE stream: %v", err)
	}

	if eventLine != "event: state" {
		t.Errorf("Expected 'event: state', got %q", eventLine)
	}

	if !strings.Contains(dataLine, "StateChange") || !strings.Contains(dataLine, "Email") {
		t.Errorf("Expected StateChange data payload, got %q", dataLine)
	}
}

// TestRFC8620_Section7_1_PushTokenMatchesChangesState verifies that the state token delivered
// in a push StateChange is exactly the token a subsequent */changes call returns as newState,
// so a client can reconcile without gaps (RFC 8620 Section 7.1).
func TestRFC8620_Section7_1_PushTokenMatchesChangesState(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	getState := func(method string) string {
		reqPayload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			"methodCalls": []any{
				[]any{method, map[string]any{"accountId": "primary", "ids": []string{}}, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
			t.Fatalf("Failed to decode Response: %v", err)
		}
		state, _ := jr.MethodResponses[0].Args["state"].(string)
		return state
	}

	oldEmailState := getState("Email/get")
	oldMailboxState := getState("Mailbox/get")

	// Listen for push events, then create an email in the background.
	req, err := http.NewRequest("GET", ts.URL+"/eventsource", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /eventsource failed: %v", err)
	}
	defer resp.Body.Close()

	go func() {
		time.Sleep(100 * time.Millisecond)
		reqPayload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			"methodCalls": []any{
				[]any{"Email/set", map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"e1": map[string]any{
							"subject":    "Push Token Consistency",
							"mailboxIds": map[string]any{"mb-inbox": true},
						},
					},
				}, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		_, _ = http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	}()

	pushed := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var sc jmap.StateChange
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data:")), &sc); err != nil {
			continue
		}
		for typeName, token := range sc.Changed["primary"] {
			if _, seen := pushed[typeName]; !seen {
				pushed[typeName] = token
			}
		}
		if _, ok := pushed["Email"]; ok {
			if _, ok := pushed["Mailbox"]; ok {
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error reading SSE stream: %v", err)
	}

	pushedEmail, okEmail := pushed["Email"]
	pushedMailbox, okMailbox := pushed["Mailbox"]
	if !okEmail {
		t.Fatalf("Expected pushed Email state token, got %v", pushed)
	}
	if !okMailbox {
		t.Fatalf("Expected pushed Mailbox state token, got %v", pushed)
	}

	// Email/changes since the pre-mutation state must return the pushed token as newState.
	changes := func(method, sinceState string) (string, []string, []string) {
		reqPayload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			"methodCalls": []any{
				[]any{method, map[string]any{"accountId": "primary", "sinceState": sinceState}, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
			t.Fatalf("Failed to decode Response: %v", err)
		}
		args := jr.MethodResponses[0].Args
		newState, _ := args["newState"].(string)
		collect := func(key string) []string {
			raw, _ := args[key].([]any)
			out := make([]string, 0, len(raw))
			for _, v := range raw {
				out = append(out, v.(string))
			}
			return out
		}
		return newState, collect("created"), collect("updated")
	}

	newEmailState, createdEmails, _ := changes("Email/changes", oldEmailState)
	if newEmailState != pushedEmail {
		t.Errorf("Email/changes newState %q != pushed token %q", newEmailState, pushedEmail)
	}
	if len(createdEmails) != 1 {
		t.Errorf("Expected 1 created email in Email/changes, got %v", createdEmails)
	}

	newMailboxState, _, updatedMailboxes := changes("Mailbox/changes", oldMailboxState)
	if newMailboxState != pushedMailbox {
		t.Errorf("Mailbox/changes newState %q != pushed token %q", newMailboxState, pushedMailbox)
	}
	found := false
	for _, mbID := range updatedMailboxes {
		if mbID == "mb-inbox" {
			found = true
		}
	}
	if !found {
		t.Errorf("Expected mb-inbox in Mailbox/changes updated, got %v", updatedMailboxes)
	}
}
