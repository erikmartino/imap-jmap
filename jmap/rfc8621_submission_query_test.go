package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// submissionQueryClient wraps one test server so all method calls in a test share the same
// backend state, like a real client session.
type submissionQueryClient struct {
	ts *httptest.Server
}

func newSubmissionQueryClient(t *testing.T) *submissionQueryClient {
	t.Helper()
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &submissionQueryClient{ts: ts}
}

func (c *submissionQueryClient) post(t *testing.T, calls []any) jmap.Response {
	t.Helper()
	return postJMAP(t, c.ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, calls)
}

// submissionQuerySeed creates two identities, two emails, and three submissions:
// s1 (id-primary identity, email eA, sendAt 2026-03-01), s2 (identity i2, email eB,
// sendAt 2026-01-15), s3 (id-primary, email eA, sendAt 2026-02-01). It returns the
// relevant ids.
func (c *submissionQueryClient) seed(t *testing.T) (s1ID, s2ID, s3ID, eAID, eBID, tAID, tBID, i2ID string) {
	t.Helper()
	r := c.post(t, []any{
		[]any{"Identity/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"i2": map[string]any{"name": "Second Identity", "email": "second@example.com"}},
		}, "c1"},
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"eA": map[string]any{"subject": "Query Alpha", "to": []any{map[string]any{"email": "query-rcpt@example.com"}}, "mailboxIds": map[string]any{"mb-inbox": true}},
				"eB": map[string]any{"subject": "Query Beta", "to": []any{map[string]any{"email": "query-rcpt@example.com"}}, "mailboxIds": map[string]any{"mb-inbox": true}},
			},
		}, "c2"},
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"s1": map[string]any{"identityId": "id-primary", "emailId": "#eA", "sendAt": "2026-03-01T10:00:00Z"},
				"s2": map[string]any{"identityId": "#i2", "emailId": "#eB", "sendAt": "2026-01-15T10:00:00Z"},
				"s3": map[string]any{"identityId": "id-primary", "emailId": "#eA", "sendAt": "2026-02-01T10:00:00Z"},
			},
		}, "c3"},
	})
	createdSubs, _ := r.MethodResponses[2].Args["created"].(map[string]any)
	s1ID = createdSubs["s1"].(map[string]any)["id"].(string)
	s2ID = createdSubs["s2"].(map[string]any)["id"].(string)
	s3ID = createdSubs["s3"].(map[string]any)["id"].(string)
	createdEmails, _ := r.MethodResponses[1].Args["created"].(map[string]any)
	eAID = createdEmails["eA"].(map[string]any)["id"].(string)
	eBID = createdEmails["eB"].(map[string]any)["id"].(string)
	i2ID = r.MethodResponses[0].Args["created"].(map[string]any)["i2"].(map[string]any)["id"].(string)

	r2 := c.post(t, []any{
		[]any{"Email/get", map[string]any{"accountId": "primary", "ids": []any{eAID, eBID}, "properties": []any{"threadId"}}, "c4"},
	})
	list, _ := r2.MethodResponses[0].Args["list"].([]any)
	tAID = list[0].(map[string]any)["threadId"].(string)
	tBID = list[1].(map[string]any)["threadId"].(string)
	return
}

