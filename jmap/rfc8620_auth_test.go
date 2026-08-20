package jmap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/spectest"

	"github.com/coder/websocket"
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
	validatedID, _, err := auth.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if validatedID != idAlice {
		t.Errorf("Expected validated accountID %q, got %q", idAlice, validatedID)
	}
}

// TestRFC8620_PermissionGuard_Dispatch tests that self account access is allowed and foreign account access returns accountNotFound error.
func TestRFC8620_PermissionGuard_Dispatch(t *testing.T) {
	srv, authBackend := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tokenA, err := authBackend.Authenticate(context.Background(), "userA", "userA")
	if err != nil {
		t.Fatalf("Authenticate userA failed: %v", err)
	}

	idA := jmap.AccountIDForSubject("userA")
	idB := jmap.AccountIDForSubject("userB")
	usingMail := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Own account via "primary" alias -> OK
	res1 := postJMAPWithToken(t, ts.URL, tokenA, usingMail, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c1"},
	})
	if res1.MethodResponses[0].Name == "error" {
		t.Errorf("Accessing primary account failed: %v", res1.MethodResponses[0].Args)
	}

	// 2. Own account via derived accountId -> OK
	res2 := postJMAPWithToken(t, ts.URL, tokenA, usingMail, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": idA}, "c2"},
	})
	if res2.MethodResponses[0].Name == "error" {
		t.Errorf("Accessing own derived accountID failed: %v", res2.MethodResponses[0].Args)
	}

	// 3. Foreign account via userB's accountId -> method error accountNotFound
	res3 := postJMAPWithToken(t, ts.URL, tokenA, usingMail, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": idB}, "c3"},
	})
	if res3.MethodResponses[0].Name != "error" {
		t.Errorf("Expected error response for foreign account, got %s", res3.MethodResponses[0].Name)
	}
	errType, _ := res3.MethodResponses[0].Args["type"].(string)
	if errType != "accountNotFound" {
		t.Errorf("Expected accountNotFound method error, got %q", errType)
	}
}

// TestRFC8620_Auth_OIDCRejectsCredentialsWithoutFallback is the AUTH-2 gate:
// with OIDC active and no credential fallback configured, plain username ==
// password matches MUST be rejected (fail closed) rather than accepted through
// the development credential path.
func TestRFC8620_Auth_OIDCRejectsCredentialsWithoutFallback(t *testing.T) {
	spectest.Require(t, "RFC8620", "8.2", spectest.MUST,
		"Credential authentication must fail closed: when no credential backend is configured, a plain username == password match MUST NOT be accepted.")

	oidcBackend, err := jmap.NewOIDCAuthBackend(jmap.OIDCConfig{Issuer: "https://auth.example.com"})
	if err != nil {
		t.Fatalf("NewOIDCAuthBackend: %v", err)
	}
	ctx := context.Background()

	// Even credentials that equal the username must be rejected.
	if _, err := oidcBackend.Authenticate(ctx, "alice@example.com", "alice@example.com"); err == nil {
		t.Fatalf("Authenticate must reject username==password when no fallback is configured")
	}
	if _, err := oidcBackend.ValidateCredentials(ctx, "alice@example.com", "alice@example.com"); err == nil {
		t.Fatalf("ValidateCredentials must reject username==password when no fallback is configured")
	}

	// Basic authentication through the HTTP middleware must 401.
	srv := newTestServer(jmap.WithAuthBackend(oidcBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	req.SetBasicAuth("alice@example.com", "alice@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Basic auth with password==email must 401 on a fallback-less OIDC backend, got %d", resp.StatusCode)
	}

	// Login (credential exchange) must fail too.
	loginBody, _ := json.Marshal(map[string]string{"username": "alice@example.com", "password": "alice@example.com"})
	loginResp, err := http.Post(ts.URL+"/jmap/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("POST /jmap/login failed: %v", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login with password==email must 401 on a fallback-less OIDC backend, got %d", loginResp.StatusCode)
	}
}

// TestRFC8620_Auth_OIDCDelegatesCredentialsToFallback preserves the development
// behavior: an explicitly configured fallback backend (e.g. MemoryAuthBackend)
// still accepts its credentials so local development and the test harness work.
func TestRFC8620_Auth_OIDCDelegatesCredentialsToFallback(t *testing.T) {
	fallback := memory.NewMemoryAuthBackend()
	oidcBackend, err := jmap.NewOIDCAuthBackend(jmap.OIDCConfig{
		Issuer:          "https://auth.example.com",
		FallbackBackend: fallback,
	})
	if err != nil {
		t.Fatalf("NewOIDCAuthBackend: %v", err)
	}
	ctx := context.Background()

	id, err := oidcBackend.ValidateCredentials(ctx, "alice@example.com", "alice@example.com")
	if err != nil {
		t.Fatalf("ValidateCredentials with explicit fallback: %v", err)
	}
	if id != jmap.AccountIDForSubject("alice@example.com") {
		t.Fatalf("expected accountID %q, got %q", jmap.AccountIDForSubject("alice@example.com"), id)
	}
	token, err := oidcBackend.Authenticate(ctx, "bob@example.com", "bob@example.com")
	if err != nil {
		t.Fatalf("Authenticate with explicit fallback: %v", err)
	}
	if token == "" {
		t.Fatalf("expected a token from the fallback backend")
	}
}

// TestRFC8620_AccountResolver_PrimaryDomain tests local vs external email address resolution.
func TestRFC8620_AccountResolver_PrimaryDomain(t *testing.T) {
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	ctx := context.Background()

	// Local address -> returns derived accountID and local = true
	id, local := resolver.ResolveAccountID(ctx, "user@example.com")
	if !local {
		t.Errorf("Expected user@example.com to be local")
	}
	if id != jmap.AccountIDForSubject("user@example.com") {
		t.Errorf("Expected accountID %q, got %q", jmap.AccountIDForSubject("user@example.com"), id)
	}

	// Case insensitive domain match
	_, localUpper := resolver.ResolveAccountID(ctx, "user@EXAMPLE.COM")
	if !localUpper {
		t.Errorf("Expected case-insensitive local match for user@EXAMPLE.COM")
	}

	// External address -> local = false
	idExt, localExt := resolver.ResolveAccountID(ctx, "user@external.com")
	if localExt || idExt != "" {
		t.Errorf("Expected external address to resolve ( \"\", false ), got ( %q, %v )", idExt, localExt)
	}

	// Malformed address -> local = false
	idBad, localBad := resolver.ResolveAccountID(ctx, "not-an-email")
	if localBad || idBad != "" {
		t.Errorf("Expected malformed address to resolve ( \"\", false ), got ( %q, %v )", idBad, localBad)
	}
}
