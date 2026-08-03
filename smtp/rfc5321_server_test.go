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

// TestRFC5322_ParseMessageToEmail tests parsing raw RFC 5322 messages into JMAP Email objects.
func TestRFC5322_ParseMessageToEmail(t *testing.T) {
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

// TestRFC5321_SMTPServerReceive tests SMTP message ingestion and storage per RFC 5321.
func TestRFC5321_SMTPServerReceive(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()

	// Find free local port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on random port: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	srv := jmapsmtp.NewServer(addr, memBackend, memBlobBackend, nil)
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

// TestRFC6047_SMTPServerReceiveIMIPReply tests SMTP intake of RFC 6047 iMIP reply messages.
func TestRFC6047_SMTPServerReceiveIMIPReply(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	memCalBackend := memory.NewMemoryCalendarsBackend()

	// 1. Create a calendar event with external participant
	ev, err := memCalBackend.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		ID:    "evt-imip-100",
		Title: "Project Review",
		Start: "2026-09-10T14:00:00Z",
		Participants: map[string]*jmap.JSCalendarParticipant{
			"client@example.com": {
				Name:   "Client",
				Email:  "client@example.com",
				Status: "needs-action",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateCalendarEvent failed: %v", err)
	}

	// 2. Start SMTP receiver
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	srv := jmapsmtp.NewServer(addr, memBackend, memBlobBackend, memCalBackend)
	go func() {
		_ = srv.ListenAndServe()
	}()
	defer srv.Close()

	time.Sleep(50 * time.Millisecond)

	// 3. Deliver iMIP METHOD:REPLY email over SMTP from client@example.com
	from := "client@example.com"
	to := []string{"organizer@example.com"}
	imipMsg := []byte("From: <client@example.com>\r\n" +
		"To: <organizer@example.com>\r\n" +
		"Subject: Accepted: Project Review\r\n" +
		"Content-Type: text/calendar; method=REPLY\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REPLY\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:evt-imip-100\r\n" +
		"SUMMARY:Project Review\r\n" +
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:client@example.com\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	err = smtp.SendMail(addr, nil, from, to, imipMsg)
	if err != nil {
		t.Fatalf("smtp.SendMail failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// 4. Verify participant status in memCalBackend was auto-updated to "accepted"
	events, _, err := memCalBackend.GetCalendarEvents(context.Background(), []jmap.Id{ev.ID})
	if err != nil || len(events) == 0 {
		t.Fatalf("GetCalendarEvents failed: %v", err)
	}

	updatedEv := events[0]
	p, ok := updatedEv.Participants["client@example.com"]
	if !ok || p == nil {
		t.Fatalf("Expected participant client@example.com in event")
	}

	if p.Status != "accepted" {
		t.Errorf("Expected participant status 'accepted', got %q", p.Status)
	}
}
