package jmap_test

import (
	"testing"

	"imap-jmap/jmap/imapsmtp"
)

// TestRFC9051_IMAP4rev2Mapping tests RFC 9051 IMAP4rev2 protocol mapping into JMAP keywords & mailbox attributes.
func TestRFC9051_IMAP4rev2Mapping(t *testing.T) {
	backend, cleanup := imapsmtp.NewEmbeddedBackend(testUsername)
	defer cleanup()

	mbs, _, err := backend.GetMailboxes(seedCtx(), nil)
	if err != nil {
		t.Fatalf("GetMailboxes failed per RFC 9051: %v", err)
	}

	for _, mb := range mbs {
		if mb.Name == "INBOX" && (mb.Role == nil || *mb.Role != "inbox") {
			t.Errorf("Expected INBOX role 'inbox' per RFC 9051, got %v", mb.Role)
		}
	}
}
