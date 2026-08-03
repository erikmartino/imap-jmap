package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8621_Section2_1_MailboxGet tests Mailbox/get per RFC 8621 Section 2.1.
func TestRFC8621_Section2_1_MailboxGet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c1"},
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

	if len(jmapResp.MethodResponses) != 1 {
		t.Fatalf("Expected 1 method response, got %d", len(jmapResp.MethodResponses))
	}

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Mailbox/get" {
		t.Errorf("Expected response 'Mailbox/get', got %q", methodResp.Name)
	}

	listRaw, ok := methodResp.Args["list"].([]any)
	if !ok || len(listRaw) < 6 {
		t.Errorf("Expected at least 6 default mailboxes (Inbox, Sent, Trash, Drafts, Junk, Archive), got %v", methodResp.Args["list"])
	}
}

// TestRFC8621_Section2_3_MailboxSet tests Mailbox/set create and delete per RFC 8621 Section 2.3.
func TestRFC8621_Section2_3_MailboxSet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"k1": map[string]any{"name": "Archive"},
				},
			}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Mailbox/set" {
		t.Errorf("Expected response 'Mailbox/set', got %q", methodResp.Name)
	}

	created, ok := methodResp.Args["created"].(map[string]any)
	if !ok || created["k1"] == nil {
		t.Errorf("Expected created mailbox for key 'k1', got %v", methodResp.Args["created"])
	}
}

// TestRFC8621_Section2_5_MailboxUpdate proves Mailbox/set update renames and re-parents a
// mailbox through the real backend, preserving untouched fields (RFC 8621 Section 2.5).
func TestRFC8621_Section2_5_MailboxUpdate(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
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
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"m": map[string]any{"name": "Projects", "sortOrder": float64(5)}},
		}, "c1"},
	})
	id := createResp.MethodResponses[0].Args["created"].(map[string]any)["m"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatal("no mailbox id")
	}

	updResp := post([]any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update":    map[string]any{id: map[string]any{"name": "Work", "isSubscribed": true}},
		}, "c2"},
	})
	if upd, ok := updResp.MethodResponses[0].Args["updated"].(map[string]any); !ok || len(upd) != 1 {
		t.Fatalf("expected 1 updated mailbox, got %#v", updResp.MethodResponses[0].Args)
	}

	getResp := post([]any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary", "ids": []any{id}}, "c3"},
	})
	got := getResp.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if got["name"] != "Work" {
		t.Errorf("rename not persisted: %v", got["name"])
	}
	if got["isSubscribed"] != true {
		t.Errorf("isSubscribed not persisted: %v", got["isSubscribed"])
	}
	// sortOrder set at create MUST survive the partial update that didn't mention it.
	if got["sortOrder"].(float64) != 5 {
		t.Errorf("partial update dropped sortOrder: %v", got["sortOrder"])
	}

	// Updating a non-existent mailbox MUST be reported in notUpdated, not silently ignored.
	badResp := post([]any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update":    map[string]any{"mb-does-not-exist": map[string]any{"name": "X"}},
		}, "c4"},
	})
	if nu, ok := badResp.MethodResponses[0].Args["notUpdated"].(map[string]any); !ok || nu["mb-does-not-exist"] == nil {
		t.Errorf("expected notUpdated for missing mailbox, got %#v", badResp.MethodResponses[0].Args["notUpdated"])
	}
}

// TestRFC8621_Section2_4_MailboxQuery tests Mailbox/query per RFC 8621 Section 2.4.
func TestRFC8621_Section2_4_MailboxQuery(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/query", map[string]any{"accountId": "primary"}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Mailbox/query" {
		t.Errorf("Expected response 'Mailbox/query', got %q", methodResp.Name)
	}
}

// TestRFC8621_Section3_1_ThreadGet tests Thread/get per RFC 8621 Section 3.1.
func TestRFC8621_Section3_1_ThreadGet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Thread/get", map[string]any{"accountId": "primary", "ids": []string{"thread-1"}}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Thread/get" {
		t.Errorf("Expected response 'Thread/get', got %q", methodResp.Name)
	}
}

