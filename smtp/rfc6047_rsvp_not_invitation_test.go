package smtp_test

import (
	"context"
	"net"
	"net/smtp"
	"testing"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/spectest"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC6047_InboundRSVPNeverCreatesInvitation tests that over SMTP:
// 1. An inbound RSVP response for an unknown event never creates a new calendar event.
// 2. An inbound RSVP response where METHOD is only in the MIME header updates the existing event and does not create an invitation.
// 3. An inbound RSVP response where METHOD:REPLY is in VEVENT updates the existing event and does not create an invitation.
func TestRFC6047_InboundRSVPNeverCreatesInvitation(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.4", spectest.MUST,
		"A received text/calendar body part with method=REPLY updates the attendee's status.")
	spectest.Require(t, "RFC5546", "3.2.3", spectest.MUST,
		"A REPLY updates the replying attendee's PARTSTAT (participationStatus), not the event status.")

	resolver := jmapauth.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	mailBackend, blobBackend, calBackend := newSecurityTestBackends(t, "organizer@example.com", "attendee@example.com")

	const organizer = "organizer@example.com"
	const attendee = "attendee@example.com"
	orgCtx := jmapauth.ContextWithAccountID(context.Background(), jmapauth.AccountIDForSubject(organizer))

	// Seed existing event
	ev, err := calBackend.CreateCalendarEvent(orgCtx, &jmapcalendar.CalendarEvent{
		UID:    "inbound-rsvp-test-uid@example.com",
		Title:  "Design Discussion",
		Start:  "2026-10-15T14:00:00Z",
		Status: "confirmed",
		Participants: map[string]*jmapcalendar.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			attendee:  {Email: attendee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "needs-action"},
		},
	})
	if err != nil {
		t.Fatalf("CreateCalendarEvent failed: %v", err)
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

	// 1. Send RSVP for an unknown event: must NOT create an event in organizer's calendar
	unknownEventRSVP := []byte("From: Attendee <" + attendee + ">\r\n" +
		"To: Organizer <" + organizer + ">\r\n" +
		"Subject: Re: Some Unknown Meeting\r\n" +
		"Content-Type: text/calendar; method=REPLY; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REPLY\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:completely-unknown-uid-12345@example.com\r\n" +
		"SEQUENCE:0\r\n" +
		"SUMMARY:Some Unknown Meeting\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	if err := smtp.SendMail(addr, nil, attendee, []string{organizer}, unknownEventRSVP); err != nil {
		t.Fatalf("SendMail unknownEventRSVP failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	eventsAfterUnknown, err := calBackend.GetAllCalendarEvents(orgCtx)
	if err != nil || len(eventsAfterUnknown) != 1 {
		t.Fatalf("RSVP for unknown UID must NOT create an event; expected 1 event, got %d", len(eventsAfterUnknown))
	}

	// 2. Send RSVP where method=REPLY is declared only in MIME header (omitted from ICS)
	mimeOnlyRSVP := []byte("From: Attendee <" + attendee + ">\r\n" +
		"To: Organizer <" + organizer + ">\r\n" +
		"Subject: Re: Design Discussion\r\n" +
		"Content-Type: text/calendar; method=REPLY; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:inbound-rsvp-test-uid@example.com\r\n" +
		"SEQUENCE:0\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	if err := smtp.SendMail(addr, nil, attendee, []string{organizer}, mimeOnlyRSVP); err != nil {
		t.Fatalf("SendMail mimeOnlyRSVP failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	eventsAfterMIME, err := calBackend.GetAllCalendarEvents(orgCtx)
	if err != nil || len(eventsAfterMIME) != 1 {
		t.Fatalf("MIME method RSVP must NOT create an event; expected 1 event, got %d", len(eventsAfterMIME))
	}

	updated, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{ev.ID})
	if len(updated) == 0 || updated[0].Participants[attendee] == nil || updated[0].Participants[attendee].ParticipationStatus != "accepted" {
		t.Errorf("expected attendee participationStatus accepted, got %+v", updated)
	}
	if updated[0].Status != "confirmed" {
		t.Errorf("event status must remain confirmed, got %s", updated[0].Status)
	}

	// 3. Send RSVP where METHOD:REPLY is inside VEVENT
	veventRSVP := []byte("From: Attendee <" + attendee + ">\r\n" +
		"To: Organizer <" + organizer + ">\r\n" +
		"Subject: Re: Design Discussion\r\n" +
		"Content-Type: text/calendar; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\n" +
		"METHOD:REPLY\r\n" +
		"UID:inbound-rsvp-test-uid@example.com\r\n" +
		"SEQUENCE:0\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=TENTATIVE:mailto:" + attendee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	if err := smtp.SendMail(addr, nil, attendee, []string{organizer}, veventRSVP); err != nil {
		t.Fatalf("SendMail veventRSVP failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	eventsAfterVEVENT, err := calBackend.GetAllCalendarEvents(orgCtx)
	if err != nil || len(eventsAfterVEVENT) != 1 {
		t.Fatalf("VEVENT method RSVP must NOT create an event; expected 1 event, got %d", len(eventsAfterVEVENT))
	}

	updatedVEVENT, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{ev.ID})
	if len(updatedVEVENT) == 0 || updatedVEVENT[0].Participants[attendee] == nil || updatedVEVENT[0].Participants[attendee].ParticipationStatus != "tentative" {
		t.Errorf("expected attendee participationStatus tentative, got %+v", updatedVEVENT)
	}

	// 4. Send message where client mistakenly had METHOD:REQUEST but sender is attendee with PARTSTAT=ACCEPTED
	requestMethodRSVP := []byte("From: Attendee <" + attendee + ">\r\n" +
		"To: Organizer <" + organizer + ">\r\n" +
		"Subject: Re: Design Discussion\r\n" +
		"Content-Type: text/calendar; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REQUEST\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:inbound-rsvp-test-uid@example.com\r\n" +
		"SEQUENCE:0\r\n" +
		"SUMMARY:Overwritten Title Not Allowed\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	if err := smtp.SendMail(addr, nil, attendee, []string{organizer}, requestMethodRSVP); err != nil {
		t.Fatalf("SendMail requestMethodRSVP failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	eventsAfterReq, err := calBackend.GetAllCalendarEvents(orgCtx)
	if err != nil || len(eventsAfterReq) != 1 {
		t.Fatalf("RSVP must NOT create an event; expected 1 event, got %d", len(eventsAfterReq))
	}

	updatedReq, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{ev.ID})
	if len(updatedReq) == 0 || updatedReq[0].Title != "Design Discussion" {
		t.Errorf("RSVP must NOT overwrite title as an invitation would, got %q", updatedReq[0].Title)
	}
	if updatedReq[0].Participants[attendee].ParticipationStatus != "accepted" {
		t.Errorf("expected attendee participationStatus accepted, got %s", updatedReq[0].Participants[attendee].ParticipationStatus)
	}
}