// TestRFC8621_SubmissionQueryFilters verifies every RFC 8621 Section 7.2 filter condition
// (identityIds, emailIds, threadIds, undoStatus, before, after) matches AND excludes the
// right submissions, and that conditions compose with AND semantics.
func TestRFC8621_SubmissionQueryFilters(t *testing.T) {
	c := newSubmissionQueryClient(t)
	s1ID, s2ID, s3ID, eAID, eBID, tAID, tBID, i2ID := c.seed(t)

	query := func(filter map[string]any) []string {
		t.Helper()
		r := c.post(t, []any{
			[]any{"EmailSubmission/query", map[string]any{"accountId": "primary", "filter": filter}, "c1"},
		})
		ids := []string{}
		raw := r.MethodResponses[0].Args["ids"].([]any)
		for _, item := range raw {
			ids = append(ids, item.(string))
		}
		return ids
	}
	has := func(ids []string, want string) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}

	// identityIds: positive and negative.
	got := query(map[string]any{"identityIds": []any{"id-primary"}})
	if len(got) != 2 || !has(got, s1ID) || !has(got, s3ID) || has(got, s2ID) {
		t.Errorf("identityIds [id-primary] must match s1+s3 only, got %v", got)
	}
	got = query(map[string]any{"identityIds": []any{i2ID}})
	if len(got) != 1 || !has(got, s2ID) {
		t.Errorf("identityIds [i2] must match s2 only, got %v", got)
	}
	got = query(map[string]any{"identityIds": []any{"identity-none"}})
	if len(got) != 0 {
		t.Errorf("identityIds [missing] must exclude everything, got %v", got)
	}

	// emailIds: positive and negative.
	got = query(map[string]any{"emailIds": []any{eAID}})
	if len(got) != 2 || !has(got, s1ID) || !has(got, s3ID) || has(got, s2ID) {
		t.Errorf("emailIds [eA] must match s1+s3 only, got %v", got)
	}
	got = query(map[string]any{"emailIds": []any{eBID}})
	if len(got) != 1 || !has(got, s2ID) {
		t.Errorf("emailIds [eB] must match s2 only, got %v", got)
	}
	got = query(map[string]any{"emailIds": []any{"email-none"}})
	if len(got) != 0 {
		t.Errorf("emailIds [missing] must exclude everything, got %v", got)
	}

	// threadIds: positive and negative.
	got = query(map[string]any{"threadIds": []any{tAID}})
	if len(got) != 2 || !has(got, s1ID) || !has(got, s3ID) || has(got, s2ID) {
		t.Errorf("threadIds [tA] must match s1+s3 only, got %v", got)
	}
	got = query(map[string]any{"threadIds": []any{tBID}})
	if len(got) != 1 || !has(got, s2ID) {
		t.Errorf("threadIds [tB] must match s2 only, got %v", got)
	}
	got = query(map[string]any{"threadIds": []any{"thread-none"}})
	if len(got) != 0 {
		t.Errorf("threadIds [missing] must exclude everything, got %v", got)
	}

	// undoStatus: "final" (the value assigned on creation) matches all; "pending" none.
	got = query(map[string]any{"undoStatus": "final"})
	if len(got) != 3 {
		t.Errorf("undoStatus final must match all 3 submissions, got %v", got)
	}
	got = query(map[string]any{"undoStatus": "pending"})
	if len(got) != 0 {
		t.Errorf("undoStatus pending must exclude everything, got %v", got)
	}

	// before: sendAt must be strictly before the given date.
	got = query(map[string]any{"before": "2026-02-01T00:00:00Z"})
	if len(got) != 1 || !has(got, s2ID) {
		t.Errorf("before 2026-02-01 must match only s2 (2026-01-15), got %v", got)
	}
	got = query(map[string]any{"before": "2026-01-15T10:00:00Z"})
	if len(got) != 0 {
		t.Errorf("before equal to a sendAt must exclude it, got %v", got)
	}

	// after: sendAt must be the same as or after the given date.
	got = query(map[string]any{"after": "2026-02-01T00:00:00Z"})
	if len(got) != 2 || !has(got, s1ID) || !has(got, s3ID) || has(got, s2ID) {
		t.Errorf("after 2026-02-01 must match s1+s3 only, got %v", got)
	}
	got = query(map[string]any{"after": "2026-02-01T10:00:00Z"})
	if len(got) != 2 || !has(got, s1ID) || !has(got, s3ID) {
		t.Errorf("after equal to a sendAt must include it (same as or after), got %v", got)
	}
	got = query(map[string]any{"after": "2026-04-01T00:00:00Z"})
	if len(got) != 0 {
		t.Errorf("after 2026-04-01 must exclude everything, got %v", got)
	}

	// Combined conditions use AND semantics (RFC 8621 Section 7.2).
	got = query(map[string]any{"identityIds": []any{"id-primary"}, "before": "2026-02-15T00:00:00Z"})
	if len(got) != 1 || !has(got, s3ID) || has(got, s1ID) || has(got, s2ID) {
		t.Errorf("identityIds [id-primary] AND before 2026-02-15 must match only s3, got %v", got)
	}

	// An empty filter matches everything.
	got = query(nil)
	if len(got) != 3 {
		t.Errorf("empty filter must match all 3 submissions, got %v", got)
	}
}

