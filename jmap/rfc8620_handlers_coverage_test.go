package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_Section3_1_HandlersCoverage_ChangesAndGetMethods tests handlers for Identity/get, */changes methods, and ContactCard/* aliases per RFC 8620 / RFC 8621 / RFC 9610.
func TestRFC8620_Section3_1_HandlersCoverage_ChangesAndGetMethods(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed items
	em, _ := srv.MailBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Coverage Email",
	})
	cal, _ := srv.CalendarsBackend.CreateCalendar(context.Background(), &jmap.Calendar{Name: "Main Cal"})

	usingMail := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI, jmap.QuotaCapabilityURI}
	usingCal := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}
	usingContacts := []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}

	// 1. Identity/get
	r1 := postJMAP(t, ts.URL, usingMail, []any{
		[]any{"Identity/get", map[string]any{"accountId": "primary"}, "c1"},
	})
	if len(r1.MethodResponses) != 1 || r1.MethodResponses[0].Name != "Identity/get" {
		t.Fatalf("Expected Identity/get response, got %v", r1.MethodResponses)
	}

	// 2. EmailSubmission/changes, Quota/changes, Quota/queryChanges, Thread/changes, Calendar/changes, CalendarEvent/changes
	r2 := postJMAP(t, ts.URL, usingMail, []any{
		[]any{"EmailSubmission/changes", map[string]any{"accountId": "primary", "sinceState": "0"}, "c1"},
		[]any{"Quota/changes", map[string]any{"accountId": "primary", "sinceState": "0"}, "c2"},
		[]any{"Quota/queryChanges", map[string]any{"accountId": "primary", "sinceQueryState": "0"}, "c3"},
		[]any{"Thread/changes", map[string]any{"accountId": "primary", "sinceState": "0"}, "c4"},
	})
	if len(r2.MethodResponses) != 4 {
		t.Fatalf("Expected 4 changes responses, got %d", len(r2.MethodResponses))
	}

	r3 := postJMAP(t, ts.URL, usingCal, []any{
		[]any{"Calendar/changes", map[string]any{"accountId": "primary", "sinceState": "0"}, "c1"},
		[]any{"CalendarEvent/changes", map[string]any{"accountId": "primary", "sinceState": "0"}, "c2"},
	})
	if len(r3.MethodResponses) != 2 {
		t.Fatalf("Expected 2 calendar changes responses, got %d", len(r3.MethodResponses))
	}

	// 3. RSVP via CalendarEvent/set (draft-ietf-jmap-calendars): patch the participant's
	// participationStatus. There is no CalendarEvent/sendResponse method.
	rsvpEv, _ := srv.CalendarsBackend.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		CalendarIDs: map[jmap.Id]bool{cal.ID: true},
		Title:       "RSVP Meeting",
		Status:      "confirmed",
		Participants: map[string]*jmap.JSCalendarParticipant{
			"me": {Email: "me@example.com", ParticipationStatus: "needs-action"},
		},
	})
	r4 := postJMAP(t, ts.URL, usingCal, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				string(rsvpEv.ID): map[string]any{
					"participants/me/participationStatus": "accepted",
				},
			},
		}, "c1"},
	})
	if len(r4.MethodResponses) != 1 || r4.MethodResponses[0].Name != "CalendarEvent/set" {
		t.Fatalf("Expected CalendarEvent/set response, got %v", r4.MethodResponses)
	}
	if updated, _ := r4.MethodResponses[0].Args["updated"].(map[string]any); updated == nil {
		t.Fatalf("RSVP update not applied: %+v", r4.MethodResponses[0].Args)
	} else if _, ok := updated[string(rsvpEv.ID)]; !ok {
		t.Fatalf("RSVP update missing event id: %+v", updated)
	}
	// The participant status MUST persist and the event-level status MUST be untouched.
	after, _, _ := srv.CalendarsBackend.GetCalendarEvents(context.Background(), []jmap.Id{rsvpEv.ID})
	if len(after) != 1 {
		t.Fatalf("could not re-fetch RSVP event")
	}
	if got := after[0].Participants["me"].ParticipationStatus; got != "accepted" {
		t.Errorf("expected participationStatus=accepted, got %q", got)
	}
	if after[0].Status != "confirmed" {
		t.Errorf("event-level status must be untouched by RSVP, got %q", after[0].Status)
	}

	// 4. ContactCard/* aliases (RFC 9610 Section 3.1)
	r5 := postJMAP(t, ts.URL, usingContacts, []any{
		[]any{"ContactCard/get", map[string]any{"accountId": "primary", "ids": nil}, "c1"},
		[]any{"ContactCard/query", map[string]any{"accountId": "primary"}, "c2"},
		[]any{"ContactCard/changes", map[string]any{"accountId": "primary", "sinceState": "0"}, "c3"},
	})
	if len(r5.MethodResponses) != 3 {
		t.Fatalf("Expected 3 ContactCard response aliases, got %d", len(r5.MethodResponses))
	}
	if r5.MethodResponses[0].Name != "ContactCard/get" || r5.MethodResponses[1].Name != "ContactCard/query" {
		t.Errorf("Unexpected alias response names: %s, %s", r5.MethodResponses[0].Name, r5.MethodResponses[1].Name)
	}

	_ = em
}
