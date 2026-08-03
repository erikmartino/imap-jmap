package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_Section5_5_QueryPaginationBoundariesAndCalculateTotal verifies query pagination parameters (position, limit, position beyond end, limit 0, calculateTotal) per RFC 8620 Section 5.5.
func TestRFC8620_Section5_5_QueryPaginationBoundariesAndCalculateTotal(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	queryCases := []struct {
		method string
		using  []string
	}{
		{"Email/query", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"Mailbox/query", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"Quota/query", []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI}},
		{"CalendarEvent/query", []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}},
		{"SieveScript/query", []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}},
		{"FileNode/query", []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI}},
		{"Card/query", []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}},
	}

	for _, tc := range queryCases {
		t.Run(tc.method+"_positionBeyondEndAndCalculateTotal", func(t *testing.T) {
			res := postJMAP(t, ts.URL, tc.using, []any{
				[]any{tc.method, map[string]any{
					"accountId":      "primary",
					"position":       999999,
					"limit":          10,
					"calculateTotal": true,
				}, "c1"},
			})
			if len(res.MethodResponses) == 0 {
				t.Fatalf("No response for %s", tc.method)
			}
			mr := res.MethodResponses[0]
			if mr.Name != tc.method {
				t.Fatalf("Expected %q, got %q", tc.method, mr.Name)
			}
			ids, _ := mr.Args["ids"].([]any)
			if len(ids) != 0 {
				t.Errorf("%s position beyond end expected ids: [], got %v", tc.method, ids)
			}
			total, ok := mr.Args["total"].(float64)
			if !ok {
				totalInt, ok2 := mr.Args["total"].(int)
				if ok2 {
					total = float64(totalInt)
				} else {
					t.Errorf("%s calculateTotal expected total number, got %v", tc.method, mr.Args["total"])
				}
			}
			_ = total
		})

		t.Run(tc.method+"_limitZero", func(t *testing.T) {
			res := postJMAP(t, ts.URL, tc.using, []any{
				[]any{tc.method, map[string]any{
					"accountId":      "primary",
					"position":       0,
					"limit":          0,
					"calculateTotal": true,
				}, "c1"},
			})
			if len(res.MethodResponses) == 0 {
				t.Fatalf("No response for %s", tc.method)
			}
			mr := res.MethodResponses[0]
			if mr.Name != tc.method {
				t.Fatalf("Expected %q, got %q", tc.method, mr.Name)
			}
			ids, _ := mr.Args["ids"].([]any)
			if len(ids) != 0 {
				t.Errorf("%s limit 0 expected ids: [], got %v", tc.method, ids)
			}
		})
	}
}
