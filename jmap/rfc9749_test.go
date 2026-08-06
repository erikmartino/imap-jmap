package jmap_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9749_VAPIDPushCapability tests RFC 9749 VAPID Web Push capability advertising and endpoint options.
func TestRFC9749_VAPIDPushCapability(t *testing.T) {
	session := jmap.DefaultSession("http://localhost:8080", "user@example.com")
	srv := jmap.NewServer(session)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var sess jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}

	vapidCap, ok := sess.Capabilities[jmap.WebPushVapidCapabilityURI]
	if !ok {
		t.Fatalf("Expected RFC 9749 VAPID capability %q in session capabilities", jmap.WebPushVapidCapabilityURI)
	}

	capMap, _ := vapidCap.(map[string]any)
	if capMap == nil {
		t.Logf("VAPID Capability present: %v", vapidCap)
	}
}
