package jmap_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

func TestWellKnownJMAP_Get(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/jmap")
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

func TestWellKnownJMAP_RequestBasedURLs(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Host = "jmap.example.com:8443"
	req.Header.Set("X-Forwarded-Proto", "https")

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
	if session.DownloadURL != expectedPrefix+"/download/{accountId}/{blobId}/{name}?type={type}" {
		t.Errorf("Expected DownloadURL starting with %q, got %q", expectedPrefix, session.DownloadURL)
	}
	if session.EventSourceURL != expectedPrefix+"/eventsource?types={types}&closeafter={closeafter}&ping={ping}" {
		t.Errorf("Expected EventSourceURL starting with %q, got %q", expectedPrefix, session.EventSourceURL)
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

func TestWellKnownJMAP_Head(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Head(ts.URL + "/.well-known/jmap")
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

func TestWellKnownJMAP_MethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/.well-known/jmap", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to execute POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405 Method Not Allowed, got %d", resp.StatusCode)
	}
}

func TestOtherRoutes_NotFound(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	paths := []string{"/", "/api", "/.well-known", "/.well-known/jmap/extra", "/unknown"}
	for _, path := range paths {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("Failed to execute GET request for %s: %v", path, err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Path %q: Expected status 404 Not Found, got %d", path, resp.StatusCode)
		}
	}
}
