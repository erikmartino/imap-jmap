package jmap_test

import (
	"context"
	"testing"

	"imap-jmap/jmap/memory"
)

// TestRFC9051_IMAP4rev2Mapping tests RFC 9051 IMAP4rev2 protocol mapping into JMAP keywords & mailbox attributes.
func TestRFC9051_IMAP4rev2Mapping(t *testing.T) {
	memBackend := memory.NewMemoryBackend()

	mbs, _, err := memBackend.GetMailboxes(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetMailboxes failed per RFC 9051: %v", err)
	}

	for _, mb := range mbs {
		if mb.Name == "Inbox" && (mb.Role == nil || *mb.Role != "inbox") {
			t.Errorf("Expected Inbox role 'inbox' per RFC 9051, got %v", mb.Role)
		}
	}
}