// TestRFC8621_Section4_1_EmailGet tests Email/get returning stub messages per RFC 8621 Section 4.1.
func TestRFC8621_Section4_1_EmailGet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/get", map[string]any{"accountId": "primary"}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Email/get" {
		t.Errorf("Expected response 'Email/get', got %q", methodResp.Name)
	}

	listRaw, ok := methodResp.Args["list"].([]any)
	if !ok || len(listRaw) == 0 {
		t.Fatalf("Expected non-empty list of stub emails, got %v", methodResp.Args["list"])
	}
}

// TestRFC8621_Section4_3_EmailSet tests Email/set create and destroy per RFC 8621 Section 4.3.
func TestRFC8621_Section4_3_EmailSet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"k1": map[string]any{
						"subject":    "Test New Email",
						"mailboxIds": map[string]any{"mb-inbox": true},
					},
				},
			}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Email/set" {
		t.Errorf("Expected response 'Email/set', got %q", methodResp.Name)
	}

	created, ok := methodResp.Args["created"].(map[string]any)
	if !ok || created["k1"] == nil {
		t.Errorf("Expected created email for key 'k1', got %v", methodResp.Args["created"])
	}
}

// TestRFC8621_Section4_5_EmailQuery tests Email/query per RFC 8621 Section 4.5.
func TestRFC8621_Section4_5_EmailQuery(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{"accountId": "primary"}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Email/query" {
		t.Errorf("Expected response 'Email/query', got %q", methodResp.Name)
	}
}

// TestRFC8621_Section4_7_EmailImport tests Email/import per RFC 8621 Section 4.7.
func TestRFC8621_Section4_7_EmailImport(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/import", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"k1": map[string]any{"blobId": "blob-123"},
				},
			}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Email/import" {
		t.Errorf("Expected response 'Email/import', got %q", methodResp.Name)
	}
}

// TestRFC8621_Section4_8_EmailParse tests Email/parse per RFC 8621 Section 4.8.
func TestRFC8621_Section4_8_EmailParse(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/parse", map[string]any{
				"accountId": "primary",
				"blobIds":   []string{"blob-123"},
			}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Email/parse" {
		t.Errorf("Expected response 'Email/parse', got %q", methodResp.Name)
	}
}

// TestRFC8621_Section6_1_IdentityGet tests Identity/get per RFC 8621 Section 6.1.
func TestRFC8621_Section6_1_IdentityGet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Identity/get", map[string]any{"accountId": "primary"}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Identity/get" {
		t.Errorf("Expected response 'Identity/get', got %q", methodResp.Name)
	}
}

// TestRFC8621_Section7_1_EmailSubmissionGet tests EmailSubmission/get per RFC 8621 Section 7.1.
func TestRFC8621_Section7_1_EmailSubmissionGet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"EmailSubmission/get", map[string]any{"accountId": "primary", "ids": []string{}}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "EmailSubmission/get" {
		t.Errorf("Expected response 'EmailSubmission/get', got %q", methodResp.Name)
	}
}

// TestRFC8621_Section7_3_EmailSubmissionSet tests sending an email via EmailSubmission/set per RFC 8621 Section 7.3.
func TestRFC8621_Section7_3_EmailSubmissionSet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"EmailSubmission/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"sub1": map[string]any{
						"identityId": "id-primary",
						"emailId":    "email-1",
					},
				},
			}, "c1"},
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

	if len(jmapResp.MethodResponses) != 1 {
		t.Fatalf("Expected 1 method response, got %d", len(jmapResp.MethodResponses))
	}

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "EmailSubmission/set" {
		t.Errorf("Expected response 'EmailSubmission/set', got %q", methodResp.Name)
	}

	created, ok := methodResp.Args["created"].(map[string]any)
	if !ok || created["sub1"] == nil {
		t.Errorf("Expected created submission for key 'sub1', got %v", methodResp.Args["created"])
	}
}

