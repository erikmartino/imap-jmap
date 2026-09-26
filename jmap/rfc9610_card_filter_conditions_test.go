package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9610_Section3_3_1_CardFilterConditions covers the RFC 9610 Section 3.3.1
// filter conditions: inAddressBook, uid, kind, created/updated bounds, empty
// condition truthiness, AND semantics, and text phrase/token matching.
func TestRFC9610_Section3_3_1_CardFilterConditions(t *testing.T) {
	spectest.Require(t, "RFC9610", "3.3.1", spectest.MUST, "A card must be in this address book to match")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.MUST, "A card must have this string exactly as its uid (as defined in")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.MUST, "A card must have a \"kind\" property (as defined in Section 2")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.MUST, "condition MUST always evaluate to true")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.MUST, "specified, ALL must apply for the condition to be true (it is")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.MUST, "contact, but MUST all be present for the contact to match the")
	spectest.Require(t, "RFC9610", "3.3.1", spectest.SHOULD, "required for that exact sequence of words, excluding the")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	// Create two address books so inAddressBook is distinguishable.
	rAB := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{"accountId": "primary", "create": map[string]any{
			"ab1": map[string]any{"name": "Main"},
			"ab2": map[string]any{"name": "Other"},
		}}, "a1"},
	})
	ab1 := rAB.MethodResponses[0].Args["created"].(map[string]any)["ab1"].(map[string]any)["id"].(string)
	ab2 := rAB.MethodResponses[0].Args["created"].(map[string]any)["ab2"].(map[string]any)["id"].(string)

	rSet := postJMAP(t, ts.URL, using, []any{
		[]any{"ContactCard/set", map[string]any{"accountId": "primary", "create": map[string]any{
			"c1": map[string]any{
				"addressBookIds": map[string]any{ab1: true},
				"uid":            "u1",
				"kind":           "individual",
				"created":        "2026-01-01T00:00:00Z",
				"updated":        "2026-01-02T00:00:00Z",
				"name":           map[string]any{"full": "Alice Smith"},
				"emails":         map[string]any{"e": map[string]any{"address": "alice@example.com"}},
				"notes":          map[string]any{"n": map[string]any{"note": "quarterly figures"}},
			},
			"c2": map[string]any{
				"addressBookIds": map[string]any{ab2: true},
				"uid":            "u2",
				"kind":           "group",
				"created":        "2026-02-01T00:00:00Z",
				"updated":        "2026-02-02T00:00:00Z",
				"name":           map[string]any{"full": "Bob Jones"},
			},
		}}, "s1"},
	})
	if created, _ := rSet.MethodResponses[0].Args["created"].(map[string]any); len(created) != 2 {
		t.Fatalf("card create failed: %v", rSet.MethodResponses[0].Args)
	}

	query := func(filter map[string]any) []any {
		res := postJMAP(t, ts.URL, using, []any{
			[]any{"ContactCard/query", map[string]any{"accountId": "primary", "filter": filter}, "q"},
		})
		if res.MethodResponses[0].Name == "error" {
			t.Fatalf("query error for filter %v: %v", filter, res.MethodResponses[0].Args)
		}
		ids, _ := res.MethodResponses[0].Args["ids"].([]any)
		return ids
	}
	count := func(filter map[string]any) int { return len(query(filter)) }

	// inAddressBook
	if n := count(map[string]any{"inAddressBook": ab1}); n != 1 {
		t.Errorf("inAddressBook should match 1 card, got %d", n)
	}
	// uid
	if n := count(map[string]any{"uid": "u2"}); n != 1 {
		t.Errorf("uid should match 1 card, got %d", n)
	}
	// kind
	if n := count(map[string]any{"kind": "group"}); n != 1 {
		t.Errorf("kind should match 1 card, got %d", n)
	}
	// created bounds (half-open: before is strict, after is inclusive)
	if n := count(map[string]any{"createdBefore": "2026-01-15T00:00:00Z"}); n != 1 {
		t.Errorf("createdBefore should match 1 card, got %d", n)
	}
	if n := count(map[string]any{"createdAfter": "2026-02-01T00:00:00Z"}); n != 1 {
		t.Errorf("createdAfter should match 1 card, got %d", n)
	}
	// updated bounds
	if n := count(map[string]any{"updatedAfter": "2026-02-01T00:00:00Z"}); n != 1 {
		t.Errorf("updatedAfter should match 1 card, got %d", n)
	}
	// Empty condition is always true.
	if n := count(map[string]any{}); n != 2 {
		t.Errorf("empty filter should match all cards, got %d", n)
	}
	// Multiple conditions are ANDed.
	if n := count(map[string]any{"inAddressBook": ab1, "kind": "group"}); n != 0 {
		t.Errorf("AND of inAddressBook+kind should match none, got %d", n)
	}
	// text token search: all whitespace-separated tokens must be present.
	if n := count(map[string]any{"text": "quarterly figures"}); n != 1 {
		t.Errorf("text tokens should match the note, got %d", n)
	}
	if n := count(map[string]any{"text": "quarterly missing"}); n != 0 {
		t.Errorf("text with a missing token should match none, got %d", n)
	}
	// Quoted phrase must match the exact sequence.
	if n := count(map[string]any{"text": `"quarterly figures"`}); n != 1 {
		t.Errorf("quoted phrase should match, got %d", n)
	}
}
