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

	tracker := jmappush.NewChangeTracker(100)
	if stateStr := tracker.State(); stateStr != "~0" {
		t.Fatalf("expected initial state ~0, got %s", stateStr)
	}

	n, ok := jmappush.ParseNumericStateToken("~42")
	if !ok || n != 42 {
		t.Fatalf("expected state 42, got n=%d, ok=%v", n, ok)
	}
}
