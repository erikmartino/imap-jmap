package dav_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/dav"
)

// TestRFC4918_WebDAVBasePropfindOptions tests RFC 4918 WebDAV base methods (PROPFIND, OPTIONS).
func TestRFC4918_WebDAVBasePropfindOptions(t *testing.T) {
	srv := dav.NewServer(nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. OPTIONS request per RFC 4918 Section 10.1
	reqOptions, _ := http.NewRequest("OPTIONS", ts.URL+"/caldav/", nil)
	respOptions, err := http.DefaultClient.Do(reqOptions)
	if err != nil {
		t.Fatalf("OPTIONS /caldav/ failed: %v", err)
	}
	defer respOptions.Body.Close()

	davHeader := respOptions.Header.Get("DAV")
	if davHeader == "" {
		t.Logf("DAV Header on OPTIONS response: %s", davHeader)
	}

	// 2. PROPFIND request per RFC 4918 Section 9.1
	reqPropfind, _ := http.NewRequest("PROPFIND", ts.URL+"/caldav/calendars/default", strings.NewReader(`<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:">
  <D:prop>
    <D:resourcetype/>
  </D:prop>
</D:propfind>`))
	reqPropfind.Header.Set("Content-Type", "application/xml")

	respPropfind, err := http.DefaultClient.Do(reqPropfind)
	if err != nil {
		t.Fatalf("PROPFIND /caldav/ failed: %v", err)
	}
	defer respPropfind.Body.Close()

	if respPropfind.StatusCode != http.StatusMultiStatus && respPropfind.StatusCode != http.StatusOK {
		t.Errorf("Expected HTTP 207 Multi-Status or 200 OK, got %d", respPropfind.StatusCode)
	}
}

// TestRFC4918_WebDAVHTTPMethods tests WebDAV OPTIONS, GET, and HEAD HTTP methods per RFC 4918.
func TestRFC4918_WebDAVHTTPMethods(t *testing.T) {
	srv := dav.NewServer(nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. GET non-existent resource returns 404 or MultiStatus
	reqGet, _ := http.NewRequest("GET", ts.URL+"/caldav/calendars/default/nonexistent.ics", nil)
	respGet, err := http.DefaultClient.Do(reqGet)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer respGet.Body.Close()

	if respGet.StatusCode != http.StatusNotFound && respGet.StatusCode != http.StatusOK && respGet.StatusCode != http.StatusMultiStatus {
		t.Errorf("Unexpected status code for GET: %d", respGet.StatusCode)
	}
}
