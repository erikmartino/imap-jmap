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
		"The Session object advertises relative apiUrl/downloadUrl/uploadUrl/eventSourceUrl for the account.")

	srv := newTestServer(jmap.WithPublicBaseURL("https://jmap.example.test/"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	s := fetchSession(t, ts.URL)

	if s.APIURL != "/jmap" {
		t.Errorf("apiUrl = %q, want /jmap", s.APIURL)
	}
	if s.DownloadURL != "/download/{accountId}/{blobId}/{name}?accept={type}" {
		t.Errorf("downloadUrl = %q, want /download/{accountId}/{blobId}/{name}?accept={type}", s.DownloadURL)
	}
	if s.UploadURL != "/upload/{accountId}/" {
		t.Errorf("uploadUrl = %q, want /upload/{accountId}/", s.UploadURL)
	}
	if s.EventSourceURL != "/eventsource?types={types}&closeafter={closeafter}&ping={ping}" {
		t.Errorf("eventSourceUrl = %q, want /eventsource?types={types}&closeafter={closeafter}&ping={ping}", s.EventSourceURL)
	}
}

// TestRFC8620_SessionFallsBackToRequestURL verifies that session URLs remain relative.
func TestRFC8620_SessionFallsBackToRequestURL(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	s := fetchSession(t, ts.URL)
	if s.APIURL != "/jmap" {
		t.Errorf("apiUrl = %q, want /jmap", s.APIURL)
	}
}
