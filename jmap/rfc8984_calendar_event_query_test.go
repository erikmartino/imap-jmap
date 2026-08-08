package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestCalendarEventQuery_FilterProperties tests the CalendarEvent/query uid, updatedBefore, and
// updatedAfter FilterCondition properties. These are defined by the JMAP for Calendars I-D
// (draft-ietf-jmap-calendars Section 5.11.1); filed under rfc8984_* per repo convention since
// JMAP-for-Calendars is not yet an RFC. The uid property itself is JSCalendar (RFC 8984 Section 4.1.2).
func TestCalendarEventQuery_FilterProperties(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed a calendar, then an event referencing it (calendarIds must reference an existing
	// calendar).
	cal, _ := srv.CalendarsBackend.CreateCalendar(context.Background(), &jmap.Calendar{Name: "Cal 1"})
	ev, err := srv.CalendarsBackend.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		CalendarIDs: map[jmap.Id]bool{cal.ID: true},
		Title:       "Meeting",
		UID:         "unique-uid-123",
		Updated:     "2026-06-01T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("Failed to create CalendarEvent: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}
	calls := []any{
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary", "filter": map[string]any{"uid": "unique-uid-123"}}, "c1"},
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary", "filter": map[string]any{"uid": "wrong-uid"}}, "c2"},
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary", "filter": map[string]any{"updatedAfter": "2026-01-01T00:00:00Z"}}, "c3"},
		[]any{"CalendarEvent/query", map[string]any{"accountId": "primary", "filter": map[string]any{"updatedBefore": "2026-01-01T00:00:00Z"}}, "c4"},
	}

	res := postJMAP(t, ts.URL, using, calls)
	if len(res.MethodResponses) != 4 {
		t.Fatalf("Expected 4 responses, got %d", len(res.MethodResponses))
	}

	ids1, _ := res.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 || ids1[0] != string(ev.ID) {
		t.Errorf("uid match expected [%s], got %v", ev.ID, ids1)
	}

	ids2, _ := res.MethodResponses[1].Args["ids"].([]any)
	if len(ids2) != 0 {
		t.Errorf("uid mismatch expected [], got %v", ids2)
	}

	ids3, _ := res.MethodResponses[2].Args["ids"].([]any)
	if len(ids3) != 1 || ids3[0] != string(ev.ID) {
		t.Errorf("updatedAfter match expected [%s], got %v", ev.ID, ids3)
	}

	ids4, _ := res.MethodResponses[3].Args["ids"].([]any)
	if len(ids4) != 0 {
		t.Errorf("updatedBefore mismatch expected [], got %v", ids4)
	}
}
