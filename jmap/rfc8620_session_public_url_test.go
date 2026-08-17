package jmap_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

type sessionURLs struct {
	APIURL         string         `json:"apiUrl"`
	DownloadURL    string         `json:"downloadUrl"`
	UploadURL      string         `json:"uploadUrl"`
	EventSourceURL string         `json:"eventSourceUrl"`
	Capabilities   map[string]any `json:"capabilities"`
}

func fetchSession(t *testing.T, tsURL string) sessionURLs {
	t.Helper()
	resp, err := authedGet(tsURL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer resp.Body.Close()
	var s sessionURLs
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return s
}

func websocketURL(caps map[string]any) string {
	for name, v := range caps {
		if !strings.Contains(name, "websocket") {
			continue
		}
		if m, ok := v.(map[string]any); ok {
			if u, ok := m["url"].(string); ok {
				return u
			}
		}
	}
	return ""
}

// TestRFC8620_SessionHonorsPublicBaseURL verifies that a configured public base URL is used
// for the session's absolute service URLs (RFC 8620 Section 2), so a TLS-terminating proxy
// that omits X-Forwarded-Proto cannot cause a cleartext http:// apiUrl — the failure mode
// that breaks Android clients (Ltt.rs), which refuse cleartext traffic by default.
func TestRFC8620_SessionHonorsPublicBaseURL(t *testing.T) {
	spectest.Require(t, "RFC8620", "2", spectest.MUST,
		"apiUrl is the URL to use for JMAP API requests, advertised as an absolute URL built from the externally-reachable base URL.")

	srv := newTestServer(jmap.WithPublicBaseURL("https://jmap.example.test/"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	s := fetchSession(t, ts.URL)

	if s.APIURL != "https://jmap.example.test/jmap" {
		t.Errorf("apiUrl = %q, want https://jmap.example.test/jmap", s.APIURL)
	}
	if s.DownloadURL != "https://jmap.example.test/download/{accountId}/{blobId}/{name}?accept={type}" {
		t.Errorf("downloadUrl = %q, want https://jmap.example.test/download/{accountId}/{blobId}/{name}?accept={type}", s.DownloadURL)
	}
	if s.UploadURL != "https://jmap.example.test/upload/{accountId}/" {
		t.Errorf("uploadUrl = %q, want https://jmap.example.test/upload/{accountId}/", s.UploadURL)
	}
	if s.EventSourceURL != "https://jmap.example.test/eventsource?types={types}&closeafter={closeafter}&ping={ping}" {
		t.Errorf("eventSourceUrl = %q, want https://jmap.example.test/eventsource?types={types}&closeafter={closeafter}&ping={ping}", s.EventSourceURL)
	}
}

// TestRFC8620_SessionFallsBackToRequestURL verifies that session URLs are derived from the
// request's host and scheme when no public base URL is configured.
func TestRFC8620_SessionFallsBackToRequestURL(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	s := fetchSession(t, ts.URL)
	wantPrefix := ts.URL
	if s.APIURL != wantPrefix+"/jmap" {
		t.Errorf("apiUrl = %q, want %q", s.APIURL, wantPrefix+"/jmap")
	}
}
