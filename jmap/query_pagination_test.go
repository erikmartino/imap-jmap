package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestQueryNegativePositionFromEnd verifies that a negative "position" counts from the end of
// the results per RFC 8620 Section 5.5 ("If a negative value is given, the position is counted
// from the end"): the effective position is total + position, clamped to 0. Every */query
// method must apply this semantics over live data, never panic or index out of range.
func TestQueryNegativePositionFromEnd(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cases := []struct {
		name   string
		using  []string
		method string
		filter map[string]any
		// stableOrder marks methods whose result order is deterministic (sorted).
		// Quota/query has no sort per RFC 9425 Section 4.4.1, so its order is
		// server-defined and may differ between calls.
		stableOrder bool
	}{
		{name: "Email/query", using: []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, method: "Email/query", filter: map[string]any{"inMailbox": "mb-inbox"}, stableOrder: true},
		{name: "Mailbox/query", using: []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, method: "Mailbox/query", stableOrder: true},
		{name: "EmailSubmission/query", using: []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, method: "EmailSubmission/query", stableOrder: true},
		{name: "Quota/query", using: []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI}, method: "Quota/query", stableOrder: false},
		{name: "CalendarEvent/query", using: []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}, method: "CalendarEvent/query", stableOrder: true},
		{name: "Card/query", using: []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}, method: "Card/query", stableOrder: true},
		{name: "SieveScript/query", using: []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}, method: "SieveScript/query", stableOrder: true},
		{name: "FileNode/query", using: []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI}, method: "FileNode/query", stableOrder: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			post := func(args map[string]any) map[string]any {
				t.Helper()
				reqPayload := map[string]any{
					"using":       tc.using,
					"methodCalls": []any{[]any{tc.method, args, "c1"}},
				}
				body, _ := json.Marshal(reqPayload)

				resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
				if err != nil {
					t.Fatalf("POST /jmap failed: %v", err)
				}
				defer resp.Body.Close()

				var jmapResp jmap.Response
				if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
					t.Fatalf("Failed to decode Response: %v", err)
				}
				if len(jmapResp.MethodResponses) != 1 {
					t.Fatalf("Expected 1 method response, got %d", len(jmapResp.MethodResponses))
				}
				mr := jmapResp.MethodResponses[0]
				if mr.Name != tc.method {
					t.Fatalf("Expected %s response, got %q", tc.method, mr.Name)
				}
				return mr.Args
			}

			baseArgs := map[string]any{"accountId": "primary", "calculateTotal": true}
			for k, v := range tc.filter {
				baseArgs[k] = v
			}

			// Learn the baseline result set so assertions hold for any seed data.
			baseline := post(baseArgs)
			raw, _ := baseline["ids"].([]any)
			if len(raw) == 0 {
				return // nothing to page; negative positions on empty results are vacuously empty
			}
			ids := make([]string, 0, len(raw))
			for _, item := range raw {
				ids = append(ids, item.(string))
			}
			total := len(ids)
			posOf := func(res map[string]any) int {
				if pos, _ := res["position"].(float64); pos != 0 {
					return int(pos)
				}
				return 0
			}

			// position -1: the last result, reported at index total-1.
			args := map[string]any{"accountId": "primary"}
			for k, v := range tc.filter {
				args[k] = v
			}
			args["position"] = -1
			res := post(args)
			got := []string{}
			for _, item := range res["ids"].([]any) {
				got = append(got, item.(string))
			}
			if len(got) != 1 {
				t.Errorf("position -1 must return exactly the last result, got %v", got)
			}
			if tc.stableOrder && got[0] != ids[total-1] {
				t.Errorf("position -1 must return the last result [%s], got %v", ids[total-1], got)
			}
			if posOf(res) != total-1 {
				t.Errorf("expected response position %d for position -1, got %d", total-1, posOf(res))
			}

			// position -(total+5): beyond the start, clamped to 0 -> the full result set.
			args["position"] = -(total + 5)
			res = post(args)
			got = got[:0]
			for _, item := range res["ids"].([]any) {
				got = append(got, item.(string))
			}
			if len(got) != total {
				t.Errorf("position -(total+5) must return all %d results, got %v", total, got)
			}
			if posOf(res) != 0 {
				t.Errorf("expected response position 0 for position -(total+5), got %d", posOf(res))
			}

			// Negative position combined with a limit slices from the from-end offset.
			if total >= 2 && tc.stableOrder {
				args["position"] = -2
				args["limit"] = 1
				res = post(args)
				got = got[:0]
				for _, item := range res["ids"].([]any) {
					got = append(got, item.(string))
				}
				if len(got) != 1 || got[0] != ids[total-2] {
					t.Errorf("position -2 limit 1 must return [%s], got %v", ids[total-2], got)
				}
			}
		})
	}
}

