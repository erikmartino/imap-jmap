package smtp_test

import (
	"context"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	jmapsmtp "imap-jmap/smtp"
)

func TestParseMessageToEmail(t *testing.T) {
	rawMsg := []byte("From: Alice <alice@example.com>\r\n" +
		"To: Bob <bob@example.com>\r\n" +
		"Subject: Hello World JMAP\r\n" +
		"Message-ID: <msg-123@example.com>\r\n" +
		"Date: Sun, 02 Aug 2026 10:00:00 +0000\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"This is a test email body for JMAP ingestion.\r\n")

	email, err := jmapsmtp.ParseMessageToEmail(rawMsg, "blob-1")
	if err != nil {
		t.Fatalf("ParseMessageToEmail failed: %v", err)
	}

	if email.Subject != "Hello World JMAP" {
		t.Errorf("expected Subject 'Hello World JMAP', got %q", email.Subject)
	}
	if len(email.From) != 1 || email.From[0].Email != "alice@example.com" {
		t.Errorf("unexpected From: %+v", email.From)
	}
	if len(email.To) != 1 || email.To[0].Email != "bob@example.com" {
		t.Errorf("unexpected To: %+v", email.To)
	}
	if len(email.MessageID) != 1 || email.MessageID[0] != "msg-123@example.com" {
		t.Errorf("unexpected MessageID: %+v", email.MessageID)
	}
	if !email.MailboxIDs["mb-inbox"] {
		t.Errorf("expected email to be placed in mb-inbox")
	}
	if !email.Keywords["$unread"] {
		t.Errorf("expected email to have $unread keyword")
	}
	if email.Preview == "" {
		t.Errorf("expected non-empty preview")
	}
}

func TestSMTPServerReceive(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()

	// Find free local port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	srv := jmapsmtp.NewServer(addr, memBackend, memBlobBackend)
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	defer srv.Close()

	// Give server time to listen
	time.Sleep(100 * time.Millisecond)

	initialEmails, err := memBackend.GetAllEmails(context.Background())
	if err != nil {
		t.Fatalf("GetAllEmails failed: %v", err)
	}
	initialCount := len(initialEmails)

	// Send message over SMTP
	from := "sender@example.com"
	to := []string{"recipient@example.com"}
	msg := []byte("From: <sender@example.com>\r\n" +
		"To: <recipient@example.com>\r\n" +
		"Subject: SMTP Test Delivery\r\n" +
		"Message-ID: <smtp-delivery-1@example.com>\r\n" +
		"\r\n" +
		"Received via SMTP server!")

	err = smtp.SendMail(addr, nil, from, to, msg)
	if err != nil {
		t.Fatalf("smtp.SendMail failed: %v", err)
	}

	// Verify email stored in JMAP MailBackend
	emails, err := memBackend.GetAllEmails(context.Background())
	if err != nil {
		t.Fatalf("GetAllEmails failed after delivery: %v", err)
	}

	if len(emails) != initialCount+1 {
		t.Fatalf("expected email count to increase from %d to %d, got %d", initialCount, initialCount+1, len(emails))
	}

	// Find newly delivered email
	var delivered *jmap.Email
	for _, em := range emails {
		if em.Subject == "SMTP Test Delivery" {
			delivered = em
			break
		}
	}

	if delivered == nil {
		t.Fatalf("delivered email with subject 'SMTP Test Delivery' not found")
	}

	if delivered.BlobID == "" {
		t.Errorf("expected valid BlobID on delivered email")
	}

	// Verify Blob backend has stored the message payload
	blob, found, err := memBlobBackend.GetBlob(context.Background(), "primary", string(delivered.BlobID))
	if err != nil || !found {
		t.Errorf("expected blob %s to exist in BlobBackend, found=%v, err=%v", delivered.BlobID, found, err)
	} else if strings.TrimRight(string(blob.Data), "\r\n") != strings.TrimRight(string(msg), "\r\n") {
		t.Errorf("blob content mismatch. Expected %q, got %q", string(msg), string(blob.Data))
	}

	// Verify Mailbox stats updated
	mailboxes, _, err := memBackend.GetMailboxes(context.Background(), []jmap.Id{"mb-inbox"})
	if err != nil || len(mailboxes) == 0 {
		t.Fatalf("failed to retrieve Inbox mailbox")
	}
	inbox := mailboxes[0]
	if inbox.UnreadEmails == 0 {
		t.Errorf("expected Inbox unread count > 0 after delivery")
	}
}
