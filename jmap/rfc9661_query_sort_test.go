package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/managesieve"
	"imap-jmap/jmap/spectest"
)

// TestRFC9661_Section2_5_SieveScriptSort verifies that SieveScript/query
// supports sorting by name and isActive (RFC 9661 Section 2.5).
func TestRFC9661_Section2_5_SieveScriptSort(t *testing.T) {
	spectest.Require(t, "RFC9661", "2.5", spectest.MUST,
		"The following SieveScript properties MUST be supported for sorting:")

	_, sieveBackend, cleanup := managesieve.NewEmbeddedBackend()
	defer cleanup()
	srv := jmap.NewServer(nil, jmap.WithSieveBackend(sieveBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}

	const script = `require ["fileinto"]; if header :contains "subject" "x" { fileinto "INBOX.x"; }`
	post := func(calls []any) jmap.Response {
		req := map[string]any{"using": using, "methodCalls": calls}
		body, _ := json.Marshal(req)
		resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr
	}

	r := post([]any{
		[]any{"SieveScript/set", map[string]any{"accountId": "primary", "create": map[string]any{
			"c": map[string]any{"name": "Charlie", "content": script},
			"a": map[string]any{"name": "Alice", "content": script},
			"b": map[string]any{"name": "Bravo", "content": script},
		}}, "s1"},
	})
	created := r.MethodResponses[0].Args["created"].(map[string]any)
	charlieID := created["c"].(map[string]any)["id"].(string)

	ids := func(m map[string]any) []string {
		raw, _ := m["ids"].([]any)
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			out = append(out, v.(string))
		}
		return out
	}

	q := func(sort []any) map[string]any {
		return post([]any{
			[]any{"SieveScript/query", map[string]any{"accountId": "primary", "sort": sort}, "q"},
		}).MethodResponses[0].Args
	}

	got := ids(q([]any{map[string]any{"property": "name", "isAscending": true}}))
	if len(got) != 3 || got[0] != created["a"].(map[string]any)["id"] || got[1] != created["b"].(map[string]any)["id"] || got[2] != charlieID {
		t.Errorf("sort by name asc failed: %v", got)
	}

	// Activate Charlie, then sort by isActive descending.
	post([]any{
		[]any{"SieveScript/set", map[string]any{"accountId": "primary",
			"onSuccessActivateScript": charlieID}, "s2"},
	})
	got = ids(q([]any{map[string]any{"property": "isActive", "isAscending": false}}))
	if len(got) == 0 || got[0] != charlieID {
		t.Errorf("active script should sort first (isActive desc), got %v", got)
	}

	// Unknown sort property is rejected.
	res := post([]any{
		[]any{"SieveScript/query", map[string]any{"accountId": "primary", "sort": []any{map[string]any{"property": "id"}}}, "q2"},
	})
	mr := res.MethodResponses[0]
	if mr.Name != "error" || mr.Args["type"] != "unsupportedSort" {
		t.Errorf("expected unsupportedSort for an unknown property, got %v", mr)
	}
}
