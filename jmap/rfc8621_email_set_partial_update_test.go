package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8621_Section4_4_EmailSetPartialUpdateDataLossPrevention verifies that partial update patches on Email leave unpatched fields intact per AGENTS.md data-loss prevention rules.
func TestRFC8621_Section4_4_EmailSetPartialUpdateDataLossPrevention(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed source email with rich fields
	origSubj := "Important Subject"
	origFrom := []jmap.EmailAddress{{Name: "Sender", Email: "sender@example.com"}}
	origTo := []jmap.EmailAddress{{Name: "Recipient", Email: "recipient@example.com"}}

	em, err := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    origSubj,
		From:       origFrom,
		To:         origTo,
		Keywords:   map[string]bool{"$draft": true},
		Size:       1024,
	})
	if err != nil {
		t.Fatalf("Failed to create initial email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Send partial update patch for keywords/$seen
	calls1 := []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				string(em.ID): map[string]any{
					"keywords/$seen": true,
				},
			},
		}, "c1"},
	}

	res1 := postJMAP(t, ts.URL, using, calls1)
	if len(res1.MethodResponses) == 0 {
		t.Fatalf("Empty response for Email/set update")
	}
	updated, _ := res1.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := updated[string(em.ID)]; !ok {
		t.Fatalf("Email/set update failed: %v", res1.MethodResponses[0].Args)
	}

	// 2. Fetch updated Email and verify subject, from, to are preserved intact
	calls2 := []any{
		[]any{"Email/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{string(em.ID)},
		}, "c2"},
	}
	res2 := postJMAP(t, ts.URL, using, calls2)
	list, _ := res2.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("Email/get expected 1 item, got %d", len(list))
	}

	fetched, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("Invalid item in Email/get list")
	}

	// Assert subject is intact
	if subj, _ := fetched["subject"].(string); subj != origSubj {
		t.Errorf("Data loss detected! Subject expected %q, got %q", origSubj, subj)
	}

	// Assert keywords contains both $draft and $seen
	kwMap, _ := fetched["keywords"].(map[string]any)
	if kwMap == nil || !kwMap["$seen"].(bool) || !kwMap["$draft"].(bool) {
		t.Errorf("Keywords merge failed! Expected $draft and $seen, got %v", kwMap)
	}
}
