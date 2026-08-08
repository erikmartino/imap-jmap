package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8621_EmailQueryTextSearchWildcard verifies the Email/query free-text filters
// (text/subject/body/from) are token/prefix searches, not literal substrings — clients such as
// Bulwark append a "*" prefix-wildcard (e.g. {text:"core*"}), which must still match the word
// "Core" (RFC 8621 Section 4.4.1).
func TestRFC8621_EmailQueryTextSearchWildcard(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.4.1", spectest.MUST,
		"Email/query text/subject/body/from filters are free-text searches; a client prefix-wildcard term matches a word.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// Seed an email with known subject/body/from.
	inboxID := "mb-inbox"
	create := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e": map[string]any{
					"mailboxIds": map[string]any{inboxID: true},
					"subject":    "Quarterly Core Report",
					"from":       []any{map[string]any{"name": "Alice Example", "email": "alice@example.com"}},
					"keywords":   map[string]any{"$seen": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "annual figures inside"}},
					"textBody":   []any{map[string]any{"partId": "1", "type": "text/plain"}},
				},
			},
		}, "c1"},
	})
	id, _ := create.MethodResponses[0].Args["created"].(map[string]any)["e"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("seed create failed: %+v", create.MethodResponses[0].Args)
	}

	query := func(filter map[string]any) []any {
		resp := postJMAP(t, ts.URL, using, []any{
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": filter}, "q"},
		})
		if resp.MethodResponses[0].Name == "error" {
			t.Fatalf("query error: %+v", resp.MethodResponses[0].Args)
		}
		ids, _ := resp.MethodResponses[0].Args["ids"].([]any)
		return ids
	}
	has := func(ids []any) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}

	// The exact case Bulwark sends: a "text" filter with a trailing "*" wildcard.
	if !has(query(map[string]any{"text": "core*"})) {
		t.Errorf(`text:"core*" should match subject "Quarterly Core Report"`)
	}
	if !has(query(map[string]any{"subject": "quarterly*"})) {
		t.Errorf(`subject:"quarterly*" should match`)
	}
	if !has(query(map[string]any{"body": "figures*"})) {
		t.Errorf(`body:"figures*" should match the body`)
	}
	if !has(query(map[string]any{"from": "alice*"})) {
		t.Errorf(`from:"alice*" should match alice@example.com`)
	}
	// Multi-term text query spanning subject + body.
	if !has(query(map[string]any{"text": "quarterly figures"})) {
		t.Errorf(`text:"quarterly figures" should match across subject and body`)
	}
	// Negative: a term present nowhere must not match.
	if has(query(map[string]any{"text": "nonexistentterm*"})) {
		t.Errorf(`text:"nonexistentterm*" must not match`)
	}
}