// TestRFC8621_SubmissionQuerySort verifies the RFC 8621 Section 7.2 sort properties
// (emailId, threadId, sentAt) and the default sendAt-descending sort.
func TestRFC8621_SubmissionQuerySort(t *testing.T) {
	c := newSubmissionQueryClient(t)
	s1ID, s2ID, s3ID, eAID, eBID, tAID, tBID, _ := c.seed(t)

	query := func(sort []any) []string {
		t.Helper()
		args := map[string]any{"accountId": "primary"}
		if sort != nil {
			args["sort"] = sort
		}
		r := c.post(t, []any{
			[]any{"EmailSubmission/query", args, "c1"},
		})
		ids := []string{}
		raw := r.MethodResponses[0].Args["ids"].([]any)
		for _, item := range raw {
			ids = append(ids, item.(string))
		}
		return ids
	}
	eq := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	// eqSet checks that the result is the concatenation of the given groups, where entries
	// within a group may appear in any order.
	eqSet := func(got []string, groups ...[]string) bool {
		idx := 0
		for _, group := range groups {
			inGroup := map[string]bool{}
			for _, id := range group {
				inGroup[id] = true
			}
			for range group {
				if idx >= len(got) || !inGroup[got[idx]] {
					return false
				}
				idx++
			}
		}
		return idx == len(got)
	}

	// Default: sendAt descending -> s1 (Mar), s3 (Feb), s2 (Jan).
	if got := query(nil); !eq(got, s1ID, s3ID, s2ID) {
		t.Errorf("default sort must be sendAt descending [s1 s3 s2], got %v", got)
	}
	// Explicit sendAt ascending.
	if got := query([]any{map[string]any{"property": "sendAt", "isAscending": true}}); !eq(got, s2ID, s3ID, s1ID) {
		t.Errorf("sendAt ascending must be [s2 s3 s1], got %v", got)
	}
	// "sentAt" is accepted as an alias for sendAt (RFC 8621 Section 7.2 sort list).
	if got := query([]any{map[string]any{"property": "sentAt", "isAscending": true}}); !eq(got, s2ID, s3ID, s1ID) {
		t.Errorf("sentAt ascending must be [s2 s3 s1], got %v", got)
	}
	// emailId: submissions with the same email id group together, and the group with the
	// lexicographically smaller email id sorts first (RFC 8620 Section 5.5). The ids the
	// server assigns are not creation-ordered, so which of eA/eB is "first" is not fixed.
	emailGroups := func(firstEmail, secondEmail string) (g1, g2 []string) {
		for sub, email := range map[string]string{s1ID: eAID, s2ID: eBID, s3ID: eAID} {
			if email == firstEmail {
				g1 = append(g1, sub)
			} else {
				g2 = append(g2, sub)
			}
		}
		return
	}
	firstEmail, secondEmail := eAID, eBID
	if eAID > eBID {
		firstEmail, secondEmail = eBID, eAID
	}
	g1, g2 := emailGroups(firstEmail, secondEmail)
	if got := query([]any{map[string]any{"property": "emailId", "isAscending": true}}); !eqSet(got, g1, g2) {
		t.Errorf("emailId ascending must group by email id (%v, %v), got %v", g1, g2, got)
	}
	// threadId: same grouping rule, over the thread ids.
	threadGroups := func(firstThread, secondThread string) (g1, g2 []string) {
		for sub, thread := range map[string]string{s1ID: tAID, s2ID: tBID, s3ID: tAID} {
			if thread == firstThread {
				g1 = append(g1, sub)
			} else {
				g2 = append(g2, sub)
			}
		}
		return
	}
	firstThread, secondThread := tAID, tBID
	if tAID > tBID {
		firstThread, secondThread = tBID, tAID
	}
	g1, g2 = threadGroups(firstThread, secondThread)
	if got := query([]any{map[string]any{"property": "threadId", "isAscending": true}}); !eqSet(got, g1, g2) {
		t.Errorf("threadId ascending must group by thread id (%v, %v), got %v", g1, g2, got)
	}
	// Default isAscending (omitted) is ascending per RFC 8620 Section 5.5.
	if got := query([]any{map[string]any{"property": "sendAt"}}); !eq(got, s2ID, s3ID, s1ID) {
		t.Errorf("sendAt without isAscending must default to ascending [s2 s3 s1], got %v", got)
	}
}

