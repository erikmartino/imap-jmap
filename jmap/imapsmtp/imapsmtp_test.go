package imapsmtp

import (
	"context"
	"testing"

	"imap-jmap/jmap"
)

func TestIMAPSMTPBackend_InterfaceCompliance(t *testing.T) {
	be := New("dovecot:143", "smtp:25")
	var _ jmap.MailBackend = be
	var _ jmap.BlobBackend = be

	ctx := context.Background()
	if state := be.State(ctx); state == "" {
		t.Errorf("expected non-empty state")
	}

	mailboxes, err := be.GetAllMailboxes(ctx)
	if err != nil {
		t.Fatalf("unexpected error getting mailboxes: %v", err)
	}
	if len(mailboxes) != 0 {
		t.Errorf("expected 0 mailboxes initially, got %d", len(mailboxes))
	}
}
