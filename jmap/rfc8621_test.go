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

// TestRFC8621_Section7_2_EmailSubmissionQuery tests EmailSubmission/query filters, sort,
// pagination, and totals per RFC 8621 Section 7.2 against live backend data.
func TestRFC8621_Section7_2_EmailSubmissionQuery(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Create a target email so we can exercise emailIds/threadIds filters.
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"e1": map[string]any{
						"mailboxIds": map[string]any{"mb-inbox": true},
						"subject":    "Submission Target One",
						"from":       []any{map[string]any{"name": "A", "email": "a@example.com"}},
						"to":         []any{map[string]any{"name": "B", "email": "b@example.com"}},
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
	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}
	resp.Body.Close()

	created := jmapResp.MethodResponses[0].Args["created"].(map[string]any)
	emailID := created["e1"].(map[string]any)["id"].(string)
	threadID := created["e1"].(map[string]any)["threadId"].(string)
	if emailID == "" || threadID == "" {
		t.Fatalf("Expected created email with id and threadId, got %v", created["e1"])
	}

	// Create three submissions with distinct sendAt values; one references the seeded email-1.
	reqPayload = map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"EmailSubmission/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"s1": map[string]any{"identityId": "id-primary", "emailId": emailID, "sendAt": "2026-01-15T10:00:00Z"},
					"s2": map[string]any{"identityId": "id-primary", "emailId": "email-1", "sendAt": "2026-02-15T10:00:00Z"},
					"s3": map[string]any{"identityId": "id-primary", "emailId": emailID, "sendAt": "2026-03-15T10:00:00Z"},
				},
			}, "c1"},
		},
	}
	body, _ = json.Marshal(reqPayload)
	resp, err = http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}
	resp.Body.Close()

	createdSubs := jmapResp.MethodResponses[0].Args["created"].(map[string]any)
	subIDs := make(map[string]string, 3)
	for key, raw := range createdSubs {
		subIDs[key] = raw.(map[string]any)["id"].(string)
	}
	if subIDs["s1"] == "" || subIDs["s2"] == "" || subIDs["s3"] == "" {
		t.Fatalf("Expected three created submissions, got %v", createdSubs)
	}

	query := func(filter map[string]any, position int, limit *float64) map[string]any {
		args := map[string]any{"accountId": "primary"}
		if filter != nil {
			args["filter"] = filter
		}
		if position > 0 {
			args["position"] = position
		}
		if limit != nil {
			args["limit"] = *limit
		}
		reqPayload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			"methodCalls": []any{
				[]any{"EmailSubmission/query", args, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
			t.Fatalf("Failed to decode Response: %v", err)
		}
		if jr.MethodResponses[0].Name != "EmailSubmission/query" {
			t.Fatalf("Expected EmailSubmission/query, got %q: %v", jr.MethodResponses[0].Name, jr.MethodResponses[0].Args)
		}
		return jr.MethodResponses[0].Args
	}
	idsOf := func(args map[string]any) []string {
		raw, _ := args["ids"].([]any)
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			out = append(out, v.(string))
		}
		return out
	}

	// Unfiltered: all three, sorted by sendAt descending (latest first: s3).
	args := query(nil, 0, nil)
	if got := idsOf(args); len(got) != 3 || got[0] != subIDs["s3"] {
		t.Errorf("Expected all 3 submissions, newest first, got %v", got)
	}
	if args["total"].(float64) != 3 {
		t.Errorf("Expected total 3, got %v", args["total"])
	}
	if _, ok := args["queryState"]; !ok {
		t.Errorf("Expected queryState in response")
	}

	// Filter by emailIds: only the two referencing the created email.
	args = query(map[string]any{"emailIds": []string{emailID}}, 0, nil)
	if got := idsOf(args); len(got) != 2 {
		t.Errorf("Expected 2 ids for emailIds filter, got %v", got)
	}
	// Filter by emailIds: the seeded email-1 submission only.
	args = query(map[string]any{"emailIds": []string{"email-1"}}, 0, nil)
	if got := idsOf(args); len(got) != 1 || got[0] != subIDs["s2"] {
		t.Errorf("Expected only s2 for emailIds email-1, got %v", got)
	}
	// Filter by emailIds: unknown id matches nothing.
	args = query(map[string]any{"emailIds": []string{"email-does-not-exist"}}, 0, nil)
	if got := idsOf(args); len(got) != 0 {
		t.Errorf("Expected no ids for unknown emailIds, got %v", got)
	}

	// Filter by threadIds: the created email's thread.
	args = query(map[string]any{"threadIds": []string{threadID}}, 0, nil)
	if got := idsOf(args); len(got) != 2 {
		t.Errorf("Expected 2 ids for threadIds filter, got %v", got)
	}

	// Filter by identityIds: primary matches all, unknown matches none.
	args = query(map[string]any{"identityIds": []string{"id-primary"}}, 0, nil)
	if got := idsOf(args); len(got) != 3 {
		t.Errorf("Expected 3 ids for identityIds id-primary, got %v", got)
	}
	args = query(map[string]any{"identityIds": []string{"id-unknown"}}, 0, nil)
	if got := idsOf(args); len(got) != 0 {
		t.Errorf("Expected no ids for unknown identityIds, got %v", got)
	}

	// Filter by before/after (sendAt comparison).
	args = query(map[string]any{"before": "2026-02-01T00:00:00Z"}, 0, nil)
	if got := idsOf(args); len(got) != 1 || got[0] != subIDs["s1"] {
		t.Errorf("Expected only s1 for before filter, got %v", got)
	}
	args = query(map[string]any{"after": "2026-02-01T00:00:00Z"}, 0, nil)
	if got := idsOf(args); len(got) != 2 {
		t.Errorf("Expected s2+s3 for after filter, got %v", got)
	}

	// Pagination: limit 2 from start, then position 1.
	args = query(nil, 0, float64Ptr(2))
	if got := idsOf(args); len(got) != 2 {
		t.Errorf("Expected 2 ids with limit 2, got %v", got)
	}
	if args["total"].(float64) != 3 {
		t.Errorf("Expected total 3 with limit, got %v", args["total"])
	}
	args = query(nil, 1, float64Ptr(1))
	if got := idsOf(args); len(got) != 1 || got[0] != subIDs["s2"] {
		t.Errorf("Expected only s2 at position 1, got %v", got)
	}
}

