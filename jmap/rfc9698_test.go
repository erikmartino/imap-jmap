package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC9698_CapabilityDiscovery tests RFC 9698 capability advertising in session.
func TestRFC9698_CapabilityDiscovery(t *testing.T) {
	session := jmap.DefaultSession("http://localhost:8080", "user@example.com")
	srv := jmap.NewServer(session)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var sess jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}

	_, ok := sess.Capabilities[jmap.ImapAccessCapabilityURI]
	if !ok {
		t.Errorf("Expected RFC 9698 imapaccess capability %q in session", jmap.ImapAccessCapabilityURI)
	}
}

// TestRFC9698_IMAPAccountGetSetChanges tests IMAPAccount/get, IMAPAccount/set, and IMAPAccount/changes per RFC 9698.
func TestRFC9698_IMAPAccountGetSetChanges(t *testing.T) {
	imapBackend := memory.NewMemoryIMAPAccessBackend()
	srv := jmap.NewServer(nil, jmap.WithIMAPAccessBackend(imapBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. IMAPAccount/get
	getReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ImapAccessCapabilityURI},
		"methodCalls": []any{
			[]any{"IMAPAccount/get", map[string]any{"accountId": "primary"}, "c1"},
		},
	}
	bodyBytes, _ := json.Marshal(getReq)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST IMAPAccount/get failed: %v", err)
	}
	defer resp.Body.Close()

	var getResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&getResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	listRaw, ok := getResp.MethodResponses[0].Args["list"].([]any)
	if !ok || len(listRaw) != 1 {
		t.Fatalf("Expected 1 default IMAPAccount, got %v", getResp.MethodResponses[0].Args["list"])
	}

	accObj := listRaw[0].(map[string]any)
	if accObj["host"] != "imap.example.com" {
		t.Errorf("Expected host 'imap.example.com', got %v", accObj["host"])
	}

	// 2. IMAPAccount/set create
	createReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ImapAccessCapabilityURI},
		"methodCalls": []any{
			[]any{
				"IMAPAccount/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"a2": map[string]any{
							"host":     "imap2.example.com",
							"port":     993,
							"tls":      "always",
							"username": "altuser@example.com",
						},
					},
				},
				"c2",
			},
		},
	}

	bodyCreate, _ := json.Marshal(createReq)
	respCreate, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyCreate))
	if err != nil {
		t.Fatalf("POST IMAPAccount/set create failed: %v", err)
	}
	defer respCreate.Body.Close()

	var setResp jmap.Response
	_ = json.NewDecoder(respCreate.Body).Decode(&setResp)

	createdMap, ok := setResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || createdMap["a2"] == nil {
		t.Fatalf("Expected created IMAPAccount a2")
	}

	// 3. IMAPAccount/changes
	changesReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.ImapAccessCapabilityURI},
		"methodCalls": []any{
			[]any{
				"IMAPAccount/changes",
				map[string]any{
					"accountId":  "primary",
					"sinceState": "old-state-0",
				},
				"c3",
			},
		},
	}

	bodyChanges, _ := json.Marshal(changesReq)
	respChanges, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyChanges))
	if err != nil {
		t.Fatalf("POST IMAPAccount/changes failed: %v", err)
	}
	defer respChanges.Body.Close()

	var changesResp jmap.Response
	_ = json.NewDecoder(respChanges.Body).Decode(&changesResp)

	updatedList, ok := changesResp.MethodResponses[0].Args["updated"].([]any)
	if !ok || len(updatedList) == 0 {
		t.Errorf("Expected updated IMAPAccount IDs in changes response")
	}
}
