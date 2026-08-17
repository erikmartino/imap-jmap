package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8621_Section2_4_MailboxFilterPropertiesPosNeg tests Mailbox/query name and parentId filter conditions per RFC 8621 Section 2.4.1.
func TestRFC8621_Section2_4_MailboxFilterPropertiesPosNeg(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := seedCtx()

	parent, _ := srv.MailBackend.CreateMailbox(ctx, &jmap.Mailbox{Name: "ProjectsParent"})
	child, _ := srv.MailBackend.CreateMailbox(ctx, &jmap.Mailbox{
		Name:     "SubProjectAlpha",
		ParentID: &parent.ID,
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Positive filter by parentId
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"parentId": string(parent.ID)},
		}, "c1"},
	})
	ids1, _ := res1.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 || ids1[0] != string(child.ID) {
		t.Errorf("Mailbox parentId positive expected [%s], got %v", child.ID, ids1)
	}

	// 2. Positive filter by name substring
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"name": "ProjectsParent"},
		}, "c2"},
	})
	ids2, _ := res2.MethodResponses[0].Args["ids"].([]any)
	if len(ids2) != 1 || ids2[0] != string(parent.ID) {
		t.Errorf("Mailbox name positive expected [%s], got %v", parent.ID, ids2)
	}

	// 3. Negative filter by name substring
	res3 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"name": "NonExistentFolder123"},
		}, "c3"},
	})
	ids3, _ := res3.MethodResponses[0].Args["ids"].([]any)
	if len(ids3) != 0 {
		t.Errorf("Mailbox name negative expected empty [], got %v", ids3)
	}
}