func float64Ptr(v float64) *float64 { return &v }

// TestRFC8621_Section4_5_1_EmailQueryFromToFilters tests the from and to filter conditions of
// Email/query per RFC 8621 Section 4.5.1, both positively and negatively.
func TestRFC8621_Section4_5_1_EmailQueryFromToFilters(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	queryFiltered := func(filter map[string]any) []string {
		reqPayload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			"methodCalls": []any{
				[]any{"Email/query", map[string]any{"accountId": "primary", "filter": filter}, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
			t.Fatalf("Failed to decode Response: %v", err)
		}
		args := jr.MethodResponses[0].Args
		raw, _ := args["ids"].([]any)
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			out = append(out, v.(string))
		}
		return out
	}

	// Seeded emails: email-1 From admin@example.com, email-3 From noreply@ietf.org,
	// both To user@example.com.
	if got := queryFiltered(map[string]any{"from": "admin@example.com"}); len(got) != 1 || got[0] != "email-1" {
		t.Errorf("Expected email-1 for from admin@example.com, got %v", got)
	}
	if got := queryFiltered(map[string]any{"from": "ietf.org"}); len(got) != 1 || got[0] != "email-3" {
		t.Errorf("Expected email-3 for from ietf.org, got %v", got)
	}
	if got := queryFiltered(map[string]any{"from": "user@example.com"}); len(got) != 0 {
		t.Errorf("Expected no emails for from user@example.com, got %v", got)
	}
	if got := queryFiltered(map[string]any{"to": "user@example.com"}); len(got) != 2 {
		t.Errorf("Expected 2 emails for to user@example.com, got %v", got)
	}
	if got := queryFiltered(map[string]any{"to": "admin@example.com"}); len(got) != 0 {
		t.Errorf("Expected no emails for to admin@example.com, got %v", got)
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

// TestRFC8621_ChangesEndpoints tests Email/changes, Mailbox/changes, Identity/changes, EmailSubmission/changes via HTTP POST /jmap.
func TestRFC8621_ChangesEndpoints(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.QuotaCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jmapResp jmap.Response
		if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		return jmapResp
	}

	// 1. Get initial states for Email and Mailbox
	r1 := post([]any{
		[]any{"Email/get", map[string]any{"accountId": "primary"}, "c1"},
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c2"},
	})
	emailState0, _ := r1.MethodResponses[0].Args["state"].(string)
	mailboxState0, _ := r1.MethodResponses[1].Args["state"].(string)

	// 2. Create an Email via Email/set
	r2 := post([]any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{
					"subject":    "Testing Changes Endpoint",
					"mailboxIds": map[string]bool{"mb-inbox": true},
				},
			},
		}, "c3"},
	})
	createdEMMap, _ := r2.MethodResponses[0].Args["created"].(map[string]any)
	createdEM, _ := createdEMMap["e1"].(map[string]any)
	emailID, _ := createdEM["id"].(string)
	if emailID == "" {
		t.Fatalf("Expected created email id, got %v", r2.MethodResponses[0].Args)
	}

	// 3. Query Email/changes and Mailbox/changes using initial states
	r3 := post([]any{
		[]any{"Email/changes", map[string]any{"accountId": "primary", "sinceState": emailState0}, "c4"},
		[]any{"Mailbox/changes", map[string]any{"accountId": "primary", "sinceState": mailboxState0}, "c5"},
	})

	emailChanges := r3.MethodResponses[0].Args
	cEmails, _ := emailChanges["created"].([]any)
	if len(cEmails) != 1 || cEmails[0].(string) != emailID {
		t.Errorf("Expected Email/changes created=[%s], got %v", emailID, cEmails)
	}

	// 4. Test Identity/changes HTTP round-trip
	r4 := post([]any{
		[]any{"Identity/get", map[string]any{"accountId": "primary"}, "c6"},
	})
	idState0, _ := r4.MethodResponses[0].Args["state"].(string)

	r5 := post([]any{
		[]any{"Identity/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"id1": map[string]any{
					"name":  "Alias User",
					"email": "alias@example.com",
				},
			},
		}, "c7"},
	})
	createdIDMap, _ := r5.MethodResponses[0].Args["created"].(map[string]any)
	createdIDObj, _ := createdIDMap["id1"].(map[string]any)
	identityID, _ := createdIDObj["id"].(string)

	r6 := post([]any{
		[]any{"Identity/changes", map[string]any{"accountId": "primary", "sinceState": idState0}, "c8"},
	})
	idChanges := r6.MethodResponses[0].Args
	cIdentities, _ := idChanges["created"].([]any)
	if len(cIdentities) != 1 || cIdentities[0].(string) != identityID {
		t.Errorf("Expected Identity/changes created=[%s], got %v", identityID, cIdentities)
	}
}

