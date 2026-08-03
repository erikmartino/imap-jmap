package dav_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emersion/go-webdav/carddav"

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

	vcardData := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Alice Morgan\r\nNICKNAME:Ali\r\nORG:ACME Corp\r\nEMAIL:alice.m@example.com\r\nTEL:+15559876543\r\nEND:VCARD\r\n"

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
			if n1, ok := c.Nicknames["n1"]; !ok || n1.Name != "Ali" {
				t.Errorf("Expected nickname 'Ali', got %v", c.Nicknames)
			}
			if o1, ok := c.Organizations["o1"]; !ok || o1.Name != "ACME Corp" {
				t.Errorf("Expected organization 'ACME Corp', got %v", c.Organizations)
			}
			break
		}
	}
	if !found {
		t.Errorf("Expected card with FN 'Alice Morgan'")
	}
}

// TestRFC6352_CardDAVFullLifecycleAndReport tests complete CardDAV object lifecycle (GET, REPORT, DELETE) per RFC 6352.
func TestRFC6352_CardDAVFullLifecycleAndReport(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	_, _ = contactsBackend.CreateCard(context.Background(), &jmap.Card{
		ID:   "card-6352-lc",
		Name: &jmap.JSContactName{Full: "Bob Builder"},
		Emails: map[string]*jmap.JSContactEmailAddress{
			"e1": {Address: "bob@example.com"},
		},
	})

	srv := dav.NewServer(nil, contactsBackend)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. GET existing address object
	reqGet, _ := http.NewRequest("GET", ts.URL+"/carddav/addressbooks/default/card-6352-lc.vcf", nil)
	respGet, err := http.DefaultClient.Do(reqGet)
	if err != nil {
		t.Fatalf("GET /carddav/ vcard failed: %v", err)
	}
	defer respGet.Body.Close()
	if respGet.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK on GET, got %d", respGet.StatusCode)
	}

	// 2. GET non-existent address object returns 404
	reqGet404, _ := http.NewRequest("GET", ts.URL+"/carddav/addressbooks/default/nonexistent-card.vcf", nil)
	respGet404, err := http.DefaultClient.Do(reqGet404)
	if err != nil {
		t.Fatalf("GET /carddav/ 404 failed: %v", err)
	}
	defer respGet404.Body.Close()
	if respGet404.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found on non-existent GET, got %d", respGet404.StatusCode)
	}

	// 3. REPORT addressbook-query request
	reqReport, _ := http.NewRequest("REPORT", ts.URL+"/carddav/addressbooks/default", strings.NewReader(`<?xml version="1.0" encoding="utf-8" ?>
<C:addressbook-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:prop>
    <D:getetag/>
    <C:address-data/>
  </D:prop>
  <C:filter>
    <C:prop-filter name="FN"/>
  </C:filter>
</C:addressbook-query>`))
	reqReport.Header.Set("Content-Type", "application/xml")
	respReport, err := http.DefaultClient.Do(reqReport)
	if err != nil {
		t.Fatalf("REPORT /carddav/ failed: %v", err)
	}
	defer respReport.Body.Close()
	if respReport.StatusCode != http.StatusMultiStatus && respReport.StatusCode != http.StatusOK {
		t.Errorf("Expected 207 Multi-Status or 200 OK on REPORT, got %d", respReport.StatusCode)
	}

	// 4. DELETE address object
	reqDel, _ := http.NewRequest("DELETE", ts.URL+"/carddav/addressbooks/default/card-6352-lc.vcf", nil)
	respDel, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("DELETE /carddav/ vcard failed: %v", err)
	}
	defer respDel.Body.Close()
	if respDel.StatusCode != http.StatusOK && respDel.StatusCode != http.StatusNoContent {
		t.Errorf("Expected 200/204 on DELETE, got %d", respDel.StatusCode)
	}
}

// TestRFC6352_CardDAVPrincipalAndAddressBookManagement tests CardDAV backend principal paths and addressbook creation/deletion.
func TestRFC6352_CardDAVPrincipalAndAddressBookManagement(t *testing.T) {
	contactsBackend := memory.NewMemoryContactsBackend()
	b := dav.NewCardDAVBackend(contactsBackend)
	ctx := context.Background()

	principal, err := b.CurrentUserPrincipal(ctx)
	if err != nil || principal != "/carddav/principals/user" {
		t.Errorf("CurrentUserPrincipal = %q, want '/carddav/principals/user'", principal)
	}

	homeSet, err := b.AddressBookHomeSetPath(ctx)
	if err != nil || homeSet != "/carddav/addressbooks/" {
		t.Errorf("AddressBookHomeSetPath = %q, want '/carddav/addressbooks/'", homeSet)
	}

	// Test CreateAddressBook & DeleteAddressBook
	err = b.CreateAddressBook(ctx, &carddav.AddressBook{
		Path:        "/carddav/addressbooks/work-ab",
		Name:        "Work Contacts",
		Description: "Work contacts list",
	})
	if err != nil {
		t.Fatalf("CreateAddressBook failed: %v", err)
	}

	err = b.DeleteAddressBook(ctx, "/carddav/addressbooks/work-ab")
	if err != nil {
		t.Errorf("DeleteAddressBook failed: %v", err)
	}
}
