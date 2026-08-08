package smtp_test

import (
	"context"
	"net"
	"net/smtp"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/spectest"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC6047_InboundReplyUpdatesParticipationStatus verifies that an inbound iMIP REPLY
// (RFC 6047) updates the replying attendee's participationStatus on the event correlated by
// UID (RFC 5546 Section 2.1.5), leaving the event-level status untouched, and records a
// CalendarEventNotification for the externally-made change (draft-ietf-jmap-calendars-27
// Section 7).
func TestRFC6047_InboundReplyUpdatesParticipationStatus(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.4", spectest.MUST,
		"A received text/calendar body part with method=REPLY updates the attendee's status.")
	spectest.Require(t, "RFC5546", "3.2.3", spectest.MUST,
		"A REPLY updates the replying attendee's PARTSTAT (participationStatus), not the event status.")

	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	calBackend := memory.NewMemoryCalendarsBackend()
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()

	// Organizer bob owns the event in his account; attendee alice will reply.
	const organizer = "bob@example.com"
	const attendee = "alice@example.com"
	bobCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(organizer))
	ev, err := calBackend.CreateCalendarEvent(bobCtx, &jmap.CalendarEvent{
		UID:    "inbound-reply-uid@example.com",
		Title:  "Roadmap",
		Start:  "2026-09-20T10:00:00Z",
		Status: "confirmed",
		Participants: map[string]*jmap.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			attendee:  {Email: attendee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "needs-action"},
		},
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	notifBefore, _ := calBackend.GetAllCalendarEventNotifications(bobCtx)

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

	msg := []byte("From: Alice <" + attendee + ">\r\n" +
		"To: Bob <" + organizer + ">\r\n" +
		"Subject: Re: Roadmap\r\n" +
		"Content-Type: text/calendar; method=REPLY; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REPLY\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:inbound-reply-uid@example.com\r\n" +
		"SEQUENCE:0\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	if err := smtp.SendMail(addr, nil, attendee, []string{organizer}, msg); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	updated, _, err := calBackend.GetCalendarEvents(bobCtx, []jmap.Id{ev.ID})
	if err != nil || len(updated) == 0 {
		t.Fatalf("re-fetch event: err=%v len=%d", err, len(updated))
	}
	got := updated[0]
	p := got.Participants[attendee]
	if p == nil || p.ParticipationStatus != "accepted" {
		t.Errorf("attendee participationStatus = %v, want accepted", p)
	}
	if got.Status != "confirmed" {
		t.Errorf("event-level status must be untouched by a REPLY, got %q", got.Status)
	}

	notifAfter, _ := calBackend.GetAllCalendarEventNotifications(bobCtx)
	if len(notifAfter) <= len(notifBefore) {
		t.Errorf("expected a CalendarEventNotification for the inbound REPLY (before=%d after=%d)", len(notifBefore), len(notifAfter))
	}
}
