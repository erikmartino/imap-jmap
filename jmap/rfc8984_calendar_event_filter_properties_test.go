package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_CalendarEventFilterPropertiesPosNeg tests CalendarEvent/query filter conditions (inCalendar, text) per RFC 8984.
func TestRFC8984_CalendarEventFilterPropertiesPosNeg(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := context.Background()

	cal1, _ := srv.CalendarsBackend.CreateCalendar(ctx, &jmap.Calendar{Name: "Work"})
	cal2, _ := srv.CalendarsBackend.CreateCalendar(ctx, &jmap.Calendar{Name: "Personal"})

	ev1, _ := srv.CalendarsBackend.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
		CalendarIDs: map[jmap.Id]bool{cal1.ID: true},
		Title:       "Quarterly Review",
	})
	ev2, _ := srv.CalendarsBackend.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
		CalendarIDs: map[jmap.Id]bool{cal2.ID: true},
		Title:       "Vacation Planning",
	})

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Positive filter by inCalendar: cal1.ID -> returns ev1
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inCalendar": string(cal1.ID)},
		}, "c1"},
	})
	ids1, _ := res1.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 || ids1[0] != string(ev1.ID) {
		t.Errorf("CalendarEvent inCalendar positive expected [%s], got %v", ev1.ID, ids1)
	}

	// 2. Positive filter by text substring: "Quarterly" -> returns ev1
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"text": "Quarterly"},
		}, "c2"},
	})
	ids2, _ := res2.MethodResponses[0].Args["ids"].([]any)
	if len(ids2) != 1 || ids2[0] != string(ev1.ID) {
		t.Errorf("CalendarEvent text positive expected [%s], got %v", ev1.ID, ids2)
	}

	// 3. Negative filter by text substring: "NonExistentMeeting999" -> returns empty []
	res3 := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"text": "NonExistentMeeting999"},
		}, "c3"},
	})
	ids3, _ := res3.MethodResponses[0].Args["ids"].([]any)
	if len(ids3) != 0 {
		t.Errorf("CalendarEvent text negative expected empty [], got %v", ids3)
	}

	_ = ev2
}
