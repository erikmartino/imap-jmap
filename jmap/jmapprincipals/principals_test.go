package jmapprincipals_test

import (
	"testing"

	"imap-jmap/jmap/jmapprincipals"
	"imap-jmap/jmap/spectest"
)

func TestMatchPrincipal(t *testing.T) {
	spectest.RequireID(t, "draft-ietf-jmap-principals#2-p1-MUST", "Principal query filtering")

	p := &jmapprincipals.Principal{
		ID:          "p1",
		Type:        "individual",
		Name:        "Alice Wonderland",
		Description: "Engineer",
		Email:       "alice@example.com",
		AccountIDs:  map[string]bool{"acc1": true},
	}

	// Match all with nil filter
	if !jmapprincipals.MatchPrincipal(p, nil) {
		t.Fatal("expected match with nil filter")
	}

	// Match by type
	if !jmapprincipals.MatchPrincipal(p, map[string]any{"type": "individual"}) {
		t.Fatal("expected match by type")
	}
	if jmapprincipals.MatchPrincipal(p, map[string]any{"type": "group"}) {
		t.Fatal("expected no match for wrong type")
	}

	// Match by name
	if !jmapprincipals.MatchPrincipal(p, map[string]any{"name": "Alice"}) {
		t.Fatal("expected match by name substring")
	}
	if jmapprincipals.MatchPrincipal(p, map[string]any{"name": "Bob"}) {
		t.Fatal("expected no match for wrong name")
	}

	// Match by email
	if !jmapprincipals.MatchPrincipal(p, map[string]any{"email": "alice@"}) {
		t.Fatal("expected match by email substring")
	}

	// Match by text (searches name, email, description)
	if !jmapprincipals.MatchPrincipal(p, map[string]any{"text": "engineer"}) {
		t.Fatal("expected match by text in description")
	}

	// Match by accountIds
	if !jmapprincipals.MatchPrincipal(p, map[string]any{"accountIds": []any{"acc1"}}) {
		t.Fatal("expected match by accountIds")
	}
	if jmapprincipals.MatchPrincipal(p, map[string]any{"accountIds": []any{"acc2"}}) {
		t.Fatal("expected no match for wrong accountId")
	}
}
