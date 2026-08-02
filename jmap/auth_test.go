package jmap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// newAuthTestServer creates a test server with MemoryAuthBackend wired in.
func newAuthTestServer() (*jmap.Server, *memory.MemoryAuthBackend) {
	authBackend := memory.NewMemoryAuthBackend()
	srv := newTestServer(jmap.WithAuthBackend(authBackend))
	return srv, authBackend
}

// loginAndGetToken is a test helper that performs a login and returns the Bearer token.
func loginAndGetToken(t *testing.T, ts *httptest.Server, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := http.Post(ts.URL+"/jmap/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap/login failed: %v", err)
	}
	defer resp.Body.Close()
	var result map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result["token"]
}

// TestAuth_Login_Success tests that matching username/password returns a Bearer token.
func TestAuth_Login_Success(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "alice"})
	resp, err := http.Post(ts.URL+"/jmap/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap/login failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&result)

	if result["token"] == "" {
		t.Error("Expected a token in the response")
	}
	if result["accountId"] != "alice" {
		t.Errorf("Expected accountId 'alice', got %q", result["accountId"])
	}
}

// TestAuth_Login_WrongPassword tests that mismatched credentials return 401.
func TestAuth_Login_WrongPassword(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "wrong"})
	resp, err := http.Post(ts.URL+"/jmap/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap/login failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}
}

// TestAuth_Login_EmptyUsername tests that empty username is rejected.
func TestAuth_Login_EmptyUsername(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"username": "", "password": ""})
	resp, err := http.Post(ts.URL+"/jmap/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap/login failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}
}

// TestAuth_ProtectedEndpoint_NoToken tests that /.well-known/jmap returns 401 without a token.
func TestAuth_ProtectedEndpoint_NoToken(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}

	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Error("Expected WWW-Authenticate header to be set on 401")
	}
}

// TestAuth_BasicAuth_Valid tests that a valid HTTP Basic username/password grants access.
func TestAuth_BasicAuth_Valid(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	req.SetBasicAuth("alice", "alice")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Errorf("Failed to decode session: %v", err)
	}
}

// TestAuth_BasicAuth_InvalidPassword tests that a Basic auth with wrong password returns 401.
func TestAuth_BasicAuth_InvalidPassword(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	req.SetBasicAuth("alice", "wrong")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}
}

// TestAuth_ProtectedEndpoint_ValidToken tests that a valid Bearer token grants access.
func TestAuth_ProtectedEndpoint_ValidToken(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	token := loginAndGetToken(t, ts, "alice", "alice")
	if token == "" {
		t.Fatal("Login failed, no token returned")
	}

	req, _ := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Errorf("Failed to decode session: %v", err)
	}
}

// TestAuth_ProtectedEndpoint_InvalidToken tests that an invalid Bearer token returns 401.
func TestAuth_ProtectedEndpoint_InvalidToken(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-xyz")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}
}

// TestAuth_AccessToken_QueryParam tests that ?access_token= is accepted (RFC 6750 Section 2.3).
// This is required for browser EventSource which cannot set Authorization headers.
func TestAuth_AccessToken_QueryParam(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	token := loginAndGetToken(t, ts, "bob", "bob")

	// Use query param instead of Authorization header.
	resp, err := http.Get(ts.URL + "/.well-known/jmap?access_token=" + token)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 with ?access_token, got %d", resp.StatusCode)
	}
}

// TestAuth_CORS_Preflight tests that OPTIONS preflight passes through without auth.
func TestAuth_CORS_Preflight(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("OPTIONS", ts.URL+"/jmap", nil)
	req.Header.Set("Origin", "https://mail.profundo.dk")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		t.Error("CORS preflight OPTIONS should not require authentication")
	}
}

// TestAuth_JMAP_API_WithToken tests POST /jmap with valid token processes method calls.
func TestAuth_JMAP_API_WithToken(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	token := loginAndGetToken(t, ts, "charlie", "charlie")

	reqBody, _ := json.Marshal(map[string]any{
		"using":       []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c0"}},
	})

	req, _ := http.NewRequest("POST", ts.URL+"/jmap", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

// TestAuth_WebSocket_AccessToken tests WebSocket upgrade with ?access_token= query param.
func TestAuth_WebSocket_AccessToken(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	token := loginAndGetToken(t, ts, "dave", "dave")

	wsURL := "ws" + ts.URL[4:] + "/jmap/ws?access_token=" + token

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
	})
	if err != nil {
		t.Fatalf("WebSocket dial with access_token failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a simple request to confirm the connection is functional.
	req := map[string]any{
		"@type":       "Request",
		"id":          "auth-ws-1",
		"using":       []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{},
	}
	reqBytes, _ := json.Marshal(req)
	if err := conn.Write(ctx, websocket.MessageText, reqBytes); err != nil {
		t.Fatalf("Failed to write WebSocket message: %v", err)
	}

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Failed to read WebSocket response: %v", err)
	}

	var resp map[string]any
	_ = json.Unmarshal(msg, &resp)
	if resp["@type"] != "Response" {
		t.Errorf("Expected @type 'Response', got %v", resp["@type"])
	}
}

// TestAuth_WebSocket_NoToken tests WebSocket upgrade is rejected without a token.
func TestAuth_WebSocket_NoToken(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/jmap/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
	})
	if err == nil {
		t.Error("Expected WebSocket dial to fail without auth token")
		return
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", resp.StatusCode)
	}
}
