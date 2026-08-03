package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// postJMAP is defined in query_pagination_test.go.

// TestQueryChangesFilterReevaluation verifies /queryChanges deltas respect the query's
// filter (RFC 8620 Section 5.6): objects that changed since the client's query state but
// do not match the filter MUST NOT appear in "added", and matching objects MUST be added
// at their real index within the filtered result set.
func TestQueryChangesFilterReevaluation(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		return postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, calls)
	}

	// 1. Baseline query states using the same filter the deltas will be computed against.
	r1 := post([]any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}}, "c1"},
		[]any{"EmailSubmission/query", map[string]any{"accountId": "primary"}, "c2"},
	})
	eState0, _ := r1.MethodResponses[0].Args["queryState"].(string)
	sState0, _ := r1.MethodResponses[1].Args["queryState"].(string)

	// 2. Create an email OUTSIDE mb-inbox (must be filtered out of the deltas), one INSIDE
	//    mb-inbox (must be added), and submissions referencing different emails so the
	//    emailIds filter splits them.
	r2 := post([]any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{"subject": "Filtered Out Email"},
				"e2": map[string]any{"subject": "Filtered In Email", "mailboxIds": map[string]any{"mb-inbox": true}},
			},
		}, "c3"},
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"s1": map[string]any{"identityId": "id-primary", "emailId": "email-3"},
				"s2": map[string]any{"identityId": "id-primary", "emailId": "email-1"},
			},
		}, "c4"},
	})
	createdEmails, _ := r2.MethodResponses[0].Args["created"].(map[string]any)
	e1Obj, _ := createdEmails["e1"].(map[string]any)
	e1ID, _ := e1Obj["id"].(string)
	e2Obj, _ := createdEmails["e2"].(map[string]any)
	e2ID, _ := e2Obj["id"].(string)
	createdSubs, _ := r2.MethodResponses[1].Args["created"].(map[string]any)
	s2Obj, _ := createdSubs["s2"].(map[string]any)

	// 3. Deltas computed with the same filters: only e2 and s2 match.
	r3 := post([]any{
		[]any{"Email/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": eState0,
			"filter":          map[string]any{"inMailbox": "mb-inbox"},
		}, "c5"},
		[]any{"EmailSubmission/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": sState0,
			"filter":          map[string]any{"emailIds": []any{"email-1"}},
		}, "c6"},
	})

	eAdded := r3.MethodResponses[0].Args["added"].([]any)
	if len(eAdded) != 1 {
		t.Fatalf("Expected exactly 1 added email (the inbox one), got %v", eAdded)
	}
	addedEm := eAdded[0].(map[string]any)
	if addedEm["id"] != e2ID {
		t.Errorf("Expected added email %q (matching filter), got %v (filtered-out email %q must not be added)", e2ID, addedEm, e1ID)
	}
	if idx, _ := addedEm["index"].(float64); idx != 0 {
		t.Errorf("Expected added email at index 0 in the filtered results, got %v", idx)
	}

	sAdded := r3.MethodResponses[1].Args["added"].([]any)
	if len(sAdded) != 1 {
		t.Fatalf("Expected exactly 1 added submission (the email-1 one), got %v", sAdded)
	}
	addedSub := sAdded[0].(map[string]any)
	if addedSub["id"] != s2Obj["id"] {
		t.Errorf("Expected added submission %v (matching emailIds filter), got %v (submission for email-3 must not be added)", s2Obj["id"], addedSub)
	}
	if idx, _ := addedSub["index"].(float64); idx != 0 {
		t.Errorf("Expected added submission at index 0 in the filtered results, got %v", idx)
	}
}

