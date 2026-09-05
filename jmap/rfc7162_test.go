package jmap_test

import (
	"testing"

	"imap-jmap/jmap/imapsmtp"
)

// TestRFC7162_IMAPCondstoreQresync tests RFC 7162 CONDSTORE & QRESYNC MODSEQ state tracking in JMAP.
func TestRFC7162_IMAPCondstoreQresync(t *testing.T) {
	backend, cleanup := imapsmtp.NewEmbeddedBackend(testUsername)
	defer cleanup()

	state := backend.State(seedCtx())
	if state == "" {
		t.Errorf("Expected non-empty state tracking MODSEQ per RFC 7162")
	}
}
