package smtp_test

import (
	"context"
	"net"
	"net/smtp"
	"testing"
	"time"

	"imap-jmap/jmap/memory"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC5321_SMTPProtocolDelivery tests RFC 5321 SMTP delivery sequence (EHLO, MAIL FROM, RCPT TO, DATA, QUIT).
func TestRFC5321_SMTPProtocolDelivery(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	srv := jmapsmtp.NewServer(addr, memBackend, memBlobBackend, nil)
	go func() {
		_ = srv.ListenAndServe()
	}()
	defer srv.Close()

	time.Sleep(50 * time.Millisecond)

	// Issue RFC 5321 SMTP transaction
	from := "sender@example.com"
	to := []string{"recipient@example.com"}
	msg := []byte("From: <sender@example.com>\r\n" +
		"To: <recipient@example.com>\r\n" +
		"Subject: RFC 5321 Delivery Test\r\n" +
		"\r\n" +
		"RFC 5321 SMTP Delivery Body\r\n")

	err = smtp.SendMail(addr, nil, from, to, msg)
	if err != nil {
		t.Fatalf("smtp.SendMail (RFC 5321 transaction) failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	emails, err := memBackend.GetAllEmails(context.Background())
	if err != nil || len(emails) == 0 {
		t.Fatalf("Expected delivered email in MailBackend per RFC 5321, got 0")
	}
}
