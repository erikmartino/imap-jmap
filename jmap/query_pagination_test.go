package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestQueryNegativePositionRejected verifies that every */query method rejects a negative
// "position" with an invalidArguments method error instead of panicking (RFC 8620 Section 5.5:
// position is a non-negative integer). Mailbox/query, Quota/query, and SieveScript/query
// previously indexed slices with filtered[-1], a remotely triggerable server crash.
func TestQueryNegativePositionRejected(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cases := []struct {
		name      string
		using     []string
		method    string
		extraArgs map[string]any
	}{
		{name: "Email/query", using: []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, method: "Email/query"},
		{name: "Mailbox/query", using: []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, method: "Mailbox/query"},
		{name: "EmailSubmission/query", using: []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, method: "EmailSubmission/query"},
		{name: "Quota/query", using: []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI}, method: "Quota/query"},
		{name: "CalendarEvent/query", using: []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}, method: "CalendarEvent/query"},
		{name: "Card/query", using: []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}, method: "Card/query"},
		{name: "SieveScript/query", using: []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}, method: "SieveScript/query"},
		{name: "FileNode/query", using: []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI}, method: "FileNode/query"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]any{"accountId": "primary", "position": -1}
			for k, v := range tc.extraArgs {
				args[k] = v
			}
			reqPayload := map[string]any{
				"using":       tc.using,
				"methodCalls": []any{[]any{tc.method, args, "c1"}},
			}
			body, _ := json.Marshal(reqPayload)

			resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("POST /jmap failed: %v", err)
			}
			defer resp.Body.Close()

			var jmapResp jmap.Response
			if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
				t.Fatalf("Failed to decode Response: %v", err)
			}

			if len(jmapResp.MethodResponses) != 1 {
				t.Fatalf("Expected 1 method response, got %d", len(jmapResp.MethodResponses))
			}
			methodResp := jmapResp.MethodResponses[0]
			if methodResp.Name != "error" {
				t.Fatalf("Expected method error response, got %q (server must not panic on negative position)", methodResp.Name)
			}
			errType, _ := methodResp.Args["type"].(string)
			if errType != jmap.MethodErrorInvalidArguments {
				t.Errorf("Expected error type %q, got %q", jmap.MethodErrorInvalidArguments, errType)
			}
		})
	}
}

// TestQueryPagination verifies position/limit slicing behavior per RFC 8620 Section 5.5 on the
// seeded mail account: position beyond the end yields an empty ids array with the correct total,
// and a limit slices the results.
func TestQueryPagination(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// The default test server seeds 2 emails in mb-inbox.
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}, "calculateTotal": true}, "all"},
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}, "position": 1, "limit": 1, "calculateTotal": true}, "page"},
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}, "position": 50, "calculateTotal": true}, "beyond"},
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}, "limit": 0, "calculateTotal": true}, "zerolimit"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	allIDs := idsOf(t, jmapResp.MethodResponses[0].Args["ids"])
	if len(allIDs) != 2 {
		t.Fatalf("Expected 2 seeded inbox emails, got %v", allIDs)
	}
	if total, _ := jmapResp.MethodResponses[0].Args["total"].(float64); total != 2 {
		t.Errorf("Expected total 2, got %v", total)
	}

	pageIDs := idsOf(t, jmapResp.MethodResponses[1].Args["ids"])
	if len(pageIDs) != 1 || pageIDs[0] != allIDs[1] {
		t.Errorf("Expected page [%s], got %v", allIDs[1], pageIDs)
	}
	if total, _ := jmapResp.MethodResponses[1].Args["total"].(float64); total != 2 {
		t.Errorf("Expected total 2 on paged query, got %v", total)
	}

	beyondIDs := idsOf(t, jmapResp.MethodResponses[2].Args["ids"])
	if len(beyondIDs) != 0 {
		t.Errorf("Expected empty ids for position beyond end, got %v", beyondIDs)
	}
	if total, _ := jmapResp.MethodResponses[2].Args["total"].(float64); total != 2 {
		t.Errorf("Expected total 2 for position beyond end, got %v", total)
	}

	zeroIDs := idsOf(t, jmapResp.MethodResponses[3].Args["ids"])
	if len(zeroIDs) != 0 {
		t.Errorf("Expected empty ids for limit 0, got %v", zeroIDs)
	}
}

// TestQueryPaginationMailbox verifies position/limit slicing for Mailbox/query, whose
// pagination is implemented in the handler.
func TestQueryPaginationMailbox(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Mailbox/query", map[string]any{"accountId": "primary", "position": 1, "limit": 2}, "page"},
			[]any{"Mailbox/query", map[string]any{"accountId": "primary", "position": 100}, "beyond"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	// 6 default mailboxes exist; page from index 1 with limit 2 -> exactly 2 ids.
	pageIDs := idsOf(t, jmapResp.MethodResponses[0].Args["ids"])
	if len(pageIDs) != 2 {
		t.Errorf("Expected 2 mailboxes on page, got %v", pageIDs)
	}
	if total, _ := jmapResp.MethodResponses[0].Args["total"].(float64); total != 6 {
		t.Errorf("Expected total 6, got %v", total)
	}

	beyondIDs := idsOf(t, jmapResp.MethodResponses[1].Args["ids"])
	if len(beyondIDs) != 0 {
		t.Errorf("Expected empty ids for position beyond end, got %v", beyondIDs)
	}
}

func idsOf(t *testing.T, raw any) []string {
	t.Helper()
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("Expected ids array, got %v", raw)
	}
	ids := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			ids = append(ids, s)
		}
	}
	return ids
}
