package jmap_test

import (
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
)

// TestRFC6851_IMAPMoveMailboxAssignment tests RFC 6851 IMAP MOVE command mailbox reassignment mapping.
func TestRFC6851_IMAPMoveMailboxAssignment(t *testing.T) {
	backend, cleanup := imapsmtp.NewEmbeddedBackend(testUsername)
	defer cleanup()

	ctx := seedCtx()
	inboxID := imapsmtp.MailboxIDForName("INBOX")
	archiveID := imapsmtp.MailboxIDForName("Archive")

	email, err := backend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{inboxID: true},
		Subject:    "RFC 6851 Move Test",
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	patch := map[string]any{
		"mailboxIds": map[string]any{string(archiveID): true},
	}
	up, err := backend.UpdateEmail(ctx, email.ID, patch)
	if err != nil {
		t.Fatalf("UpdateEmail (Move) failed per RFC 6851: %v", err)
	}

	if !up.MailboxIDs[archiveID] {
		t.Errorf("Expected email to be moved to %s per RFC 6851, got %v", archiveID, up.MailboxIDs)
	}
}
