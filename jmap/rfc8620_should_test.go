package jmap_test

import (
	"testing"

	"imap-jmap/jmap/spectest"
)

func idSet(v any) map[string]bool {
	out := map[string]bool{}
	if list, ok := v.([]any); ok {
		for _, x := range list {
			if s, ok := x.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

// TestRFC8620_Should_ChangesCreatedUpdatedDestroyed covers the RFC 8620 Section 5.2
// SHOULD clauses describing how a record that was both created and updated (or both
// updated and destroyed, or both created and destroyed) since the client's state is
// reported by Foo/changes.
func TestRFC8620_Should_ChangesCreatedUpdatedDestroyed(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.2", spectest.SHOULD,
		`server SHOULD just return the id in the "created" list but MAY return`)
	spectest.Require(t, "RFC8620", "5.2", spectest.SHOULD,
		`server SHOULD just return the id in the "destroyed" list but MAY`)
	spectest.Require(t, "RFC8620", "5.2", spectest.SHOULD,
		"server SHOULD remove the id from the response entirely")

	srv := newTestServer()

	baseline := postMethod(t, srv, "AddressBook/set", map[string]any{
		"accountId": "primary",
		"create":    map[string]any{"ab0": map[string]any{"name": "Baseline"}},
	})
	s0 := baseline["newState"].(string)
	ab0 := baseline["created"].(map[string]any)["ab0"].(map[string]any)["id"].(string)

	created1 := postMethod(t, srv, "AddressBook/set", map[string]any{
		"accountId": "primary",
		"create":    map[string]any{"ab1": map[string]any{"name": "One"}},
	})
	s1 := created1["newState"].(string)
	ab1 := created1["created"].(map[string]any)["ab1"].(map[string]any)["id"].(string)

	postMethod(t, srv, "AddressBook/set", map[string]any{
		"accountId": "primary",
		"update":    map[string]any{ab1: map[string]any{"name": "One Renamed"}},
	})

	created2 := postMethod(t, srv, "AddressBook/set", map[string]any{
		"accountId": "primary",
		"create":    map[string]any{"ab2": map[string]any{"name": "Two"}},
	})
	ab2 := created2["created"].(map[string]any)["ab2"].(map[string]any)["id"].(string)
	postMethod(t, srv, "AddressBook/set", map[string]any{
		"accountId": "primary",
		"destroy":   []any{ab2},
	})

	// ab1 was created AND updated since s0: it SHOULD be reported in "created".
	changes := postMethod(t, srv, "AddressBook/changes", map[string]any{
		"accountId": "primary", "sinceState": s0,
	})
	if !idSet(changes["created"])[ab1] {
		t.Errorf("created-and-updated record SHOULD appear in created, got created=%v updated=%v", changes["created"], changes["updated"])
	}
	// ab2 was created AND destroyed since s0: it SHOULD be removed entirely.
	for _, list := range []string{"created", "updated", "destroyed"} {
		if idSet(changes[list])[ab2] {
			t.Errorf("created-and-destroyed record SHOULD be removed entirely, found in %s=%v", list, changes[list])
		}
	}

	// /changes must be calculable from any state string previously returned.
	fromIntermediate := postMethod(t, srv, "AddressBook/changes", map[string]any{
		"accountId": "primary", "sinceState": s1,
	})
	if !idSet(fromIntermediate["created"])[ab1] && !idSet(fromIntermediate["updated"])[ab1] {
		t.Errorf("changes from intermediate state %q lost %q: %v", s1, ab1, fromIntermediate)
	}

	// ab0 was updated AND destroyed since s0: it SHOULD be reported in "destroyed".
	postMethod(t, srv, "AddressBook/set", map[string]any{
		"accountId": "primary",
		"update":    map[string]any{ab0: map[string]any{"name": "Baseline 2"}},
	})
	postMethod(t, srv, "AddressBook/set", map[string]any{
		"accountId": "primary",
		"destroy":   []any{ab0},
	})
	final := postMethod(t, srv, "AddressBook/changes", map[string]any{
		"accountId": "primary", "sinceState": s0,
	})
	if !idSet(final["destroyed"])[ab0] {
		t.Errorf("updated-and-destroyed record SHOULD appear in destroyed, got %v", final["destroyed"])
	}
}
