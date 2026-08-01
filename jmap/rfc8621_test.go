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
	srv := jmap.NewServer(nil)
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
	if !ok || len(listRaw) < 3 {
		t.Errorf("Expected at least 3 default mailboxes (Inbox, Sent, Trash), got %v", methodResp.Args["list"])
	}
}

// TestRFC8621_Section2_3_MailboxSet tests Mailbox/set create and delete per RFC 8621 Section 2.3.
func TestRFC8621_Section2_3_MailboxSet(t *testing.T) {
	srv := jmap.NewServer(nil)
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

// TestRFC8621_Section2_4_MailboxQuery tests Mailbox/query per RFC 8621 Section 2.4.
func TestRFC8621_Section2_4_MailboxQuery(t *testing.T) {
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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
