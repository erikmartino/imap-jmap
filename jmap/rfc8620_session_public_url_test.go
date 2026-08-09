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
		"The Session object advertises absolute apiUrl/downloadUrl/uploadUrl/eventSourceUrl for the account.")

	srv := newTestServer(jmap.WithPublicBaseURL("https://jmap.example.test/"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	s := fetchSession(t, ts.URL)

	if s.APIURL != "https://jmap.example.test/jmap" {
		t.Errorf("apiUrl = %q, want https://jmap.example.test/jmap", s.APIURL)
	}
	for name, got := range map[string]string{
		"downloadUrl":    s.DownloadURL,
		"uploadUrl":      s.UploadURL,
		"eventSourceUrl": s.EventSourceURL,
	} {
		if !strings.HasPrefix(got, "https://jmap.example.test/") {
			t.Errorf("%s = %q, want https://jmap.example.test/ prefix", name, got)
		}
	}
	if ws := websocketURL(s.Capabilities); !strings.HasPrefix(ws, "wss://jmap.example.test/") {
		t.Errorf("websocket url = %q, want wss://jmap.example.test/ prefix", ws)
	}
}

// TestRFC8620_SessionFallsBackToRequestURL verifies that without a configured public base
// URL the session URLs are derived from the request (the pre-existing behaviour), so the
// override is opt-in.
func TestRFC8620_SessionFallsBackToRequestURL(t *testing.T) {
	srv := newTestServer() // no WithPublicBaseURL
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	s := fetchSession(t, ts.URL)
	if s.APIURL != ts.URL+"/jmap" {
		t.Errorf("apiUrl = %q, want request-derived %q", s.APIURL, ts.URL+"/jmap")
	}
}
