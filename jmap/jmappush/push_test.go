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

func TestStateVectorHelpers(t *testing.T) {
	// Empty vector
	emptyToken := jmappush.EncodeStateVector("test-v1:", nil)
	if emptyToken != "test-v1:empty" {
		t.Fatalf("expected test-v1:empty, got %q", emptyToken)
	}
	decoded, err := jmappush.DecodeStateVector("test-v1:", emptyToken)
	if err != nil || len(decoded) != 0 {
		t.Fatalf("failed decoding empty vector: %v, %v", err, decoded)
	}

	// Normal vector
	m := map[string]string{
		"id-2": "fp2",
		"id-1": "fp1",
	}
	token := jmappush.EncodeStateVector("test-v1:", m)
	decoded2, err := jmappush.DecodeStateVector("test-v1:", token)
	if err != nil {
		t.Fatalf("failed decoding vector: %v", err)
	}
	if decoded2["id-1"] != "fp1" || decoded2["id-2"] != "fp2" {
		t.Fatalf("unexpected decoded content: %v", decoded2)
	}

	// Wrong prefix
	if _, err := jmappush.DecodeStateVector("other-v1:", token); err == nil {
		t.Fatalf("expected error decoding with wrong prefix")
	}

	// Diffing
	cur := map[string]string{
		"id-2": "fp2-modified",
		"id-3": "fp3",
	}
	created, updated, destroyed := jmappush.DiffStateVectors(m, cur)
	if len(created) != 1 || created[0] != "id-3" {
		t.Fatalf("expected created=[id-3], got %v", created)
	}
	if len(updated) != 1 || updated[0] != "id-2" {
		t.Fatalf("expected updated=[id-2], got %v", updated)
	}
	if len(destroyed) != 1 || destroyed[0] != "id-1" {
		t.Fatalf("expected destroyed=[id-1], got %v", destroyed)
	}

	// ObjectFingerprint
	type sample struct {
		Name  string
		Count int
	}
	fp1 := jmappush.ObjectFingerprint(&sample{Name: "a", Count: 1})
	fp2 := jmappush.ObjectFingerprint(&sample{Name: "a", Count: 1})
	fp3 := jmappush.ObjectFingerprint(&sample{Name: "a", Count: 2})
	if fp1 == "" || fp1 != fp2 {
		t.Fatalf("expected identical fingerprints, got %q vs %q", fp1, fp2)
	}
	if fp1 == fp3 {
		t.Fatalf("expected different fingerprints for different objects")
	}
}

