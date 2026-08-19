package jmap_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestEmailQuerySortComparatorsBackend exercises every RFC 8621 Section 4.4.2 email sort
// property through the memory backend's QueryEmails with fully unique comparator values, so
// each expected order is exact. Keyword sorts with ties are asserted by group.
func TestRFC8621_Section4_4_2_EmailSortComparators(t *testing.T) {
	ctx := context.Background()
	mb := memory.NewMemoryBackend()
	if _, err := mb.CreateMailbox(ctx, &jmap.Mailbox{ID: "mb-sort", Name: "Sort Box"}); err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Six emails; every comparator-relevant value is unique so single-comparator sorts are
	// fully deterministic. e1/e1b share a thread, as do e4/e4b (for thread keyword sorts).
	// They live in their own mailbox so the seeded emails never enter the result set.
	mk := func(id string, thread string, subject string, from string, to string, size uint64, keywords map[string]bool, receivedAt, sentAt string) *jmap.Email {
		return &jmap.Email{
			ID:         jmap.Id(id),
			ThreadID:   jmap.Id(thread),
			Subject:    subject,
			From:       []jmap.EmailAddress{{Name: from}},
			To:         []jmap.EmailAddress{{Name: to}},
			MailboxIDs: map[jmap.Id]bool{"mb-sort": true},
			Keywords:   keywords,
			Size:       size,
			ReceivedAt: receivedAt,
			SentAt:     sentAt,
		}
	}
	emails := []*jmap.Email{
		mk("e1", "t1", "alpha", "Charlie", "to-charlie", 500, map[string]bool{"$seen": true}, "2026-08-10T10:00:00Z", "2026-08-10T09:00:00Z"),
		mk("e1b", "t1", "alpha beta", "Charlie Zulu", "to-charlie-zulu", 900, map[string]bool{"$seen": true}, "2026-08-10T10:30:00Z", "2026-08-10T09:30:00Z"),
		mk("e2", "t2", "Bravo", "Alpha", "to-alpha", 200, map[string]bool{"$flagged": true}, "2026-08-09T10:00:00Z", "2026-08-09T09:00:00Z"),
		mk("e3", "t3", "charlie", "Bravo", "to-bravo", 300, nil, "2026-08-08T10:00:00Z", "2026-08-08T09:00:00Z"),
		mk("e4", "t4", "Delta", "Delta", "to-delta", 700, map[string]bool{"$seen": true}, "2026-08-07T10:00:00Z", "2026-08-07T09:00:00Z"),
		mk("e4b", "t4", "delta epsilon", "Delta Zulu", "to-delta-zulu", 800, nil, "2026-08-07T10:30:00Z", "2026-08-07T09:30:00Z"),
	}
	for _, em := range emails {
		if _, err := mb.CreateEmail(ctx, em); err != nil {
			t.Fatalf("CreateEmail %s failed: %v", em.ID, err)
		}
	}

	query := func(comparators ...jmap.Comparator) []string {
		t.Helper()
		ids, total, err := mb.QueryEmails(ctx, map[string]any{"inMailbox": "mb-sort"}, comparators, 0, nil)
		if err != nil {
			t.Fatalf("QueryEmails failed: %v", err)
		}
		if total != 6 {
			t.Fatalf("Expected 6 emails, got %d", total)
		}
		got := make([]string, 0, len(ids))
		for _, id := range ids {
			got = append(got, string(id))
		}
		return got
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
	// eqGroups checks that got is the concatenation of the given groups, with entries
	// within a group allowed in any order (for equal-key ties, RFC 8620 Section 5.5).
	eqGroups := func(got []string, groups ...[]string) bool {
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

	// Default sort: receivedAt descending (RFC 8621 Section 4.4.2).
	if got := query(); !eq(got, "e1b", "e1", "e2", "e3", "e4b", "e4") {
		t.Errorf("default sort must be receivedAt descending, got %v", got)
	}
	// receivedAt ascending.
	if got := query(jmap.Comparator{Property: "receivedAt", IsAscending: true}); !eq(got, "e4", "e4b", "e3", "e2", "e1", "e1b") {
		t.Errorf("receivedAt ascending, got %v", got)
	}
	// sentAt ascending.
	if got := query(jmap.Comparator{Property: "sentAt", IsAscending: true}); !eq(got, "e4", "e4b", "e3", "e2", "e1", "e1b") {
		t.Errorf("sentAt ascending, got %v", got)
	}
	// subject ascending uses the case-insensitive default collation (i;ascii-casemap).
	if got := query(jmap.Comparator{Property: "subject", IsAscending: true}); !eq(got, "e1", "e1b", "e2", "e3", "e4", "e4b") {
		t.Errorf("subject ascending (casemap), got %v", got)
	}
	// subject descending.
	if got := query(jmap.Comparator{Property: "subject", IsAscending: false}); !eq(got, "e4b", "e4", "e3", "e2", "e1b", "e1") {
		t.Errorf("subject descending, got %v", got)
	}
	// subject with the i;octet collation is a case-sensitive binary comparison: uppercase
	// "Bravo" (0x42) sorts before lowercase "alpha" (0x61).
	if got := query(jmap.Comparator{Property: "subject", IsAscending: true, Collation: "i;octet"}); !eq(got, "e2", "e4", "e1", "e1b", "e3", "e4b") {
		t.Errorf("subject ascending (i;octet), got %v", got)
	}
	// from: the "name" of the first From EmailAddress (RFC 8621 Section 4.4.2).
	if got := query(jmap.Comparator{Property: "from", IsAscending: true}); !eq(got, "e2", "e3", "e1", "e1b", "e4", "e4b") {
		t.Errorf("from ascending, got %v", got)
	}
	// to: the "name" of the first To EmailAddress.
	if got := query(jmap.Comparator{Property: "to", IsAscending: true}); !eq(got, "e2", "e3", "e1", "e1b", "e4", "e4b") {
		t.Errorf("to ascending, got %v", got)
	}
	// size ascending.
	if got := query(jmap.Comparator{Property: "size", IsAscending: true}); !eq(got, "e2", "e3", "e1", "e4", "e4b", "e1b") {
		t.Errorf("size ascending, got %v", got)
	}
	// hasKeyword $seen ascending: false (no keyword) before true.
	if got := query(jmap.Comparator{Property: "hasKeyword", Keyword: "$seen", IsAscending: true}); !eqGroups(got, []string{"e2", "e3", "e4b"}, []string{"e1", "e1b", "e4"}) {
		t.Errorf("hasKeyword $seen ascending, got %v", got)
	}
	// hasKeyword $seen descending: keyword-bearing emails first.
	if got := query(jmap.Comparator{Property: "hasKeyword", Keyword: "$seen", IsAscending: false}); !eqGroups(got, []string{"e1", "e1b", "e4"}, []string{"e2", "e3", "e4b"}) {
		t.Errorf("hasKeyword $seen descending, got %v", got)
	}
	// allInThreadHaveKeyword $seen: t1 (e1+e1b both $seen) is all-true; t2/t3/t4 are not.
	if got := query(jmap.Comparator{Property: "allInThreadHaveKeyword", Keyword: "$seen", IsAscending: true}); !eqGroups(got, []string{"e2", "e3", "e4", "e4b"}, []string{"e1", "e1b"}) {
		t.Errorf("allInThreadHaveKeyword $seen ascending, got %v", got)
	}
	// allInThreadHaveKeyword $flagged: only t2 (e2) is all-true.
	if got := query(jmap.Comparator{Property: "allInThreadHaveKeyword", Keyword: "$flagged", IsAscending: false}); !eqGroups(got, []string{"e2"}, []string{"e1", "e1b", "e3", "e4", "e4b"}) {
		t.Errorf("allInThreadHaveKeyword $flagged descending, got %v", got)
	}
	// someInThreadHaveKeyword $seen: t1 (all $seen) and t4 (e4 $seen, e4b not) are some-true;
	// t2/t3 are not.
	if got := query(jmap.Comparator{Property: "someInThreadHaveKeyword", Keyword: "$seen", IsAscending: true}); !eqGroups(got, []string{"e2", "e3"}, []string{"e1", "e1b", "e4", "e4b"}) {
		t.Errorf("someInThreadHaveKeyword $seen ascending, got %v", got)
	}
	// someInThreadHaveKeyword $flagged: only t2 is some-true.
	if got := query(jmap.Comparator{Property: "someInThreadHaveKeyword", Keyword: "$flagged", IsAscending: true}); !eqGroups(got, []string{"e1", "e1b", "e3", "e4", "e4b"}, []string{"e2"}) {
		t.Errorf("someInThreadHaveKeyword $flagged ascending, got %v", got)
	}
}

// TestEmailQuerySortMultiComparator verifies that multiple comparators are applied in order
// and that a later comparator breaks ties left by an earlier one (RFC 8620 Section 5.5).
func TestRFC8621_Section4_4_2_EmailSortMultiComparator(t *testing.T) {
	ctx := context.Background()
	mb := memory.NewMemoryBackend()
	if _, err := mb.CreateMailbox(ctx, &jmap.Mailbox{ID: "mb-sort", Name: "Sort Box"}); err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	mk := func(id string, subject string, size uint64, receivedAt string) *jmap.Email {
		return &jmap.Email{
			ID:         jmap.Id(id),
			Subject:    subject,
			MailboxIDs: map[jmap.Id]bool{"mb-sort": true},
			Size:       size,
			ReceivedAt: receivedAt,
		}
	}
	// eX and eY share subject AND receivedAt; the size comparator must break the tie.
	for _, em := range []*jmap.Email{
		mk("eX", "Zulu tie", 10, "2026-08-11T10:00:00Z"),
		mk("eY", "Zulu tie", 5, "2026-08-11T10:00:00Z"),
		mk("eZ", "Alpha tie", 99, "2026-08-11T11:00:00Z"),
	} {
		if _, err := mb.CreateEmail(ctx, em); err != nil {
			t.Fatalf("CreateEmail %s failed: %v", em.ID, err)
		}
	}

	query := func(comparators ...jmap.Comparator) []string {
		t.Helper()
		ids, _, err := mb.QueryEmails(ctx, map[string]any{"inMailbox": "mb-sort"}, comparators, 0, nil)
		if err != nil {
			t.Fatalf("QueryEmails failed: %v", err)
		}
		got := make([]string, 0, len(ids))
		for _, id := range ids {
			got = append(got, string(id))
		}
		return got
	}

	// [subject asc, size desc]: subjects equal, so the larger size comes first.
	if got := query(jmap.Comparator{Property: "subject", IsAscending: true}, jmap.Comparator{Property: "size", IsAscending: false}); len(got) != 3 || got[0] != "eZ" || got[1] != "eX" || got[2] != "eY" {
		t.Errorf("subject asc + size desc must be [eZ eX eY], got %v", got)
	}
	// [subject asc, size asc]: the smaller size comes first within the tie.
	if got := query(jmap.Comparator{Property: "subject", IsAscending: true}, jmap.Comparator{Property: "size", IsAscending: true}); len(got) != 3 || got[0] != "eZ" || got[1] != "eY" || got[2] != "eX" {
		t.Errorf("subject asc + size asc must be [eZ eY eX], got %v", got)
	}
	// [size asc, subject desc]: no tie on size, so the subject comparator is never consulted.
	if got := query(jmap.Comparator{Property: "size", IsAscending: true}, jmap.Comparator{Property: "subject", IsAscending: false}); len(got) != 3 || got[0] != "eY" || got[1] != "eX" || got[2] != "eZ" {
		t.Errorf("size asc + subject desc must be [eY eX eZ], got %v", got)
	}
}

// TestEmailQuerySortBaseSubject verifies that "subject" sorting uses the RFC 5256 Section 2.1
// base subject: reply/forward prefixes, bracketed list tags, and a trailing "(fwd)" are
// stripped before comparison (RFC 8621 Section 4.4.2).
func TestRFC8621_Section4_4_2_EmailSortBaseSubject(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	created := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"mb-sort": map[string]any{"name": "Sort Box"}},
		}, "c0"},
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{"subject": "Re: alpha", "mailboxIds": map[string]any{"mb-sort": true}},
				"e2": map[string]any{"subject": "fwd[2]: Bravo", "mailboxIds": map[string]any{"mb-sort": true}},
				"e3": map[string]any{"subject": "[tag] charlie", "mailboxIds": map[string]any{"mb-sort": true}},
				"e4": map[string]any{"subject": "delta (fwd)", "mailboxIds": map[string]any{"mb-sort": true}},
			},
		}, "c1"},
	})
	createdMap, _ := created.MethodResponses[1].Args["created"].(map[string]any)
	if len(createdMap) != 4 {
		t.Fatalf("Expected 4 created emails, got %v", createdMap)
	}
	idsByKey := map[string]string{}
	for key, obj := range createdMap {
		idsByKey[key] = obj.(map[string]any)["id"].(string)
	}

	// All subjects share a distinct base subject once prefixes are stripped, so sorting
	// must ignore the prefixes entirely.
	resp := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": "mb-sort"},
			"sort":      []any{map[string]any{"property": "subject", "isAscending": true}},
		}, "c1"},
	})
	ids := idsOf(t, resp.MethodResponses[0].Args["ids"])
	want := []string{idsByKey["e1"], idsByKey["e2"], idsByKey["e3"], idsByKey["e4"]}
	if len(ids) != len(want) {
		t.Fatalf("Expected %d emails, got %v", len(want), ids)
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Fatalf("base subject sort must ignore Re:/fwd:/[tag]/(fwd) prefixes, got %v want %v", ids, want)
		}
	}
}

