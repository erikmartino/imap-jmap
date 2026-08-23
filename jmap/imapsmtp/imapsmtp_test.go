package imapsmtp

import (
	"context"
	"os"
	"testing"

	"imap-jmap/jmap"
)

// getTestTargetServers returns the Dovecot IMAP and SMTP endpoints from environment or defaults to localhost
func getTestTargetServers() (string, string) {
	imapAddr := os.Getenv("TEST_IMAP_SERVER")
	if imapAddr == "" {
		imapAddr = "127.0.0.1:993"
	}

	smtpAddr := os.Getenv("TEST_SMTP_SERVER")
	if smtpAddr == "" {
		smtpAddr = "127.0.0.1:25"
	}
	return imapAddr, smtpAddr
}

func TestClientPool_DovecotContainerConnection(t *testing.T) {
	imapAddr, _ := getTestTargetServers()

	pool := NewClientPool(imapAddr)
	ctx := context.Background()

	// Authenticate against live local Dovecot container with matching username/password
	client, err := pool.GetClient(ctx, "user@example.com", "user@example.com")


	if err != nil {
		t.Fatalf("failed to connect/login to Docker Compose Dovecot server at %s: %v", imapAddr, err)
	}
	client.Close()
}

func TestIMAPSMTPBackend_DockerComposeIntegration(t *testing.T) {
	imapAddr, smtpAddr := getTestTargetServers()

	be := New(imapAddr, smtpAddr)
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
	_ = mailboxes
}
