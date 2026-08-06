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

func TestRFC9610_ContactCard_QueryChanges(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Initial ContactCard/query to get initial state and verify canCalculateChanges is true
	queryReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{"ContactCard/query", map[string]any{"accountId": "primary"}, "c1"},
		},
	}
	bodyBytes, _ := json.Marshal(queryReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	var jmapResp jmap.Response
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
	resp.Body.Close()

	if len(jmapResp.MethodResponses) != 1 || jmapResp.MethodResponses[0].Name != "ContactCard/query" {
		t.Fatalf("Unexpected query response: %v", jmapResp.MethodResponses)
	}
	queryRes := jmapResp.MethodResponses[0].Args
	initialState, ok := queryRes["queryState"].(string)
	if !ok || initialState == "" {
		t.Fatalf("Expected non-empty queryState, got %v", queryRes["queryState"])
	}
	if canCalc, ok := queryRes["canCalculateChanges"].(bool); !ok || !canCalc {
		t.Fatalf("Expected canCalculateChanges=true, got %v", queryRes["canCalculateChanges"])
	}

	// 2. Create a card via ContactCard/set
	setReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"ContactCard/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"card1": map[string]any{
							"name": map[string]any{
								"full": "Jane Doe",
							},
						},
					},
				},
				"c2",
			},
		},
	}
	bodyBytes, _ = json.Marshal(setReq)
	resp, err = http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
	resp.Body.Close()

	createdMap := jmapResp.MethodResponses[0].Args["created"].(map[string]any)
	createdCard := createdMap["card1"].(map[string]any)
	cardID := createdCard["id"].(string)

	// 3. Query changes using canonical ContactCard/queryChanges
	qcReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"ContactCard/queryChanges",
				map[string]any{
					"accountId":       "primary",
					"sinceQueryState": initialState,
				},
				"c3",
			},
		},
	}
	bodyBytes, _ = json.Marshal(qcReq)
	resp, err = http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
	resp.Body.Close()

	if len(jmapResp.MethodResponses) != 1 || jmapResp.MethodResponses[0].Name != "ContactCard/queryChanges" {
		t.Fatalf("Unexpected queryChanges response: %v", jmapResp.MethodResponses)
	}
	qcArgs := jmapResp.MethodResponses[0].Args
	addedRaw := qcArgs["added"].([]any)
	if len(addedRaw) != 1 {
		t.Fatalf("Expected 1 added item, got %d", len(addedRaw))
	}
	addedItem := addedRaw[0].(map[string]any)
	if addedItem["id"] != cardID {
		t.Errorf("Expected added id %s, got %v", cardID, addedItem["id"])
	}

	// 4. Also test legacy Card/queryChanges alias
	cardQcReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"Card/queryChanges",
				map[string]any{
					"accountId":       "primary",
					"sinceQueryState": initialState,
				},
				"c4",
			},
		},
	}
	bodyBytes, _ = json.Marshal(cardQcReq)
	resp, err = http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
	resp.Body.Close()

	if len(jmapResp.MethodResponses) != 1 || jmapResp.MethodResponses[0].Name != "Card/queryChanges" {
		t.Fatalf("Unexpected Card/queryChanges response name: %v", jmapResp.MethodResponses[0].Name)
	}
}
