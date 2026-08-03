package dav_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emersion/go-webdav/caldav"

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

	icsData := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example Corp.//EN\r\nBEGIN:VEVENT\r\nUID:event-4791-put\r\nSUMMARY:CalDAV Test Event\r\nDESCRIPTION:Extended description for CalDAV event\r\nDURATION:PT1H\r\nDTSTART:20261101T090000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

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
			if ev.Description != "Extended description for CalDAV event" {
				t.Errorf("Expected Description 'Extended description for CalDAV event', got %q", ev.Description)
			}
			if ev.Duration != "PT1H" {
				t.Errorf("Expected Duration 'PT1H', got %q", ev.Duration)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected event-4791-put with title 'CalDAV Test Event'")
	}
}

// TestRFC4791_CalDAVFullLifecycleAndReport tests complete CalDAV object lifecycle (GET, REPORT, DELETE) per RFC 4791.
func TestRFC4791_CalDAVFullLifecycleAndReport(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	_, _ = calBackend.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		ID:    "evt-4791-lc",
		Title: "Lifecycle Meeting",
		Start: "2026-12-01T10:00:00Z",
	})

	srv := dav.NewServer(calBackend, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. GET existing calendar object
	reqGet, _ := http.NewRequest("GET", ts.URL+"/caldav/calendars/default/evt-4791-lc.ics", nil)
	respGet, err := http.DefaultClient.Do(reqGet)
	if err != nil {
		t.Fatalf("GET /caldav/ event failed: %v", err)
	}
	defer respGet.Body.Close()
	if respGet.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK on GET, got %d", respGet.StatusCode)
	}

	// 2. GET non-existent object returns 404
	reqGet404, _ := http.NewRequest("GET", ts.URL+"/caldav/calendars/default/nonexistent-evt.ics", nil)
	respGet404, err := http.DefaultClient.Do(reqGet404)
	if err != nil {
		t.Fatalf("GET /caldav/ 404 failed: %v", err)
	}
	defer respGet404.Body.Close()
	if respGet404.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found on non-existent GET, got %d", respGet404.StatusCode)
	}

	// 3. REPORT calendar-query request
	reqReport, _ := http.NewRequest("REPORT", ts.URL+"/caldav/calendars/default", strings.NewReader(`<?xml version="1.0" encoding="utf-8" ?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <D:getetag/>
    <C:calendar-data/>
  </D:prop>
  <C:filter>
    <C:comp-filter name="VCALENDAR">
      <C:comp-filter name="VEVENT"/>
    </C:comp-filter>
  </C:filter>
</C:calendar-query>`))
	reqReport.Header.Set("Content-Type", "application/xml")
	respReport, err := http.DefaultClient.Do(reqReport)
	if err != nil {
		t.Fatalf("REPORT /caldav/ failed: %v", err)
	}
	defer respReport.Body.Close()
	if respReport.StatusCode != http.StatusMultiStatus && respReport.StatusCode != http.StatusOK {
		t.Errorf("Expected 207 Multi-Status or 200 OK on REPORT, got %d", respReport.StatusCode)
	}

	// 4. DELETE calendar object
	reqDel, _ := http.NewRequest("DELETE", ts.URL+"/caldav/calendars/default/evt-4791-lc.ics", nil)
	respDel, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("DELETE /caldav/ event failed: %v", err)
	}
	defer respDel.Body.Close()
	if respDel.StatusCode != http.StatusOK && respDel.StatusCode != http.StatusNoContent {
		t.Errorf("Expected 200/204 on DELETE, got %d", respDel.StatusCode)
	}
}

// TestRFC4791_CalDAVPrincipalAndCalendarManagement tests CalDAV backend principal paths and calendar creation/deletion.
func TestRFC4791_CalDAVPrincipalAndCalendarManagement(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	b := dav.NewCalDAVBackend(calBackend)
	ctx := context.Background()

	principal, err := b.CurrentUserPrincipal(ctx)
	if err != nil || principal != "/caldav/principals/user" {
		t.Errorf("CurrentUserPrincipal = %q, want '/caldav/principals/user'", principal)
	}

	homeSet, err := b.CalendarHomeSetPath(ctx)
	if err != nil || homeSet != "/caldav/calendars/" {
		t.Errorf("CalendarHomeSetPath = %q, want '/caldav/calendars/'", homeSet)
	}

	// Test CreateCalendar & DeleteCalendar
	err = b.CreateCalendar(ctx, &caldav.Calendar{
		Path:        "/caldav/calendars/work-cal",
		Name:        "Work Cal",
		Description: "Work events",
	})
	if err != nil {
		t.Fatalf("CreateCalendar failed: %v", err)
	}

	err = b.DeleteCalendar(ctx, "/caldav/calendars/work-cal")
	if err != nil {
		t.Errorf("DeleteCalendar failed: %v", err)
	}
}
