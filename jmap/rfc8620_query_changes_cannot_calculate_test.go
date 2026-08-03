package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_Section5_6_QueryChanges_CannotCalculateChanges tests error handling when sinceQueryState or sinceState is invalid or too old per RFC 8620 Section 5.6.
func TestRFC8620_Section5_6_QueryChanges_CannotCalculateChanges(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	invalidQueryState := "invalid-state-99999"

	qcCases := []struct {
		method string
		using  []string
	}{
		{"Email/queryChanges", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"Mailbox/queryChanges", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}},
		{"Quota/queryChanges", []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI}},
		{"CalendarEvent/queryChanges", []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}},
		{"SieveScript/queryChanges", []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}},
		{"FileNode/queryChanges", []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI}},
	}

	for _, tc := range qcCases {
		t.Run(tc.method+"_cannotCalculateChanges", func(t *testing.T) {
			res := postJMAP(t, ts.URL, tc.using, []any{
				[]any{tc.method, map[string]any{
					"accountId":       "primary",
					"sinceQueryState": invalidQueryState,
				}, "c1"},
			})
			if len(res.MethodResponses) == 0 {
				t.Fatalf("No response for %s", tc.method)
			}
			mr := res.MethodResponses[0]
			if mr.Name != "error" {
				t.Errorf("Expected 'error' method response for %s, got %q", tc.method, mr.Name)
			}
			errType, _ := mr.Args["type"].(string)
			if errType != "cannotCalculateChanges" {
				t.Errorf("Expected error type 'cannotCalculateChanges' for %s, got %q", tc.method, errType)
			}
		})
	}
}
