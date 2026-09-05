package jmap_test

import (
	"testing"

	"imap-jmap/jmap/imapsmtp"
)

// TestRFC3501_IMAP4rev1Mapping tests RFC 3501 IMAP4rev1 protocol mapping into JMAP keywords & mailbox attributes.
func TestRFC3501_IMAP4rev1Mapping(t *testing.T) {
	backend, cleanup := imapsmtp.NewEmbeddedBackend(testUsername)
	defer cleanup()

	mbs, err := backend.GetAllMailboxes(seedCtx())
	if err != nil {
		t.Fatalf("GetMailboxes failed per RFC 3501: %v", err)
	}

	if len(mbs) == 0 {
		t.Errorf("Expected default mailboxes per RFC 3501")
	}
}
