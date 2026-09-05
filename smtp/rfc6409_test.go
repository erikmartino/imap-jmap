package smtp_test

import (
	"context"
	"net"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC6409_MessageSubmission tests RFC 6409 Message Submission protocol processing via EmailSubmission.
func TestRFC6409_MessageSubmission(t *testing.T) {
	backend, cleanup := imapsmtp.NewEmbeddedBackend("user@example.com")
	defer cleanup()

	// Start a local SMTP server to receive the submission
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, backend, backend, nil)
	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	backend.SetSMTPAddr(addr)

	ctx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject("user@example.com"))
	em, err := backend.CreateEmail(ctx, &jmap.Email{
		Subject: "Submission Test",
		From:    []jmap.EmailAddress{{Email: "user@example.com"}},
		To:      []jmap.EmailAddress{{Email: "recipient@example.com"}},
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	sub, err := backend.CreateSubmission(ctx, &jmap.EmailSubmission{
		EmailID:  em.ID,
		ThreadID: em.ThreadID,
	})
	if err != nil {
		t.Fatalf("CreateSubmission failed per RFC 6409: %v", err)
	}

	if sub.ID == "" {
		t.Errorf("Expected submission ID per RFC 6409")
	}
}
