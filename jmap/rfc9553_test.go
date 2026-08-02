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

// TestRFC9553_JSContactDataModel tests JSContact (RFC 9553) Card data model creation and retrieval via JMAP.
func TestRFC9553_JSContactDataModel(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Create JSContact Card per RFC 9553
	cardReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"Card/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"c9553": map[string]any{
							"@type": "Card",
							"name": map[string]any{
								"full": "Jane Doe",
							},
							"emails": map[string]any{
								"work": map[string]any{
									"address": "jane.doe@example.com",
								},
							},
						},
					},
				},
				"c1",
			},
		},
	}

	bodyBytes, _ := json.Marshal(cardReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST Card/set failed: %v", err)
	}
	defer resp.Body.Close()

	var setResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&setResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	createdMap := setResp.MethodResponses[0].Args["created"].(map[string]any)
	if _, ok := createdMap["c9553"]; !ok {
		t.Fatalf("Expected JSContact Card c9553 to be created")
	}
}
