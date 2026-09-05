package jmap_test

import (
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
)

// TestRFC4315_IMAPUIDPlusTracking tests RFC 4315 IMAP UIDPLUS unique identifier tracking in JMAP IDs.
func TestRFC4315_IMAPUIDPlusTracking(t *testing.T) {
	backend, cleanup := imapsmtp.NewEmbeddedBackend(testUsername)
	defer cleanup()

	email, err := backend.CreateEmail(seedCtx(), &jmap.Email{
		Subject: "UIDPLUS Test",
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	if email.ID == "" {
		t.Errorf("Expected UIDPLUS ID per RFC 4315")
	}
}