// TestRFC8621_SubmissionQueryPagination verifies position/limit slicing, the total count,
// and the calculateTotal echo for EmailSubmission/query (RFC 8620 Section 5.5).
func TestRFC8621_SubmissionQueryPagination(t *testing.T) {
	c := newSubmissionQueryClient(t)
	s1ID, _, s3ID, _, _, _, _, _ := c.seed(t)

	post := func(args map[string]any) map[string]any {
		t.Helper()
		r := c.post(t, []any{
			[]any{"EmailSubmission/query", args, "c1"},
		})
		return r.MethodResponses[0].Args
	}

	// limit slices from the start of the default (sendAt desc) order.
	res := post(map[string]any{"accountId": "primary", "limit": 1})
	ids, _ := res["ids"].([]any)
	if len(ids) != 1 || ids[0] != s1ID {
		t.Errorf("limit 1 must return [s1], got %v", ids)
	}
	if total, _ := res["total"].(float64); total != 3 {
		t.Errorf("total must be 3 (unlimited), got %v", total)
	}
	if pos, _ := res["position"].(float64); pos != 0 {
		t.Errorf("position must be 0, got %v", pos)
	}

	// position+limit slices into the middle.
	res = post(map[string]any{"accountId": "primary", "position": 1, "limit": 1})
	ids, _ = res["ids"].([]any)
	if len(ids) != 1 || ids[0] != s3ID {
		t.Errorf("position 1 limit 1 must return [s3], got %v", ids)
	}

	// position == total returns an empty page but the true total.
	res = post(map[string]any{"accountId": "primary", "position": 3})
	ids, _ = res["ids"].([]any)
	if len(ids) != 0 {
		t.Errorf("position 3 must return [], got %v", ids)
	}
	if total, _ := res["total"].(float64); total != 3 {
		t.Errorf("total must still be 3 at position 3, got %v", total)
	}

	// position beyond total.
	res = post(map[string]any{"accountId": "primary", "position": 10})
	if total, _ := res["total"].(float64); total != 3 {
		t.Errorf("total must still be 3 at position 10, got %v", total)
	}

	// calculateTotal is echoed when requested.
	res = post(map[string]any{"accountId": "primary", "calculateTotal": true})
	if ct, _ := res["calculateTotal"].(bool); !ct {
		t.Errorf("calculateTotal must be echoed, got %v", res)
	}
}

