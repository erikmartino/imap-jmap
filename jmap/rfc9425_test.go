package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9425_Section2_Capability tests urn:ietf:params:jmap:quota capability discovery per RFC 9425 Section 2.
func TestRFC9425_Section2_Capability(t *testing.T) {
	spectest.Require(t, "RFC9425", "2.1", "MUST",
		"Servers supporting this specification MUST add a property")

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

	capRaw, ok := session.Capabilities[jmap.QuotaCapabilityURI]
	if !ok {
		t.Fatalf("Capability %q missing in Session", jmap.QuotaCapabilityURI)
	}
	if m, ok := capRaw.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("session capability %q must be an empty object, got %v", jmap.QuotaCapabilityURI, capRaw)
	}

	acc, ok := session.Accounts[jmap.AccountIDForSubject(testUsername)]
	if !ok {
		t.Fatalf("primary account missing from session")
	}
	accCap, ok := acc.AccountCapabilities[jmap.QuotaCapabilityURI]
	if !ok {
		t.Fatalf("accountCapabilities missing %q", jmap.QuotaCapabilityURI)
	}
	if m, ok := accCap.(map[string]any); !ok || len(m) != 0 {
		t.Errorf("accountCapabilities %q must be an empty object, got %v", jmap.QuotaCapabilityURI, accCap)
	}
}

// TestRFC9425_Section4_QuotaGet tests Quota/get method per RFC 9425 Section 4.
func TestRFC9425_Section4_QuotaGet(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI},
		"methodCalls": []any{
			[]any{"Quota/get", map[string]any{
				"accountId": "primary",
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Quota/get" {
		t.Fatalf("Expected method response 'Quota/get', got %q", methodResp.Name)
	}

	listRaw, ok := methodResp.Args["list"].([]any)
	if !ok || len(listRaw) != 2 {
		t.Fatalf("Expected 2 quotas in list, got %v", methodResp.Args["list"])
	}
}

// TestRFC9425_Section5_QuotaQuery tests Quota/query method per RFC 9425 Section 5.
func TestRFC9425_Section5_QuotaQuery(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI},
		"methodCalls": []any{
			[]any{"Quota/query", map[string]any{
				"accountId": "primary",
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

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Quota/query" {
		t.Fatalf("Expected method response 'Quota/query', got %q", methodResp.Name)
	}

	idsRaw, ok := methodResp.Args["ids"].([]any)
	if !ok || len(idsRaw) != 2 {
		t.Fatalf("Expected 2 quota IDs, got %v", methodResp.Args["ids"])
	}
}
