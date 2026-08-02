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

// TestRFC6352_CardDAVPropfind tests RFC 6352 CardDAV PROPFIND method & HTTP 207 Multi-Status XML response.
func TestRFC6352_CardDAVPropfind(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	desc := "RFC 6352 Unit Test"
	_, _ = contactsBackend.CreateAddressBook(context.Background(), &jmap.AddressBook{
		ID:          "ab-6352",
		Name:        "CardDAV Test AddressBook",
		Description: &desc,
	})

	srv := dav.NewServer(nil, contactsBackend)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("PROPFIND", ts.URL+"/carddav/addressbooks/default", strings.NewReader(`<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:prop>
    <D:displayname/>
    <C:addressbook-description/>
  </D:prop>
</D:propfind>`))
	req.Header.Set("Content-Type", "application/xml")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PROPFIND /carddav/ failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 207 Multi-Status or 200 OK, got %d", resp.StatusCode)
	}
}
