package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOIDCServerDiscoveryAndJWKS(t *testing.T) {
	srv, err := NewOIDCServer("https://auth.profundo.dk", "profundo.dk")
	if err != nil {
		t.Fatalf("Failed to create OIDC server: %v", err)
	}

	handler := srv.Handler()

	// Test Discovery
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Discovery returned %d, expected 200", rec.Code)
	}

	var discovery map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &discovery); err != nil {
		t.Fatalf("Failed to decode discovery JSON: %v", err)
	}

	if discovery["issuer"] != "https://auth.profundo.dk" {
		t.Errorf("Unexpected issuer: %v", discovery["issuer"])
	}
	if discovery["authorization_endpoint"] != "https://auth.profundo.dk/oauth/auth" {
		t.Errorf("Unexpected auth endpoint: %v", discovery["authorization_endpoint"])
	}

	// Test JWKS
	req = httptest.NewRequest(http.MethodGet, "/oauth/keys", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("JWKS returned %d, expected 200", rec.Code)
	}

	var jwks map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &jwks); err != nil {
		t.Fatalf("Failed to decode JWKS JSON: %v", err)
	}

	keys, ok := jwks["keys"].([]any)
	if !ok || len(keys) == 0 {
		t.Fatalf("JWKS missing keys array")
	}
}

func TestOIDCServerFullPKCEFlow(t *testing.T) {
	srv, err := NewOIDCServer("https://auth.profundo.dk", "profundo.dk")
	if err != nil {
		t.Fatalf("Failed to create OIDC server: %v", err)
	}

	handler := srv.Handler()

	// 1. Generate PKCE verifier and challenge
	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	redirectURI := "https://calendar.profundo.dk/callback"
	clientID := "twake-calendar"
	state := "random-state-123"

	// 2. GET /oauth/auth -> renders login page
	req := httptest.NewRequest(http.MethodGet, "/oauth/auth?client_id="+clientID+"&redirect_uri="+url.QueryEscape(redirectURI)+"&state="+state+"&code_challenge="+codeChallenge+"&code_challenge_method=S256", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /oauth/auth returned %d, expected 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Sign In") {
		t.Errorf("Login form HTML missing expected Sign In header")
	}

	// 3. POST /oauth/auth with invalid credentials -> error
	form := url.Values{}
	form.Set("username", "erik")
	form.Set("password", "wrongpass")
	form.Set("client_id", clientID)
	form.Set("redirect_uri", redirectURI)
	form.Set("state", state)
	form.Set("code_challenge", codeChallenge)
	form.Set("code_challenge_method", "S256")

	req = httptest.NewRequest(http.MethodPost, "/oauth/auth", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Invalid username or password") {
		t.Fatalf("POST /oauth/auth with bad password should show error, got %d", rec.Code)
	}

	// 4. POST /oauth/auth with valid credentials -> 302 Redirect with code and session cookie
	form.Set("password", "erik")
	form.Set("remember_me", "on")

	req = httptest.NewRequest(http.MethodPost, "/oauth/auth", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("POST /oauth/auth returned %d, expected 302", rec.Code)
	}

	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatalf("Location header missing")
	}

	parsedLoc, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("Failed to parse redirect location: %v", err)
	}

	authCode := parsedLoc.Query().Get("code")
	if authCode == "" {
		t.Fatalf("Auth code missing from redirect query: %s", loc)
	}
	if parsedLoc.Query().Get("state") != state {
		t.Errorf("State mismatch in redirect: %s", parsedLoc.Query().Get("state"))
	}

	// Check session cookie
	cookie := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookie {
		if c.Name == "oidc_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Errorf("Expected oidc_session cookie to be set")
	}

	// 5. POST /oauth/token with WRONG code_verifier -> error
	tokenForm := url.Values{}
	tokenForm.Set("grant_type", "authorization_code")
	tokenForm.Set("code", authCode)
	tokenForm.Set("redirect_uri", redirectURI)
	tokenForm.Set("client_id", clientID)
	tokenForm.Set("code_verifier", "wrong-verifier")

	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Token with bad verifier should return 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// 6. Login again to get new one-time code and exchange with CORRECT code_verifier
	req = httptest.NewRequest(http.MethodPost, "/oauth/auth", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	newLoc, _ := url.Parse(rec.Header().Get("Location"))
	newAuthCode := newLoc.Query().Get("code")

	tokenForm.Set("code", newAuthCode)
	tokenForm.Set("code_verifier", codeVerifier)

	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /oauth/token returned %d: %s", rec.Code, rec.Body.String())
	}

	var tokenResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tokenResp); err != nil {
		t.Fatalf("Failed to parse token response: %v", err)
	}

	accessToken, _ := tokenResp["access_token"].(string)
	idToken, _ := tokenResp["id_token"].(string)

	if accessToken == "" {
		t.Errorf("access_token missing in token response")
	}
	if idToken == "" {
		t.Errorf("id_token missing in token response")
	}

	// 7. GET /oauth/userinfo with Bearer token
	req = httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("UserInfo returned %d: %s", rec.Code, rec.Body.String())
	}

	var userInfo map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &userInfo); err != nil {
		t.Fatalf("Failed to parse userinfo: %v", err)
	}

	if userInfo["sub"] != "erik@profundo.dk" {
		t.Errorf("Unexpected userinfo sub: %v", userInfo["sub"])
	}
	if userInfo["email"] != "erik@profundo.dk" {
		t.Errorf("Unexpected userinfo email: %v", userInfo["email"])
	}

	// 8. POST /ws/ticket with Bearer token
	req = httptest.NewRequest(http.MethodPost, "/ws/ticket", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ws/ticket returned %d: %s", rec.Code, rec.Body.String())
	}

	var ticket map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ticket); err != nil {
		t.Fatalf("Failed to parse ticket: %v", err)
	}
	if ticket["value"] == "" || ticket["username"] != "erik@profundo.dk" {
		t.Errorf("Invalid ticket response: %v", ticket)
	}

	// 9. GET /dav/calendars/{userId}.json -> returns calendar list with _embedded["dav:calendar"]
	req = httptest.NewRequest(http.MethodGet, "/dav/calendars/erik@profundo.dk.json?personal=true&withRights=true", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dav/calendars/... returned %d: %s", rec.Code, rec.Body.String())
	}

	var calList map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &calList); err != nil {
		t.Fatalf("Failed to parse calendar list JSON: %v", err)
	}
	embedded, ok := calList["_embedded"].(map[string]any)
	if !ok || embedded["dav:calendar"] == nil {
		t.Errorf("Calendar list missing _embedded['dav:calendar']: %v", calList)
	}

	// 10. Test Remember Me: With session cookie, GET /oauth/auth should immediately redirect without form
	req = httptest.NewRequest(http.MethodGet, "/oauth/auth?client_id="+clientID+"&redirect_uri="+url.QueryEscape(redirectURI)+"&state="+state+"&code_challenge="+codeChallenge+"&code_challenge_method=S256", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("Remember-me session should redirect with 302, got %d", rec.Code)
	}
}
