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

// TestRFC9553_JSContactFullCard tests full JSContact Card specification properties per RFC 9553.
func TestRFC9553_JSContactFullCard(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Create Card with full JSContact properties
	cardReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"Card/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"c1": map[string]any{
							"@type": "Card",
							"kind":  "individual",
							"name": map[string]any{
								"full":   "Dr. Alex Vance, Jr.",
								"sortAs": "Vance, Alex",
								"components": []any{
									map[string]any{"kind": "prefix", "value": "Dr."},
									map[string]any{"kind": "given", "value": "Alex"},
									map[string]any{"kind": "surname", "value": "Vance"},
								},
							},
							"nicknames": map[string]any{
								"n1": map[string]any{"name": "Lex", "contexts": map[string]any{"work": true}},
							},
							"emails": map[string]any{
								"e1": map[string]any{
									"address":  "alex@example.com",
									"contexts": map[string]any{"work": true},
									"pref":     1,
								},
							},
							"phones": map[string]any{
								"p1": map[string]any{
									"number":   "+15551234567",
									"contexts": map[string]any{"work": true},
									"features": map[string]any{"voice": true, "cell": true},
									"pref":     1,
								},
							},
							"addresses": map[string]any{
								"a1": map[string]any{
									"full":        "123 Tech Way, Suite 400, San Francisco, CA 94107, USA",
									"street":      "123 Tech Way",
									"locality":    "San Francisco",
									"region":      "CA",
									"postcode":    "94107",
									"country":     "United States",
									"countryCode": "US",
									"contexts":    map[string]any{"work": true},
								},
							},
							"organizations": map[string]any{
								"o1": map[string]any{
									"name":     "Acme Corp",
									"units":    []string{"R&D", "Engineering"},
									"contexts": map[string]any{"work": true},
								},
							},
							"titles": map[string]any{
								"t1": map[string]any{
									"title":    "Principal Architect",
									"contexts": map[string]any{"work": true},
								},
							},
							"notes": map[string]any{
								"k1": map[string]any{"note": "Key speaker at TechConf 2026"},
							},
							"onlineServices": map[string]any{
								"s1": map[string]any{
									"service":  "GitHub",
									"uri":      "https://github.com/alexvance",
									"contexts": map[string]any{"work": true},
								},
							},
							"links": map[string]any{
								"l1": map[string]any{
									"uri":   "https://alexvance.example.com",
									"label": "Personal Blog",
									"pref":  1,
								},
							},
							"media": map[string]any{
								"m1": map[string]any{
									"uri":  "https://example.com/photos/alex.jpg",
									"kind": "photo",
									"pref": 1,
								},
							},
							"gender": map[string]any{
								"grammatical": "masculine",
								"identity":    "man",
							},
							"speakToAs": map[string]any{
								"pronouns": map[string]any{"he/him": true},
							},
							"anniversaries": map[string]any{
								"b1": map[string]any{
									"date":  "1990-05-15",
									"kind":  "birth",
									"label": "Birthday",
								},
							},
							"keywords": map[string]any{
								"vip":  true,
								"tech": true,
							},
						},
					},
				},
				"call1",
			},
		},
	}

	bodyBytes, _ := json.Marshal(cardReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("Card/set failed: %v", err)
	}
	defer resp.Body.Close()

	var setResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&setResp); err != nil {
		t.Fatalf("Decode Card/set failed: %v", err)
	}

	createdMap := setResp.MethodResponses[0].Args["created"].(map[string]any)
	createdCard := createdMap["c1"].(map[string]any)
	cardID := createdCard["id"].(string)

	// 2. Retrieve Card via Card/get
	getReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{"Card/get", map[string]any{"accountId": "primary", "ids": []string{cardID}}, "call2"},
		},
	}

	bodyBytesGet, _ := json.Marshal(getReq)
	respGet, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytesGet))
	if err != nil {
		t.Fatalf("Card/get failed: %v", err)
	}
	defer respGet.Body.Close()

	var getResp jmap.Response
	if err := json.NewDecoder(respGet.Body).Decode(&getResp); err != nil {
		t.Fatalf("Decode Card/get failed: %v", err)
	}

	list := getResp.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("Expected 1 card, got %d", len(list))
	}

	cardData := list[0].(map[string]any)
	if cardData["@type"] != "Card" {
		t.Errorf("Expected @type 'Card', got %v", cardData["@type"])
	}
	if cardData["kind"] != "individual" {
		t.Errorf("Expected kind 'individual', got %v", cardData["kind"])
	}

	nameMap := cardData["name"].(map[string]any)
	if nameMap["full"] != "Dr. Alex Vance, Jr." {
		t.Errorf("Expected name.full 'Dr. Alex Vance, Jr.', got %v", nameMap["full"])
	}

	linksMap := cardData["links"].(map[string]any)
	if l1, ok := linksMap["l1"].(map[string]any); !ok || l1["uri"] != "https://alexvance.example.com" {
		t.Errorf("Expected links.l1 uri 'https://alexvance.example.com', got %v", linksMap["l1"])
	}

	mediaMap := cardData["media"].(map[string]any)
	if m1, ok := mediaMap["m1"].(map[string]any); !ok || m1["kind"] != "photo" {
		t.Errorf("Expected media.m1 kind 'photo', got %v", mediaMap["m1"])
	}

	genderMap := cardData["gender"].(map[string]any)
	if genderMap["identity"] != "man" {
		t.Errorf("Expected gender identity 'man', got %v", genderMap["identity"])
	}

	annivMap := cardData["anniversaries"].(map[string]any)
	if b1, ok := annivMap["b1"].(map[string]any); !ok || b1["date"] != "1990-05-15" {
		t.Errorf("Expected anniversary date '1990-05-15', got %v", annivMap["b1"])
	}
}

// TestRFC9553_JSContactGroup tests JSContact group card properties per RFC 9553 Section 2.1.6.
func TestRFC9553_JSContactGroup(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	groupReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"Card/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"g1": map[string]any{
							"@type": "Card",
							"kind":  "group",
							"name": map[string]any{
								"full": "Engineering Leads",
							},
							"members": map[string]any{
								"urn:uuid:11111111-2222-3333-4444-555555555555": true,
								"urn:uuid:66666666-7777-8888-9999-000000000000": true,
							},
						},
					},
				},
				"c1",
			},
		},
	}

	bodyBytes, _ := json.Marshal(groupReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("Card/set group failed: %v", err)
	}
	defer resp.Body.Close()

	var setResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&setResp); err != nil {
		t.Fatalf("Decode group response failed: %v", err)
	}

	createdMap := setResp.MethodResponses[0].Args["created"].(map[string]any)
	grpCard := createdMap["g1"].(map[string]any)
	if grpCard["kind"] != "group" {
		t.Errorf("Expected kind 'group', got %v", grpCard["kind"])
	}

	membersMap := grpCard["members"].(map[string]any)
	if len(membersMap) != 2 {
		t.Errorf("Expected 2 members in group, got %d", len(membersMap))
	}
}