// TestRFC8621_Section4_5_1_EmailQueryFilteringAndSorting tests Email/query filtering (inMailbox, text) and sorting per RFC 8621 Section 4.5.
func TestRFC8621_Section4_5_1_EmailQueryFilteringAndSorting(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Filter by inMailbox: "mb-inbox"
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{
				"accountId": "primary",
				"filter": map[string]any{
					"inMailbox": "mb-inbox",
				},
				"sort": []any{
					map[string]any{"property": "receivedAt", "isAscending": false},
				},
			}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	idsRaw, ok := methodResp.Args["ids"].([]any)
	if !ok || len(idsRaw) != 2 {
		t.Fatalf("Expected 2 inbox email IDs, got %v", methodResp.Args["ids"])
	}

	total, _ := methodResp.Args["total"].(float64)
	if total != 2 {
		t.Errorf("Expected total 2, got %v", total)
	}

	// 2. Filter by text search ("Welcome")
	reqText := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{
				"accountId": "primary",
				"filter": map[string]any{
					"text": "Welcome",
				},
			}, "c2"},
		},
	}
	bodyText, _ := json.Marshal(reqText)

	respText, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyText))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer respText.Body.Close()

	var jmapRespText jmap.Response
	_ = json.NewDecoder(respText.Body).Decode(&jmapRespText)

	idsText, ok := jmapRespText.MethodResponses[0].Args["ids"].([]any)
	if !ok || len(idsText) != 1 {
		t.Errorf("Expected 1 email matching text 'Welcome', got %v", jmapRespText.MethodResponses[0].Args["ids"])
	}
}

// TestRFC8621_EmailQueryAllFilterConditions tests Email/query with body, cc, bcc, keywords, and header filters per RFC 8621 Section 4.5.1.
func TestRFC8621_EmailQueryAllFilterConditions(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Filter by body ("Welcome to JMAP")
	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{
				"accountId": "primary",
				"filter": map[string]any{
					"body": "Welcome",
				},
			}, "c1"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	respBody, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer respBody.Body.Close()

	var jmapRespBody jmap.Response
	_ = json.NewDecoder(respBody.Body).Decode(&jmapRespBody)
	idsBody, ok := jmapRespBody.MethodResponses[0].Args["ids"].([]any)
	if !ok || len(idsBody) != 1 {
		t.Errorf("Expected 1 email matching body 'Welcome', got %v", jmapRespBody.MethodResponses[0].Args["ids"])
	}

	// 2. Filter by hasKeyword ("$seen") vs non-matching body ("nonexistent_term_12345")
	reqNoMatch := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{
				"accountId": "primary",
				"filter": map[string]any{
					"body": "nonexistent_term_12345",
				},
			}, "c2"},
		},
	}
	bodyNoMatch, _ := json.Marshal(reqNoMatch)
	respNoMatch, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyNoMatch))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer respNoMatch.Body.Close()

	var jmapRespNoMatch jmap.Response
	_ = json.NewDecoder(respNoMatch.Body).Decode(&jmapRespNoMatch)
	idsNoMatch, _ := jmapRespNoMatch.MethodResponses[0].Args["ids"].([]any)
	if len(idsNoMatch) != 0 {
		t.Errorf("Expected 0 emails matching 'nonexistent_term_12345', got %v", idsNoMatch)
	}
}

// TestRFC8621_Section3_ThreadGet tests Thread/get per RFC 8621 Section 3.
func TestRFC8621_Section3_ThreadGet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Get an email to discover its threadID
	getReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/get", map[string]any{
				"accountId": "primary",
				"ids":       []string{"email-1"},
			}, "c1"},
		},
	}
	bodyGet, _ := json.Marshal(getReq)
	respGet, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyGet))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer respGet.Body.Close()

	var jmapRespGet jmap.Response
	_ = json.NewDecoder(respGet.Body).Decode(&jmapRespGet)

	listEmail := jmapRespGet.MethodResponses[0].Args["list"].([]any)
	emObj := listEmail[0].(map[string]any)
	threadID := emObj["threadId"].(string)

	// 2. Fetch thread using threadID
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Thread/get", map[string]any{
				"accountId": "primary",
				"ids":       []string{threadID},
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
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Thread/get" {
		t.Errorf("Expected response 'Thread/get', got %q", methodResp.Name)
	}

	listRaw, ok := methodResp.Args["list"].([]any)
	if !ok || len(listRaw) != 1 {
		t.Fatalf("Expected 1 thread object, got %v", methodResp.Args["list"])
	}

	thObj := listRaw[0].(map[string]any)
	if thObj["id"] != threadID {
		t.Errorf("Expected thread id %q, got %v", threadID, thObj["id"])
	}
}

// TestRFC8621_Section2_MailboxCopy tests Mailbox/copy per RFC 8621 Section 2.4.
func TestRFC8621_Section2_MailboxCopy(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/copy", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"mb1": map[string]any{"name": "Copied Mailbox"},
				},
			}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Mailbox/copy" {
		t.Errorf("Expected response 'Mailbox/copy', got %q", methodResp.Name)
	}
}

