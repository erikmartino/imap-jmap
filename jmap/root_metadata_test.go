package jmap_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRootEndpointMetadata(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected application/json, got %q", contentType)
	}

	var meta map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("Decode root metadata JSON failed: %v", err)
	}

	if meta["name"] != "imap-jmap" {
		t.Errorf("Expected name 'imap-jmap', got %v", meta["name"])
	}

	endpoints, ok := meta["endpoints"].(map[string]any)
	if !ok {
		t.Fatalf("Expected endpoints map in metadata")
	}
	if endpoints["session"] != "/.well-known/jmap" {
		t.Errorf("Expected session endpoint '/.well-known/jmap', got %v", endpoints["session"])
	}
}
