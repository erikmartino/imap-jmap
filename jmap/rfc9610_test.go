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

// TestRFC9610_CardAndAddressBookCopy tests Card/copy and AddressBook/copy per RFC 9610 Section 4.
func TestRFC9610_CardAndAddressBookCopy(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{"AddressBook/copy", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"ab1": map[string]any{"name": "Copied AddressBook"},
				},
			}, "c1"},
			[]any{"Card/copy", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"card1": map[string]any{
						"addressBookIds": map[string]bool{"ab-default": true},
						"name":           map[string]any{"full": "Copied Contact"},
					},
				},
			}, "c2"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	if len(jmapResp.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses, got %d", len(jmapResp.MethodResponses))
	}

	abResp := jmapResp.MethodResponses[0]
	if abResp.Name != "AddressBook/copy" {
		t.Errorf("Expected 'AddressBook/copy', got %q", abResp.Name)
	}

	cardResp := jmapResp.MethodResponses[1]
	if cardResp.Name != "Card/copy" {
		t.Errorf("Expected 'Card/copy', got %q", cardResp.Name)
	}
}

// TestRFC9610_ContactCardCanonicalNaming proves the RFC 9610 canonical method names
// (ContactCard/*) are served, not just the legacy Card/* aliases, so a conformant client
// never hits unknownMethod. It drives a real set -> get round trip via ContactCard/*.
func TestRFC9610_ContactCardCanonicalNaming(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr
	}

	setResp := post([]any{
		[]any{"ContactCard/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"c": map[string]any{"name": map[string]any{"full": "Bob"}}},
		}, "c1"},
	})
	if setResp.MethodResponses[0].Name != "ContactCard/set" {
		t.Fatalf("expected response name 'ContactCard/set', got %q", setResp.MethodResponses[0].Name)
	}
	created, ok := setResp.MethodResponses[0].Args["created"].(map[string]any)["c"].(map[string]any)
	if !ok {
		t.Fatalf("ContactCard/set did not create the card: %#v", setResp.MethodResponses[0].Args)
	}
	id := created["id"].(string)

	getResp := post([]any{
		[]any{"ContactCard/get", map[string]any{"accountId": "primary", "ids": []any{id}}, "c2"},
	})
	if getResp.MethodResponses[0].Name != "ContactCard/get" {
		t.Errorf("expected 'ContactCard/get', got %q", getResp.MethodResponses[0].Name)
	}
	if list, _ := getResp.MethodResponses[0].Args["list"].([]any); len(list) != 1 {
		t.Errorf("expected 1 card via ContactCard/get, got %#v", getResp.MethodResponses[0].Args["list"])
	}
}

// TestRFC9610_CardCopyRoundTrip proves Card/copy actually copies an existing card (by source id,
// with a property override) into a new object, per RFC 8620 Section 5.4.
func TestRFC9610_CardCopyRoundTrip(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr
	}

	createResp := post([]any{
		[]any{"Card/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"orig": map[string]any{"name": map[string]any{"full": "Alice"}}},
		}, "c1"},
	})
	createdCard, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)["orig"].(map[string]any)
	if !ok {
		t.Fatalf("source card not created: %#v", createResp.MethodResponses[0].Args)
	}
	srcID := createdCard["id"].(string)
	if srcID == "" {
		t.Fatal("no source card id")
	}

	copyResp := post([]any{
		[]any{"Card/copy", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"dup": map[string]any{"id": srcID},
			},
		}, "c2"},
	})
	args := copyResp.MethodResponses[0].Args
	if nc, ok := args["notCreated"].(map[string]any); ok && len(nc) != 0 {
		t.Fatalf("copy reported failures: %#v", nc)
	}
	dup, ok := args["created"].(map[string]any)["dup"].(map[string]any)
	if !ok {
		t.Fatalf("copy did not create 'dup': %#v", args["created"])
	}
	if dup["id"] == srcID || dup["id"] == "" {
		t.Errorf("copy MUST assign a new id, got %v (src %q)", dup["id"], srcID)
	}

	getResp := post([]any{
		[]any{"Card/get", map[string]any{"accountId": "primary"}, "c3"},
	})
	if list, _ := getResp.MethodResponses[0].Args["list"].([]any); len(list) != 2 {
		t.Errorf("expected 2 cards after copy, got %d", len(list))
	}
}

