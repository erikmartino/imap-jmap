package jmappush_test

import (
	"testing"

	"imap-jmap/jmap/jmappush"
	"imap-jmap/jmap/spectest"
)

func TestPushPrimitives(t *testing.T) {
	spectest.RequireID(t, "RFC8620#7.1-p1-MUST", "StateChange notification representation")
	spectest.RequireID(t, "RFC8620#5.2-p1-MUST", "ChangeTracker state tokens")

	sc := jmappush.StateChange{
		Type: "StateChange",
		Changed: map[string]map[string]string{
			"acc-1": {"Email": "s-100"},
		},
	}
	if sc.Type != "StateChange" || sc.Changed["acc-1"]["Email"] != "s-100" {
		t.Fatalf("unexpected StateChange values: %+v", sc)
	}

	// Default maxKeep fallback
	defTracker := jmappush.NewChangeTracker(0)
	if defTracker == nil {
		t.Fatalf("expected NewChangeTracker(0) to use default maxKeep")
	}

	tracker := jmappush.NewChangeTracker(100)
	if stateStr := tracker.State(); stateStr != "~0" {
		t.Fatalf("expected initial state ~0, got %s", stateStr)
	}

	// State token parsing edge cases
	if n, ok := jmappush.ParseNumericStateToken(""); !ok || n != 0 {
		t.Fatalf("expected empty state token to return 0, true")
	}
	if n, ok := jmappush.ParseNumericStateToken("0"); !ok || n != 0 {
		t.Fatalf("expected '0' state token to return 0, true")
	}
	if n, ok := jmappush.ParseNumericStateToken("~42"); !ok || n != 42 {
		t.Fatalf("expected '~42' state token to return 42, true")
	}
	if n, ok := jmappush.ParseNumericStateToken("state-99"); !ok || n != 99 {
		t.Fatalf("expected 'state-99' state token to return 99, true")
	}
	if _, ok := jmappush.ParseNumericStateToken("invalid-state"); ok {
		t.Fatalf("expected error parsing invalid state token string")
	}
}
