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
	//    mb-inbox (must be added), and submissions referencing those emails via creation
	//    references so the emailIds filter splits them. The created emails have no
	//    recipient headers, so no delivery copies are produced.
	r2 := post([]any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{"subject": "Filtered Out Email", "to": []any{map[string]any{"email": "querychanges@example.com"}}, "mailboxIds": map[string]any{"mb-archive": true}},
				"e2": map[string]any{"subject": "Filtered In Email", "to": []any{map[string]any{"email": "querychanges@example.com"}}, "mailboxIds": map[string]any{"mb-inbox": true}},
			},
		}, "c3"},
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"s1": map[string]any{"identityId": "id-primary", "emailId": "#e1"},
				"s2": map[string]any{"identityId": "id-primary", "emailId": "#e2"},
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
			"filter":          map[string]any{"emailIds": []any{e2ID}},
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
	//    submissions referencing them via creation references with explicit sendAt values
	//    (sendAt descending is the default sort).
	r2 := post([]any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{"subject": "aaa Anchor Email", "to": []any{map[string]any{"email": "querychanges@example.com"}}, "mailboxIds": map[string]any{"mb-inbox": true}},
				"e2": map[string]any{"subject": "bbb Beyond Email", "to": []any{map[string]any{"email": "querychanges@example.com"}}, "mailboxIds": map[string]any{"mb-inbox": true}},
			},
		}, "c3"},
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"s1": map[string]any{"identityId": "id-primary", "emailId": "#e1", "sendAt": "2026-01-15T10:00:00Z"},
				"s2": map[string]any{"identityId": "id-primary", "emailId": "#e2", "sendAt": "2026-02-15T10:00:00Z"},
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

// TestRFC9661_SieveScriptQueryChanges verifies the registered SieveScript/queryChanges
// (RFC 9661 Section 2.5 / RFC 8620 Section 5.6): deltas respect the isActive filter,
// updated/destroyed scripts are removed, created/updated scripts are re-added at their real
// sorted index, upToId truncates added ids beyond the anchor, and the /query response
// carries a real query state token so the flow is usable.
func TestRFC9661_SieveScriptQueryChanges(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}
	post := func(calls []any) jmap.Response {
		return postJMAP(t, ts.URL, using, calls)
	}

	validScript := `require ["fileinto"]; if header :contains "X-Spam" "Yes" { fileinto "Junk"; }`

	// 1. Create two scripts and capture the post-create query state. Names sort
	//    alphabetically ("Alpha" before "Beta"), and the query state must be a real token
	//    (not a hardcoded "0") so the client can round-trip it into queryChanges.
	r1 := post([]any{
		[]any{"SieveScript/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"s1": map[string]any{"name": "Alpha", "content": validScript, "isActive": true},
				"s2": map[string]any{"name": "Beta", "content": validScript, "isActive": false},
			},
		}, "c1"},
	})
	createdScripts, _ := r1.MethodResponses[0].Args["created"].(map[string]any)
	s1Obj, _ := createdScripts["s1"].(map[string]any)
	s1ID, _ := s1Obj["id"].(string)
	s2Obj, _ := createdScripts["s2"].(map[string]any)
	s2ID, _ := s2Obj["id"].(string)

	stateResp := post([]any{
		[]any{"SieveScript/query", map[string]any{"accountId": "primary"}, "c2"},
	})
	qState, _ := stateResp.MethodResponses[0].Args["queryState"].(string)
	if qState == "" || qState == "0" {
		t.Fatalf("SieveScript/query must return a real query state token, got %q", qState)
	}

	// 2. Create Gamma (inactive), activate Beta (which deactivates Alpha), destroy Alpha.
	//    Activating a script at the same time as creating one would deactivate the new
	//    script, so Gamma is created inactive.
	r2 := post([]any{
		[]any{"SieveScript/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"s3": map[string]any{"name": "Gamma", "content": validScript}},
			"update":    map[string]any{s2ID: map[string]any{"isActive": true}},
			"destroy":   []any{s1ID},
		}, "c3"},
	})
	destroyed, _ := r2.MethodResponses[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != s1ID {
		t.Fatalf("expected script %q destroyed, got %v", s1ID, destroyed)
	}
	createdScripts3, _ := r2.MethodResponses[0].Args["created"].(map[string]any)
	s3Obj, _ := createdScripts3["s3"].(map[string]any)
	s3ID, _ := s3Obj["id"].(string)
	if s3ID == "" {
		t.Fatal("created script has no id")
	}

	// 3. Deltas since the post-create state with the isActive filter: only Beta (updated to
	//    active) is re-added at index 0; created Gamma is inactive so the filter excludes
	//    it; destroyed Alpha is only removed.
	r3 := post([]any{
		[]any{"SieveScript/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": qState,
			"filter":          map[string]any{"isActive": true},
		}, "c4"},
	})
	mr := r3.MethodResponses[0]
	if mr.Name != "SieveScript/queryChanges" {
		t.Fatalf("SieveScript/queryChanges must be registered, got %q", mr.Name)
	}
	added := mr.Args["added"].([]any)
	if len(added) != 1 {
		t.Fatalf("Expected 1 added script under isActive filter (updated Beta), got %v", added)
	}
	onlyAdded := added[0].(map[string]any)
	if onlyAdded["id"] != s2ID {
		t.Errorf("Expected added %q (updated Beta, now active), got %v", s2ID, onlyAdded)
	}
	if idx, _ := onlyAdded["index"].(float64); idx != 0 {
		t.Errorf("Expected added script at index 0, got %v", idx)
	}
	removed := mr.Args["removed"].([]any)
	removedSet := make(map[string]bool, len(removed))
	for _, raw := range removed {
		removedSet[raw.(string)] = true
	}
	if !removedSet[s1ID] {
		t.Errorf("Expected destroyed script %q in removed, got %v", s1ID, removed)
	}
	if !removedSet[s2ID] {
		t.Errorf("Expected updated script %q in removed, got %v", s2ID, removed)
	}
	if len(removed) != 2 {
		t.Errorf("Expected exactly removed {updated, destroyed}, got %v", removed)
	}

	// 4. Without the filter, created Gamma joins updated Beta at its real sorted index 1.
	r4 := post([]any{
		[]any{"SieveScript/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": qState,
		}, "c5"},
	})
	addedAll := r4.MethodResponses[0].Args["added"].([]any)
	if len(addedAll) != 2 {
		t.Fatalf("Expected 2 added scripts without filter (Beta + Gamma), got %v", addedAll)
	}
	firstAll := addedAll[0].(map[string]any)
	if firstAll["id"] != s2ID {
		t.Errorf("Expected first added %q (name order), got %v", s2ID, firstAll)
	}
	secondAll := addedAll[1].(map[string]any)
	if secondAll["id"] != s3ID {
		t.Errorf("Expected second added %q (created Gamma), got %v", s3ID, secondAll)
	}
	if idx, _ := secondAll["index"].(float64); idx != 1 {
		t.Errorf("Expected second added script at index 1, got %v", idx)
	}

	// 5. upToId truncates added ids with a higher index than the anchor (Beta is index 0).
	r5 := post([]any{
		[]any{"SieveScript/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": qState,
			"upToId":          s2ID,
		}, "c6"},
	})
	addedUpTo := r5.MethodResponses[0].Args["added"].([]any)
	if len(addedUpTo) != 1 {
		t.Fatalf("Expected upToId to truncate sieve added to 1, got %v", addedUpTo)
	}
	if upTo := addedUpTo[0].(map[string]any); upTo["id"] != s2ID {
		t.Errorf("Expected only %q added with upToId, got %v", s2ID, upTo)
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
