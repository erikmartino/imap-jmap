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
	"imap-jmap/jmap/spectest"
	jmapsmtp "imap-jmap/smtp"
)

// TestLocalDelivery_UserToUserOverSMTP is an end-to-end check of local mail delivery to the
// primary domain by connecting directly to the SMTP port: one user sends to another, the
// message arrives in the recipient's account (and not the sender's), and the delivery is
// visible in the message headers via the RFC 5321 Section 4.4 "Received:" trace header the
// receiver prepends.
//
// Local loopback delivery to the primary domain is a product feature (not defined by any RFC);
// the Received trace header it leaves IS an RFC 5321 Section 4.4 requirement.
func TestLocalDelivery_UserToUserOverSMTP(t *testing.T) {
	spectest.Require(t, "RFC5321", "4.4", spectest.MUST,
		"A receiving SMTP server MUST prepend a Received: trace header to the message content.")

	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, memBackend, memBlobBackend, nil, jmapsmtp.WithAccountResolver(resolver))
	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	// Prove there is a real SMTP server listening on a TCP socket: dial it raw and read
	// the 220 greeting. This is a genuine over-the-wire transfer, not an in-process
	// MailBackend.CreateEmail call.
	probe, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial SMTP socket %s: %v", addr, err)
	}
	greeting := make([]byte, 256)
	_ = probe.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := probe.Read(greeting)
	if err != nil || !strings.HasPrefix(string(greeting[:n]), "220") {
		t.Fatalf("expected 220 SMTP greeting over the socket, got %q (err=%v)", string(greeting[:n]), err)
	}
	_ = probe.Close()

	const sender = "alice@example.com"
	const recipient = "bob@example.com"
	subject := "Direct SMTP local delivery"
	msg := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: " + subject + "\r\n" +
		"Message-ID: <local-delivery-1@example.com>\r\n" +
		"\r\n" +
		"Hello Bob, this went straight through the SMTP port.\r\n")

	// Connect directly to the SMTP port and deliver.
	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msg); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// It arrives in the recipient's account.
	bobID := jmap.AccountIDForSubject(recipient)
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)
	var delivered *jmap.Email
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		emails, _ := memBackend.GetAllEmails(bobCtx)
		for _, em := range emails {
			if em.Subject == subject {
				delivered = em
				break
			}
		}
		if delivered != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if delivered == nil {
		t.Fatalf("recipient %s did not receive the message", recipient)
	}
	if !delivered.MailboxIDs["mb-inbox"] {
		t.Errorf("expected the delivered message in the recipient's Inbox")
	}

	// It is NOT delivered to the sender's account (the sender is not a local recipient).
	aliceCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(sender))
	aliceEmails, _ := memBackend.GetAllEmails(aliceCtx)
	for _, em := range aliceEmails {
		if em.Subject == subject {
			t.Errorf("message must not be delivered to the sender's account")
		}
	}

	// Delivery via SMTP is visible in a mail header: the Received: trace header.
	blob, found, err := memBlobBackend.GetBlob(bobCtx, bobID, string(delivered.BlobID))
	if err != nil || !found {
		t.Fatalf("recipient blob %s not found: err=%v", delivered.BlobID, err)
	}
	stored := string(blob.Data)
	received, _, ok := strings.Cut(stored, "\r\n\r\n") // header block
	if !ok {
		received = stored
	}
	for _, want := range []string{"Received: from", "by localhost", "with ESMTP", "for <" + recipient + ">"} {
		if !strings.Contains(received, want) {
			t.Errorf("Received header missing %q; headers were:\n%s", want, received)
		}
	}
	// The Received header records the client's TCP remote address (loopback here),
	// which only exists because the message arrived over an accepted socket — proof
	// this is a real SMTP transfer, not an in-process memory-to-memory delivery.
	if !strings.Contains(received, "127.0.0.1") {
		t.Errorf("Received header must record the client's socket address (127.0.0.1); got:\n%s", received)
	}
}
