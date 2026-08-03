package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8621_Section4_4_SortOrderAndMultiComparatorTieBreak tests Email/query sort ordering and multi-comparator tie-breaking per RFC 8621 Section 4.4.2.
func TestRFC8621_Section4_4_SortOrderAndMultiComparatorTieBreak(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := context.Background()

	// Seed 3 emails with different receivedAt and subjects
	e1, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Alpha Subject",
		ReceivedAt: "2026-01-01T10:00:00Z",
		Size:       100,
	})
	e2, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Beta Subject",
		ReceivedAt: "2026-01-02T10:00:00Z",
		Size:       500,
	})
	e3, _ := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Alpha Subject",
		ReceivedAt: "2026-01-03T10:00:00Z",
		Size:       300,
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Sort by size ascending (e1:100, e3:300, e2:500)
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"sort": []any{
				map[string]any{"property": "size", "isAscending": true},
			},
		}, "c1"},
	})
	ids1, _ := res1.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) < 3 {
		t.Fatalf("Expected at least 3 emails, got %d", len(ids1))
	}
	// Check order of our 3 seeded emails
	var idx1, idx2, idx3 int
	for i, id := range ids1 {
		if id == string(e1.ID) {
			idx1 = i
		}
		if id == string(e2.ID) {
			idx2 = i
		}
		if id == string(e3.ID) {
			idx3 = i
		}
	}
	if !(idx1 < idx3 && idx3 < idx2) {
		t.Errorf("Sort size asc failed! Expected idx1 < idx3 < idx2, got idx1=%d, idx3=%d, idx2=%d", idx1, idx3, idx2)
	}

	// 2. Multi-comparator: subject asc, receivedAt desc (e3: Alpha 2026-01-03, e1: Alpha 2026-01-01, e2: Beta)
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"sort": []any{
				map[string]any{"property": "subject", "isAscending": true},
				map[string]any{"property": "receivedAt", "isAscending": false},
			},
		}, "c2"},
	})
	ids2, _ := res2.MethodResponses[0].Args["ids"].([]any)
	for i, id := range ids2 {
		if id == string(e1.ID) {
			idx1 = i
		}
		if id == string(e2.ID) {
			idx2 = i
		}
		if id == string(e3.ID) {
			idx3 = i
		}
	}
	if !(idx3 < idx1 && idx1 < idx2) {
		t.Errorf("Multi-sort subject asc + receivedAt desc failed! Expected idx3 < idx1 < idx2, got idx3=%d, idx1=%d, idx2=%d", idx3, idx1, idx2)
	}
}
