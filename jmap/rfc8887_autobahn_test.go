package jmap_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"

	"github.com/coder/websocket"
)

// TestRFC8887_Autobahn_BinaryFrameIgnored tests that binary frames sent over WebSocket
// are ignored per RFC 8887 Section 4.3.1 without disrupting the connection.
func TestRFC8887_Autobahn_BinaryFrameIgnored(t *testing.T) {
	spectest.Require(t, "RFC8887", "4.3.1", spectest.MUST,
		"Only text frames are used for JMAP messages over WebSocket; binary frames are ignored.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/jmap/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
		HTTPHeader:   basicAuthHeader(),
	})
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test finished")

	// Send binary frame
	binaryData := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	if err := conn.Write(ctx, websocket.MessageBinary, binaryData); err != nil {
		t.Fatalf("Failed to send binary frame: %v", err)
	}

	// Immediately follow with valid text request
	req := map[string]any{
		"@type": "Request",
		"id":    "req-after-binary",
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c0"},
		},
	}
	reqBytes, _ := json.Marshal(req)
	if err := conn.Write(ctx, websocket.MessageText, reqBytes); err != nil {
		t.Fatalf("Failed to send text frame after binary: %v", err)
	}

	// Read response - must receive Response for req-after-binary, not an error or disruption
	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Failed to read response after binary frame: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(msg, &resp); err != nil {
		t.Fatalf("Invalid response JSON: %v", err)
	}
	if resp["@type"] != "Response" || resp["requestId"] != "req-after-binary" {
		t.Errorf("Unexpected response: %v", resp)
	}
}

// TestRFC8887_Autobahn_PingPong tests ping/pong heartbeat frames (Autobahn Case 9.x).
func TestRFC8887_Autobahn_PingPong(t *testing.T) {
	spectest.Require(t, "RFC8887", "4.1", spectest.MUST,
		"WebSocket transport supports standard WebSocket ping/pong framing.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/jmap/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
		HTTPHeader:   basicAuthHeader(),
	})
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// In coder/websocket, client-side Read must be running to receive control frames (Pong)
	readErrCh := make(chan error, 1)
	go func() {
		for {
			_, _, err := conn.Read(ctx)
			if err != nil {
				readErrCh <- err
				return
			}
		}
	}()

	// Send Ping and verify Pong response within deadline
	pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
	defer pingCancel()
	if err := conn.Ping(pingCtx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

// TestRFC8887_Autobahn_CloseHandshakes tests clean WebSocket close handshakes (Autobahn Case 7.x).
func TestRFC8887_Autobahn_CloseHandshakes(t *testing.T) {
	spectest.Require(t, "RFC8887", "4.1", spectest.MUST,
		"WebSocket connection closes cleanly via standard close frame handshakes.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/jmap/ws"

	closeCodes := []websocket.StatusCode{
		websocket.StatusNormalClosure,
		websocket.StatusGoingAway,
	}

	for _, code := range closeCodes {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
			Subprotocols: []string{"jmap"},
			HTTPHeader:   basicAuthHeader(),
		})
		if err != nil {
			cancel()
			t.Fatalf("Dial with code %v failed: %v", code, err)
		}

		err = conn.Close(code, "closing test")
		if err != nil {
			t.Errorf("Close with code %v returned error: %v", code, err)
		}
		cancel()
	}
}

// TestRFC8887_Autobahn_InvalidJSON tests invalid JSON payload handling (Autobahn Cases 1.x-6.x).
func TestRFC8887_Autobahn_InvalidJSON(t *testing.T) {
	spectest.Require(t, "RFC8887", "4.3.4", spectest.MUST,
		"Malformed JSON messages return a RequestError with type notJSON.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/jmap/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
		HTTPHeader:   basicAuthHeader(),
	})
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Send non-JSON text
	malformed := []byte("{invalid json, not closed")
	if err := conn.Write(ctx, websocket.MessageText, malformed); err != nil {
		t.Fatalf("Failed to send text: %v", err)
	}

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Failed to read error response: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(msg, &resp); err != nil {
		t.Fatalf("Invalid response JSON: %v", err)
	}

	if resp["@type"] != "RequestError" {
		t.Errorf("Expected @type 'RequestError', got %v", resp["@type"])
	}
	if resp["type"] != jmap.ErrorNotJSON {
		t.Errorf("Expected error type %q, got %v", jmap.ErrorNotJSON, resp["type"])
	}
}

// TestRFC8887_Autobahn_PipelinedRequests tests sending multiple sequential requests over a single connection.
func TestRFC8887_Autobahn_PipelinedRequests(t *testing.T) {
	spectest.Require(t, "RFC8887", "4.3.2", spectest.MUST,
		"Multiple JMAP Request/Response cycles can execute over a single persistent WebSocket connection.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/jmap/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
		HTTPHeader:   basicAuthHeader(),
	})
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	for i := 1; i <= 5; i++ {
		reqID := strings.Repeat("req-", 1) + string(rune('0'+i))
		req := map[string]any{
			"@type": "Request",
			"id":    reqID,
			"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			"methodCalls": []any{
				[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c0"},
			},
		}
		reqBytes, _ := json.Marshal(req)
		if err := conn.Write(ctx, websocket.MessageText, reqBytes); err != nil {
			t.Fatalf("Request %d write failed: %v", i, err)
		}

		_, msg, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("Request %d read failed: %v", i, err)
		}

		var resp map[string]any
		if err := json.Unmarshal(msg, &resp); err != nil {
			t.Fatalf("Request %d invalid JSON: %v", i, err)
		}
		if resp["requestId"] != reqID {
			t.Errorf("Expected requestId %q, got %v", reqID, resp["requestId"])
		}
	}
}

// TestRFC8887_Autobahn_MaxSizeRequestLimit tests size limit enforcement on WebSocket messages.
func TestRFC8887_Autobahn_MaxSizeRequestLimit(t *testing.T) {
	spectest.Require(t, "RFC8887", "4.3.4", spectest.MUST,
		"Oversized WebSocket messages exceeding maxSizeRequest return an error limit response.")

	srv := newTestServer()
	srv.Session.Capabilities[jmap.CoreCapabilityURI] = jmap.CoreCapability{
		MaxSizeRequest: 500, // Low limit for test
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/jmap/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
		HTTPHeader:   basicAuthHeader(),
	})
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Send oversized message (> 500 bytes)
	hugePayload := strings.Repeat("x", 600)
	req := map[string]any{
		"@type": "Request",
		"id":    "req-huge",
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/get", map[string]any{"accountId": "primary", "junk": hugePayload}, "c0"},
		},
	}
	reqBytes, _ := json.Marshal(req)

	if err := conn.Write(ctx, websocket.MessageText, reqBytes); err != nil {
		t.Fatalf("Failed to write oversized message: %v", err)
	}

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Failed to read error response: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(msg, &resp); err != nil {
		t.Fatalf("Invalid response JSON: %v", err)
	}

	if resp["@type"] != "RequestError" {
		t.Errorf("Expected @type 'RequestError', got %v", resp["@type"])
	}
	if resp["type"] != jmap.ErrorLimit {
		t.Errorf("Expected type %q, got %v", jmap.ErrorLimit, resp["type"])
	}
	if resp["limit"] != "maxSizeRequest" {
		t.Errorf("Expected limit 'maxSizeRequest', got %v", resp["limit"])
	}
}