// TestQueryPagination verifies position/limit slicing behavior per RFC 8620 Section 5.5 on the
// seeded mail account: position beyond the end yields an empty ids array with the correct total,
// and a limit slices the results.
func TestQueryPagination(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// The default test server seeds 2 emails in mb-inbox.
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}, "calculateTotal": true}, "all"},
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}, "position": 1, "limit": 1, "calculateTotal": true}, "page"},
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}, "position": 50, "calculateTotal": true}, "beyond"},
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}, "limit": 0, "calculateTotal": true}, "zerolimit"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	allIDs := idsOf(t, jmapResp.MethodResponses[0].Args["ids"])
	if len(allIDs) != 2 {
		t.Fatalf("Expected 2 seeded inbox emails, got %v", allIDs)
	}
	if total, _ := jmapResp.MethodResponses[0].Args["total"].(float64); total != 2 {
		t.Errorf("Expected total 2, got %v", total)
	}

	pageIDs := idsOf(t, jmapResp.MethodResponses[1].Args["ids"])
	if len(pageIDs) != 1 || pageIDs[0] != allIDs[1] {
		t.Errorf("Expected page [%s], got %v", allIDs[1], pageIDs)
	}
	if total, _ := jmapResp.MethodResponses[1].Args["total"].(float64); total != 2 {
		t.Errorf("Expected total 2 on paged query, got %v", total)
	}

	beyondIDs := idsOf(t, jmapResp.MethodResponses[2].Args["ids"])
	if len(beyondIDs) != 0 {
		t.Errorf("Expected empty ids for position beyond end, got %v", beyondIDs)
	}
	if total, _ := jmapResp.MethodResponses[2].Args["total"].(float64); total != 2 {
		t.Errorf("Expected total 2 for position beyond end, got %v", total)
	}

	zeroIDs := idsOf(t, jmapResp.MethodResponses[3].Args["ids"])
	if len(zeroIDs) != 0 {
		t.Errorf("Expected empty ids for limit 0, got %v", zeroIDs)
	}
}

// TestQueryPaginationMailbox verifies position/limit slicing for Mailbox/query, whose
// pagination is implemented in the handler.
func TestQueryPaginationMailbox(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/query", map[string]any{"accountId": "primary", "position": 1, "limit": 2}, "page"},
			[]any{"Mailbox/query", map[string]any{"accountId": "primary", "position": 100}, "beyond"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	// 6 default mailboxes exist; page from index 1 with limit 2 -> exactly 2 ids.
	pageIDs := idsOf(t, jmapResp.MethodResponses[0].Args["ids"])
	if len(pageIDs) != 2 {
		t.Errorf("Expected 2 mailboxes on page, got %v", pageIDs)
	}
	if total, _ := jmapResp.MethodResponses[0].Args["total"].(float64); total != 6 {
		t.Errorf("Expected total 6, got %v", total)
	}

	beyondIDs := idsOf(t, jmapResp.MethodResponses[1].Args["ids"])
	if len(beyondIDs) != 0 {
		t.Errorf("Expected empty ids for position beyond end, got %v", beyondIDs)
	}
}

func idsOf(t *testing.T, raw any) []string {
	t.Helper()
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("Expected ids array, got %v", raw)
	}
	ids := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			ids = append(ids, s)
		}
	}
	return ids
}

// postJMAP posts a single JMAP request body and decodes the response.
func postJMAP(t *testing.T, url string, using []string, calls []any) jmap.Response {
	t.Helper()
	payload := map[string]any{"using": using, "methodCalls": calls}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()
	var jr jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}
	return jr
}

