package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9404_FileNodeCapabilityAndHandlers tests advertising urn:ietf:params:jmap:filenode and FileNode/* handlers per RFC 9404 JMAP Blob & File Management.
func TestRFC9404_FileNodeCapabilityAndHandlers(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Verify capability URI in /.well-known/jmap
	respSession, err := http.Get(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer respSession.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(respSession.Body).Decode(&session); err != nil {
		t.Fatalf("Decode session failed: %v", err)
	}

	if _, ok := session.Capabilities[jmap.FileNodeCapabilityURI]; !ok {
		t.Errorf("Expected capability %q in session.Capabilities", jmap.FileNodeCapabilityURI)
	}

	// 2. Issue FileNode/get and FileNode/query method calls
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI},
		"methodCalls": []any{
			[]any{"FileNode/get", map[string]any{
				"accountId": "primary",
			}, "c1"},
			[]any{"FileNode/query", map[string]any{
				"accountId": "primary",
			}, "c2"},
		},
	}
	bodyBytes, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Decode Response failed: %v", err)
	}

	if len(jmapResp.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses, got %d", len(jmapResp.MethodResponses))
	}

	if jmapResp.MethodResponses[0].Name != "FileNode/get" {
		t.Errorf("Expected 'FileNode/get', got %q", jmapResp.MethodResponses[0].Name)
	}
	if jmapResp.MethodResponses[1].Name != "FileNode/query" {
		t.Errorf("Expected 'FileNode/query', got %q", jmapResp.MethodResponses[1].Name)
	}
}