// TestQueryChangesUpToId verifies the upToId argument truncates the added array per RFC 8620
// Section 5.6: when upToId exists in the results, added ids with a higher index than the
// anchor are omitted, and the total is reported when calculateTotal is requested.
func TestQueryChangesUpToId(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		return postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, calls)
	}

	// 1. Baseline states.
	r1 := post([]any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "subject", "isAscending": true}}, "filter": map[string]any{"inMailbox": "mb-inbox"}}, "c1"},
		[]any{"EmailSubmission/query", map[string]any{"accountId": "primary"}, "c2"},
	})
	eState0, _ := r1.MethodResponses[0].Args["queryState"].(string)
	sState0, _ := r1.MethodResponses[1].Args["queryState"].(string)

	// 2. Create two inbox emails ("aaa" sorts before "bbb" under the subject sort) and two
	//    submissions with explicit sendAt values (sendAt descending is the default sort).
	r2 := post([]any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{"subject": "aaa Anchor Email", "mailboxIds": map[string]any{"mb-inbox": true}},
				"e2": map[string]any{"subject": "bbb Beyond Email", "mailboxIds": map[string]any{"mb-inbox": true}},
			},
		}, "c3"},
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"s1": map[string]any{"identityId": "id-primary", "emailId": "email-1", "sendAt": "2026-01-15T10:00:00Z"},
				"s2": map[string]any{"identityId": "id-primary", "emailId": "email-1", "sendAt": "2026-02-15T10:00:00Z"},
			},
		}, "c4"},
	})
	createdEmails, _ := r2.MethodResponses[0].Args["created"].(map[string]any)
	e1Obj, _ := createdEmails["e1"].(map[string]any)
	e1ID, _ := e1Obj["id"].(string)
	createdSubs, _ := r2.MethodResponses[1].Args["created"].(map[string]any)
	s2Obj, _ := createdSubs["s2"].(map[string]any)

	// 3a. Without upToId both new emails are added at their real sorted indices 0 and 1.
	r3a := post([]any{
		[]any{"Email/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": eState0,
			"sort":            []any{map[string]any{"property": "subject", "isAscending": true}},
			"filter":          map[string]any{"inMailbox": "mb-inbox"},
			"calculateTotal":  true,
		}, "c5"},
		[]any{"EmailSubmission/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": sState0,
		}, "c6"},
	})
	eAdded := r3a.MethodResponses[0].Args["added"].([]any)
	if len(eAdded) != 2 {
		t.Fatalf("Expected 2 added emails without upToId, got %v", eAdded)
	}
	firstEm := eAdded[0].(map[string]any)
	if firstEm["id"] != e1ID {
		t.Errorf("Expected first added email %q (subject sort), got %v", e1ID, firstEm)
	}
	if total, _ := r3a.MethodResponses[0].Args["total"].(float64); total != 4 {
		t.Errorf("Expected calculateTotal total 4 (2 seeded + 2 new), got %v", total)
	}

	// 3b. With upToId=e1 (index 0), the beyond-anchor email e2 must be truncated from added.
	r3b := post([]any{
		[]any{"Email/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": eState0,
			"sort":            []any{map[string]any{"property": "subject", "isAscending": true}},
			"filter":          map[string]any{"inMailbox": "mb-inbox"},
			"upToId":          e1ID,
		}, "c7"},
		[]any{"EmailSubmission/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": sState0,
			"upToId":          s2Obj["id"],
		}, "c8"},
	})
	eAddedUpTo := r3b.MethodResponses[0].Args["added"].([]any)
	if len(eAddedUpTo) != 1 {
		t.Fatalf("Expected upToId to truncate added to 1 email, got %v", eAddedUpTo)
	}
	upToEm := eAddedUpTo[0].(map[string]any)
	if upToEm["id"] != e1ID {
		t.Errorf("Expected only %q added with upToId, got %v", e1ID, upToEm)
	}

	// Submission deltas sort sendAt descending: [s2, s1]. upToId=s2 (index 0) truncates s1.
	sAddedUpTo := r3b.MethodResponses[1].Args["added"].([]any)
	if len(sAddedUpTo) != 1 {
		t.Fatalf("Expected upToId to truncate submission added to 1, got %v", sAddedUpTo)
	}
	upToSub := sAddedUpTo[0].(map[string]any)
	if upToSub["id"] != s2Obj["id"] {
		t.Errorf("Expected only %v added with upToId, got %v", s2Obj["id"], upToSub)
	}
}