// TestQueryAnchorPositioning verifies anchor/anchorOffset positioning semantics per RFC 8620
// Section 5.5 on Email/query: the anchor is looked up in the filtered and sorted results, its
// index plus anchorOffset (clamped to 0 if negative) is used exactly as though it were the
// position argument, a supplied position is ignored when an anchor is given, anchorOffset is
// ignored without an anchor, and the total is unaffected.
func TestQueryAnchorPositioning(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// Learn the seeded inbox order (default sort: receivedAt descending) so the test is
	// robust to seed changes.
	all := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}}, "c1"},
	})
	allIDs := idsOf(t, all.MethodResponses[0].Args["ids"])
	if len(allIDs) != 2 {
		t.Fatalf("Expected 2 seeded inbox emails, got %v", allIDs)
	}

	cases := []struct {
		name    string
		args    map[string]any
		wantIDs []string
		wantPos float64
	}{
		{name: "anchor+offset=1", args: map[string]any{"anchor": allIDs[0], "anchorOffset": 1}, wantIDs: []string{allIDs[1]}, wantPos: 1},
		{name: "anchor+negative offset", args: map[string]any{"anchor": allIDs[1], "anchorOffset": -1, "limit": 1}, wantIDs: []string{allIDs[0]}, wantPos: 0},
		{name: "anchor+offset beyond start counts from end", args: map[string]any{"anchor": allIDs[1], "anchorOffset": -2}, wantIDs: []string{allIDs[1]}, wantPos: 1},
		{name: "anchor+limit", args: map[string]any{"anchor": allIDs[0], "anchorOffset": 1, "limit": 1}, wantIDs: []string{allIDs[1]}, wantPos: 1},
		{name: "anchor ignores position", args: map[string]any{"anchor": allIDs[0], "position": 1, "limit": 1}, wantIDs: []string{allIDs[0]}, wantPos: 0},
		{name: "anchor+offset beyond end", args: map[string]any{"anchor": allIDs[1], "anchorOffset": 5}, wantIDs: []string{}, wantPos: 6},
		{name: "anchor+limit 0", args: map[string]any{"anchor": allIDs[0], "limit": 0}, wantIDs: []string{}, wantPos: 0},
		{name: "anchorOffset without anchor ignored", args: map[string]any{"position": 1, "anchorOffset": -5}, wantIDs: []string{allIDs[1]}, wantPos: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{
				"accountId":      "primary",
				"filter":         map[string]any{"inMailbox": "mb-inbox"},
				"calculateTotal": true,
			}
			for k, v := range tc.args {
				args[k] = v
			}
			resp := postJMAP(t, ts.URL, using, []any{[]any{"Email/query", args, "c1"}})
			mr := resp.MethodResponses[0]
			if mr.Name != "Email/query" {
				t.Fatalf("Expected Email/query response, got %q", mr.Name)
			}
			gotIDs := idsOf(t, mr.Args["ids"])
			if len(gotIDs) != len(tc.wantIDs) {
				t.Fatalf("Expected ids %v, got %v", tc.wantIDs, gotIDs)
			}
			for i := range gotIDs {
				if gotIDs[i] != tc.wantIDs[i] {
					t.Fatalf("Expected ids %v, got %v", tc.wantIDs, gotIDs)
				}
			}
			if pos, _ := mr.Args["position"].(float64); pos != tc.wantPos {
				t.Errorf("Expected response position %v, got %v", tc.wantPos, pos)
			}
			if total, _ := mr.Args["total"].(float64); total != 2 {
				t.Errorf("Expected total 2, got %v", total)
			}
		})
	}
}