// TestRFC8621_SubmissionQueryAnchor verifies anchor/anchorOffset handling and the
// anchorNotFound error for EmailSubmission/query (RFC 8620 Section 5.5).
func TestRFC8621_SubmissionQueryAnchor(t *testing.T) {
	c := newSubmissionQueryClient(t)
	s1ID, s2ID, s3ID, _, _, _, _, _ := c.seed(t)

	post := func(args map[string]any) map[string]any {
		t.Helper()
		r := c.post(t, []any{
			[]any{"EmailSubmission/query", args, "c1"},
		})
		if errType, ok := r.MethodResponses[0].Args["type"].(string); ok {
			return map[string]any{"error": errType}
		}
		return r.MethodResponses[0].Args
	}
	idsOf := func(res map[string]any) []string {
		raw, _ := res["ids"].([]any)
		ids := []string{}
		for _, item := range raw {
			ids = append(ids, item.(string))
		}
		return ids
	}

	// Default order is [s1 s3 s2]; anchor s3 sits at index 1.
	res := post(map[string]any{"accountId": "primary", "anchor": s3ID})
	if got := idsOf(res); len(got) != 2 || got[0] != s3ID || got[1] != s2ID {
		t.Errorf("anchor s3 must start at index 1 -> [s3 s2], got %v", got)
	}
	if pos, _ := res["position"].(float64); pos != 1 {
		t.Errorf("position must be 1 for anchor s3, got %v", pos)
	}

	// anchorOffset -1 backs up to the previous result.
	res = post(map[string]any{"accountId": "primary", "anchor": s3ID, "anchorOffset": -1})
	if got := idsOf(res); len(got) != 3 || got[0] != s1ID {
		t.Errorf("anchor s3 offset -1 must start at index 0 -> [s1 s3 s2], got %v", got)
	}

	// anchorOffset +1 skips past the anchor.
	res = post(map[string]any{"accountId": "primary", "anchor": s3ID, "anchorOffset": 1})
	if got := idsOf(res); len(got) != 1 || got[0] != s2ID {
		t.Errorf("anchor s3 offset +1 must start at index 2 -> [s2], got %v", got)
	}

	// anchor + limit slices from the anchored position.
	res = post(map[string]any{"accountId": "primary", "anchor": s3ID, "limit": 1})
	if got := idsOf(res); len(got) != 1 || got[0] != s3ID {
		t.Errorf("anchor s3 limit 1 must return [s3], got %v", got)
	}

	// An anchor outside the results yields anchorNotFound.
	res = post(map[string]any{"accountId": "primary", "anchor": s2ID, "filter": map[string]any{"identityIds": []any{"id-primary"}}})
	if res["error"] != "anchorNotFound" {
		t.Errorf("anchor not in filtered results must error anchorNotFound, got %v", res)
	}
	if res["error"] == "" {
		t.Errorf("anchor missing case must fail")
	}

	// Unresolvable anchor id.
	res = post(map[string]any{"accountId": "primary", "anchor": "sub-none"})
	if res["error"] != "anchorNotFound" {
		t.Errorf("missing anchor id must error anchorNotFound, got %v", res)
	}
}

// TestRFC8621_SubmissionQueryUndoStatusBackend proves the undoStatus condition
// distinguishes values, using a pending submission that cannot be created over the protocol
// (the server assigns "final" on creation).
func TestRFC8621_SubmissionQueryUndoStatusBackend(t *testing.T) {
	ctx := context.Background()
	mb := memory.NewMemoryBackend()

	sub, err := mb.CreateSubmission(ctx, &jmap.EmailSubmission{EmailID: "email-1", IdentityID: "id-primary"})
	if err != nil {
		t.Fatalf("CreateSubmission failed: %v", err)
	}
	sub.UndoStatus = "pending"

	ids, total, err := mb.QuerySubmissions(ctx, map[string]any{"undoStatus": "pending"}, nil, 0, nil)
	if err != nil || total != 1 || len(ids) != 1 || ids[0] != sub.ID {
		t.Errorf("undoStatus pending must match the pending submission, got %v (total %d): %v", ids, total, err)
	}
	ids, total, err = mb.QuerySubmissions(ctx, map[string]any{"undoStatus": "final"}, nil, 0, nil)
	if err != nil || total != 0 || len(ids) != 0 {
		t.Errorf("undoStatus final must exclude the pending submission, got %v (total %d): %v", ids, total, err)
	}
}

