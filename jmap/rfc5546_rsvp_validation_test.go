package jmap_test

import (
	"context"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/nextcloud"
	"imap-jmap/jmap/spectest"
)

// TestRFC5546_RSVPResponsesNeverInterpretedAsInvitations verifies that RSVP responses
// (RFC 5546 Section 3.2.3 METHOD:REPLY) are strictly applied as participant status updates
// and are NEVER interpreted as new invitations or tentative events (METHOD:REQUEST),
// across standard, variant, and edge-case iTIP formats.
func TestRFC5546_RSVPResponsesNeverInterpretedAsInvitations(t *testing.T) {
	spectest.Require(t, "RFC5546", "3.2.3", spectest.MUST,
		"A REPLY carries the ORGANIZER being answered and the replying ATTENDEE with its PARTSTAT.")

	const organizer = "organizer@example.com"
	const attendee = "attendee@example.com"
	orgCtx := jmapauth.ContextWithAccountID(context.Background(), jmapauth.AccountIDForSubject(organizer))

	_, calBackend, _, _, _, cleanup := nextcloud.NewEmbeddedBackend(organizer, attendee)
	defer cleanup()

	// 1. Seed existing event on organizer's calendar
	ev, err := calBackend.CreateCalendarEvent(orgCtx, &jmapcalendar.CalendarEvent{
		UID:    "rsvp-val-uid@example.com",
		Title:  "Sprint Planning",
		Start:  "2026-10-10T10:00:00Z",
		Status: "confirmed",
		Participants: map[string]*jmapcalendar.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			attendee:  {Email: attendee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "needs-action"},
		},
	})
	if err != nil {
		t.Fatalf("CreateCalendarEvent failed: %v", err)
	}

	initialEvents, err := calBackend.GetAllCalendarEvents(orgCtx)
	if err != nil || len(initialEvents) != 1 {
		t.Fatalf("expected 1 initial event, got %d (err: %v)", len(initialEvents), err)
	}

	// -------------------------------------------------------------------------
	// Case A: Standard RSVP with METHOD:REPLY on VCALENDAR
	// -------------------------------------------------------------------------
	replyA := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"METHOD:REPLY",
		"BEGIN:VEVENT",
		"UID:rsvp-val-uid@example.com",
		"SEQUENCE:0",
		"ORGANIZER:mailto:" + organizer,
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee,
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	applied := jmap.ApplyITIP(orgCtx, calBackend, replyA, attendee)
	if !applied {
		t.Errorf("Case A: expected ApplyITIP to succeed")
	}

	eventsA, _ := calBackend.GetAllCalendarEvents(orgCtx)
	if len(eventsA) != 1 {
		t.Errorf("Case A: RSVP must NOT create an invitation; event count should be 1, got %d", len(eventsA))
	}
	updatedA, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{ev.ID})
	if len(updatedA) == 0 || updatedA[0].Participants[attendee] == nil || updatedA[0].Participants[attendee].ParticipationStatus != "accepted" {
		t.Errorf("Case A: participant status should be accepted, got %+v", updatedA)
	}
	if updatedA[0].Status != "confirmed" {
		t.Errorf("Case A: event status must remain confirmed, got %s", updatedA[0].Status)
	}

	// -------------------------------------------------------------------------
	// Case B: RSVP for a non-existent / unknown UID must NOT create an event
	// -------------------------------------------------------------------------
	replyB := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"METHOD:REPLY",
		"BEGIN:VEVENT",
		"UID:non-existent-uid@example.com",
		"SEQUENCE:0",
		"ORGANIZER:mailto:" + organizer,
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee,
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	appliedB := jmap.ApplyITIP(orgCtx, calBackend, replyB, attendee)
	if appliedB {
		t.Errorf("Case B: ApplyITIP for unknown UID must return false, got true")
	}
	eventsB, _ := calBackend.GetAllCalendarEvents(orgCtx)
	if len(eventsB) != 1 {
		t.Errorf("Case B: RSVP for unknown UID must NOT create an invitation; expected 1 event, got %d", len(eventsB))
	}

	// -------------------------------------------------------------------------
	// Case C: RSVP with method=REPLY declared only in MIME header (omitted from ICS)
	// -------------------------------------------------------------------------
	rawMIME := []byte(strings.Join([]string{
		"From: Attendee <" + attendee + ">",
		"To: Organizer <" + organizer + ">",
		"Subject: Re: Sprint Planning",
		"Content-Type: text/calendar; method=REPLY; charset=utf-8",
		"",
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"BEGIN:VEVENT",
		"UID:rsvp-val-uid@example.com",
		"SEQUENCE:0",
		"ORGANIZER:mailto:" + organizer,
		"ATTENDEE;PARTSTAT=DECLINED:mailto:" + attendee,
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n"))

	bodyC, methodC := jmap.ExtractCalendarPart(rawMIME)
	if !strings.EqualFold(methodC, "REPLY") {
		t.Errorf("Case C: expected MIME method REPLY, got %q", methodC)
	}
	parsedC, err := jmap.ParseITIPMessage(bodyC, methodC)
	if err != nil {
		t.Fatalf("Case C: ParseITIPMessage failed: %v", err)
	}
	if parsedC.Method != "REPLY" {
		t.Errorf("Case C: expected parsed Method REPLY, got %q", parsedC.Method)
	}

	appliedC := jmap.ApplyITIP(orgCtx, calBackend, bodyC, attendee, methodC)
	if !appliedC {
		t.Errorf("Case C: expected ApplyITIP to succeed")
	}
	eventsC, _ := calBackend.GetAllCalendarEvents(orgCtx)
	if len(eventsC) != 1 {
		t.Errorf("Case C: RSVP must NOT create an invitation; expected 1 event, got %d", len(eventsC))
	}
	updatedC, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{ev.ID})
	if len(updatedC) == 0 || updatedC[0].Participants[attendee] == nil || updatedC[0].Participants[attendee].ParticipationStatus != "declined" {
		t.Errorf("Case C: participant status should be declined, got %+v", updatedC)
	}

	// -------------------------------------------------------------------------
	// Case D: RSVP with METHOD:REPLY placed inside VEVENT instead of VCALENDAR
	// -------------------------------------------------------------------------
	replyD := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"BEGIN:VEVENT",
		"METHOD:REPLY",
		"UID:rsvp-val-uid@example.com",
		"SEQUENCE:0",
		"ORGANIZER:mailto:" + organizer,
		"ATTENDEE;PARTSTAT=TENTATIVE:mailto:" + attendee,
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	parsedD, err := jmap.ParseITIPMessage(replyD)
	if err != nil {
		t.Fatalf("Case D: ParseITIPMessage failed: %v", err)
	}
	if parsedD.Method != "REPLY" {
		t.Errorf("Case D: expected Method REPLY from VEVENT, got %q", parsedD.Method)
	}

	appliedD := jmap.ApplyITIP(orgCtx, calBackend, replyD, attendee)
	if !appliedD {
		t.Errorf("Case D: expected ApplyITIP to succeed")
	}
	eventsD, _ := calBackend.GetAllCalendarEvents(orgCtx)
	if len(eventsD) != 1 {
		t.Errorf("Case D: RSVP must NOT create an invitation; expected 1 event, got %d", len(eventsD))
	}
	updatedD, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{ev.ID})
	if len(updatedD) == 0 || updatedD[0].Participants[attendee] == nil || updatedD[0].Participants[attendee].ParticipationStatus != "tentative" {
		t.Errorf("Case D: participant status should be tentative, got %+v", updatedD)
	}

	// -------------------------------------------------------------------------
	// Case E: RSVP with NO METHOD property anywhere, but PARTSTAT=ACCEPTED
	// -------------------------------------------------------------------------
	replyE := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"BEGIN:VEVENT",
		"UID:rsvp-val-uid@example.com",
		"SEQUENCE:0",
		"ORGANIZER:mailto:" + organizer,
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee,
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	parsedE, err := jmap.ParseITIPMessage(replyE)
	if err != nil {
		t.Fatalf("Case E: ParseITIPMessage failed: %v", err)
	}
	if parsedE.Method != "REPLY" {
		t.Errorf("Case E: expected inferred Method REPLY, got %q", parsedE.Method)
	}

	appliedE := jmap.ApplyITIP(orgCtx, calBackend, replyE, attendee)
	if !appliedE {
		t.Errorf("Case E: expected ApplyITIP to succeed")
	}
	eventsE, _ := calBackend.GetAllCalendarEvents(orgCtx)
	if len(eventsE) != 1 {
		t.Errorf("Case E: RSVP must NOT create an invitation; expected 1 event, got %d", len(eventsE))
	}

	// -------------------------------------------------------------------------
	// Case F: Client mistakenly sent METHOD:REQUEST, but sender is attendee with PARTSTAT=ACCEPTED
	// -------------------------------------------------------------------------
	replyF := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"METHOD:REQUEST",
		"BEGIN:VEVENT",
		"UID:rsvp-val-uid@example.com",
		"SEQUENCE:0",
		"SUMMARY:Overwritten Title",
		"ORGANIZER:mailto:" + organizer,
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee,
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	appliedF := jmap.ApplyITIP(orgCtx, calBackend, replyF, attendee)
	if !appliedF {
		t.Errorf("Case F: expected ApplyITIP to apply as an RSVP reply")
	}
	updatedF, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{ev.ID})
	if len(updatedF) == 0 || updatedF[0].Title != "Sprint Planning" {
		t.Errorf("Case F: RSVP must NOT overwrite event title as an invitation would, got %q", updatedF[0].Title)
	}
	if updatedF[0].Participants[attendee].ParticipationStatus != "accepted" {
		t.Errorf("Case F: attendee status should be accepted, got %s", updatedF[0].Participants[attendee].ParticipationStatus)
	}

	// -------------------------------------------------------------------------
	// Case G: Client mistakenly sent METHOD:REQUEST, but event does NOT exist and sender is attendee
	// -------------------------------------------------------------------------
	replyG := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"METHOD:REQUEST",
		"BEGIN:VEVENT",
		"UID:brand-new-fake-uid@example.com",
		"SEQUENCE:0",
		"SUMMARY:Phantom Event",
		"ORGANIZER:mailto:" + organizer,
		"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee,
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n")

	appliedG := jmap.ApplyITIP(orgCtx, calBackend, replyG, attendee)
	if appliedG {
		t.Errorf("Case G: RSVP response for non-existent event must NOT be applied, got true")
	}
	eventsG, _ := calBackend.GetAllCalendarEvents(orgCtx)
	if len(eventsG) != 1 {
		t.Errorf("Case G: RSVP response must NEVER create a new calendar event; expected 1 event, got %d", len(eventsG))
	}
}