// TestQueryAnchorAllMethods verifies anchor support on every */query method: an anchor that
// exists in the filtered and sorted results positions the response from its index, and an
// anchor that does not exist rejects the call with an anchorNotFound error (RFC 8620
// Section 5.5). Each method is exercised against live data in the memory backend.
func TestQueryAnchorAllMethods(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	validScript := `require ["fileinto"]; if header :contains "X-Spam" "Yes" { fileinto "Junk"; }`

	cases := []struct {
		name      string
		using     []string
		method    string
		queryArgs map[string]any
		setup     []any
	}{
		{
			name:   "Email/query",
			using:  []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			method: "Email/query",
			queryArgs: map[string]any{
				"filter": map[string]any{"inMailbox": "mb-inbox"},
			},
		},
		{
			name:   "Mailbox/query",
			using:  []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			method: "Mailbox/query",
		},
		{
			name:   "Quota/query",
			using:  []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI},
			method: "Quota/query",
		},
		{
			name:   "EmailSubmission/query",
			using:  []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			method: "EmailSubmission/query",
			setup: []any{
				[]any{"EmailSubmission/set", map[string]any{
					"accountId": "primary",
					"create":    map[string]any{"s1": map[string]any{"identityId": "id-primary", "emailId": "email-1"}},
				}, "c1"},
			},
		},
		{
			name:   "CalendarEvent/query",
			using:  []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
			method: "CalendarEvent/query",
			setup: []any{
				[]any{"CalendarEvent/set", map[string]any{
					"accountId": "primary",
					"create":    map[string]any{"ev1": map[string]any{"title": "Team Sync", "start": "2026-08-05T10:00:00Z", "duration": "PT1H"}},
				}, "c1"},
			},
		},
		{
			name:   "Card/query",
			using:  []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
			method: "Card/query",
			setup: []any{
				[]any{"Card/set", map[string]any{
					"accountId": "primary",
					"create":    map[string]any{"card1": map[string]any{"name": map[string]any{"full": "Alice"}}},
				}, "c1"},
			},
		},
		{
			name:   "SieveScript/query",
			using:  []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI},
			method: "SieveScript/query",
			setup: []any{
				[]any{"SieveScript/set", map[string]any{
					"accountId": "primary",
					"create":    map[string]any{"script1": map[string]any{"name": "Spam Filter", "content": validScript}},
				}, "c1"},
			},
		},
		{
			name:   "FileNode/query",
			using:  []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI},
			method: "FileNode/query",
			setup: []any{
				[]any{"FileNode/set", map[string]any{
					"accountId": "primary",
					"create":    map[string]any{"node1": map[string]any{"name": "Documents", "isFolder": true}},
				}, "c1"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anchorID := ""
			if tc.setup != nil {
				setupResp := postJMAP(t, ts.URL, tc.using, tc.setup)
				created, ok := setupResp.MethodResponses[0].Args["created"].(map[string]any)
				if !ok || len(created) != 1 {
					t.Fatalf("setup did not create one object: %#v", setupResp.MethodResponses[0].Args)
				}
				for _, raw := range created {
					obj, _ := raw.(map[string]any)
					anchorID, _ = obj["id"].(string)
				}
				if anchorID == "" {
					t.Fatal("setup created object without an id")
				}
			} else {
				first := postJMAP(t, ts.URL, tc.using, []any{
					[]any{tc.method, map[string]any{"accountId": "primary"}, "c1"},
				})
				ids := idsOf(t, first.MethodResponses[0].Args["ids"])
				if len(ids) == 0 {
					t.Fatal("no seeded results to anchor on")
				}
				anchorID = ids[0]
			}

			args := map[string]any{"accountId": "primary"}
			for k, v := range tc.queryArgs {
				args[k] = v
			}
			args["anchor"] = anchorID

			resp := postJMAP(t, ts.URL, tc.using, []any{[]any{tc.method, args, "c1"}})
			mr := resp.MethodResponses[0]
			if mr.Name != tc.method {
				t.Fatalf("Expected %s response, got %q", tc.method, mr.Name)
			}
			ids := idsOf(t, mr.Args["ids"])
			if len(ids) == 0 || ids[0] != anchorID {
				t.Errorf("Expected first anchored result %q, got %v", anchorID, ids)
			}

			missingArgs := map[string]any{"accountId": "primary"}
			for k, v := range tc.queryArgs {
				missingArgs[k] = v
			}
			missingArgs["anchor"] = "does-not-exist-id"
			missingResp := postJMAP(t, ts.URL, tc.using, []any{[]any{tc.method, missingArgs, "c2"}})
			missing := missingResp.MethodResponses[0]
			if missing.Name != "error" {
				t.Fatalf("Expected anchorNotFound error for missing anchor, got %q", missing.Name)
			}
			if errType, _ := missing.Args["type"].(string); errType != jmap.MethodErrorAnchorNotFound {
				t.Errorf("Expected error type %q, got %q", jmap.MethodErrorAnchorNotFound, errType)
			}
		})
	}
}

// TestQueryAnchorInvalidArguments verifies that malformed anchor arguments are rejected with
// an invalidArguments error per RFC 8620 Section 5.5 (anchor must be an Id, anchorOffset an
// integer).
func TestQueryAnchorInvalidArguments(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	cases := []struct {
		name string
		args map[string]any
	}{
		{name: "anchor must be a string", args: map[string]any{"anchor": 123}},
		{name: "fractional anchorOffset", args: map[string]any{"anchor": "email-1", "anchorOffset": 0.5}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"accountId": "primary"}
			for k, v := range tc.args {
				args[k] = v
			}
			resp := postJMAP(t, ts.URL, using, []any{[]any{"Email/query", args, "c1"}})
			mr := resp.MethodResponses[0]
			if mr.Name != "error" {
				t.Fatalf("Expected method error response, got %q", mr.Name)
			}
			if errType, _ := mr.Args["type"].(string); errType != jmap.MethodErrorInvalidArguments {
				t.Errorf("Expected error type %q, got %q", jmap.MethodErrorInvalidArguments, errType)
			}
		})
	}
}
