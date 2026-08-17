package smtp_test

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/spectest"
	jmapsmtp "imap-jmap/smtp"
)

// startSMTPServerWithResolver starts an SMTP receiver on a loopback port using the
// supplied backends and the given AccountResolver, waiting until the socket accepts.
func startSMTPServerWithResolver(t *testing.T, mailBackend jmap.MailBackend, blobBackend jmap.BlobBackend, calBackend jmap.CalendarsBackend, resolver jmap.AccountResolver) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, mailBackend, blobBackend, calBackend, jmapsmtp.WithAccountResolver(resolver))
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() { _ = srv.Close() })
	time.Sleep(50 * time.Millisecond)
	return addr
}

// TestRFC5321_RejectExternalRecipient verifies that with an AccountResolver configured
// the receiver refuses RCPT TO for addresses it cannot deliver to (RFC 5321 Section 3.4:
// the server must not accept mail for a recipient it cannot serve) with a permanent
// 550 5.7.1 "Relaying denied" reply (RFC 3463 Section 3.7), and stores nothing in any
// account — instead of the previous silent accept-and-drop-into-a-default-account.
func TestRFC5321_RejectExternalRecipient(t *testing.T) {
	spectest.Require(t, "RFC5321", "3.4", spectest.MUST,
		"A receiving SMTP server rejects recipients it cannot deliver to (relaying denied) rather than accepting the message.")

	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	addr := startSMTPServerWithResolver(t, memBackend, memBlobBackend, nil, resolver)

	msg := []byte("From: sender@example.com\r\n" +
		"To: external@other.com\r\n" +
		"Subject: Must be refused\r\n" +
		"\r\n" +
		"body\r\n")

	err := smtp.SendMail(addr, nil, "sender@example.com", []string{"external@other.com"}, msg)
	if err == nil {
		t.Fatalf("expected RCPT rejection for non-local recipient, send succeeded")
	}
	if !strings.Contains(err.Error(), "550") || !strings.Contains(err.Error(), "Relaying denied") {
		t.Errorf("expected 550 Relaying denied reply, got: %v", err)
	}

	// Nothing may have been stored anywhere: the external recipient must not land in
	// the default fallback account or in an account derived from the recipient.
	fallbackCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject("user@example.com"))
	fallbackEmails, err := memBackend.GetAllEmails(fallbackCtx)
	if err != nil {
		t.Fatalf("GetAllEmails(fallback): %v", err)
	}
	for _, em := range fallbackEmails {
		if em.Subject == "Must be refused" {
			t.Errorf("message for external recipient must not be stored in the fallback account")
		}
	}
	derivedCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject("external@other.com"))
	derivedEmails, err := memBackend.GetAllEmails(derivedCtx)
	if err != nil {
		t.Fatalf("GetAllEmails(derived): %v", err)
	}
	for _, em := range derivedEmails {
		if em.Subject == "Must be refused" {
			t.Errorf("message for external recipient must not be stored in a derived account")
		}
	}
}

// TestRFC5321_MixedRecipientsRejectExternal verifies per-recipient RCPT handling in one
// transaction: the local recipient is accepted with 250, the external one is refused
// with 550 5.7.1, and the message is delivered only to the local recipient's account.
func TestRFC5321_MixedRecipientsRejectExternal(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	addr := startSMTPServerWithResolver(t, memBackend, memBlobBackend, nil, resolver)

	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.Hello("tester.example.com"); err != nil {
		t.Fatalf("hello: %v", err)
	}
	if err := c.Mail("sender@example.com"); err != nil {
		t.Fatalf("mail: %v", err)
	}
	if err := c.Rcpt("user@example.com"); err != nil {
		t.Fatalf("local RCPT should be accepted: %v", err)
	}
	if err := c.Rcpt("external@other.com"); err == nil {
		t.Fatalf("external RCPT should be rejected")
	} else if !strings.Contains(err.Error(), "550") {
		t.Errorf("expected 550 for external RCPT, got: %v", err)
	}

	w, err := c.Data()
	if err != nil {
		t.Fatalf("data: %v", err)
	}
	if _, err := w.Write([]byte("From: sender@example.com\r\nTo: user@example.com, external@other.com\r\nSubject: Mixed\r\n\r\nbody\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("data close: %v", err)
	}
	if err := c.Quit(); err != nil {
		t.Fatalf("quit: %v", err)
	}

	// Delivered only to the local recipient.
	localCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject("user@example.com"))
	localEmails, err := memBackend.GetAllEmails(localCtx)
	if err != nil {
		t.Fatalf("GetAllEmails(local): %v", err)
	}
	var delivered *jmap.Email
	for _, em := range localEmails {
		if em.Subject == "Mixed" {
			delivered = em
			break
		}
	}
	if delivered == nil {
		t.Fatalf("local recipient should have received the message")
	}
	if !delivered.MailboxIDs["mb-inbox"] {
		t.Errorf("expected the delivered message in the local recipient's Inbox")
	}
	externalCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject("external@other.com"))
	externalEmails, err := memBackend.GetAllEmails(externalCtx)
	if err != nil {
		t.Fatalf("GetAllEmails(external): %v", err)
	}
	for _, em := range externalEmails {
		if em.Subject == "Mixed" {
			t.Errorf("external recipient must not receive the message")
		}
	}
}

// failingMailBackend wraps the memory MailBackend and fails every CreateEmail call to
// simulate a storage outage, so the DATA transaction must report a failure reply
// instead of acknowledging a message that was never stored.
type failingMailBackend struct {
	*memory.MemoryBackend
}

func (f *failingMailBackend) CreateEmail(ctx context.Context, em *jmap.Email) (*jmap.Email, error) {
	return nil, fmt.Errorf("simulated storage outage")
}

// TestRFC5321_DataStorageFailureReturns451 verifies that when the message cannot be
// stored for any recipient, the receiver replies with a transient failure (451 4.3.0,
// RFC 3463: mail system error) instead of the previous unconditional "250 OK: queued"
// that left clients believing the mail had been accepted.
func TestRFC5321_DataStorageFailureReturns451(t *testing.T) {
	spectest.Require(t, "RFC5321", "3.7", spectest.MUST,
		"A receiving SMTP server must not reply 250 to DATA when the message could not be stored for any recipient; it must return a failure reply.")

	memBlobBackend := memory.NewMemoryBlobBackend()
	badBackend := &failingMailBackend{MemoryBackend: memory.NewMemoryBackend()}
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	addr := startSMTPServerWithResolver(t, badBackend, memBlobBackend, nil, resolver)

	msg := []byte("From: sender@example.com\r\n" +
		"To: user@example.com\r\n" +
		"Subject: Storage outage\r\n" +
		"\r\n" +
		"body\r\n")

	err := smtp.SendMail(addr, nil, "sender@example.com", []string{"user@example.com"}, msg)
	if err == nil {
		t.Fatalf("expected DATA failure reply when storage fails, send succeeded")
	}
	if !strings.Contains(err.Error(), "451") {
		t.Errorf("expected 451 transient failure reply, got: %v", err)
	}

	// The message must not be acknowledged as stored: the recipient account must not
	// contain an email for the failed transaction. (The blob store has no delete API,
	// so the raw blob PutBlob already wrote remains; the email referencing it is not.)
	ctx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject("user@example.com"))
	emails, err := badBackend.GetAllEmails(ctx)
	if err != nil {
		t.Fatalf("GetAllEmails: %v", err)
	}
	for _, em := range emails {
		if em.Subject == "Storage outage" {
			t.Errorf("failed transaction must not leave stored emails behind")
		}
	}
}