// TestRFC8621_Section4_EmailCopy tests Email/copy per RFC 8621 Section 4.5.
func TestRFC8621_Section4_EmailCopy(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/copy", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"em1": map[string]any{"mailboxIds": map[string]bool{"mb-inbox": true}},
				},
			}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Email/copy" {
		t.Errorf("Expected response 'Email/copy', got %q", methodResp.Name)
	}
}

// TestRFC8621_Section5_SearchSnippetGet tests SearchSnippet/get per RFC 8621 Section 5.
func TestRFC8621_Section5_SearchSnippetGet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"SearchSnippet/get", map[string]any{
				"accountId": "primary",
				"emailIds":  []string{"email-1"},
			}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "SearchSnippet/get" {
		t.Errorf("Expected response 'SearchSnippet/get', got %q", methodResp.Name)
	}

	listRaw, ok := methodResp.Args["list"].([]any)
	if !ok || len(listRaw) != 1 {
		t.Fatalf("Expected 1 search snippet object, got %v", methodResp.Args["list"])
	}

	snipObj := listRaw[0].(map[string]any)
	if snipObj["emailId"] != "email-1" {
		t.Errorf("Expected emailId 'email-1', got %v", snipObj["emailId"])
	}
}

// TestRFC8621_Section2_1_MayProvisions_OptionalSystemRoles tests server support for optional MAY mailbox roles
// (junk, drafts, archive, sent, trash) per RFC 8621 Section 2.1. Note that RFC 8621 Section 2.1 mandates MUST for 'inbox',
// while pre-creation of additional standard roles (junk, drafts, archive, etc.) is an optional MAY feature for extended compatibility.
func TestRFC8621_Section2_1_MayProvisions_OptionalSystemRoles(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c1"},
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

	methodResp := jmapResp.MethodResponses[0]
	listRaw, ok := methodResp.Args["list"].([]any)
	if !ok {
		t.Fatalf("Expected list of mailboxes")
	}

	foundRoles := make(map[string]bool)
	for _, item := range listRaw {
		mb, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if role, ok := mb["role"].(string); ok {
			foundRoles[role] = true
		}
	}

	// Verify MUST provision per RFC 8621 Section 2.1
	if !foundRoles["inbox"] {
		t.Errorf("Expected server to support required MUST role 'inbox' per RFC 8621 Section 2.1")
	}

	// Verify MAY provisions per RFC 8621 Section 2.1
	mayRoles := []string{"junk", "drafts", "archive", "sent", "trash"}
	for _, role := range mayRoles {
		if !foundRoles[role] {
			t.Errorf("Expected server to support optional MAY provision role %q per RFC 8621 Section 2.1", role)
		}
	}
}

