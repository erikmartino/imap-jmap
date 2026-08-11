package jmap_test

// Tests for the POST /jmap/login credential-exchange endpoint. This endpoint is a custom
// (non-RFC) convenience: RFC 8620 defers authentication to OAuth 2.0 and defines no such
// endpoint (see handleLogin in auth_middleware.go). Hence the rfcless_ test prefix.
//
// The shared newAuthTestServer/loginAndGetToken helpers live in rfc8620_auth_test.go
// (same package) and back the RFC-adjacent Bearer/Basic/token tests there.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLogin_Success tests that matching username/password returns a Bearer token.
func TestLogin_Success(t *testing.T) {
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
	expectedAccountID := "YWxpY2U"
	if result["accountId"] != expectedAccountID {
		t.Errorf("Expected accountId %q, got %q", expectedAccountID, result["accountId"])
	}
}

// TestLogin_WrongPassword tests that mismatched credentials return 401.
func TestLogin_WrongPassword(t *testing.T) {
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

// TestLogin_EmptyUsername tests that empty username is rejected.
func TestLogin_EmptyUsername(t *testing.T) {
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