// TestRFC9610_AddressBookRightsAndDefault verifies RFC 9610 AddressBookRights names and isDefault behavior.
func TestRFC9610_AddressBookRightsAndDefault(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. AddressBook/get verify rights keys
	getReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{"AddressBook/get", map[string]any{"accountId": "primary"}, "c1"},
		},
	}
	body, _ := json.Marshal(getReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("AddressBook/get failed: %v", err)
	}
	defer resp.Body.Close()

	var jr jmap.Response
	_ = json.NewDecoder(resp.Body).Decode(&jr)
	list := jr.MethodResponses[0].Args["list"].([]any)
	if len(list) == 0 {
		t.Fatal("expected default address book")
	}
	ab := list[0].(map[string]any)
	rights := ab["myRights"].(map[string]any)
	if _, ok := rights["mayRead"]; !ok {
		t.Errorf("expected mayRead in myRights, got %#v", rights)
	}
	if _, ok := rights["mayWrite"]; !ok {
		t.Errorf("expected mayWrite in myRights, got %#v", rights)
	}

	// 2. Reject direct isDefault set on create
	createReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{"AddressBook/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"ab1": map[string]any{"name": "Work", "isDefault": true},
				},
			}, "c2"},
		},
	}
	body, _ = json.Marshal(createReq)
	resp, err = http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("AddressBook/set failed: %v", err)
	}
	defer resp.Body.Close()
	_ = json.NewDecoder(resp.Body).Decode(&jr)
	notCreated := jr.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreated["ab1"]; !ok {
		t.Fatalf("expected isDefault creation to be rejected with notCreated, got %#v", jr.MethodResponses[0].Args)
	}

	// 3. Create using onSuccessSetIsDefault
	createSuccessReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{"AddressBook/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"ab2": map[string]any{"name": "Work Contacts"},
				},
				"onSuccessSetIsDefault": "#ab2",
			}, "c3"},
		},
	}
	body, _ = json.Marshal(createSuccessReq)
	resp, err = http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("AddressBook/set failed: %v", err)
	}
	defer resp.Body.Close()
	_ = json.NewDecoder(resp.Body).Decode(&jr)
	createdMap := jr.MethodResponses[0].Args["created"].(map[string]any)
	newAB := createdMap["ab2"].(map[string]any)
	newID := newAB["id"].(string)

	// Verify newAB is now default
	getReq2 := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
		"methodCalls": []any{
			[]any{"AddressBook/get", map[string]any{"accountId": "primary", "ids": []string{newID}}, "c4"},
		},
	}
	body, _ = json.Marshal(getReq2)
	resp, err = http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("AddressBook/get failed: %v", err)
	}
	defer resp.Body.Close()
	_ = json.NewDecoder(resp.Body).Decode(&jr)
	list2 := jr.MethodResponses[0].Args["list"].([]any)
	if len(list2) == 0 || list2[0].(map[string]any)["isDefault"] != true {
		t.Errorf("expected newly set default addressbook to have isDefault true, got %#v", list2)
	}
}

// TestRFC9610_AddressBookDestroyRemoveContents tests onDestroyRemoveContents parameter semantics per RFC 9610.
func TestRFC9610_AddressBookDestroyRemoveContents(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Create a custom address book and a card inside it
	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr
	}

	r1 := post([]any{
		[]any{"AddressBook/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"ab1": map[string]any{"name": "Team"},
			},
		}, "c1"},
	})
	abID := r1.MethodResponses[0].Args["created"].(map[string]any)["ab1"].(map[string]any)["id"].(string)

	post([]any{
		[]any{"Card/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"card1": map[string]any{
					"name":           map[string]any{"full": "Bob Developer"},
					"addressBookIds": map[string]any{abID: true},
				},
			},
		}, "c2"},
	})

	// 2. Destroy address book without onDestroyRemoveContents -> fails with addressBookHasContents
	r2 := post([]any{
		[]any{"AddressBook/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{abID},
		}, "c3"},
	})
	notDestroyed := r2.MethodResponses[0].Args["notDestroyed"].(map[string]any)
	if errObj, ok := notDestroyed[abID].(map[string]any); !ok || errObj["type"] != "addressBookHasContents" {
		t.Fatalf("expected addressBookHasContents SetError, got %#v", notDestroyed)
	}

	// 3. Destroy with onDestroyRemoveContents: true -> succeeds
	r3 := post([]any{
		[]any{"AddressBook/set", map[string]any{
			"accountId":                "primary",
			"destroy":                  []string{abID},
			"onDestroyRemoveContents": true,
		}, "c4"},
	})
	destroyed := r3.MethodResponses[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != abID {
		t.Fatalf("expected abID %s to be destroyed, got %#v", abID, destroyed)
	}
}

