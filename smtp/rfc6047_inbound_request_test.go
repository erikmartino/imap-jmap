package smtp_test

import (
	"context"
	"encoding/base64"
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

// TestRFC6047_InboundRequestFullFidelityMultipart proves the mail-based invite path is
// lossless and MIME-correct: a multipart/mixed message whose text/calendar REQUEST part
// is base64-encoded (RFC 6047 Section 2.4 / RFC 2045) is delivered over SMTP, the real
// MIME parser extracts and decodes the calendar part, and the invitee's calendar receives
// the full event — recurrence, duration, location, and all participants (RFC 5545 → 8984).
func TestRFC6047_InboundRequestFullFidelityMultipart(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.4", spectest.MUST,
		"The text/calendar part is located and decoded via MIME (honouring Content-Transfer-Encoding), not raw scanning.")
	spectest.Require(t, "RFC5546", "3.2.2", spectest.MUST,
		"An inbound REQUEST imports the full event (recurrence, duration, location, participants).")

	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	calBackend := memory.NewMemoryCalendarsBackend()
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()

	const organizer = "organizer@ext.test" // external organizer
	const invitee = "invitee@example.com"  // local invitee
	inviteeCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(invitee))

	// The iCalendar REQUEST, CRLF-delimited per RFC 5545 Section 3.1.
	ics := strings.Join([]string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "METHOD:REQUEST", "BEGIN:VEVENT",
		"UID:inbound-req-uid@example.com",
		"SUMMARY:Weekly Standup",
		"DTSTART:20261005T090000Z",
		"DURATION:PT30M",
		"RRULE:FREQ=WEEKLY;COUNT=10",
		"LOCATION:Room 5",
		"ORGANIZER:mailto:" + organizer,
		"ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:" + invitee,
		"ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=OPT-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:other@ext.test",
		"END:VEVENT", "END:VCALENDAR", "",
	}, "\r\n")
	b64 := base64.StdEncoding.EncodeToString([]byte(ics))

	msg := strings.Join([]string{
		"From: Organizer <" + organizer + ">",
		"To: Invitee <" + invitee + ">",
		"Subject: Invitation: Weekly Standup",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=\"BOUND1\"",
		"",
		"--BOUND1",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"You are invited to the weekly standup.",
		"--BOUND1",
		"Content-Type: text/calendar; method=REQUEST; charset=UTF-8",
		"Content-Transfer-Encoding: base64",
		"",
		b64,
		"--BOUND1--",
		"",
	}, "\r\n")

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

	if err := smtp.SendMail(addr, nil, organizer, []string{invitee}, []byte(msg)); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	all, err := calBackend.GetAllCalendarEvents(inviteeCtx)
	if err != nil {
		t.Fatalf("GetAllCalendarEvents: %v", err)
	}
	var ev *jmap.CalendarEvent
	for _, e := range all {
		if e != nil && e.UID == "inbound-req-uid@example.com" {
			ev = e
		}
	}
	if ev == nil {
		t.Fatalf("invitee did not receive the imported invitation; events=%d", len(all))
	}
	if ev.Title != "Weekly Standup" {
		t.Errorf("title = %q", ev.Title)
	}
	if ev.Duration != "PT30M" {
		t.Errorf("duration = %q (MIME/base64 decode or import lost it)", ev.Duration)
	}
	if len(ev.RecurrenceRules) != 1 || ev.RecurrenceRules[0].Frequency != "weekly" || ev.RecurrenceRules[0].Count != 10 {
		t.Errorf("recurrence not imported: %+v", ev.RecurrenceRules)
	}
	locOK := false
	for _, l := range ev.Locations {
		if l.Name == "Room 5" {
			locOK = true
		}
	}
	if !locOK {
		t.Errorf("location not imported: %v", ev.Locations)
	}
	if ev.Participants[invitee] == nil {
		t.Errorf("invitee participant missing: %v", ev.Participants)
	}
	if ev.Participants["other@ext.test"] == nil {
		t.Errorf("second attendee missing: %v", ev.Participants)
	}
}
