package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

func TestRFC9610_ContactCard_QuerySort(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Create cards with different names, created, updated times
	cardsToCreate := map[string]map[string]any{
		"c1": {
			"created": "2026-01-01T10:00:00Z",
			"updated": "2026-01-03T10:00:00Z",
			"name": map[string]any{
				"components": []map[string]any{
					{"kind": "given", "value": "Charlie"},
					{"kind": "surname", "value": "Brown"},
				},
			},
		},
		"c2": {
			"created": "2026-01-02T10:00:00Z",
			"updated": "2026-01-01T10:00:00Z",
			"name": map[string]any{
				"components": []map[string]any{
					{"kind": "given", "value": "Alice"},
					{"kind": "surname", "value": "Zimmerman"},
				},
			},
		},
		"c3": {
			"created": "2026-01-03T10:00:00Z",
			"updated": "2026-01-02T10:00:00Z",
			"name": map[string]any{
				"components": []map[string]any{
					{"kind": "given", "value": "Bob"},
					{"kind": "surname", "value": "Adams"},
				},
			},
		},
	}

	setReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{"ContactCard/set", map[string]any{"accountId": "primary", "create": cardsToCreate}, "s1"},
		},
	}
	bodyBytes, _ := json.Marshal(setReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	var jmapResp jmap.Response
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
	resp.Body.Close()

	createdMap := jmapResp.MethodResponses[0].Args["created"].(map[string]any)
	idMap := make(map[string]string) // key -> real ID
	for key, item := range createdMap {
		m := item.(map[string]any)
		idMap[key] = m["id"].(string)
	}

	queryWithSort := func(sort []map[string]any) []string {
		req := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
			"methodCalls": []any{
				[]any{"ContactCard/query", map[string]any{"accountId": "primary", "sort": sort}, "q1"},
			},
		}
		b, _ := json.Marshal(req)
		r, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatalf("POST failed: %v", err)
		}
		var res jmap.Response
		_ = json.NewDecoder(r.Body).Decode(&res)
		r.Body.Close()

		if res.MethodResponses[0].Name == "error" {
			return nil
		}
		idsRaw, ok := res.MethodResponses[0].Args["ids"].([]any)
		if !ok {
			return nil
		}
		var out []string
		for _, v := range idsRaw {
			out = append(out, v.(string))
		}
		return out
	}

	// Test sort by created asc
	got := queryWithSort([]map[string]any{{"property": "created", "isAscending": true}})
	if len(got) != 3 || got[0] != idMap["c1"] || got[1] != idMap["c2"] || got[2] != idMap["c3"] {
		t.Errorf("Sort by created asc failed: got %v", got)
	}

	// Test sort by created desc
	got = queryWithSort([]map[string]any{{"property": "created", "isAscending": false}})
	if len(got) != 3 || got[0] != idMap["c3"] || got[1] != idMap["c2"] || got[2] != idMap["c1"] {
		t.Errorf("Sort by created desc failed: got %v", got)
	}

	// Test sort by name/given asc (Alice [c2], Bob [c3], Charlie [c1])
	got = queryWithSort([]map[string]any{{"property": "name/given", "isAscending": true}})
	if len(got) != 3 || got[0] != idMap["c2"] || got[1] != idMap["c3"] || got[2] != idMap["c1"] {
		t.Errorf("Sort by name/given asc failed: got %v", got)
	}

	// Test sort by name/surname asc (Adams [c3], Brown [c1], Zimmerman [c2])
	got = queryWithSort([]map[string]any{{"property": "name/surname", "isAscending": true}})
	if len(got) != 3 || got[0] != idMap["c3"] || got[1] != idMap["c1"] || got[2] != idMap["c2"] {
		t.Errorf("Sort by name/surname asc failed: got %v", got)
	}

	// Test invalid sort property returns error
	got = queryWithSort([]map[string]any{{"property": "invalidProperty", "isAscending": true}})
	if got != nil {
		t.Errorf("Expected error for invalid sort property, got %v", got)
	}
}
