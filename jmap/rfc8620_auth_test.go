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

// TestRFC8620_Auth_ProtectedEndpoint_NoToken tests that /.well-known/jmap returns 401 without a token.
func TestRFC8620_Auth_ProtectedEndpoint_NoToken(t *testing.T) {
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

// TestRFC8620_Auth_BasicAuth_Valid tests that a valid HTTP Basic username/password grants access.
func TestRFC8620_Auth_BasicAuth_Valid(t *testing.T) {
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

// TestRFC8620_Auth_BasicAuth_InvalidPassword tests that a Basic auth with wrong password returns 401.
func TestRFC8620_Auth_BasicAuth_InvalidPassword(t *testing.T) {
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

// TestRFC8620_Auth_ProtectedEndpoint_ValidToken tests that a valid Bearer token grants access.
func TestRFC8620_Auth_ProtectedEndpoint_ValidToken(t *testing.T) {
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

// TestRFC8620_Auth_ProtectedEndpoint_InvalidToken tests that an invalid Bearer token returns 401.
func TestRFC8620_Auth_ProtectedEndpoint_InvalidToken(t *testing.T) {
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

// TestRFC8620_Auth_AccessToken_QueryParam tests that ?access_token= is accepted (RFC 6750 Section 2.3).
// This is required for browser EventSource which cannot set Authorization headers.
func TestRFC8620_Auth_AccessToken_QueryParam(t *testing.T) {
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

// TestRFC8620_Auth_CORS_Preflight tests that OPTIONS preflight passes through without auth.
func TestRFC8620_Auth_CORS_Preflight(t *testing.T) {
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

// TestRFC8620_Auth_JMAP_API_WithToken tests POST /jmap with valid token processes method calls.
func TestRFC8620_Auth_JMAP_API_WithToken(t *testing.T) {
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

// TestRFC8620_Auth_WebSocket_AccessToken tests WebSocket upgrade with ?access_token= query param.
func TestRFC8620_Auth_WebSocket_AccessToken(t *testing.T) {
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

// TestRFC8620_Auth_WebSocket_NoToken tests WebSocket upgrade is rejected without a token.
func TestRFC8620_Auth_WebSocket_NoToken(t *testing.T) {
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

// TestAccountIDForSubject tests that AccountIDForSubject is stable, URL-safe, and deterministic.
func TestAccountIDForSubject(t *testing.T) {
	cases := []string{
		"alice",
		"user@example.com",
		"subject+with-special/chars?=&",
	}
	for _, subject := range cases {
		id := jmap.AccountIDForSubject(subject)
		if id == "" {
			t.Errorf("Expected non-empty account ID for subject %q", subject)
		}
		if id2 := jmap.AccountIDForSubject(subject); id2 != id {
			t.Errorf("AccountIDForSubject not deterministic for %q: %q vs %q", subject, id, id2)
		}
	}
	if jmap.AccountIDForSubject("alice") == jmap.AccountIDForSubject("bob") {
		t.Error("Different subjects should yield different account IDs")
	}
}

// TestRFC8620_Auth_DerivedAccountID tests that MemoryAuthBackend returns derived accountIDs for subjects.
func TestRFC8620_Auth_DerivedAccountID(t *testing.T) {
	auth := memory.NewMemoryAuthBackend()
	ctx := context.Background()

	idAlice, err := auth.ValidateCredentials(ctx, "alice", "alice")
	if err != nil {
		t.Fatalf("ValidateCredentials failed: %v", err)
	}
	if idAlice != jmap.AccountIDForSubject("alice") {
		t.Errorf("Expected accountID %q, got %q", jmap.AccountIDForSubject("alice"), idAlice)
	}

	idBob, err := auth.ValidateCredentials(ctx, "bob", "bob")
	if err != nil {
		t.Fatalf("ValidateCredentials failed: %v", err)
	}
	if idBob != jmap.AccountIDForSubject("bob") {
		t.Errorf("Expected accountID %q, got %q", jmap.AccountIDForSubject("bob"), idBob)
	}

	if idAlice == idBob {
		t.Errorf("Distinct subjects must yield distinct accountIDs")
	}

	token, err := auth.Authenticate(ctx, "alice", "alice")
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	validatedID, err := auth.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if validatedID != idAlice {
		t.Errorf("Expected validated accountID %q, got %q", idAlice, validatedID)
	}
}