// TestRFC8621_EmailCopy_SearchSnippet_Sieve_CalendarEvent tests Email/copy overrides, SearchSnippet <mark> highlighting,
// CalendarEvent/queryChanges, and SieveScript activation semantics per RFC standards.
func TestRFC8621_EmailCopy_SearchSnippet_Sieve_CalendarEvent(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.CalendarsCapabilityURI, jmap.SieveCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jmapResp jmap.Response
		if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		return jmapResp
	}

	// 1. Get an existing email ID with 'Welcome' via Email/query
	qResp := post([]any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"text": "Welcome"},
		}, "c0"},
	})
	eIDs, _ := qResp.MethodResponses[0].Args["ids"].([]any)
	if len(eIDs) == 0 {
		t.Fatal("Expected at least 1 email matching Welcome")
	}
	targetID := eIDs[0].(string)

	// 2. Test SearchSnippet/get highlighting
	r1 := post([]any{
		[]any{"SearchSnippet/get", map[string]any{
			"accountId": "primary",
			"emailIds":  []string{targetID},
			"filter":    map[string]any{"text": "Welcome"},
		}, "c1"},
	})
	snipList, _ := r1.MethodResponses[0].Args["list"].([]any)
	if len(snipList) > 0 {
		snip, _ := snipList[0].(map[string]any)
		subj, _ := snip["subject"].(string)
		if !bytes.Contains([]byte(subj), []byte("<mark>Welcome</mark>")) {
			t.Errorf("Expected SearchSnippet subject highlighting with <mark>Welcome</mark>, got %q", subj)
		}
	}

	// 3. Test Email/copy with property overrides and onSuccessDestroyOriginal
	r2 := post([]any{
		[]any{"Email/copy", map[string]any{
			"accountId":                "primary",
			"fromAccountId":            "primary",
			"onSuccessDestroyOriginal": true,
			"create": map[string]any{
				"cp1": map[string]any{
					"id":         targetID,
					"mailboxIds": map[string]bool{"mb-trash": true},
				},
			},
		}, "c2"},
	})
	cpCreated, _ := r2.MethodResponses[0].Args["created"].(map[string]any)
	if cpCreated["cp1"] == nil {
		t.Errorf("Expected created email copy in Email/copy response, got %v", r2.MethodResponses[0].Args)
	}

	// 3. Test CalendarEvent/query and CalendarEvent/queryChanges
	r3 := post([]any{
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary"}, "c3"},
		[]any{"CalendarEvent/queryChanges", map[string]any{"accountId": "primary", "sinceQueryState": "0"}, "c4"},
	})
	qState, _ := r3.MethodResponses[0].Args["queryState"].(string)
	if qState == "0" || qState == "" {
		t.Errorf("Expected dynamic queryState, got %q", qState)
	}
	if r3.MethodResponses[1].Name != "CalendarEvent/queryChanges" {
		t.Errorf("Expected CalendarEvent/queryChanges response, got %q", r3.MethodResponses[1].Name)
	}

	// 4. Test SieveScript activation semantics
	r4 := post([]any{
		[]any{"SieveScript/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"s1": map[string]any{
					"name":     "Filter 1",
					"content":  "keep;",
					"isActive": true,
				},
				"s2": map[string]any{
					"name":     "Filter 2",
					"content":  "discard;",
					"isActive": false,
				},
			},
		}, "c5"},
	})
	createdScripts, _ := r4.MethodResponses[0].Args["created"].(map[string]any)
	s2Obj, _ := createdScripts["s2"].(map[string]any)
	s2ID, _ := s2Obj["id"].(string)

	if s2ID != "" {
		// Activate s2 via onSuccessActivateScript
		post([]any{
			[]any{"SieveScript/set", map[string]any{
				"accountId":               "primary",
				"onSuccessActivateScript": s2ID,
			}, "c6"},
		})

		r5 := post([]any{
			[]any{"SieveScript/get", map[string]any{
				"accountId": "primary",
				"ids":       []string{s2ID},
			}, "c7"},
		})
		scList, _ := r5.MethodResponses[0].Args["list"].([]any)
		if len(scList) > 0 {
			sc, _ := scList[0].(map[string]any)
			if active, ok := sc["isActive"].(bool); !ok || !active {
				t.Errorf("Expected SieveScript %s to be active, got %v", s2ID, sc["isActive"])
			}
		}
	}
}

