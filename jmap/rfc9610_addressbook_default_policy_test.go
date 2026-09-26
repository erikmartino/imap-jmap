package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9610_Section2_3_OnSuccessSetIsDefaultPolicy verifies that an
// onSuccessSetIsDefault id that is not found (or not permitted) is ignored with
// no error, leaving the current default unchanged (RFC 9610 Section 2.3).
func TestRFC9610_Section2_3_OnSuccessSetIsDefaultPolicy(t *testing.T) {
	spectest.Require(t, "RFC9610", "2.3", spectest.MUST,
		"server for policy reasons, it MUST be ignored and the current default")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	getDefault := func() string {
		r := postJMAP(t, ts.URL, using, []any{
			[]any{"AddressBook/get", map[string]any{"accountId": "primary"}, "g"},
		})
		list, _ := r.MethodResponses[0].Args["list"].([]any)
		def := ""
		for _, item := range list {
			if item.(map[string]any)["isDefault"] == true {
				def = item.(map[string]any)["id"].(string)
			}
		}
		return def
	}

	// Create A and set it as default explicitly.
	r := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{
			"accountId":             "primary",
			"create":                map[string]any{"a": map[string]any{"name": "Alpha"}, "b": map[string]any{"name": "Bravo"}},
			"onSuccessSetIsDefault": "#a",
		}, "c1"},
	})
	aID, _ := r.MethodResponses[0].Args["created"].(map[string]any)["a"].(map[string]any)["id"].(string)
	if aID == "" {
		t.Fatalf("alpha create failed: %v", r.MethodResponses[0].Args)
	}
	if got := getDefault(); got != aID {
		t.Fatalf("expected Alpha (%s) as default, got %q", aID, got)
	}

	// An unknown onSuccessSetIsDefault is ignored; the default is unchanged.
	r = postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{
			"accountId":             "primary",
			"create":                map[string]any{"nb": map[string]any{"name": "New Book"}},
			"onSuccessSetIsDefault": "does-not-exist",
		}, "c2"},
	})
	if r.MethodResponses[0].Name != "AddressBook/set" {
		t.Fatalf("set should not fail for an unknown default id, got %v", r.MethodResponses[0])
	}
	if created, _ := r.MethodResponses[0].Args["created"].(map[string]any); created["nb"] == nil {
		t.Errorf("create should still succeed, got %v", r.MethodResponses[0].Args)
	}
	if got := getDefault(); got != aID {
		t.Errorf("default must remain Alpha (%s) after an unknown onSuccessSetIsDefault, got %q", aID, got)
	}
}
