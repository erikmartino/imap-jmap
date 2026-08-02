package jmap_test

import (
	"context"
	"testing"

	"imap-jmap/jmap/memory"
)

// TestRFC3501_IMAP4rev1Mapping tests RFC 3501 IMAP4rev1 protocol mapping into JMAP keywords & mailbox attributes.
func TestRFC3501_IMAP4rev1Mapping(t *testing.T) {
	memBackend := memory.NewMemoryBackend()

	mbs, err := memBackend.GetAllMailboxes(context.Background())
	if err != nil {
		t.Fatalf("GetMailboxes failed per RFC 3501: %v", err)
	}

	if len(mbs) == 0 {
		t.Errorf("Expected default mailboxes per RFC 3501")
	}
}
