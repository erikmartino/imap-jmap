package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/managesieve"
	"imap-jmap/jmap/spectest"
)

// TestRFC9661_Section2_1_SieveScriptNameConstraints covers SieveScript name
// validation (content characters) and account-wide uniqueness (RFC 9661
// Section 2.1 / 2.4).
func TestRFC9661_Section2_1_SieveScriptNameConstraints(t *testing.T) {
	spectest.Require(t, "RFC9661", "2.1", spectest.MUST, "If non-null, this MUST be")
	spectest.Require(t, "RFC9661", "2.1", spectest.MUST,
		"For compatibility with ManageSieve, servers MUST reject names that")
	spectest.Require(t, "RFC9661", "2.1", spectest.MUST,
		"The name MUST be unique among all SieveScripts within an account")

	_, sieveBackend, cleanup := managesieve.NewEmbeddedBackend()
	defer cleanup()
	srv := jmap.NewServer(nil, jmap.WithSieveBackend(sieveBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const script = `require ["fileinto"]; if header :contains "subject" "test" { fileinto "INBOX.test"; }`
	post := func(create map[string]any) map[string]any {
		req := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI},
			"methodCalls": []any{
				[]any{"SieveScript/set", map[string]any{"accountId": "primary", "create": create}, "c1"},
			},
		}
		body, _ := json.Marshal(req)
		resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr.MethodResponses[0].Args
	}

	args := post(map[string]any{"s1": map[string]any{"name": "Spam Filter", "content": script}})
	created, _ := args["created"].(map[string]any)
	if created["s1"] == nil {
		t.Fatalf("first script should be created, got %v", args)
	}
	firstID := created["s1"].(map[string]any)["id"].(string)

	// Duplicate name -> alreadyExists with existingId.
	args = post(map[string]any{"s2": map[string]any{"name": "Spam Filter", "content": script}})
	errObj, _ := args["notCreated"].(map[string]any)["s2"].(map[string]any)
	if errObj == nil || errObj["type"] != "alreadyExists" {
		t.Fatalf("duplicate name should be alreadyExists, got %v", args)
	}
	if errObj["existingId"] != firstID {
		t.Errorf("expected existingId %s, got %v", firstID, errObj["existingId"])
	}

	// Forbidden control characters in the name.
	for i, name := range []string{"bad\x00name", "bad\x7fname", "bad\u2028name", "bad\u2029name"} {
		args = post(map[string]any{"x": map[string]any{"name": name, "content": script}})
		errObj, _ := args["notCreated"].(map[string]any)["x"].(map[string]any)
		if errObj == nil || errObj["type"] != "invalidProperties" {
			t.Errorf("case %d: name %q should be invalidProperties, got %v", i, name, args)
		}
	}
}
