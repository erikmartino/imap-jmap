package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9007_SessionCapability tests urn:ietf:params:jmap:mdn capability declaration in JMAP session per RFC 9007 Section 2.
func TestRFC9007_SessionCapability(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("Failed to fetch session: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session JSON: %v", err)
	}

	if _, ok := session.Capabilities[jmap.MdnCapabilityURI]; !ok {
		t.Errorf("Expected session capabilities to contain %q", jmap.MdnCapabilityURI)
	}

	primaryAcc, ok := session.Accounts["primary"]
	if !ok {
		t.Fatalf("Primary account missing")
	}

	if _, ok := primaryAcc.AccountCapabilities[jmap.MdnCapabilityURI]; !ok {
		t.Errorf("Expected account capabilities to contain %q", jmap.MdnCapabilityURI)
	}
}

// TestRFC9007_MDNSend tests MDN/send method call per RFC 9007 Section 3.1.
func TestRFC9007_MDNSend(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-1",
				"send": map[string]any{
					"mdn1": map[string]any{
						"forEmailId": "email-1",
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "MDN-sent-manually",
							"type":        "displayed",
						},
						"textBody": "I have read your email.",
					},
					"mdn2": map[string]any{
						"forEmailId": "invalid-email-id",
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "MDN-sent-manually",
							"type":        "displayed",
						},
					},
				},
			}, "call-1"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap MDN/send failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode JMAP response: %v", err)
	}

	if len(jmapResp.MethodResponses) == 0 {
		t.Fatalf("Empty method responses")
	}

	methodCall := jmapResp.MethodResponses[0].([]any)
	if methodCall[0] != "MDN/send" {
		t.Fatalf("Expected MDN/send method response, got %v", methodCall[0])
	}

	args := methodCall[1].(map[string]any)
	sentMap, _ := args["sent"].(map[string]any)
	notSentMap, _ := args["notSent"].(map[string]any)

	if _, ok := sentMap["mdn1"]; !ok {
		t.Errorf("Expected mdn1 to be in sent map, got %+v", sentMap)
	}

	if _, ok := notSentMap["mdn2"]; !ok {
		t.Errorf("Expected mdn2 to be in notSent map due to invalid email ID, got %+v", notSentMap)
	}
}

// TestRFC9007_MDNParse tests MDN/parse method call per RFC 9007 Section 3.2.
func TestRFC9007_MDNParse(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/parse", map[string]any{
				"accountId": "primary",
				"blobIds":   []any{"blob-1"},
			}, "call-1"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap MDN/parse failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode JMAP response: %v", err)
	}

	if len(jmapResp.MethodResponses) == 0 {
		t.Fatalf("Empty method responses")
	}

	methodCall := jmapResp.MethodResponses[0].([]any)
	if methodCall[0] != "MDN/parse" {
		t.Fatalf("Expected MDN/parse method response, got %v", methodCall[0])
	}

	args := methodCall[1].(map[string]any)
	parsedMap, _ := args["parsed"].(map[string]any)

	if _, ok := parsedMap["blob-1"]; !ok {
		t.Errorf("Expected blob-1 to be in parsed map, got %+v", parsedMap)
	}
}
