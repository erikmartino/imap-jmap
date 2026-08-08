package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_EventStatusEnum verifies that Event "status" only accepts Event states
// (confirmed/tentative/cancelled) and rejects Task-style states, which JSCalendar tracks via
// "progress" instead (RFC 8984 Section 4.4.2 vs 5.2.5).
func TestRFC8984_EventStatusEnum(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"ok":  map[string]any{"title": "Confirmed", "status": "confirmed"},
				"bad": map[string]any{"title": "Taskish", "status": "in-progress"},
			},
		}, "c1"},
	})
	created, _ := resp.MethodResponses[0].Args["created"].(map[string]any)
	notCreated, _ := resp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if created["ok"] == nil {
		t.Errorf("event with status=confirmed should be created")
	}
	if notCreated["bad"] == nil {
		t.Errorf("event with status=in-progress must be rejected")
	} else if et, _ := notCreated["bad"].(map[string]any)["type"].(string); et != "invalidProperties" {
		t.Errorf("expected invalidProperties, got %q", et)
	}
}

// TestRFC8984_UnknownCalendarIdRejected verifies that create with a calendarIds referencing a
// non-existent calendar is rejected rather than silently pointing at a dangling calendar.
func TestRFC8984_UnknownCalendarIdRejected(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e": map[string]any{
					"title":       "Dangling",
					"start":       "2026-08-01T10:00:00Z",
					"calendarIds": map[string]any{"nonexistent-cal": true},
				},
			},
		}, "c1"},
	})
	notCreated, _ := resp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated["e"] == nil {
		t.Fatalf("expected notCreated for unknown calendarIds, got %+v", resp.MethodResponses[0].Args)
	}
}

// TestRFC8984_CalendarSetRejectsUnknownProperty verifies Calendar/set rejects unknown properties.
func TestRFC8984_CalendarSetRejectsUnknownProperty(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"cal": map[string]any{"name": "My Cal", "bogusProperty": "x"},
			},
		}, "c1"},
	})
	notCreated, _ := resp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated["cal"] == nil {
		t.Fatalf("expected notCreated for unknown Calendar property, got %+v", resp.MethodResponses[0].Args)
	}
	if et, _ := notCreated["cal"].(map[string]any)["type"].(string); et != "invalidProperties" {
		t.Errorf("expected invalidProperties, got %q", et)
	}
}

// TestRFC8984_TaskProgressEnum verifies the JSCalendar Task progress enum (RFC 8984 Section 5.2.5):
// a valid value (in-process) is accepted and a non-spec value (in-progress) is rejected.
func TestRFC8984_TaskProgressEnum(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"ok":  map[string]any{"@type": "Task", "title": "Do", "progress": "in-process"},
				"bad": map[string]any{"@type": "Task", "title": "Do", "progress": "in-progress"},
			},
		}, "c1"},
	})
	created, _ := resp.MethodResponses[0].Args["created"].(map[string]any)
	notCreated, _ := resp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if created["ok"] == nil {
		t.Errorf("progress=in-process should be accepted")
	}
	if notCreated["bad"] == nil {
		t.Errorf("progress=in-progress (non-spec) must be rejected")
	}
}
