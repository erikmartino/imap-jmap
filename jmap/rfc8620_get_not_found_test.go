package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_Section5_1_GetNotFoundWithMixedIDs tests that /get methods return valid objects in list and missing IDs in notFound per RFC 8620 Section 5.1.
func TestRFC8620_Section5_1_GetNotFoundWithMixedIDs(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed items
	em, _ := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Valid Email",
	})
	cal, _ := srv.CalendarsBackend.CreateCalendar(context.Background(), &jmap.Calendar{Name: "Cal 1"})

	missingID := "non-existent-id-99999"

	getCases := []struct {
		method  string
		using   []string
		validID string
	}{
		{"Email/get", []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, string(em.ID)},
		{"Calendar/get", []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}, string(cal.ID)},
	}

	for _, tc := range getCases {
		t.Run(tc.method+"_mixedIDs", func(t *testing.T) {
			res := postJMAP(t, ts.URL, tc.using, []any{
				[]any{tc.method, map[string]any{
					"accountId": "primary",
					"ids":       []any{tc.validID, missingID},
				}, "c1"},
			})
			if len(res.MethodResponses) == 0 {
				t.Fatalf("No response for %s", tc.method)
			}
			mr := res.MethodResponses[0]
			if mr.Name != tc.method {
				t.Fatalf("Expected response name %q, got %q", tc.method, mr.Name)
			}

			list, _ := mr.Args["list"].([]any)
			if len(list) != 1 {
				t.Errorf("%s expected 1 valid item in list, got %d", tc.method, len(list))
			}

			notFound, _ := mr.Args["notFound"].([]any)
			foundMissing := false
			for _, id := range notFound {
				if id == missingID {
					foundMissing = true
				}
			}
			if !foundMissing {
				t.Errorf("%s expected %q in notFound, got %v", tc.method, missingID, notFound)
			}
		})
	}
}