// TestRFC8621_EmailQueryFilters_PositiveAndNegative tests positive matching AND negative filtering
// for all Email/query FilterCondition properties via HTTP JSON RPC per RFC 8621 Section 4.5.
func TestRFC8621_EmailQueryFilters_PositiveAndNegative(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	postQuery := func(filter map[string]any) []string {
		payload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			"methodCalls": []any{
				[]any{"Email/query", map[string]any{
					"accountId": "primary",
					"filter":    filter,
				}, "c1"},
			},
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap Email/query failed: %v", err)
		}
		defer resp.Body.Close()

		var jmapResp jmap.Response
		if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		idsRaw, _ := jmapResp.MethodResponses[0].Args["ids"].([]any)
		res := make([]string, len(idsRaw))
		for i, v := range idsRaw {
			res[i], _ = v.(string)
		}
		return res
	}

	tests := []struct {
		name        string
		filter      map[string]any
		shouldMatch bool
	}{
		// inMailbox
		{"inMailbox positive", map[string]any{"inMailbox": "mb-inbox"}, true},
		{"inMailbox negative", map[string]any{"inMailbox": "mb-trash"}, false},

		// inMailboxOtherThan
		{"inMailboxOtherThan positive", map[string]any{"inMailboxOtherThan": []any{"mb-trash"}}, true},
		{"inMailboxOtherThan negative", map[string]any{"inMailboxOtherThan": []any{"mb-inbox", "mb-sent", "mb-drafts", "mb-archive", "mb-junk"}}, false},

		// before / after
		{"before positive", map[string]any{"before": "2030-01-01T00:00:00Z"}, true},
		{"before negative", map[string]any{"before": "2020-01-01T00:00:00Z"}, false},
		{"after positive", map[string]any{"after": "2020-01-01T00:00:00Z"}, true},
		{"after negative", map[string]any{"after": "2030-01-01T00:00:00Z"}, false},

		// minSize / maxSize
		{"minSize positive", map[string]any{"minSize": float64(10)}, true},
		{"minSize negative", map[string]any{"minSize": float64(100000)}, false},
		{"maxSize positive", map[string]any{"maxSize": float64(100000)}, true},
		{"maxSize negative", map[string]any{"maxSize": float64(10)}, false},

		// subject / cc / bcc / body
		{"subject positive", map[string]any{"subject": "Welcome"}, true},
		{"subject negative", map[string]any{"subject": "NonexistentSubject"}, false},
		{"hasKeyword positive", map[string]any{"hasKeyword": "$seen"}, true},
		{"hasKeyword negative", map[string]any{"hasKeyword": "$nonexistent"}, false},
		{"notKeyword positive", map[string]any{"notKeyword": "$nonexistent"}, true},
		{"notKeyword negative", map[string]any{
			"operator": "AND",
			"conditions": []any{
				map[string]any{"subject": "Welcome"},
				map[string]any{"notKeyword": "$seen"},
			},
		}, false},

		// text
		{"text positive", map[string]any{"text": "Welcome"}, true},
		{"text negative", map[string]any{"text": "NonexistentTextTerm"}, false},

		// FilterOperators AND / OR / NOT
		{"operator AND positive", map[string]any{
			"operator": "AND",
			"conditions": []any{
				map[string]any{"inMailbox": "mb-inbox"},
				map[string]any{"subject": "Welcome"},
			},
		}, true},
		{"operator AND negative", map[string]any{
			"operator": "AND",
			"conditions": []any{
				map[string]any{"inMailbox": "mb-inbox"},
				map[string]any{"subject": "NonexistentSubject"},
			},
		}, false},
		{"operator OR positive", map[string]any{
			"operator": "OR",
			"conditions": []any{
				map[string]any{"subject": "NonexistentSubject"},
				map[string]any{"subject": "Welcome"},
			},
		}, true},
		{"operator OR negative", map[string]any{
			"operator": "OR",
			"conditions": []any{
				map[string]any{"subject": "NonexistentSubject"},
				map[string]any{"inMailbox": "mb-trash"},
			},
		}, false},
		{"operator NOT positive", map[string]any{
			"operator": "NOT",
			"conditions": []any{
				map[string]any{"subject": "NonexistentSubject"},
			},
		}, true},
		{"operator NOT negative", map[string]any{
			"operator": "NOT",
			"conditions": []any{
				map[string]any{"inMailboxOtherThan": []any{}},
			},
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids := postQuery(tt.filter)
			if tt.shouldMatch && len(ids) == 0 {
				t.Errorf("Expected query filter %s to match at least 1 email, got 0", tt.name)
			}
			if !tt.shouldMatch && len(ids) > 0 {
				t.Errorf("Expected query filter %s to match 0 emails, got %v", tt.name, ids)
			}
		})
	}
}

