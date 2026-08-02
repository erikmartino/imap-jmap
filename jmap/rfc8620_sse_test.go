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
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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

	if eventLine != "event: state" {
		t.Errorf("Expected 'event: state', got %q", eventLine)
	}

	if !strings.Contains(dataLine, "StateChange") || !strings.Contains(dataLine, "Email") {
		t.Errorf("Expected StateChange data payload, got %q", dataLine)
	}
}
