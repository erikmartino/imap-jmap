package jmap_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC8621_SMTPReceiveToJMAPPushIntegration verifies that sending an email over SMTP
// triggers an RFC 8620 SSE StateChange push notification over JMAP /eventsource
// and makes the email queryable via JMAP Email/get per RFC 8621.
func TestRFC8621_SMTPReceiveToJMAPPushIntegration(t *testing.T) {
	// 1. Initialize backends & JMAP Server
	server := newTestServer()

	// 2. Start JMAP HTTP Server (httptest)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// 3. Start SMTP Receiver Server on dynamic loopback port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen on random TCP port: %v", err)
	}
	smtpAddr := listener.Addr().String()
	_ = listener.Close()

	smtpServer := jmapsmtp.NewServer(smtpAddr, server.MailBackend, server.BlobBackend, nil)
	go func() {
		_ = smtpServer.ListenAndServe()
	}()
	defer smtpServer.Close()

	// Give servers a moment to bind and finish initial account seeding
	time.Sleep(200 * time.Millisecond)

	// 4. Connect SSE Push Client to GET /eventsource
	sseURL := ts.URL + "/eventsource?types=Email,Mailbox&closeafter=state"
	req := authedRequest(t, "GET", sseURL, nil)

	sseResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /eventsource failed: %v", err)
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected SSE HTTP status 200, got %d", sseResp.StatusCode)
	}

	// 5. Send an email via SMTP in background
	sentSubject := "Live SMTP Push Update Test"
	smtpDone := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		from := "push-sender@example.com"
		to := []string{"user@example.com"}
		msg := []byte("From: Push Sender <push-sender@example.com>\r\n" +
			"To: User Example <user@example.com>\r\n" +
			"Subject: " + sentSubject + "\r\n" +
			"Message-ID: <smtp-push-1@example.com>\r\n" +
			"\r\n" +
			"Testing real-time JMAP SSE push delivery upon SMTP intake.")

		smtpDone <- smtp.SendMail(smtpAddr, nil, from, to, msg)
	}()

	// 6. Read SSE Push Stream and verify StateChange event received
	scanner := bufio.NewScanner(sseResp.Body)
	var eventLine, dataLine string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventLine = line
		} else if strings.HasPrefix(line, "data:") {
			dataLine = line
			if strings.Contains(dataLine, "StateChange") && strings.Contains(dataLine, "Email") {
				break
			}
		}
	}

	if err := <-smtpDone; err != nil {
		t.Fatalf("smtp.SendMail failed: %v", err)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("Scanner error reading SSE stream: %v", err)
	}

	if eventLine != "event: state" {
		t.Fatalf("Expected SSE event line 'event: state', got %q", eventLine)
	}

	if !strings.Contains(dataLine, "StateChange") || !strings.Contains(dataLine, "Email") {
		t.Fatalf("Expected StateChange event payload containing 'Email', got %q", dataLine)
	}

	// Parse StateChange JSON payload
	var stateChange jmap.StateChange
	rawJSON := strings.TrimPrefix(dataLine, "data: ")
	if err := json.Unmarshal([]byte(rawJSON), &stateChange); err != nil {
		t.Fatalf("Failed to parse StateChange JSON: %v", err)
	}

	if stateChange.Type != "StateChange" {
		t.Errorf("Expected StateChange type 'StateChange', got %q", stateChange.Type)
	}

	// 7. Verify email can be fetched via JMAP HTTP Email/get endpoint
	jmapReqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/get", map[string]any{
				"accountId": "primary",
			}, "c1"},
		},
	}
	reqBytes, _ := json.Marshal(jmapReqBody)

	deadline := time.Now().Add(2 * time.Second)
	found := false
	for {
		resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(reqBytes))
		if err == nil {
			var jmapResp struct {
				MethodResponses []any `json:"methodResponses"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err == nil && len(jmapResp.MethodResponses) > 0 {
				methodCall := jmapResp.MethodResponses[0].([]any)
				if methodCall[0] == "Email/get" {
					respArgs := methodCall[1].(map[string]any)
					list, _ := respArgs["list"].([]any)
					for _, rawItem := range list {
						item := rawItem.(map[string]any)
						if item["subject"] == sentSubject {
							found = true
							if item["blobId"] == "" {
								t.Errorf("Email missing blobId")
							}
							break
						}
					}
				}
			}
			resp.Body.Close()
		}
		if found || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !found {
		t.Errorf("Email with subject %q not found in JMAP Email/get list", sentSubject)
	}
}
