package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9219_Section2_Capability tests urn:ietf:params:jmap:smime capability discovery per RFC 9219 Section 2.
func TestRFC9219_Section2_Capability(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}

	capRaw, ok := session.Capabilities[jmap.SmimeCapabilityURI]
	if !ok {
		t.Fatalf("Capability %q missing in Session", jmap.SmimeCapabilityURI)
	}

	capBytes, _ := json.Marshal(capRaw)
	var smimeCap jmap.SmimeCapability
	_ = json.Unmarshal(capBytes, &smimeCap)

	if !smimeCap.SmimeVerificationSupported {
		t.Error("Expected smimeVerificationSupported to be true")
	}
}

// TestRFC9219_Section3_EmailSMIMEProperties tests S/MIME fields on Email/get per RFC 9219 Section 3.
func TestRFC9219_Section3_EmailSMIMEProperties(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SmimeCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/get", map[string]any{"accountId": "primary"}, "c1"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	methodResp := jmapResp.MethodResponses[0]
	listRaw, ok := methodResp.Args["list"].([]any)
	if !ok || len(listRaw) == 0 {
		t.Fatalf("Expected email list, got %v", methodResp.Args["list"])
	}

	var foundSmimeEmail map[string]any
	for _, item := range listRaw {
		if emMap, ok := item.(map[string]any); ok {
			if st, _ := emMap["smimeStatus"].(string); st == "signed" {
				foundSmimeEmail = emMap
				break
			}
		}
	}

	if foundSmimeEmail == nil {
		t.Fatalf("Expected at least one email with smimeStatus 'signed', got %v", listRaw)
	}

	verifiedWith, _ := foundSmimeEmail["smimeVerifiedWith"].(string)
	if verifiedWith != "admin@example.com" {
		t.Errorf("Expected smimeVerifiedWith 'admin@example.com', got %q", verifiedWith)
	}
}

// TestRFC9219_Section4_EmailVerifySmime tests Email/verifySmime method per RFC 9219 Section 4.
func TestRFC9219_Section4_EmailVerifySmime(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SmimeCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/verifySmime", map[string]any{
				"accountId": "primary",
				"emailIds":  []string{"email-1", "non-existent-email"},
			}, "c1"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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
	if methodResp.Name != "Email/verifySmime" {
		t.Errorf("Expected response 'Email/verifySmime', got %q", methodResp.Name)
	}

	verified, ok := methodResp.Args["verified"].(map[string]any)
	if !ok || verified["email-1"] == nil {
		t.Errorf("Expected verified result for 'email-1', got %v", methodResp.Args["verified"])
	}

	notFound, ok := methodResp.Args["notFound"].([]any)
	if !ok || len(notFound) != 1 {
		t.Errorf("Expected 1 notFound ID, got %v", methodResp.Args["notFound"])
	}
}
