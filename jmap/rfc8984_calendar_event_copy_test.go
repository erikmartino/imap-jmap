package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_CalendarEventCopyRoundTrip tests CalendarEvent/copy round-trip per RFC 8984 / RFC 8620 Section 5.4.
func TestRFC8984_CalendarEventCopyRoundTrip(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed calendar & event
	cal, _ := srv.CalendarsBackend.CreateCalendar(context.Background(), &jmap.Calendar{Name: "Cal 1"})
	ev, err := srv.CalendarsBackend.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		CalendarIDs: map[jmap.Id]bool{cal.ID: true},
		Title:       "Original Event",
	})
	if err != nil {
		t.Fatalf("Failed to seed event: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Copy CalendarEvent with title override
	calls1 := []any{
		[]any{"CalendarEvent/copy", map[string]any{
			"fromAccountId": "primary",
			"accountId":     "primary",
			"create": map[string]any{
				"copy1": map[string]any{
					"id":          string(ev.ID),
					"calendarIds": map[string]bool{string(cal.ID): true},
					"title":       "Copied Event Title",
				},
			},
		}, "c1"},
	}

	res1 := postJMAP(t, ts.URL, using, calls1)
	if len(res1.MethodResponses) == 0 {
		t.Fatalf("Empty response for CalendarEvent/copy")
	}
	created, _ := res1.MethodResponses[0].Args["created"].(map[string]any)
	copyObj, ok := created["copy1"].(map[string]any)
	if !ok {
		t.Fatalf("Failed to copy CalendarEvent: %v", res1.MethodResponses[0].Args)
	}
	newID, _ := copyObj["id"].(string)
	if newID == "" || newID == string(ev.ID) {
		t.Errorf("Copied CalendarEvent ID should be new, got %q", newID)
	}

	// 2. CalendarEvent/copy missing source ID -> notCreated with type notFound
	calls2 := []any{
		[]any{"CalendarEvent/copy", map[string]any{
			"fromAccountId": "primary",
			"accountId":     "primary",
			"create": map[string]any{
				"copyBad": map[string]any{
					"id":          "non-existent-ev-999",
					"calendarIds": map[string]bool{string(cal.ID): true},
				},
			},
		}, "c2"},
	}
	res2 := postJMAP(t, ts.URL, using, calls2)
	notCreated, _ := res2.MethodResponses[0].Args["notCreated"].(map[string]any)
	errObj, ok := notCreated["copyBad"].(map[string]any)
	if !ok {
		t.Fatalf("Expected copyBad in notCreated")
	}
	errType, _ := errObj["type"].(string)
	if errType != "notFound" {
		t.Errorf("Expected notFound in notCreated for missing source event, got %q", errType)
	}
}

// TestRFC8984_CalendarEventCopyDestroyOriginal verifies onSuccessDestroyOriginal removes the
// source event after a successful copy, and destroyFromIfInState guards it (RFC 8620 Section 5.4).
func TestRFC8984_CalendarEventCopyDestroyOriginal(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cal, _ := srv.CalendarsBackend.CreateCalendar(context.Background(), &jmap.Calendar{Name: "Cal Src"})
	ev, err := srv.CalendarsBackend.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		CalendarIDs: map[jmap.Id]bool{cal.ID: true},
		Title:       "Move Me",
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// Wrong destroyFromIfInState -> stateMismatch method error, nothing copied or destroyed.
	res0 := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/copy", map[string]any{
			"fromAccountId":            "primary",
			"accountId":                "primary",
			"onSuccessDestroyOriginal": true,
			"destroyFromIfInState":     "not-the-state",
			"create": map[string]any{
				"c": map[string]any{"id": string(ev.ID), "calendarIds": map[string]bool{string(cal.ID): true}},
			},
		}, "c0"},
	})
	if res0.MethodResponses[0].Name != "error" {
		t.Fatalf("expected stateMismatch error for wrong destroyFromIfInState, got %+v", res0.MethodResponses[0])
	}
	if still, _, _ := srv.CalendarsBackend.GetCalendarEvents(context.Background(), []jmap.Id{ev.ID}); len(still) != 1 {
		t.Fatalf("source event must survive a failed copy")
	}

	// Correct copy with onSuccessDestroyOriginal -> copy created, original destroyed.
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/copy", map[string]any{
			"fromAccountId":            "primary",
			"accountId":                "primary",
			"onSuccessDestroyOriginal": true,
			"create": map[string]any{
				"c": map[string]any{"id": string(ev.ID), "calendarIds": map[string]bool{string(cal.ID): true}, "title": "Moved"},
			},
		}, "c1"},
	})
	created, _ := res.MethodResponses[0].Args["created"].(map[string]any)
	if created["c"] == nil {
		t.Fatalf("copy failed: %+v", res.MethodResponses[0].Args)
	}
	if still, _, _ := srv.CalendarsBackend.GetCalendarEvents(context.Background(), []jmap.Id{ev.ID}); len(still) != 0 {
		t.Errorf("expected original event destroyed after onSuccessDestroyOriginal, still present")
	}
}
