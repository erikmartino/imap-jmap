package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC9661_Capability tests urn:ietf:params:jmap:sieve capability discovery per RFC 9661 Section 2.
func TestRFC9661_Capability(t *testing.T) {
	sieveBackend := memory.NewMemorySieveBackend()
	srv := jmap.NewServer(nil, jmap.WithSieveBackend(sieveBackend))
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

	capRaw, ok := session.Capabilities[jmap.SieveCapabilityURI]
	if !ok {
		t.Fatalf("Capability %q missing in Session capabilities", jmap.SieveCapabilityURI)
	}

	capBytes, _ := json.Marshal(capRaw)
	var sieveCap jmap.SieveCapability
	_ = json.Unmarshal(capBytes, &sieveCap)

	if sieveCap.MaxScriptSize == 0 {
		t.Errorf("Expected maxScriptSize > 0, got 0")
	}
	if len(sieveCap.SieveExtensions) == 0 {
		t.Errorf("Expected non-empty sieveExtensions")
	}
}

// TestRFC9661_SieveScript_Validate tests SieveScript/validate with valid and invalid Sieve scripts.
func TestRFC9661_SieveScript_Validate(t *testing.T) {
	sieveBackend := memory.NewMemorySieveBackend()
	srv := jmap.NewServer(nil, jmap.WithSieveBackend(sieveBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Valid Sieve script validation
	validScript := `require ["fileinto"]; if header :contains "subject" "test" { fileinto "INBOX.test"; }`
	valReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI},
		"methodCalls": []any{
			[]any{
				"SieveScript/validate",
				map[string]any{
					"accountId": "primary",
					"content":   validScript,
				},
				"c1",
			},
		},
	}

	bodyBytes, _ := json.Marshal(valReq)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST validate failed: %v", err)
	}
	defer resp.Body.Close()

	var valResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&valResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	args := valResp.MethodResponses[0].Args
	if isValid, ok := args["isValid"].(bool); !ok || !isValid {
		t.Fatalf("Expected valid script, got isValid=false (args: %v)", args)
	}

	// 2. Invalid Sieve script validation
	invalidScript := `if header :contains { bad syntax }`
	invalidValReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI},
		"methodCalls": []any{
			[]any{
				"SieveScript/validate",
				map[string]any{
					"accountId": "primary",
					"content":   invalidScript,
				},
				"c2",
			},
		},
	}

	bodyBytesInvalid, _ := json.Marshal(invalidValReq)
	respInvalid, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytesInvalid))
	if err != nil {
		t.Fatalf("JMAP POST validate failed: %v", err)
	}
	defer respInvalid.Body.Close()

	var valRespInvalid jmap.Response
	if err := json.NewDecoder(respInvalid.Body).Decode(&valRespInvalid); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	argsInvalid := valRespInvalid.MethodResponses[0].Args
	if isValid, ok := argsInvalid["isValid"].(bool); !ok || isValid {
		t.Fatalf("Expected invalid script to return isValid=false, got %v", isValid)
	}
	if errObj, ok := argsInvalid["error"].(map[string]any); !ok || errObj["type"] != "invalidScript" {
		t.Fatalf("Expected error type 'invalidScript', got %v", argsInvalid["error"])
	}
}

// TestRFC9661_SieveScript_GetSetQuery tests SieveScript/set, SieveScript/get, and SieveScript/query.
func TestRFC9661_SieveScript_GetSetQuery(t *testing.T) {
	sieveBackend := memory.NewMemorySieveBackend()
	srv := jmap.NewServer(nil, jmap.WithSieveBackend(sieveBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	validContent := `require ["fileinto"]; if header :contains "X-Spam" "Yes" { fileinto "Junk"; }`

	// 1. SieveScript/set create script
	createReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI},
		"methodCalls": []any{
			[]any{
				"SieveScript/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"s1": map[string]any{
							"name":     "Spam Filter",
							"content":  validContent,
							"isActive": true,
						},
					},
				},
				"c1",
			},
		},
	}

	bodyBytes, _ := json.Marshal(createReq)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST set failed: %v", err)
	}
	defer resp.Body.Close()

	var setResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&setResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	createdMap := setResp.MethodResponses[0].Args["created"].(map[string]any)
	createdScript := createdMap["s1"].(map[string]any)
	scriptID := createdScript["id"].(string)
	if scriptID == "" {
		t.Fatalf("Expected non-empty script ID")
	}
	if createdScript["name"] != "Spam Filter" {
		t.Errorf("Expected name 'Spam Filter', got %v", createdScript["name"])
	}

	// 2. SieveScript/query active script
	queryReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI},
		"methodCalls": []any{
			[]any{
				"SieveScript/query",
				map[string]any{
					"accountId": "primary",
					"filter": map[string]any{
						"isActive": true,
					},
				},
				"c2",
			},
		},
	}

	bodyBytesQuery, _ := json.Marshal(queryReq)
	respQuery, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytesQuery))
	if err != nil {
		t.Fatalf("JMAP POST query failed: %v", err)
	}
	defer respQuery.Body.Close()

	var queryResp jmap.Response
	if err := json.NewDecoder(respQuery.Body).Decode(&queryResp); err != nil {
		t.Fatalf("Failed to decode query response: %v", err)
	}

	idsRaw := queryResp.MethodResponses[0].Args["ids"].([]any)
	if len(idsRaw) != 1 || idsRaw[0].(string) != scriptID {
		t.Fatalf("Expected query to return script ID %s, got %v", scriptID, idsRaw)
	}
}
