package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestQuotaQuery_Filter tests Quota/query filtering by resourceType per RFC 9425 Section 4.4.
func TestQuotaQuery_Filter(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.QuotaCapabilityURI}
	calls := []any{
		[]any{"Quota/query", map[string]any{"accountId": "primary", "filter": map[string]any{"resourceType": "octets"}}, "c1"},
		[]any{"Quota/query", map[string]any{"accountId": "primary", "filter": map[string]any{"resourceType": "non-existent"}}, "c2"},
	}

	res := postJMAP(t, ts.URL, using, calls)
	if len(res.MethodResponses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(res.MethodResponses))
	}

	ids1, _ := res.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 {
		t.Errorf("Quota/query resourceType:octets expected 1 item, got %d", len(ids1))
	}

	ids2, _ := res.MethodResponses[1].Args["ids"].([]any)
	if len(ids2) != 0 {
		t.Errorf("Quota/query resourceType:non-existent expected 0 items, got %d", len(ids2))
	}
}
