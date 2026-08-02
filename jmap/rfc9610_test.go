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

// TestRFC9610_Capability tests urn:ietf:params:jmap:contacts capability discovery per RFC 9610 Section 2.
func TestRFC9610_Capability(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}

	capRaw, ok := session.Capabilities[jmap.ContactsCapabilityURI]
	if !ok {
		t.Fatalf("Capability %q missing in Session capabilities", jmap.ContactsCapabilityURI)
	}

	capBytes, _ := json.Marshal(capRaw)
	var contactsCap jmap.ContactsCapability
	_ = json.Unmarshal(capBytes, &contactsCap)

	if !contactsCap.MayCreateAddressBook {
		t.Errorf("Expected mayCreateAddressBook true, got false")
	}
}

// TestRFC9610_AddressBook_GetAndSet tests AddressBook/get and AddressBook/set per RFC 9610 Section 2.
func TestRFC9610_AddressBook_GetAndSet(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. AddressBook/get default address book
	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{"AddressBook/get", map[string]any{"accountId": "primary"}, "c1"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(jmapResp.MethodResponses) != 1 || jmapResp.MethodResponses[0].Name != "AddressBook/get" {
		t.Fatalf("Unexpected response format: %v", jmapResp.MethodResponses)
	}

	args := jmapResp.MethodResponses[0].Args
	listRaw := args["list"].([]any)
	if len(listRaw) != 1 {
		t.Fatalf("Expected 1 default address book, got %d", len(listRaw))
	}

	// 2. AddressBook/set create new address book
	setReqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"AddressBook/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"ab1": map[string]any{
							"name": "Work Contacts",
						},
					},
				},
				"c2",
			},
		},
	}

	setBytes, _ := json.Marshal(setReqBody)
	resp2, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(setBytes))
	if err != nil {
		t.Fatalf("JMAP POST set failed: %v", err)
	}
	defer resp2.Body.Close()

	var setResp jmap.Response
	if err := json.NewDecoder(resp2.Body).Decode(&setResp); err != nil {
		t.Fatalf("Failed to decode set response: %v", err)
	}

	setArgs := setResp.MethodResponses[0].Args
	created := setArgs["created"].(map[string]any)
	if created["ab1"] == nil {
		t.Fatalf("AddressBook creation failed: %v", setArgs)
	}
}

// TestRFC9610_Card_GetSetQuery_JSContact tests Card methods with JSContact payloads per RFC 9610 & RFC 9553.
func TestRFC9610_Card_GetSetQuery_JSContact(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Create a Card with JSContact RFC 9553 fields via Card/set
	cardPayload := map[string]any{
		"addressBookIds": map[string]bool{
			"ab-default": true,
		},
		"kind": "individual",
		"name": map[string]any{
			"full": "Jane Doe",
			"components": []map[string]any{
				{"kind": "given", "value": "Jane"},
				{"kind": "surname", "value": "Doe"},
			},
		},
		"emails": map[string]any{
			"email1": map[string]any{
				"address": "jane.doe@example.com",
				"contexts": map[string]bool{
					"work": true,
				},
				"pref": 1,
			},
		},
		"phones": map[string]any{
			"phone1": map[string]any{
				"number": "+1-555-0199",
				"features": map[string]bool{
					"voice": true,
					"cell":  true,
				},
			},
		},
	}

	setReqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"Card/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"card1": cardPayload,
					},
				},
				"c1",
			},
		},
	}

	setBytes, _ := json.Marshal(setReqBody)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(setBytes))
	if err != nil {
		t.Fatalf("Card/set POST failed: %v", err)
	}
	defer resp.Body.Close()

	var setResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&setResp); err != nil {
		t.Fatalf("Failed to decode Card/set response: %v", err)
	}

	setArgs := setResp.MethodResponses[0].Args
	created := setArgs["created"].(map[string]any)
	createdCardRaw, ok := created["card1"].(map[string]any)
	if !ok {
		t.Fatalf("Card creation failed: %v", setArgs)
	}

	cardID := createdCardRaw["id"].(string)

	// 2. Query Cards via Card/query
	queryReqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"Card/query",
				map[string]any{
					"accountId": "primary",
					"filter": map[string]any{
						"inAddressBook": "ab-default",
					},
				},
				"c2",
			},
		},
	}

	queryBytes, _ := json.Marshal(queryReqBody)
	resp2, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(queryBytes))
	if err != nil {
		t.Fatalf("Card/query POST failed: %v", err)
	}
	defer resp2.Body.Close()

	var queryResp jmap.Response
	if err := json.NewDecoder(resp2.Body).Decode(&queryResp); err != nil {
		t.Fatalf("Failed to decode Card/query response: %v", err)
	}

	queryArgs := queryResp.MethodResponses[0].Args
	idsRaw := queryArgs["ids"].([]any)
	if len(idsRaw) != 1 || idsRaw[0].(string) != cardID {
		t.Fatalf("Card/query unexpected result: %v", queryArgs)
	}

	// 3. Retrieve card details via Card/get
	getReqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"Card/get",
				map[string]any{
					"accountId": "primary",
					"ids":       []string{cardID},
				},
				"c3",
			},
		},
	}

	getBytes, _ := json.Marshal(getReqBody)
	resp3, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(getBytes))
	if err != nil {
		t.Fatalf("Card/get POST failed: %v", err)
	}
	defer resp3.Body.Close()

	var getResp jmap.Response
	if err := json.NewDecoder(resp3.Body).Decode(&getResp); err != nil {
		t.Fatalf("Failed to decode Card/get response: %v", err)
	}

	getArgs := getResp.MethodResponses[0].Args
	cardList := getArgs["list"].([]any)
	if len(cardList) != 1 {
		t.Fatalf("Expected 1 card in Card/get response, got %d", len(cardList))
	}
}
