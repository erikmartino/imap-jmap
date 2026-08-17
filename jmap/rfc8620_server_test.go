package jmap_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_WellKnownJMAP_Get tests RFC 8620 Section 2.2 /.well-known/jmap GET session resource discovery.
func TestRFC8620_WellKnownJMAP_Get(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("Failed to execute GET request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", contentType)
	}

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session JSON: %v", err)
	}

	if session.Username == "" {
		t.Error("Expected non-empty username in session")
	}

	if _, ok := session.Capabilities[jmap.CoreCapabilityURI]; !ok {
		t.Errorf("Expected core capability URI %q in capabilities", jmap.CoreCapabilityURI)
	}

	if _, ok := session.PrimaryAccounts[jmap.CoreCapabilityURI]; !ok {
		t.Errorf("Expected primary account for %q", jmap.CoreCapabilityURI)
	}
}

// TestRFC8620_WellKnownJMAP_RequestBasedURLs tests RFC 8620 Section 2.2 X-Forwarded-Proto dynamic URL generation.
func TestRFC8620_WellKnownJMAP_RequestBasedURLs(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Host = "jmap.example.com:8443"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.SetBasicAuth(testUsername, testUsername)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session JSON: %v", err)
	}

	expectedPrefix := "https://jmap.example.com:8443"
	if session.APIURL != expectedPrefix+"/jmap" {
		t.Errorf("Expected APIURL %q, got %q", expectedPrefix+"/jmap", session.APIURL)
	}
	if session.UploadURL != expectedPrefix+"/upload/{accountId}/" {
		t.Errorf("Expected UploadURL %q, got %q", expectedPrefix+"/upload/{accountId}/", session.UploadURL)
	}
	if session.DownloadURL != expectedPrefix+"/download/{accountId}/{blobId}/{name}?accept={type}" {
		t.Errorf("Expected DownloadURL %q, got %q", expectedPrefix+"/download/{accountId}/{blobId}/{name}?accept={type}", session.DownloadURL)
	}
	if session.EventSourceURL != expectedPrefix+"/eventsource?types={types}&closeafter={closeafter}&ping={ping}" {
		t.Errorf("Expected EventSourceURL %q, got %q", expectedPrefix+"/eventsource?types={types}&closeafter={closeafter}&ping={ping}", session.EventSourceURL)
	}

	wsCapRaw, ok := session.Capabilities[jmap.WebSocketCapabilityURI]
	if !ok {
		t.Fatalf("Expected websocket capability in session")
	}
	wsMap, ok := wsCapRaw.(map[string]any)
	if !ok {
		t.Fatalf("Expected websocket capability map, got %T", wsCapRaw)
	}
	expectedWSURL := "wss://jmap.example.com:8443/jmap/ws"
	if wsMap["url"] != expectedWSURL {
		t.Errorf("Expected websocket url %q, got %q", expectedWSURL, wsMap["url"])
	}
}

// TestRFC8620_WellKnownJMAP_Head tests RFC 8620 HEAD request on /.well-known/jmap.
func TestRFC8620_WellKnownJMAP_Head(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req := authedRequest(t, "HEAD", ts.URL+"/.well-known/jmap", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to execute HEAD request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", contentType)
	}
}

// TestRFC8620_WellKnownJMAP_MethodNotAllowed tests POST on /.well-known/jmap returns 405 Method Not Allowed per RFC 8620.
func TestRFC8620_WellKnownJMAP_MethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := authedPost(ts.URL+"/.well-known/jmap", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to execute POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405 Method Not Allowed, got %d", resp.StatusCode)
	}
}

// TestRFC8620_OtherRoutes_NotFound tests unmapped endpoints return 404 Not Found per RFC 8620.
func TestRFC8620_OtherRoutes_NotFound(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	paths := []string{"/api", "/.well-known", "/.well-known/jmap/extra", "/unknown"}
	for _, path := range paths {
		resp, err := authedGet(ts.URL + path)
		if err != nil {
			t.Fatalf("Failed to execute GET request for %s: %v", path, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Path %q: Expected status 404 Not Found, got %d", path, resp.StatusCode)
		}
	}
}
