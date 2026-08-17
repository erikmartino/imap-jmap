package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_InCalendarsFilter verifies the inCalendars (plural Id[]) FilterCondition, the
// canonical draft-ietf-jmap-calendars form, matches events in ANY listed calendar and excludes
// events in none of them.
func TestRFC8984_InCalendarsFilter(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	calA, _ := srv.CalendarsBackend.CreateCalendar(seedCtx(), &jmap.Calendar{Name: "A"})
	calB, _ := srv.CalendarsBackend.CreateCalendar(seedCtx(), &jmap.Calendar{Name: "B"})
	calC, _ := srv.CalendarsBackend.CreateCalendar(seedCtx(), &jmap.Calendar{Name: "C"})

	evA, _ := srv.CalendarsBackend.CreateCalendarEvent(seedCtx(), &jmap.CalendarEvent{
		CalendarIDs: map[jmap.Id]bool{calA.ID: true}, Title: "In A", Start: "2026-08-01T10:00:00Z",
	})
	evC, _ := srv.CalendarsBackend.CreateCalendarEvent(seedCtx(), &jmap.CalendarEvent{
		CalendarIDs: map[jmap.Id]bool{calC.ID: true}, Title: "In C", Start: "2026-08-02T10:00:00Z",
	})

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}
	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inCalendars": []any{string(calA.ID), string(calB.ID)}},
		}, "c1"},
	})
	ids, _ := resp.MethodResponses[0].Args["ids"].([]any)
	if len(ids) != 1 || ids[0] != string(evA.ID) {
		t.Fatalf("expected only event in calendar A, got %+v", ids)
	}
	// The event only in calendar C must be excluded.
	for _, id := range ids {
		if id == string(evC.ID) {
			t.Errorf("event in calendar C must not match inCalendars [A,B]")
		}
	}
}
