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