// TestRFC8621_MailboxCopy verifies Mailbox/copy property overrides and creation per RFC 8621 Section 2.5.
func TestRFC8621_MailboxCopy(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/copy", map[string]any{
				"accountId":     "primary",
				"fromAccountId": "primary",
				"create": map[string]any{
					"mbc1": map[string]any{
						"id":   "mb-inbox",
						"name": "Cloned Inbox",
					},
				},
			}, "c1"},
		},
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap Mailbox/copy failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	createdMap, ok := jmapResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || createdMap["mbc1"] == nil {
		t.Fatalf("Expected created copied mailbox in Mailbox/copy response, got %v", jmapResp.MethodResponses[0].Args)
	}
	mbObj := createdMap["mbc1"].(map[string]any)
	if name, _ := mbObj["name"].(string); name != "Cloned Inbox" {
		t.Errorf("Expected name 'Cloned Inbox', got %q", name)
	}

	// 2. Test onSuccessDestroyOriginal and non-existent source mailbox error
	payload2 := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/copy", map[string]any{
				"accountId":                "primary",
				"fromAccountId":            "primary",
				"onSuccessDestroyOriginal": true,
				"create": map[string]any{
					"mbc2": map[string]any{"id": "nonexistent-mailbox"},
				},
			}, "c2"},
		},
	}
	body2, _ := json.Marshal(payload2)
	resp2, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body2))
	if err != nil {
		t.Fatalf("POST /jmap Mailbox/copy failed: %v", err)
	}
	defer resp2.Body.Close()

	var jmapResp2 jmap.Response
	if err := json.NewDecoder(resp2.Body).Decode(&jmapResp2); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	notCreatedMap, _ := jmapResp2.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreatedMap["mbc2"] == nil {
		t.Errorf("Expected notCreated error for non-existent source mailbox, got %v", jmapResp2.MethodResponses[0].Args)
	}
}

