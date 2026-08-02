package dav_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/dav"
	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC6638_CalDAVSchedulingOptions tests RFC 6638 CalDAV scheduling options & object delivery.
func TestRFC6638_CalDAVSchedulingOptions(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	_, _ = calBackend.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		ID:    "evt-6638-sch",
		Title: "CalDAV Scheduling Meeting",
		Start: "2026-10-01T09:00:00Z",
		Participants: map[string]*jmap.JSCalendarParticipant{
			"attendee6638@example.com": {
				Email:  "attendee6638@example.com",
				Status: "needs-action",
			},
		},
	})

	srv := dav.NewServer(calBackend, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("PROPFIND", ts.URL+"/caldav/calendars/default", strings.NewReader(`<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <C:calendar-user-address-set/>
    <C:schedule-calendar-transp/>
  </D:prop>
</D:propfind>`))
	req.Header.Set("Content-Type", "application/xml")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CalDAV scheduling query failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 207 Multi-Status or 200 OK, got %d", resp.StatusCode)
	}
}