// TestEmailQuerySortValidation verifies sort validation over the protocol: unsupported sort
// properties and collations are rejected with "unsupportedSort", keyword sorts without a
// "keyword" property with "invalidArguments" (RFC 8620 Section 5.5, RFC 8621 Section 4.4.2),
// and the advertised emailQuerySortOptions matches the implemented properties.
func TestRFC8620_Section5_5_QuerySortValidation(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}
	errType := func(resp jmap.Response) string {
		t.Helper()
		if resp.MethodResponses[0].Name != "error" {
			return ""
		}
		et, _ := resp.MethodResponses[0].Args["type"].(string)
		return et
	}

	// Unsupported sort property.
	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "bogus"}}}, "c1"},
	})
	if et := errType(resp); et != "unsupportedSort" {
		t.Errorf("Email/query with unknown sort property must error unsupportedSort, got %q", et)
	}

	// Unsupported collation.
	resp = postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "subject", "collation": "i;utf8"}}}, "c1"},
	})
	if et := errType(resp); et != "unsupportedSort" {
		t.Errorf("Email/query with unknown collation must error unsupportedSort, got %q", et)
	}

	// Keyword sort without the required "keyword" property.
	resp = postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "hasKeyword"}}}, "c1"},
	})
	if et := errType(resp); et != jmap.MethodErrorInvalidArguments {
		t.Errorf("Email/query hasKeyword sort without keyword must error invalidArguments, got %q", et)
	}
	resp = postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "allInThreadHaveKeyword"}}}, "c1"},
	})
	if et := errType(resp); et != jmap.MethodErrorInvalidArguments {
		t.Errorf("Email/query allInThreadHaveKeyword sort without keyword must error invalidArguments, got %q", et)
	}

	// A valid keyword sort succeeds.
	resp = postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "someInThreadHaveKeyword", "keyword": "$flagged"}}}, "c1"},
	})
	if resp.MethodResponses[0].Name != "Email/query" {
		t.Errorf("valid keyword sort must succeed, got %q", resp.MethodResponses[0].Name)
	}

	// Mailbox/query only supports sortOrder and name (RFC 8621 Section 2.3).
	resp = postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "bogus"}}}, "c1"},
	})
	if et := errType(resp); et != "unsupportedSort" {
		t.Errorf("Mailbox/query with unknown sort property must error unsupportedSort, got %q", et)
	}
	resp = postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "name"}}}, "c1"},
	})
	if resp.MethodResponses[0].Name != "Mailbox/query" {
		t.Errorf("Mailbox/query sort by name must succeed, got %q", resp.MethodResponses[0].Name)
	}

	// EmailSubmission/query only supports emailId, threadId, sendAt, sentAt (RFC 8621
	// Section 7.3).
	resp = postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "bogus"}}}, "c1"},
	})
	if et := errType(resp); et != "unsupportedSort" {
		t.Errorf("EmailSubmission/query with unknown sort property must error unsupportedSort, got %q", et)
	}

	// Email/queryChanges validates its sort the same way (RFC 8620 Section 5.6).
	resp = postJMAP(t, ts.URL, using, []any{
		[]any{"Email/queryChanges", map[string]any{"accountId": "primary", "sinceQueryState": "0", "sort": []any{map[string]any{"property": "bogus"}}}, "c1"},
	})
	if et := errType(resp); et != "unsupportedSort" {
		t.Errorf("Email/queryChanges with unknown sort property must error unsupportedSort, got %q", et)
	}
	resp = postJMAP(t, ts.URL, using, []any{
		[]any{"Email/queryChanges", map[string]any{"accountId": "primary", "sinceQueryState": "0", "sort": []any{map[string]any{"property": "subject"}}}, "c1"},
	})
	if resp.MethodResponses[0].Name != "Email/queryChanges" {
		t.Errorf("Email/queryChanges sort by subject must succeed, got %q", resp.MethodResponses[0].Name)
	}

	// The advertised emailQuerySortOptions lists exactly the implemented properties. RFC
	// 8621 Section 1.3.1 places it in the account capabilities; the server currently
	// advertises it in the session capabilities object.
	var sess jmap.Session
	sessResp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("Failed to fetch session: %v", err)
	}
	defer sessResp.Body.Close()
	if err := json.NewDecoder(sessResp.Body).Decode(&sess); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}
	primaryID := sess.PrimaryAccounts[jmap.MailCapabilityURI]
	acc, ok := sess.Accounts[primaryID]
	if !ok {
		t.Fatalf("Account %q missing in session accounts", primaryID)
	}
	capRaw, ok := acc.AccountCapabilities[jmap.MailCapabilityURI].(map[string]any)
	if !ok {
		t.Fatalf("Mail capability missing in account capabilities")
	}
	rawOpts, _ := capRaw["emailQuerySortOptions"].([]any)
	advertised := map[string]bool{}
	for _, o := range rawOpts {
		advertised[o.(string)] = true
	}
	want := []string{"receivedAt", "sentAt", "size", "subject", "from", "to", "hasKeyword", "allInThreadHaveKeyword", "someInThreadHaveKeyword"}
	if len(advertised) != len(want) {
		t.Errorf("emailQuerySortOptions must list exactly %d properties, got %v", len(want), rawOpts)
	}
	for _, w := range want {
		if !advertised[w] {
			t.Errorf("emailQuerySortOptions must include %q, got %v", w, rawOpts)
		}
	}
}
