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

// TestRFC4791_CalDAVPropfind tests RFC 4791 CalDAV PROPFIND method & HTTP 207 Multi-Status XML response.
func TestRFC4791_CalDAVPropfind(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	desc := "RFC 4791 Unit Test"
	_, _ = calBackend.CreateCalendar(context.Background(), &jmap.Calendar{
		ID:          "cal-4791",
		Name:        "CalDAV Test Calendar",
		Description: &desc,
	})

	srv := dav.NewServer(calBackend, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("PROPFIND", ts.URL+"/caldav/calendars/default", strings.NewReader(`<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <D:displayname/>
    <C:calendar-description/>
  </D:prop>
</D:propfind>`))
	req.Header.Set("Content-Type", "application/xml")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PROPFIND /caldav/ failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 207 Multi-Status or 200 OK, got %d", resp.StatusCode)
	}
}

// TestRFC4791_CalDAVPutAndGet tests CalDAV PUT and GET for iCalendar objects per RFC 4791.
func TestRFC4791_CalDAVPutAndGet(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := dav.NewServer(calBackend, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	icsData := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example Corp.//EN\r\nBEGIN:VEVENT\r\nUID:event-4791-put\r\nSUMMARY:CalDAV Test Event\r\nDTSTART:20261101T090000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

	// 1. PUT iCalendar object
	reqPut, _ := http.NewRequest("PUT", ts.URL+"/caldav/calendars/default/event-4791-put.ics", strings.NewReader(icsData))
	reqPut.Header.Set("Content-Type", "text/calendar")
	respPut, err := http.DefaultClient.Do(reqPut)
	if err != nil {
		t.Fatalf("PUT /caldav/ event failed: %v", err)
	}
	defer respPut.Body.Close()

	if respPut.StatusCode != http.StatusOK && respPut.StatusCode != http.StatusCreated && respPut.StatusCode != http.StatusNoContent {
		t.Errorf("Expected 200/201/204 on PUT, got %d", respPut.StatusCode)
	}

	// 2. Retrieve created event from backend
	events, _, err := calBackend.GetCalendarEvents(context.Background(), nil)
	if err != nil || len(events) == 0 {
		t.Fatalf("Expected event to be created in CalDAVBackend, got 0 events")
	}

	found := false
	for _, ev := range events {
		if ev.ID == "event-4791-put" || ev.Title == "CalDAV Test Event" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected event-4791-put with title 'CalDAV Test Event'")
	}
}
