package jmap_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9610_Section2_AddressBookNameValidation verifies the AddressBook name
// constraint: not empty and at most 255 UTF-8 octets (RFC 9610 Section 2).
func TestRFC9610_Section2_AddressBookNameValidation(t *testing.T) {
	spectest.Require(t, "RFC9610", "2", spectest.MUST,
		"empty string and MUST NOT be greater than 255 octets in size when")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	long := strings.Repeat("a", 256)
	ok255 := strings.Repeat("b", 255)

	spectest.Require(t, "RFC9610", "2", spectest.MUST, "The number MUST")

	res := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{"accountId": "primary", "create": map[string]any{
			"empty":    map[string]any{"name": ""},
			"long":     map[string]any{"name": long},
			"ok":       map[string]any{"name": ok255},
			"badorder": map[string]any{"name": "Bad Order", "sortOrder": 2147483648},
		}}, "c1"},
	})
	args := res.MethodResponses[0].Args

	created, _ := args["created"].(map[string]any)
	notCreated, _ := args["notCreated"].(map[string]any)

	if _, ok := created["ok"]; !ok {
		t.Errorf("255-octet name should be accepted, got created=%v notCreated=%v", created, notCreated)
	}
	for _, key := range []string{"empty", "long"} {
		errObj, ok := notCreated[key].(map[string]any)
		if !ok {
			t.Errorf("expected %q in notCreated, got %v", key, notCreated)
			continue
		}
		if errObj["type"] != "invalidProperties" {
			t.Errorf("expected invalidProperties for %q, got %v", key, errObj["type"])
		}
		props, _ := errObj["properties"].([]any)
		found := false
		for _, p := range props {
			if p == "name" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected properties to name the invalid property, got %v", errObj["properties"])
		}
	}

	if errObj, ok := notCreated["badorder"].(map[string]any); !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("sortOrder >= 2^31 should be rejected, got %v", notCreated["badorder"])
	}

	// Updating an existing book to an empty name must also be rejected.
	abID := created["ok"].(map[string]any)["id"].(string)
	upd := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{"accountId": "primary", "update": map[string]any{
			abID: map[string]any{"name": ""},
		}}, "c2"},
	})
	if notUpdated, _ := upd.MethodResponses[0].Args["notUpdated"].(map[string]any); notUpdated[abID] == nil {
		t.Errorf("expected empty-name update to be rejected, got %v", upd.MethodResponses[0].Args)
	}
}
