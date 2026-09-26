package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8620_EmailChangesMaxChangesFailsClosed verifies that when a delta
// exceeds maxChanges and no consistent intermediate state can be produced, the
// server returns cannotCalculateChanges rather than a state with no ids
// (RFC 8620 Section 5.2).
func TestRFC8620_EmailChangesMaxChangesFailsClosed(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.2", spectest.MUST,
		"calculate an intermediate state, it MUST return a")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	r0 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{"accountId": "primary"}, "s0"},
	})
	state0, _ := r0.MethodResponses[0].Args["state"].(string)
	if state0 == "" {
		t.Fatalf("empty initial email state: %v", r0.MethodResponses[0].Args)
	}

	rCreate := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{"subject": "one", "mailboxIds": map[string]any{"mb-inbox": true}},
				"e2": map[string]any{"subject": "two", "mailboxIds": map[string]any{"mb-inbox": true}},
			},
		}, "c1"},
	})
	if created, _ := rCreate.MethodResponses[0].Args["created"].(map[string]any); len(created) != 2 {
		t.Fatalf("expected 2 emails created, got %v", rCreate.MethodResponses[0].Args)
	}

	r := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": state0,
			"maxChanges": 1,
		}, "c2"},
	})
	mr := r.MethodResponses[0]
	if mr.Name != "error" {
		t.Fatalf("expected a cannotCalculateChanges error when maxChanges is exceeded, got %q %v", mr.Name, mr.Args)
	}
	if mr.Args["type"] != "cannotCalculateChanges" {
		t.Errorf("expected cannotCalculateChanges, got %v", mr.Args["type"])
	}
}
