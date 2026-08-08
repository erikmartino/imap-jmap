package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8621_IdentityEmailIsRealAddress guards against the default Identity surfacing the opaque
// account ID (base64url of the subject) as its From address instead of the user's real email.
// The Identity's email is the address used as From when composing/replying (RFC 8621 Section 6);
// it MUST be a real address, not the account identifier.
func TestRFC8621_IdentityEmailIsRealAddress(t *testing.T) {
	spectest.Require(t, "RFC8621", "6.1", spectest.MUST,
		"The Identity email is the account's real address (used as From), not the opaque account id.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"Identity/get", map[string]any{"accountId": "primary"}, "c1"},
	})
	list, _ := resp.MethodResponses[0].Args["list"].([]any)
	if len(list) == 0 {
		t.Fatalf("expected at least one identity, got %+v", resp.MethodResponses[0].Args)
	}
	found := false
	accountID := jmap.AccountIDForSubject(testUsername)
	for _, item := range list {
		id := item.(map[string]any)
		email, _ := id["email"].(string)
		name, _ := id["name"].(string)
		if email == accountID || email == accountID+"@example.com" {
			t.Errorf("identity email leaks the account id: %q", email)
		}
		if name == accountID {
			t.Errorf("identity name leaks the account id: %q", name)
		}
		if email == testUsername {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an identity whose email is the account's real address %q; got %+v", testUsername, list)
	}
}