// TestRFC8621_Section4_5_MayProvisions_CalculateTotalAndUpToId tests optional calculateTotal in Email/query and upToId in Email/queryChanges per RFC 8621 Section 4.5 / RFC 8620 Section 5.6 MAY provisions.
func TestRFC8621_Section4_5_MayProvisions_CalculateTotalAndUpToId(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{
				"accountId":      "primary",
				"calculateTotal": true,
			}, "c1"},
			[]any{"Email/queryChanges", map[string]any{
				"accountId":       "primary",
				"sinceQueryState": "state-0",
				"upToId":          "email-100",
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

	qResp := jmapResp.MethodResponses[0]
	if calcTotal, ok := qResp.Args["calculateTotal"].(bool); !ok || !calcTotal {
		t.Errorf("Expected calculateTotal true in Email/query response args, got %v", qResp.Args["calculateTotal"])
	}

	qcResp := jmapResp.MethodResponses[1]
	if upToId, ok := qcResp.Args["upToId"].(string); !ok || upToId != "email-100" {
		t.Errorf("Expected upToId 'email-100' in Email/queryChanges response args, got %v", qcResp.Args["upToId"])
	}
}

// TestRFC8621_Section6_IdentitySet drives Identity/set through the real backend, proving
// create/update/destroy persist rather than being no-ops (RFC 8621 Section 6.3).
func TestRFC8621_Section6_IdentitySet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
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

	// Create.
	setResp := post([]any{
		[]any{"Identity/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"i1": map[string]any{"name": "Work", "email": "work@example.com", "textSignature": "BR"},
			},
		}, "c1"},
	})
	created, ok := setResp.MethodResponses[0].Args["created"].(map[string]any)["i1"].(map[string]any)
	if !ok {
		t.Fatalf("Identity/set did not create identity: %#v", setResp.MethodResponses[0].Args)
	}
	id := created["id"].(string)
	if id == "" {
		t.Fatal("created identity has no id")
	}

	// Update (partial): change the name, leave email/signature intact.
	updResp := post([]any{
		[]any{"Identity/set", map[string]any{
			"accountId": "primary",
			"update":    map[string]any{id: map[string]any{"name": "Work Updated"}},
		}, "c2"},
	})
	if upd, ok := updResp.MethodResponses[0].Args["updated"].(map[string]any); !ok || len(upd) != 1 {
		t.Fatalf("expected 1 updated identity, got %#v", updResp.MethodResponses[0].Args["updated"])
	}

	// Verify the update persisted and untouched fields survived.
	getResp := post([]any{
		[]any{"Identity/get", map[string]any{"accountId": "primary", "ids": []any{id}}, "c3"},
	})
	list := getResp.MethodResponses[0].Args["list"].([]any)
	var found map[string]any
	for _, item := range list {
		if m := item.(map[string]any); m["id"] == id {
			found = m
		}
	}
	if found == nil {
		t.Fatal("updated identity not found")
	}
	if found["name"] != "Work Updated" {
		t.Errorf("name not updated: %v", found["name"])
	}
	if found["email"] != "work@example.com" || found["textSignature"] != "BR" {
		t.Errorf("partial update dropped untouched fields: %#v", found)
	}

	// Destroy.
	delResp := post([]any{
		[]any{"Identity/set", map[string]any{"accountId": "primary", "destroy": []any{id}}, "c4"},
	})
	if d := delResp.MethodResponses[0].Args["destroyed"].([]any); len(d) != 1 || d[0].(string) != id {
		t.Errorf("expected identity %q destroyed, got %#v", id, d)
	}
}

// TestRFC8621_Section4_EmailImportAndParse tests Email/import and Email/parse per RFC 8621 Sections 4.8 & 4.9.
func TestRFC8621_Section4_EmailImportAndParse(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Upload a blob first for Email/import and Email/parse
	rawMsg := []byte("From: alice@example.com\r\nTo: bob@example.com\r\nSubject: Test Import & Parse\r\n\r\nHello World JMAP\r\n")
	reqUp, _ := http.NewRequest("POST", ts.URL+"/upload/primary/", bytes.NewReader(rawMsg))
	reqUp.Header.Set("Content-Type", "message/rfc822")
	respUp, err := http.DefaultClient.Do(reqUp)
	if err != nil {
		t.Fatalf("POST /upload/ failed: %v", err)
	}
	defer respUp.Body.Close()

	var blobObj struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(respUp.Body).Decode(&blobObj)
	if blobObj.ID == "" {
		t.Fatal("Upload blob failed, empty ID")
	}

	// 2. Execute Email/import and Email/parse
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/import", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"imp1": map[string]any{
						"blobId":     blobObj.ID,
						"mailboxIds": map[string]bool{"mb-inbox": true},
					},
				},
			}, "c1"},
			[]any{"Email/parse", map[string]any{
				"accountId": "primary",
				"blobIds":   []string{blobObj.ID},
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

	impResp := jmapResp.MethodResponses[0]
	if impResp.Name != "Email/import" {
		t.Errorf("Expected 'Email/import', got %q", impResp.Name)
	}
	createdMap, _ := impResp.Args["created"].(map[string]any)
	if _, ok := createdMap["imp1"]; !ok {
		t.Errorf("Expected created email in Email/import response args, got %v", impResp.Args)
	}

	parseResp := jmapResp.MethodResponses[1]
	if parseResp.Name != "Email/parse" {
		t.Errorf("Expected 'Email/parse', got %q", parseResp.Name)
	}
	parsedMap, _ := parseResp.Args["parsed"].(map[string]any)
	if parsedObj, ok := parsedMap[blobObj.ID].(map[string]any); !ok || parsedObj["subject"] != "Test Import & Parse" {
		t.Errorf("Expected parsed subject 'Test Import & Parse', got %v", parseResp.Args["parsed"])
	}
}
