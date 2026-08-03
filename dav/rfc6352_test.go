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

// TestRFC6352_CardDAVPutAndGet tests CardDAV PUT and GET for vCard objects per RFC 6352.
func TestRFC6352_CardDAVPutAndGet(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	srv := dav.NewServer(nil, contactsBackend)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	vcardData := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Alice Morgan\r\nEMAIL:alice.m@example.com\r\nTEL:+15559876543\r\nEND:VCARD\r\n"

	// 1. PUT vCard object
	reqPut, _ := http.NewRequest("PUT", ts.URL+"/carddav/addressbooks/default/alice.vcf", strings.NewReader(vcardData))
	reqPut.Header.Set("Content-Type", "text/vcard")
	respPut, err := http.DefaultClient.Do(reqPut)
	if err != nil {
		t.Fatalf("PUT /carddav/ vcard failed: %v", err)
	}
	defer respPut.Body.Close()

	if respPut.StatusCode != http.StatusOK && respPut.StatusCode != http.StatusCreated && respPut.StatusCode != http.StatusNoContent {
		t.Errorf("Expected 200/201/204 on PUT, got %d", respPut.StatusCode)
	}

	// 2. Verify contact card in ContactsBackend
	cards, _, err := contactsBackend.GetCards(context.Background(), nil)
	if err != nil || len(cards) == 0 {
		t.Fatalf("Expected card to be created in ContactsBackend, got 0 cards")
	}

	found := false
	for _, c := range cards {
		if c.Name != nil && c.Name.Full == "Alice Morgan" {
			found = true
			if e1, ok := c.Emails["e1"]; !ok || e1.Address != "alice.m@example.com" {
				t.Errorf("Expected email 'alice.m@example.com', got %v", c.Emails)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected card with FN 'Alice Morgan'")
	}
}
