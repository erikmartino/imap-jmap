package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9425_Section2_Capability tests urn:ietf:params:jmap:quota capability discovery per RFC 9425 Section 2.
func TestRFC9425_Section2_Capability(t *testing.T) {
	srv := jmap.NewServer(nil)
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

	capRaw, ok := session.Capabilities[jmap.QuotaCapabilityURI]
	if !ok {
		t.Fatalf("Capability %q missing in Session", jmap.QuotaCapabilityURI)
	}

	capBytes, _ := json.Marshal(capRaw)
	var quotaCap jmap.QuotaCapability
	_ = json.Unmarshal(capBytes, &quotaCap)

	if quotaCap.MaxQuotaResources == 0 {
		t.Error("Expected maxQuotaResources > 0")
	}
}

// TestRFC9425_Section4_QuotaGet tests Quota/get method per RFC 9425 Section 4.
func TestRFC9425_Section4_QuotaGet(t *testing.T) {
	srv := jmap.NewServer(nil)
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
	srv := jmap.NewServer(nil)
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
	if methodResp.Name != "Quota/query" {
		t.Fatalf("Expected method response 'Quota/query', got %q", methodResp.Name)
	}

	idsRaw, ok := methodResp.Args["ids"].([]any)
	if !ok || len(idsRaw) != 2 {
		t.Fatalf("Expected 2 quota IDs, got %v", methodResp.Args["ids"])
	}
}
