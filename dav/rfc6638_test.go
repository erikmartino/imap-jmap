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

// TestRFC6638_CalDAVAutoiTIPOnPut tests RFC 6638 automatic iTIP participant extraction on PUT.
func TestRFC6638_CalDAVAutoiTIPOnPut(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := dav.NewServer(calBackend, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	icsData := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example Corp.//EN\r\nBEGIN:VEVENT\r\nUID:event-6638-put\r\nSUMMARY:Scheduled Meeting\r\nORGANIZER:mailto:organizer@example.com\r\nATTENDEE:mailto:attendee@example.com\r\nDTSTART:20261101T090000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

	reqPut, _ := http.NewRequest("PUT", ts.URL+"/caldav/calendars/default/event-6638-put.ics", strings.NewReader(icsData))
	reqPut.Header.Set("Content-Type", "text/calendar")
	respPut, err := http.DefaultClient.Do(reqPut)
	if err != nil {
		t.Fatalf("PUT /caldav/ scheduling event failed: %v", err)
	}
	defer respPut.Body.Close()

	if respPut.StatusCode != http.StatusOK && respPut.StatusCode != http.StatusCreated && respPut.StatusCode != http.StatusNoContent {
		t.Errorf("Expected 200/201/204 on PUT, got %d", respPut.StatusCode)
	}

	events, _, err := calBackend.GetCalendarEvents(context.Background(), nil)
	if err != nil || len(events) == 0 {
		t.Fatalf("Expected event to be created in CalDAVBackend")
	}

	found := false
	for _, ev := range events {
		if ev.ID == "event-6638-put" || ev.Title == "Scheduled Meeting" {
			found = true
			if ev.Participants == nil || ev.Participants["attendee@example.com"] == nil {
				t.Errorf("Expected attendee@example.com participant extracted via RFC 6638 auto-iTIP, got %v", ev.Participants)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected event-6638-put to be created")
	}
}
