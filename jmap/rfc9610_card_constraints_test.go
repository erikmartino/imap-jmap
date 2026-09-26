package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9610_Section3_CardConstraints covers ContactCard create constraints:
// addressBookIds values MUST be true, uids are unique per account, and a card
// always belongs to at least one AddressBook.
func TestRFC9610_Section3_CardConstraints(t *testing.T) {
	spectest.Require(t, "RFC9610", "3", spectest.MUST, "MUST be true")
	spectest.Require(t, "RFC9610", "3", spectest.MUST, "However, there MUST NOT be more than one ContactCard")
	spectest.Require(t, "RFC9610", "3", spectest.MUST, "card MUST belong to at least one AddressBook at all times (until")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	rAB := postJMAP(t, ts.URL, using, []any{
		[]any{"AddressBook/set", map[string]any{"accountId": "primary", "create": map[string]any{
			"ab": map[string]any{"name": "Main"},
		}}, "a1"},
	})
	abID := rAB.MethodResponses[0].Args["created"].(map[string]any)["ab"].(map[string]any)["id"].(string)

	set := func(create map[string]any) map[string]any {
		r := postJMAP(t, ts.URL, using, []any{
			[]any{"ContactCard/set", map[string]any{"accountId": "primary", "create": create}, "c"},
		})
		return r.MethodResponses[0].Args
	}

	// addressBookIds with a false value is rejected.
	args := set(map[string]any{
		"bad": map[string]any{"addressBookIds": map[string]any{abID: false}},
	})
	if notCreated, _ := args["notCreated"].(map[string]any); notCreated["bad"] == nil {
		t.Errorf("addressBookIds:false should be rejected, got %v", args)
	}

	// Duplicate uid is rejected.
	args = set(map[string]any{
		"one": map[string]any{"uid": "dup-uid", "addressBookIds": map[string]any{abID: true}},
	})
	if created, _ := args["created"].(map[string]any); created["one"] == nil {
		t.Fatalf("first card should be created, got %v", args)
	}
	args = set(map[string]any{
		"two": map[string]any{"uid": "dup-uid", "addressBookIds": map[string]any{abID: true}},
	})
	if notCreated, _ := args["notCreated"].(map[string]any); notCreated["two"] == nil {
		t.Errorf("duplicate uid should be rejected, got %v", args)
	}

	// A card created without addressBookIds still ends up in an AddressBook.
	args = set(map[string]any{
		"auto": map[string]any{"uid": "auto-uid"},
	})
	autoID, _ := args["created"].(map[string]any)["auto"].(map[string]any)["id"].(string)
	if autoID == "" {
		t.Fatalf("card without addressBookIds should be created, got %v", args)
	}
	got := postJMAP(t, ts.URL, using, []any{
		[]any{"ContactCard/get", map[string]any{"accountId": "primary", "ids": []any{autoID}, "properties": []any{"addressBookIds"}}, "g"},
	})
	list, _ := got.MethodResponses[0].Args["list"].([]any)
	abm, _ := list[0].(map[string]any)["addressBookIds"].(map[string]any)
	if len(abm) == 0 {
		t.Errorf("card must belong to at least one AddressBook, got %v", list[0])
	}
}
