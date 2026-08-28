package imapsmtp_test

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/smtp"
	"testing"
	"time"

	"github.com/coder/websocket"
	"imap-jmap/jmap"
)

func TestLive_WebSocketPushOverIMAPIdle(t *testing.T) {
	// 1. Establish WebSocket connection to running imap-jmap server on port 8443
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("a@example.com:a@example.com"))
	opts := &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
		HTTPHeader: http.Header{
			"Authorization": []string{authHeader},
		},
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}

	wsURL := "wss://127.0.0.1:8443/jmap/ws"
	conn, resp, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Skipf("Live server not reachable at %s: %v", wsURL, err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected status 101 Switching Protocols, got %d", resp.StatusCode)
	}

	// 2. Enable push on this WebSocket
	enableMsg := map[string]any{
		"@type":     "WebSocketPushEnable",
		"dataTypes": []string{"Email", "Mailbox", "Thread"},
	}
	enableBytes, _ := json.Marshal(enableMsg)
	if err := conn.Write(ctx, websocket.MessageText, enableBytes); err != nil {
		t.Fatalf("failed to send WebSocketPushEnable: %v", err)
	}

	// Allow IDLE connection to settle
	time.Sleep(500 * time.Millisecond)

	// 3. Send email from external tool via SMTP on port 1025
	smtpAddr := "127.0.0.1:1025"
	if _, err := net.DialTimeout("tcp", smtpAddr, 500*time.Millisecond); err != nil {
		smtpAddr = "127.0.0.1:25"
		if _, err := net.DialTimeout("tcp", smtpAddr, 500*time.Millisecond); err != nil {
			t.Skipf("SMTP server not reachable at 1025 or 25: %v", err)
		}
	}

	c, err := smtp.Dial(smtpAddr)
	if err != nil {
		t.Fatalf("failed to dial SMTP server %s: %v", smtpAddr, err)
	}
	defer c.Close()

	if err := c.Mail("sender@external-corp.com"); err != nil {
		t.Fatalf("SMTP MAIL FROM failed: %v", err)
	}
	if err := c.Rcpt("a@example.com"); err != nil {
		t.Fatalf("SMTP RCPT TO failed: %v", err)
	}
	w, err := c.Data()
	if err != nil {
		t.Fatalf("SMTP DATA failed: %v", err)
	}

	msg := "From: <sender@external-corp.com>\r\n" +
		"To: <a@example.com>\r\n" +
		"Subject: Push Verification " + time.Now().Format(time.RFC3339) + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"Hello from external SMTP sender! Push event should arrive over WebSocket.\r\n"

	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatalf("failed to write message body: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close data writer: %v", err)
	}
	_ = c.Quit()

	// 4. Wait to receive StateChange push over WebSocket
	receivedPush := false
	accountID := jmap.AccountIDForSubject("a@example.com")

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if msgType != websocket.MessageText {
				continue
			}

			var sc jmap.StateChange
			if err := json.Unmarshal(data, &sc); err == nil && sc.Type == "StateChange" {
				if sc.Changed != nil && sc.Changed[accountID] != nil && sc.Changed[accountID]["Email"] != "" {
					receivedPush = true
					return
				}
			}
		}
	}()

	select {
	case <-readDone:
		if !receivedPush {
			t.Fatalf("WebSocket connection closed without receiving StateChange")
		}
	case <-time.After(6 * time.Second):
		t.Fatalf("timed out waiting for StateChange push over WebSocket after SMTP delivery")
	}
}
