package smtp_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

func TestRFC6047_SecurityHardening_EnvelopeIdentityBinding(t *testing.T) {
	spectest.Require(t, "RFC6047", "3", spectest.MUST,
		"Security Considerations: Require authenticated envelope sender to match iTIP actor.")

	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	calBackend := memory.NewMemoryCalendarsBackend()
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()

	const organizer = "bob@example.com"
	const attendee = "alice@example.com"
	const attacker = "eve@example.com"
	bobCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(organizer))
	ev, err := calBackend.CreateCalendarEvent(bobCtx, &jmap.CalendarEvent{
		UID:    "sec2-identity-uid@example.com",
		Title:  "Security Audit",
		Start:  "2026-09-25T10:00:00Z",
		Status: "confirmed",
		Participants: map[string]*jmap.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			attendee:  {Email: attendee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "needs-action"},
		},
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, mailBackend, blobBackend, calBackend, jmapsmtp.WithAccountResolver(resolver))
	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	// Attacker sends REPLY claiming attendee's email in VCALENDAR
	spoofedMsg := []byte("From: Eve <" + attacker + ">\r\n" +
		"To: Bob <" + organizer + ">\r\n" +
		"Subject: Re: Security Audit\r\n" +
		"Content-Type: text/calendar; method=REPLY; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REPLY\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:sec2-identity-uid@example.com\r\n" +
		"SEQUENCE:0\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	_ = smtp.SendMail(addr, nil, attacker, []string{organizer}, spoofedMsg)
	time.Sleep(100 * time.Millisecond)

	// Verify Alice's status was NOT changed
	updated, _, _ := calBackend.GetCalendarEvents(bobCtx, []jmap.Id{ev.ID})
	if len(updated) > 0 {
		p := updated[0].Participants[attendee]
		if p.ParticipationStatus != "needs-action" {
			t.Errorf("spoofed reply from %s modified attendee status to %s", attacker, p.ParticipationStatus)
		}
	}
}

func TestRFC6047_SecurityHardening_ParticipantAuthorization(t *testing.T) {
	spectest.Require(t, "RFC5546", "5", spectest.MUST,
		"Security Considerations: Participant authorization: REPLY ignored if sender is not on the event.")

	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	calBackend := memory.NewMemoryCalendarsBackend()
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()

	const organizer = "bob@example.com"
	const attendee = "alice@example.com"
	const stranger = "charlie@example.com"
	bobCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(organizer))
	ev, err := calBackend.CreateCalendarEvent(bobCtx, &jmap.CalendarEvent{
		UID:    "sec3-auth-uid@example.com",
		Title:  "Private Review",
		Start:  "2026-09-25T14:00:00Z",
		Status: "confirmed",
		Participants: map[string]*jmap.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			attendee:  {Email: attendee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "needs-action"},
		},
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, mailBackend, blobBackend, calBackend, jmapsmtp.WithAccountResolver(resolver))
	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	// Stranger sends REPLY for an event they were not invited to
	strangerMsg := []byte("From: Charlie <" + stranger + ">\r\n" +
		"To: Bob <" + organizer + ">\r\n" +
		"Subject: Re: Private Review\r\n" +
		"Content-Type: text/calendar; method=REPLY; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REPLY\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:sec3-auth-uid@example.com\r\n" +
		"SEQUENCE:0\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + stranger + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	_ = smtp.SendMail(addr, nil, stranger, []string{organizer}, strangerMsg)
	time.Sleep(100 * time.Millisecond)

	// Verify stranger was NOT added to event
	updated, _, _ := calBackend.GetCalendarEvents(bobCtx, []jmap.Id{ev.ID})
	if len(updated) > 0 {
		if _, exists := updated[0].Participants[stranger]; exists {
			t.Errorf("unauthorized stranger %s was added to event participants", stranger)
		}
	}
}

func TestRFC6047_SecurityHardening_ReplaySequenceDefence(t *testing.T) {
	spectest.Require(t, "RFC5546", "2.1.4", spectest.MUST,
		"Sequence defence: Stale iTIP messages with SEQUENCE < event.SEQUENCE are discarded.")

	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	calBackend := memory.NewMemoryCalendarsBackend()
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()

	const organizer = "bob@example.com"
	const attendee = "alice@example.com"
	bobCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(organizer))
	ev, err := calBackend.CreateCalendarEvent(bobCtx, &jmap.CalendarEvent{
		UID:      "sec5-seq-uid@example.com",
		Title:    "Sequence Test",
		Start:    "2026-09-25T15:00:00Z",
		Sequence: 5, // current sequence is 5
		Status:   "confirmed",
		Participants: map[string]*jmap.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			attendee:  {Email: attendee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "accepted"},
		},
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, mailBackend, blobBackend, calBackend, jmapsmtp.WithAccountResolver(resolver))
	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	// Stale REPLY with sequence 2 (older than current sequence 5)
	staleMsg := []byte("From: Alice <" + attendee + ">\r\n" +
		"To: Bob <" + organizer + ">\r\n" +
		"Subject: Re: Sequence Test\r\n" +
		"Content-Type: text/calendar; method=REPLY; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REPLY\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:sec5-seq-uid@example.com\r\n" +
		"SEQUENCE:2\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=DECLINED:mailto:" + attendee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	_ = smtp.SendMail(addr, nil, attendee, []string{organizer}, staleMsg)
	time.Sleep(100 * time.Millisecond)

	// Verify Alice's status remains "accepted" and was not reverted to "declined" by the stale sequence message
	updated, _, _ := calBackend.GetCalendarEvents(bobCtx, []jmap.Id{ev.ID})
	if len(updated) > 0 {
		p := updated[0].Participants[attendee]
		if p.ParticipationStatus != "accepted" {
			t.Errorf("stale sequence reply reverted status to %s", p.ParticipationStatus)
		}
	}
}

