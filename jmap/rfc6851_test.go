package jmap_test

import (
	"context"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC6851_IMAPMoveMailboxAssignment tests RFC 6851 IMAP MOVE command mailbox reassignment mapping.
func TestRFC6851_IMAPMoveMailboxAssignment(t *testing.T) {
	memBackend := memory.NewMemoryBackend()

	email, err := memBackend.CreateEmail(context.Background(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "RFC 6851 Move Test",
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	patch := map[string]any{
		"mailboxIds": map[string]any{"mb-archive": true},
	}
	up, err := memBackend.UpdateEmail(context.Background(), email.ID, patch)
	if err != nil {
		t.Fatalf("UpdateEmail (Move) failed per RFC 6851: %v", err)
	}

	if !up.MailboxIDs["mb-archive"] {
		t.Errorf("Expected email to be moved to mb-archive per RFC 6851")
	}
}