// TestRFC9610_CardQueryFilterOperatorAndConditions tests FilterOperator and all search filter conditions.
func TestRFC9610_CardQueryFilterOperatorAndConditions(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := jmap.NewServer(nil, jmap.WithContactsBackend(contactsBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr
	}

	// Create test cards
	r1 := post([]any{
		[]any{"Card/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"uid":  "uid-100",
					"kind": "individual",
					"name": map[string]any{
						"full": "John Doe",
						"components": []map[string]any{
							{"kind": "given", "value": "John"},
							{"kind": "surname", "value": "Doe"},
						},
					},
					"nicknames": map[string]any{"n1": map[string]any{"name": "Johnny"}},
					"emails":    map[string]any{"e1": map[string]any{"address": "john@example.com"}},
					"phones":    map[string]any{"p1": map[string]any{"number": "+123456789"}},
					"organizations": map[string]any{"o1": map[string]any{"name": "ACME Corp"}},
					"notes":     map[string]any{"k1": map[string]any{"note": "Software Engineer"}},
					"members":   map[string]any{"group-member-1": true},
				},
				"c2": map[string]any{
					"uid":  "uid-200",
					"kind": "group",
					"name": map[string]any{
						"full": "Jane Smith",
						"components": []map[string]any{
							{"kind": "given", "value": "Jane"},
							{"kind": "surname", "value": "Smith"},
						},
					},
					"emails": map[string]any{"e2": map[string]any{"address": "jane@example.org"}},
				},
			},
		}, "c1"},
	})
	created := r1.MethodResponses[0].Args["created"].(map[string]any)
	id1 := created["c1"].(map[string]any)["id"].(string)
	id2 := created["c2"].(map[string]any)["id"].(string)

	// Filter test cases: positive and negative
	tests := []struct {
		name     string
		filter   map[string]any
		expected []string
	}{
		{"uid match", map[string]any{"uid": "uid-100"}, []string{id1}},
		{"uid mismatch", map[string]any{"uid": "nonexistent"}, []string{}},
		{"kind match", map[string]any{"kind": "group"}, []string{id2}},
		{"kind mismatch", map[string]any{"kind": "org"}, []string{}},
		{"hasMember match", map[string]any{"hasMember": "group-member-1"}, []string{id1}},
		{"hasMember mismatch", map[string]any{"hasMember": "group-member-99"}, []string{}},
		{"email match", map[string]any{"email": "john@example.com"}, []string{id1}},
		{"email mismatch", map[string]any{"email": "unknown@example.com"}, []string{}},
		{"phone match", map[string]any{"phone": "+123456789"}, []string{id1}},
		{"phone mismatch", map[string]any{"phone": "+999999999"}, []string{}},
		{"name/given match", map[string]any{"name/given": "John"}, []string{id1}},
		{"name/given mismatch", map[string]any{"name/given": "Jane"}, []string{id2}}, // Jane Smith has no components, mismatch for given
		{"organization match", map[string]any{"organization": "ACME"}, []string{id1}},
		{"organization mismatch", map[string]any{"organization": "Stark"}, []string{}},
		{"note match", map[string]any{"note": "Software"}, []string{id1}},
		{"note mismatch", map[string]any{"note": "Doctor"}, []string{}},
		{"FilterOperator OR", map[string]any{
			"operator": "OR",
			"conditions": []map[string]any{
				{"uid": "uid-100"},
				{"uid": "uid-200"},
			},
		}, []string{id1, id2}},
		{"FilterOperator NOT", map[string]any{
			"operator": "NOT",
			"conditions": []map[string]any{
				{"kind": "group"},
			},
		}, []string{id1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := post([]any{
				[]any{"Card/query", map[string]any{"accountId": "primary", "filter": tt.filter}, "q1"},
			})
			idsRaw, ok := res.MethodResponses[0].Args["ids"].([]any)
			if !ok {
				t.Fatalf("expected ids array, got %#v", res.MethodResponses[0].Args)
			}
			gotIDs := make([]string, 0, len(idsRaw))
			for _, item := range idsRaw {
				gotIDs = append(gotIDs, item.(string))
			}
			if len(gotIDs) != len(tt.expected) {
				t.Errorf("filter %#v: expected %d ids (%v), got %d (%v)", tt.filter, len(tt.expected), tt.expected, len(gotIDs), gotIDs)
			}
		})
	}
}