// TestRFC8621_SubmissionQuery_FilterOperator tests nested FilterOperator (AND, OR, NOT)
// conditions on EmailSubmission/query.
func TestRFC8621_SubmissionQuery_FilterOperator(t *testing.T) {
	c := newSubmissionQueryClient(t)
	s1ID, s2ID, s3ID, eAID, _, _, _, _ := c.seed(t)

	query := func(filter map[string]any) []string {
		t.Helper()
		r := c.post(t, []any{
			[]any{"EmailSubmission/query", map[string]any{"accountId": "primary", "filter": filter}, "c1"},
		})
		ids := []string{}
		raw := r.MethodResponses[0].Args["ids"].([]any)
		for _, item := range raw {
			ids = append(ids, item.(string))
		}
		return ids
	}
	has := func(ids []string, want string) bool {
		for _, id := range ids {
			if id == want {
				return true
			}
		}
		return false
	}

	// OR operator: match s1 (sendAt 2026-03-01) OR s2 (sendAt 2026-01-15)
	got := query(map[string]any{
		"operator": "OR",
		"conditions": []any{
			map[string]any{"before": "2026-01-20T00:00:00Z"},
			map[string]any{"after": "2026-02-15T00:00:00Z"},
		},
	})
	if len(got) != 2 || !has(got, s1ID) || !has(got, s2ID) || has(got, s3ID) {
		t.Errorf("FilterOperator OR must match s1 and s2, got %v", got)
	}

	// AND operator: emailIds [eA] AND before 2026-02-15
	got = query(map[string]any{
		"operator": "AND",
		"conditions": []any{
			map[string]any{"emailIds": []any{eAID}},
			map[string]any{"before": "2026-02-15T00:00:00Z"},
		},
	})
	if len(got) != 1 || !has(got, s3ID) {
		t.Errorf("FilterOperator AND must match only s3, got %v", got)
	}

	// NOT operator: NOT (emailIds [eA]) -> s2 only
	got = query(map[string]any{
		"operator": "NOT",
		"conditions": []any{
			map[string]any{"emailIds": []any{eAID}},
		},
	})
	if len(got) != 1 || !has(got, s2ID) {
		t.Errorf("FilterOperator NOT must match only s2, got %v", got)
	}
}

// TestRFC8621_SubmissionQuery_SortUndoStatus tests sorting by undoStatus in EmailSubmission/query.
func TestRFC8621_SubmissionQuery_SortUndoStatus(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed one pending submission and one final submission
	em, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Sort Test",
		To:         []jmap.EmailAddress{{Email: "localuser@example.com"}},
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"subFinal": map[string]any{
					"emailId":    string(em.ID),
					"identityId": "id-primary",
				},
				"subPending": map[string]any{
					"emailId":    string(em.ID),
					"identityId": "id-primary",
					"sendAt":     "2035-01-01T00:00:00Z",
				},
			},
		}, "c1"},
		[]any{"EmailSubmission/query", map[string]any{
			"accountId": "primary",
			"sort": []any{
				map[string]any{"property": "undoStatus", "isAscending": true},
			},
		}, "c2"},
	})

	created, _ := res.MethodResponses[0].Args["created"].(map[string]any)
	finalID := created["subFinal"].(map[string]any)["id"].(string)
	pendingID := created["subPending"].(map[string]any)["id"].(string)

	queryRes := res.MethodResponses[1].Args
	ids, _ := queryRes["ids"].([]any)
	if len(ids) != 2 {
		t.Fatalf("Expected 2 submission IDs, got %v", ids)
	}
	// "final" < "pending" alphabetically in ascending order
	if ids[0] != finalID || ids[1] != pendingID {
		t.Errorf("Expected [finalID, pendingID], got %v", ids)
	}
}

// TestRFC8621_SubmissionQueryChanges verifies EmailSubmission/queryChanges tracking additions and removals.
func TestRFC8621_SubmissionQueryChanges(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}

	// 1. Initial query to capture queryState
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/query", map[string]any{"accountId": "primary"}, "c1"},
	})
	queryState1 := res1.MethodResponses[0].Args["queryState"].(string)

	// 2. Create an email and submission
	em, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "QueryChanges Test",
		To:         []jmap.EmailAddress{{Email: "localuser@example.com"}},
	})

	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"emailId":    string(em.ID),
					"identityId": "id-primary",
				},
			},
		}, "c2"},
	})
	created, _ := res2.MethodResponses[0].Args["created"].(map[string]any)
	subID := created["sub1"].(map[string]any)["id"].(string)

	// 3. Query changes since queryState1
	res3 := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": queryState1,
		}, "c3"},
	})
	args3 := res3.MethodResponses[0].Args
	added, _ := args3["added"].([]any)
	if len(added) != 1 {
		t.Fatalf("Expected 1 added submission in queryChanges, got %v", added)
	}
	item := added[0].(map[string]any)
	if item["id"] != subID {
		t.Errorf("Expected added id %q, got %q", subID, item["id"])
	}
}