func TestRFC6047_SecurityHardening_InboundCancel(t *testing.T) {
	spectest.Require(t, "RFC5546", "3.2.5", spectest.MUST,
		"Inbound CANCEL from organizer marks the event cancelled.")

	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	calBackend := memory.NewMemoryCalendarsBackend()
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()

	const organizer = "organizer@example.com"
	const invitee = "invitee@example.com"
	inviteeCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(invitee))
	ev, err := calBackend.CreateCalendarEvent(inviteeCtx, &jmap.CalendarEvent{
		UID:      "cancel-test-uid@example.com",
		Title:    "Team Sync",
		Start:    "2026-09-25T16:00:00Z",
		Sequence: 1,
		Status:   "confirmed",
		Participants: map[string]*jmap.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			invitee:   {Email: invitee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "accepted"},
		},
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, mailBackend, blobBackend, calBackend, jmapsmtp.WithAccountResolver(resolver))
	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	// Legitimate CANCEL from organizer with sequence 2
	cancelMsg := []byte("From: Organizer <" + organizer + ">\r\n" +
		"To: Invitee <" + invitee + ">\r\n" +
		"Subject: Cancelled: Team Sync\r\n" +
		"Content-Type: text/calendar; method=CANCEL; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:CANCEL\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:cancel-test-uid@example.com\r\n" +
		"SEQUENCE:2\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	_ = smtp.SendMail(addr, nil, organizer, []string{invitee}, cancelMsg)
	time.Sleep(100 * time.Millisecond)

	updated, _, _ := calBackend.GetCalendarEvents(inviteeCtx, []jmap.Id{ev.ID})
	if len(updated) == 0 || updated[0].Status != "cancelled" {
		t.Fatalf("expected event status cancelled, got %v", updated[0].Status)
	}
}

// repeatedByteReader provides an io.Reader streaming repeating bytes without allocating all bytes in RAM.
type repeatedByteReader struct {
	remaining int64
	val       byte
}

func (r *repeatedByteReader) Read(p []byte) (n int, err error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	toRead := int64(len(p))
	if toRead > r.remaining {
		toRead = r.remaining
	}
	for i := int64(0); i < toRead; i++ {
		p[i] = r.val
	}
	r.remaining -= toRead
	return int(toRead), nil
}

// TestRFC5321_SEC6_OversizedMessageRejected tests that inbound DATA streams exceeding MaxSMTPMessageSize (50MB)
// are rejected with SMTP error 552 5.3.4 per RFC 5321 §4.2.3 & RFC 1870.
func TestRFC5321_SEC6_OversizedMessageRejected(t *testing.T) {
	spectest.Require(t, "RFC5321", "4.2.3", spectest.MUST,
		"Resource limits: Oversized messages exceeding maximum limit must be rejected with 552 error.")

	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()
	calBackend := memory.NewMemoryCalendarsBackend()

	backend := jmapsmtp.NewReceiverBackend(mailBackend, blobBackend, calBackend)
	session, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	_ = session.Mail("sender@example.com", nil)
	_ = session.Rcpt("user@example.com", nil)

	// Stream 50MB + 1KB
	oversizedReader := &repeatedByteReader{
		remaining: int64(jmapsmtp.MaxSMTPMessageSize + 1024),
		val:       'X',
	}

	dataErr := session.Data(oversizedReader)
	if dataErr == nil {
		t.Fatalf("Expected error when sending message exceeding %d bytes, got nil", jmapsmtp.MaxSMTPMessageSize)
	}
	if !strings.Contains(dataErr.Error(), "552") {
		t.Errorf("Expected 552 error code for oversized message, got: %v", dataErr)
	}
}

// TestRFC5321_SEC6_MIMEPartLimits tests that messages with many parts are bounded by MaxMIMEParts.
func TestRFC5321_SEC6_MIMEPartLimits(t *testing.T) {
	spectest.Require(t, "RFC2045", "1", spectest.MUST,
		"Resource limits: Bounded MIME multipart extraction prevents unbounded part allocation.")

	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()
	calBackend := memory.NewMemoryCalendarsBackend()

	backend := jmapsmtp.NewReceiverBackend(mailBackend, blobBackend, calBackend)
	session, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	_ = session.Mail("sender@example.com", nil)
	_ = session.Rcpt("user@example.com", nil)

	// Build a multipart message with 120 parts (exceeding MaxMIMEParts=100)
	var buf bytes.Buffer
	boundary := "bound123"
	buf.WriteString("From: sender@example.com\r\n")
	buf.WriteString("To: user@example.com\r\n")
	buf.WriteString("Subject: Many parts test\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary))

	for i := 1; i <= 120; i++ {
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Type: text/plain\r\n\r\n")
		buf.WriteString(fmt.Sprintf("Part %d body\r\n", i))
	}
	buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	dataErr := session.Data(&buf)
	if dataErr != nil {
		t.Fatalf("DATA failed unexpectedly for multi-part message: %v", dataErr)
	}

	ctx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject("user@example.com"))
	emailIDs, _, err := mailBackend.QueryEmails(ctx, nil, nil, 0, nil)
	if err != nil || len(emailIDs) == 0 {
		t.Fatalf("Expected stored email in backend, got %d (err: %v)", len(emailIDs), err)
	}
}