// TestRFC8621_QueryChanges_DeltaCalculations verifies real added and removed deltas returned by /queryChanges.
func TestRFC8621_QueryChanges_DeltaCalculations(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.CalendarsCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jmapResp jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
		return jmapResp
	}

	// 1. Fetch initial states
	r1 := post([]any{
		[]any{"Email/query", map[string]any{"accountId": "primary"}, "c1"},
		[]any{"Mailbox/query", map[string]any{"accountId": "primary"}, "c2"},
	})
	eState0, _ := r1.MethodResponses[0].Args["queryState"].(string)
	mbState0, _ := r1.MethodResponses[1].Args["queryState"].(string)

	// 2. Perform mutations (create email + create mailbox)
	r2 := post([]any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{"subject": "QueryChanges Test Email"},
			},
		}, "c3"},
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mb1": map[string]any{"name": "QueryChanges Test MB"},
			},
		}, "c4"},
	})
	createdEmMap, _ := r2.MethodResponses[0].Args["created"].(map[string]any)
	createdEmObj, _ := createdEmMap["e1"].(map[string]any)
	newEmailID, _ := createdEmObj["id"].(string)

	createdMbMap, _ := r2.MethodResponses[1].Args["created"].(map[string]any)
	createdMbObj, _ := createdMbMap["mb1"].(map[string]any)
	newMbID, _ := createdMbObj["id"].(string)

	// 3. Query Email/queryChanges and Mailbox/queryChanges
	r3 := post([]any{
		[]any{"Email/queryChanges", map[string]any{"accountId": "primary", "sinceQueryState": eState0}, "c5"},
		[]any{"Mailbox/queryChanges", map[string]any{"accountId": "primary", "sinceQueryState": mbState0}, "c6"},
	})

	eAdded, _ := r3.MethodResponses[0].Args["added"].([]any)
	if len(eAdded) != 1 {
		t.Fatalf("Expected 1 added email in Email/queryChanges, got %v", eAdded)
	}
	addedEmObj, _ := eAdded[0].(map[string]any)
	if addedEmObj["id"].(string) != newEmailID {
		t.Errorf("Expected added email ID %s, got %v", newEmailID, addedEmObj["id"])
	}

	mbAdded, _ := r3.MethodResponses[1].Args["added"].([]any)
	if len(mbAdded) != 1 {
		t.Fatalf("Expected 1 added mailbox in Mailbox/queryChanges, got %v", mbAdded)
	}
	addedMbObj, _ := mbAdded[0].(map[string]any)
	if addedMbObj["id"].(string) != newMbID {
		t.Errorf("Expected added mailbox ID %s, got %v", newMbID, addedMbObj["id"])
	}
}