// TestQueryChangesUpdatedReAdded verifies that an updated object is reported in both removed
// and added (re-added at its current index in the filtered results) per RFC 8620 Section 5.6,
// and that a destroyed object only appears in removed.
func TestQueryChangesUpdatedReAdded(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		return postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, calls)
	}

	// 1. Create two inbox emails and capture the post-create state so the later update and
	//    destroy fall in a fresh change window (an update folded into a create's window is
	//    correctly reported as a plain create).
	r2 := post([]any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{"subject": "Update Me", "mailboxIds": map[string]any{"mb-inbox": true}},
				"e2": map[string]any{"subject": "Destroy Me", "mailboxIds": map[string]any{"mb-inbox": true}},
			},
		}, "c2"},
	})
	eState1, _ := r2.MethodResponses[0].Args["newState"].(string)
	createdEmails, _ := r2.MethodResponses[0].Args["created"].(map[string]any)
	e1Obj, _ := createdEmails["e1"].(map[string]any)
	e1ID, _ := e1Obj["id"].(string)
	e2Obj, _ := createdEmails["e2"].(map[string]any)
	e2ID, _ := e2Obj["id"].(string)

	r3 := post([]any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"update":    map[string]any{e1ID: map[string]any{"keywords/$starred": true}},
			"destroy":   []any{e2ID},
		}, "c3"},
	})
	if _, ok := r3.MethodResponses[0].Args["updated"].(map[string]any); !ok {
		t.Fatalf("keyword update failed: %#v", r3.MethodResponses[0].Args)
	}
	destroyed, _ := r3.MethodResponses[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != e2ID {
		t.Fatalf("expected email %q destroyed, got %v", e2ID, destroyed)
	}

	// 3. Deltas since the post-create state: e1 appears in both removed and added (real
	//    index), e2 only in removed, and the seeded emails are unaffected.
	r4 := post([]any{
		[]any{"Email/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": eState1,
			"filter":          map[string]any{"inMailbox": "mb-inbox"},
		}, "c4"},
	})

	added := r4.MethodResponses[0].Args["added"].([]any)
	if len(added) != 1 {
		t.Fatalf("Expected 1 re-added (updated) email, got %v", added)
	}
	addedEm := added[0].(map[string]any)
	if addedEm["id"] != e1ID {
		t.Errorf("Expected re-added updated email %q, got %v", e1ID, addedEm)
	}

	removed := r4.MethodResponses[0].Args["removed"].([]any)
	removedIDs := make(map[string]bool, len(removed))
	for _, raw := range removed {
		removedIDs[raw.(string)] = true
	}
	if !removedIDs[e1ID] {
		t.Errorf("Expected updated email %q in removed, got %v", e1ID, removed)
	}
	if !removedIDs[e2ID] {
		t.Errorf("Expected destroyed email %q in removed, got %v", e2ID, removed)
	}
	if len(removed) != 2 {
		t.Errorf("Expected exactly removed {updated, destroyed}, got %v", removed)
	}

	// The re-added index must match the email's real position in a fresh filtered query.
	verify := post([]any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}}, "c5"},
	})
	currentIDs := idsOf(t, verify.MethodResponses[0].Args["ids"])
	wantIndex := -1
	for i, id := range currentIDs {
		if id == e1ID {
			wantIndex = i
		}
	}
	if wantIndex == -1 {
		t.Fatal("updated email missing from current query results")
	}
	if idx, _ := addedEm["index"].(float64); int(idx) != wantIndex {
		t.Errorf("Expected re-added index %d (real position), got %v", wantIndex, idx)
	}
}
