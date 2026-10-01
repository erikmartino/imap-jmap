package jmap_test

import (
	"context"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/nextcloud"
	"imap-jmap/jmap/spectest"
)

// TestITIP_AllMethods_And_Permissions verifies all RFC 5546 iTIP methods
// (REQUEST, REPLY with all statuses, CANCEL, COUNTER, ADD, REFRESH, DECLINECOUNTER, PUBLISH)
// and strict permission enforcement (identity binding, participant authorization, sequence replay).
func TestITIP_AllMethods_And_Permissions(t *testing.T) {
	spectest.Require(t, "RFC5546", "3.2.1", spectest.MUST,
		"A PUBLISH carries an informational event published by an organizer with no associated attendees requiring reply.")
	spectest.Require(t, "RFC5546", "3.2.2", spectest.MUST,
		"A REQUEST invitation carries the event's UID, SEQUENCE, ORGANIZER, and ATTENDEE lines.")
	spectest.Require(t, "RFC5546", "3.2.3", spectest.MUST,
		"A REPLY carries the ORGANIZER being answered and the replying ATTENDEE with its PARTSTAT.")
	spectest.Require(t, "RFC5546", "3.2.4", spectest.MUST,
		"An ADD method adds components to an existing event and is restricted to the organizer.")
	spectest.Require(t, "RFC5546", "3.2.5", spectest.MUST,
		"A CANCEL carries STATUS:CANCELLED with the event's UID and SEQUENCE.")
	spectest.Require(t, "RFC5546", "3.2.6", spectest.MUST,
		"A REFRESH method requests the latest version of an event from the organizer and is restricted to attendees.")
	spectest.Require(t, "RFC5546", "3.2.7", spectest.MUST,
		"A COUNTER method proposes changes to an event and must not alter event terms before organizer acceptance.")
	spectest.Require(t, "RFC5546", "3.2.8", spectest.MUST,
		"A DECLINECOUNTER method rejects a counter-proposal and is restricted to the organizer.")
	spectest.Require(t, "RFC5546", "5.1", spectest.MUST,
		"Strangers or non-participants are forbidden from replying, updating, or modifying calendar events.")
	spectest.Require(t, "RFC6047", "3", spectest.MUST,
		"iTIP processing enforces identity binding between envelope sender and iCalendar actors.")

	const organizer = "organizer@example.com"
	const attendee = "attendee@example.com"
	const stranger = "stranger@example.com"

	orgCtx := jmapauth.ContextWithAccountID(context.Background(), jmapauth.AccountIDForSubject(organizer))
	attCtx := jmapauth.ContextWithAccountID(context.Background(), jmapauth.AccountIDForSubject(attendee))

	_, calBackend, _, _, _, cleanup := nextcloud.NewEmbeddedBackend(organizer, attendee, stranger)
	defer cleanup()

	// =========================================================================
	// 1. REQUEST Method & Permissions
	// =========================================================================
	t.Run("REQUEST: Legitimate invite imports event to attendee", func(t *testing.T) {
		reqICS := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REQUEST",
			"BEGIN:VEVENT",
			"UID:allmethods-req-1@example.com",
			"SEQUENCE:1",
			"SUMMARY:Quarterly Sync",
			"DTSTART:20261101T100000Z",
			"DURATION:PT1H",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(attCtx, calBackend, reqICS, organizer)
		if !applied {
			t.Fatalf("expected ApplyITIP for legitimate REQUEST to succeed")
		}

		all, err := calBackend.GetAllCalendarEvents(attCtx)
		if err != nil || len(all) == 0 {
			t.Fatalf("expected imported event on attendee calendar, got %d (err: %v)", len(all), err)
		}
		var found *jmapcalendar.CalendarEvent
		for _, e := range all {
			if e.UID == "allmethods-req-1@example.com" {
				found = e
				break
			}
		}
		if found == nil || found.Title != "Quarterly Sync" {
			t.Fatalf("imported event not found or title mismatch: %+v", found)
		}
	})

	t.Run("REQUEST: Permission Denied when sender is not organizer", func(t *testing.T) {
		spoofedReq := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REQUEST",
			"BEGIN:VEVENT",
			"UID:spoofed-req@example.com",
			"SEQUENCE:1",
			"SUMMARY:Spoofed Event",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		// Stranger attempts to send REQUEST claiming organizer identity
		applied := jmap.ApplyITIP(attCtx, calBackend, spoofedReq, stranger)
		if applied {
			t.Errorf("ApplyITIP must reject REQUEST where envelope sender != organizer")
		}
		all, _ := calBackend.GetAllCalendarEvents(attCtx)
		for _, e := range all {
			if e.UID == "spoofed-req@example.com" {
				t.Fatalf("spoofed REQUEST must NOT create calendar event")
			}
		}
	})

	t.Run("REQUEST: Update with higher sequence updates title and start", func(t *testing.T) {
		updateReq := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REQUEST",
			"BEGIN:VEVENT",
			"UID:allmethods-req-1@example.com",
			"SEQUENCE:2",
			"SUMMARY:Quarterly Sync Rescheduled",
			"DTSTART:20261101T140000Z",
			"DURATION:PT1H",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(attCtx, calBackend, updateReq, organizer)
		if !applied {
			t.Fatalf("expected ApplyITIP for rescheduled REQUEST to succeed")
		}

		all, _ := calBackend.GetAllCalendarEvents(attCtx)
		var found *jmapcalendar.CalendarEvent
		for _, e := range all {
			if e.UID == "allmethods-req-1@example.com" {
				found = e
				break
			}
		}
		if found == nil || found.Title != "Quarterly Sync Rescheduled" {
			t.Errorf("expected updated title 'Quarterly Sync Rescheduled', got %q", found.Title)
		}
		if found.Sequence != 2 {
			t.Errorf("expected sequence 2, got %d", found.Sequence)
		}
	})

	t.Run("REQUEST: Stale sequence is discarded", func(t *testing.T) {
		staleReq := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REQUEST",
			"BEGIN:VEVENT",
			"UID:allmethods-req-1@example.com",
			"SEQUENCE:1", // older than current sequence 2
			"SUMMARY:Stale Revert Attempt",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(attCtx, calBackend, staleReq, organizer)
		if applied {
			t.Errorf("ApplyITIP must reject stale REQUEST with sequence < current sequence")
		}
	})

	// Seed event on organizer's calendar for reply and cancellation tests
	orgEvent, err := calBackend.CreateCalendarEvent(orgCtx, &jmapcalendar.CalendarEvent{
		UID:      "allmethods-event@example.com",
		Title:    "Product Roadmap",
		Start:    "2026-11-05T09:00:00Z",
		Sequence: 3,
		Status:   "confirmed",
		Participants: map[string]*jmapcalendar.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			attendee:  {Email: attendee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "needs-action"},
		},
	})
	if err != nil {
		t.Fatalf("CreateCalendarEvent failed: %v", err)
	}

	_, err = calBackend.CreateCalendarEvent(attCtx, &jmapcalendar.CalendarEvent{
		UID:      "allmethods-event@example.com",
		Title:    "Product Roadmap",
		Start:    "2026-11-05T09:00:00Z",
		Sequence: 3,
		Status:   "confirmed",
		Participants: map[string]*jmapcalendar.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			attendee:  {Email: attendee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "needs-action"},
		},
	})
	if err != nil {
		t.Fatalf("CreateCalendarEvent for attendee failed: %v", err)
	}

	// =========================================================================
	// 2. REPLY Method & Participation Statuses & Permissions
	// =========================================================================
	t.Run("REPLY: ACCEPTED status updates participationStatus and scheduleStatus", func(t *testing.T) {
		replyAccepted := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REPLY",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:3",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(orgCtx, calBackend, replyAccepted, attendee)
		if !applied {
			t.Fatalf("expected ApplyITIP for REPLY ACCEPTED to succeed")
		}

		updated, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{orgEvent.ID})
		if len(updated) == 0 {
			t.Fatalf("event not found")
		}
		p := updated[0].Participants[attendee]
		if p.ParticipationStatus != "accepted" {
			t.Errorf("expected participationStatus accepted, got %s", p.ParticipationStatus)
		}
		if p.ScheduleStatus != "2.0;delivered" {
			t.Errorf("expected scheduleStatus 2.0;delivered, got %s", p.ScheduleStatus)
		}
		if updated[0].Status != "confirmed" {
			t.Errorf("REPLY must NOT touch event-level status, got %s", updated[0].Status)
		}
	})

	t.Run("REPLY: DECLINED status updates participationStatus to declined", func(t *testing.T) {
		replyDeclined := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REPLY",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:3",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;PARTSTAT=DECLINED:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(orgCtx, calBackend, replyDeclined, attendee)
		if !applied {
			t.Fatalf("expected ApplyITIP for REPLY DECLINED to succeed")
		}

		updated, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{orgEvent.ID})
		if updated[0].Participants[attendee].ParticipationStatus != "declined" {
			t.Errorf("expected declined, got %s", updated[0].Participants[attendee].ParticipationStatus)
		}
	})

	t.Run("REPLY: TENTATIVE status updates participationStatus to tentative", func(t *testing.T) {
		replyTentative := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REPLY",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:3",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;PARTSTAT=TENTATIVE:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(orgCtx, calBackend, replyTentative, attendee)
		if !applied {
			t.Fatalf("expected ApplyITIP for REPLY TENTATIVE to succeed")
		}

		updated, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{orgEvent.ID})
		if updated[0].Participants[attendee].ParticipationStatus != "tentative" {
			t.Errorf("expected tentative, got %s", updated[0].Participants[attendee].ParticipationStatus)
		}
	})

	t.Run("REPLY: DELEGATED status updates participationStatus to delegated", func(t *testing.T) {
		replyDelegated := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REPLY",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:3",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;PARTSTAT=DELEGATED:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(orgCtx, calBackend, replyDelegated, attendee)
		if !applied {
			t.Fatalf("expected ApplyITIP for REPLY DELEGATED to succeed")
		}

		updated, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{orgEvent.ID})
		if updated[0].Participants[attendee].ParticipationStatus != "delegated" {
			t.Errorf("expected delegated, got %s", updated[0].Participants[attendee].ParticipationStatus)
		}
	})

	t.Run("REPLY: Stranger not on event is rejected (Participant Authorization)", func(t *testing.T) {
		strangerReply := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REPLY",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:3",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + stranger,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(orgCtx, calBackend, strangerReply, stranger)
		if applied {
			t.Errorf("stranger not on event must NOT be allowed to reply")
		}

		updated, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{orgEvent.ID})
		if _, exists := updated[0].Participants[stranger]; exists {
			t.Errorf("stranger must NOT be added to event participants")
		}
	})

	t.Run("REPLY: Spoofed sender is rejected (Envelope Identity Binding)", func(t *testing.T) {
		spoofedReply := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REPLY",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:3",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		// Stranger delivers email claiming to be attendee
		applied := jmap.ApplyITIP(orgCtx, calBackend, spoofedReply, stranger)
		if applied {
			t.Errorf("spoofed sender must be rejected")
		}
	})

	t.Run("REPLY: Stale sequence is rejected", func(t *testing.T) {
		staleReply := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REPLY",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:1", // current event sequence is 3
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE;PARTSTAT=ACCEPTED:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(orgCtx, calBackend, staleReply, attendee)
		if applied {
			t.Errorf("stale sequence reply must be rejected")
		}
	})

	// =========================================================================
	// 3. CANCEL Method & Permissions
	// =========================================================================
	t.Run("CANCEL: Legitimate cancellation from organizer marks event cancelled", func(t *testing.T) {
		cancelMsg := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:CANCEL",
			"BEGIN:VEVENT",
			"UID:allmethods-req-1@example.com",
			"SEQUENCE:3",
			"ORGANIZER:mailto:" + organizer,
			"STATUS:CANCELLED",
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(attCtx, calBackend, cancelMsg, organizer)
		if !applied {
			t.Fatalf("expected ApplyITIP for legitimate CANCEL to succeed")
		}

		all, _ := calBackend.GetAllCalendarEvents(attCtx)
		var found *jmapcalendar.CalendarEvent
		for _, e := range all {
			if e.UID == "allmethods-req-1@example.com" {
				found = e
				break
			}
		}
		if found == nil || found.Status != "cancelled" {
			t.Fatalf("expected event status cancelled, got %+v", found)
		}
	})

	t.Run("CANCEL: Attendee or stranger cannot cancel organizer's event", func(t *testing.T) {
		unauthorizedCancel := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:CANCEL",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:4",
			"ORGANIZER:mailto:" + organizer,
			"STATUS:CANCELLED",
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		// Attendee tries to cancel the event on organizer's calendar
		applied := jmap.ApplyITIP(orgCtx, calBackend, unauthorizedCancel, attendee)
		if applied {
			t.Errorf("attendee must NOT have permission to CANCEL an event")
		}

		// Stranger tries to cancel
		appliedStranger := jmap.ApplyITIP(orgCtx, calBackend, unauthorizedCancel, stranger)
		if appliedStranger {
			t.Errorf("stranger must NOT have permission to CANCEL an event")
		}

		updated, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{orgEvent.ID})
		if updated[0].Status == "cancelled" {
			t.Errorf("unauthorized cancel modified event status!")
		}
	})

	// =========================================================================
	// 4. COUNTER Method & Permissions
	// =========================================================================
	t.Run("COUNTER: Attendee counter-proposal is recorded via notification without altering event", func(t *testing.T) {
		counterMsg := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:COUNTER",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:3",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE:mailto:" + attendee,
			"DTSTART:20261105T110000Z",
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(orgCtx, calBackend, counterMsg, attendee)
		if !applied {
			t.Fatalf("expected ApplyITIP for valid COUNTER to succeed")
		}

		// Verify event start was NOT directly modified (organizer hasn't accepted yet)
		updated, _, _ := calBackend.GetCalendarEvents(orgCtx, []jmapcore.Id{orgEvent.ID})
		if updated[0].Start != "2026-11-05T09:00:00Z" {
			t.Errorf("COUNTER must NOT overwrite event start before organizer accepts, got %s", updated[0].Start)
		}

		// Verify notification was recorded
		notifs, _ := calBackend.GetAllCalendarEventNotifications(orgCtx)
		foundNotif := false
		for _, n := range notifs {
			if n.CalendarEventID == orgEvent.ID && n.Type == "updated" {
				if n.EventPatch != nil && n.EventPatch["proposedStart"] == "20261105T110000Z" {
					foundNotif = true
					break
				}
			}
		}
		if !foundNotif {
			t.Errorf("expected notification with proposedStart from COUNTER")
		}
	})

	t.Run("COUNTER: Stranger cannot submit counter-proposal", func(t *testing.T) {
		strangerCounter := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:COUNTER",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"SEQUENCE:3",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE:mailto:" + stranger,
			"DTSTART:20261105T120000Z",
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(orgCtx, calBackend, strangerCounter, stranger)
		if applied {
			t.Errorf("stranger must NOT have permission to submit COUNTER")
		}
	})

	// =========================================================================
	// 5. ADD Method & Permissions
	// =========================================================================
	t.Run("ADD: Organizer can add details, stranger cannot", func(t *testing.T) {
		strangerAdd := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:ADD",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"ORGANIZER:mailto:" + organizer,
			"SUMMARY:Hijacked Title",
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		// Stranger sending ADD must fail
		appliedStranger := jmap.ApplyITIP(attCtx, calBackend, strangerAdd, stranger)
		if appliedStranger {
			t.Errorf("stranger must NOT have permission to send ADD")
		}

		// Organizer sending ADD succeeds
		organizerAdd := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:ADD",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"ORGANIZER:mailto:" + organizer,
			"SUMMARY:Product Roadmap v2",
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		appliedOrg := jmap.ApplyITIP(orgCtx, calBackend, organizerAdd, organizer)
		if !appliedOrg {
			t.Errorf("organizer ADD should succeed")
		}
	})

	// =========================================================================
	// 6. REFRESH & DECLINECOUNTER Methods
	// =========================================================================
	t.Run("REFRESH: Attendee can request refresh, stranger cannot", func(t *testing.T) {
		strangerRefresh := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REFRESH",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"ATTENDEE:mailto:" + stranger,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(orgCtx, calBackend, strangerRefresh, stranger)
		if applied {
			t.Errorf("stranger must NOT be permitted to send REFRESH")
		}

		attRefresh := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:REFRESH",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"ATTENDEE:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		appliedAtt := jmap.ApplyITIP(orgCtx, calBackend, attRefresh, attendee)
		if !appliedAtt {
			t.Errorf("attendee REFRESH should succeed")
		}
	})

	t.Run("DECLINECOUNTER: Organizer can decline counter, stranger cannot", func(t *testing.T) {
		strangerDecline := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:DECLINECOUNTER",
			"BEGIN:VEVENT",
			"UID:allmethods-event@example.com",
			"ORGANIZER:mailto:" + organizer,
			"ATTENDEE:mailto:" + attendee,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(attCtx, calBackend, strangerDecline, stranger)
		if applied {
			t.Errorf("stranger must NOT be permitted to send DECLINECOUNTER")
		}

		appliedOrg := jmap.ApplyITIP(attCtx, calBackend, strangerDecline, organizer)
		if !appliedOrg {
			t.Errorf("organizer DECLINECOUNTER should succeed")
		}
	})

	// =========================================================================
	// 7. PUBLISH Method & Permissions
	// =========================================================================
	t.Run("PUBLISH: Legitimate publish imports/updates event", func(t *testing.T) {
		pubICS := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:PUBLISH",
			"BEGIN:VEVENT",
			"UID:published-event-1@example.com",
			"SUMMARY:Public Tech Talk",
			"DTSTART:20261110T180000Z",
			"DURATION:PT1H",
			"ORGANIZER:mailto:" + organizer,
			"STATUS:CONFIRMED",
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(attCtx, calBackend, pubICS, organizer)
		if !applied {
			t.Fatalf("expected ApplyITIP for legitimate PUBLISH to succeed")
		}

		all, _ := calBackend.GetAllCalendarEvents(attCtx)
		var found *jmapcalendar.CalendarEvent
		for _, e := range all {
			if e.UID == "published-event-1@example.com" {
				found = e
				break
			}
		}
		if found == nil || found.Title != "Public Tech Talk" {
			t.Fatalf("expected published event 'Public Tech Talk', got %+v", found)
		}
	})

	t.Run("PUBLISH: Mismatch sender cannot publish on behalf of organizer", func(t *testing.T) {
		spoofedPub := strings.Join([]string{
			"BEGIN:VCALENDAR",
			"VERSION:2.0",
			"METHOD:PUBLISH",
			"BEGIN:VEVENT",
			"UID:spoofed-published@example.com",
			"SUMMARY:Malicious Public Event",
			"ORGANIZER:mailto:" + organizer,
			"END:VEVENT",
			"END:VCALENDAR",
		}, "\r\n")

		applied := jmap.ApplyITIP(attCtx, calBackend, spoofedPub, stranger)
		if applied {
			t.Errorf("spoofed PUBLISH must be rejected when sender != organizer")
		}
		all, _ := calBackend.GetAllCalendarEvents(attCtx)
		for _, e := range all {
			if e.UID == "spoofed-published@example.com" {
				t.Fatalf("spoofed PUBLISH event must NOT be imported")
			}
		}
	})
}

// TestITIP_SharedCalendar_ACL_Permissions tests calendar access control rights per draft-ietf-jmap-calendars Section 1.4:
// mayRSVP grants permission to modify participant participation status on a shared calendar while rejecting
// any attempt to alter title, start time, or other event properties with a standard JMAP 'forbidden' error.
func TestITIP_SharedCalendar_ACL_Permissions(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "1.4", spectest.MUST,
		"Calendar shareWith defines access rights granted to users, and myRights reflects the caller's rights.")

	const ownerUser = "owner@example.com"
	const attendeeUser = "attendee@example.com"
	ownerID := jmapauth.AccountIDForSubject(ownerUser)
	attendeeID := jmapauth.AccountIDForSubject(attendeeUser)

	ts, cleanup := setupSharingTestServer(t, ownerUser, attendeeUser)
	defer cleanup()

	// 1. Owner creates calendar
	resCreate := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"create": map[string]any{
				"cal1": map[string]any{
					"name": "Team Schedule",
				},
			},
		}, "c1"},
	})
	createdCals, ok := resCreate[0].Args["created"].(map[string]any)
	if !ok || createdCals["cal1"] == nil {
		t.Fatalf("Failed to create calendar: %v", resCreate[0].Args)
	}
	calID := createdCals["cal1"].(map[string]any)["id"].(string)

	// 2. Owner creates event on calendar with attendee
	resEv := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": ownerID,
			"create": map[string]any{
				"ev1": map[string]any{
					"@type":       "Event",
					"uid":         "shared-acl-ev-1@example.com",
					"title":       "Sprint Retrospective",
					"start":       "2026-12-01T15:00:00Z",
					"duration":    "PT1H",
					"calendarIds": map[string]bool{calID: true},
					"participants": map[string]any{
						ownerUser: map[string]any{
							"calendarAddress":     "mailto:" + ownerUser,
							"participationStatus": "accepted",
							"roles":               map[string]bool{"owner": true},
						},
						attendeeUser: map[string]any{
							"calendarAddress":     "mailto:" + attendeeUser,
							"participationStatus": "needs-action",
							"roles":               map[string]bool{"attendee": true},
						},
					},
				},
			},
		}, "c-ev"},
	})
	createdEvs, ok := resEv[0].Args["created"].(map[string]any)
	if !ok || createdEvs["ev1"] == nil {
		t.Fatalf("Failed to create event: %v", resEv[0].Args)
	}
	eventID := createdEvs["ev1"].(map[string]any)["id"].(string)

	// 3. Owner shares calendar with attendee granting mayReadItems + mayRSVP (NOT mayWriteAll)
	resShare := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				calID: map[string]any{
					"shareWith": map[string]any{
						attendeeID: map[string]any{
							"mayReadItems": true,
							"mayRSVP":      true,
							"mayWriteAll":  false,
						},
					},
				},
			},
		}, "c-share"},
	})
	if _, ok := resShare[0].Args["updated"].(map[string]any)[calID]; !ok {
		t.Fatalf("Expected calendar updated with shareWith: %v", resShare[0].Args)
	}

	// 4. Attendee patches their participationStatus via JMAP -> MUST succeed
	resRSVP := callJMAPSharing(t, ts.URL, attendeeUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				eventID: map[string]any{
					"participants/" + attendeeUser + "/participationStatus": "accepted",
				},
			},
		}, "c-rsvp"},
	})
	updatedEvs, ok := resRSVP[0].Args["updated"].(map[string]any)
	if !ok {
		t.Fatalf("Expected updated map in response, got: %v", resRSVP[0].Args)
	}
	if _, exists := updatedEvs[eventID]; !exists {
		t.Fatalf("Expected attendee RSVP patch to succeed with mayRSVP, got: %v", resRSVP[0].Args)
	}

	// 5. Attendee attempts to modify non-RSVP property (title) -> MUST fail with 'forbidden'
	resForbidden := callJMAPSharing(t, ts.URL, attendeeUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				eventID: map[string]any{
					"title": "Hacked Title",
				},
			},
		}, "c-forbid"},
	})
	notUpdated, ok := resForbidden[0].Args["notUpdated"].(map[string]any)
	if !ok || notUpdated[eventID] == nil {
		t.Fatalf("Expected title update to fail, got: %v", resForbidden[0].Args)
	}
	errObj := notUpdated[eventID].(map[string]any)
	if errObj["type"] != "forbidden" {
		t.Errorf("Expected SetError type 'forbidden', got %v", errObj["type"])
	}

	// 6. Owner revokes mayRSVP (keeping only mayReadItems)
	resRevoke := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				calID: map[string]any{
					"shareWith": map[string]any{
						attendeeID: map[string]any{
							"mayReadItems": true,
							"mayRSVP":      false,
							"mayWriteAll":  false,
						},
					},
				},
			},
		}, "c-revoke"},
	})
	if _, ok := resRevoke[0].Args["updated"].(map[string]any)[calID]; !ok {
		t.Fatalf("Expected calendar share updated: %v", resRevoke[0].Args)
	}

	// 7. Attendee attempts to patch participationStatus without mayRSVP -> MUST fail with 'forbidden'
	resNoRSVP := callJMAPSharing(t, ts.URL, attendeeUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				eventID: map[string]any{
					"participants/" + attendeeUser + "/participationStatus": "declined",
				},
			},
		}, "c-no-rsvp"},
	})
	notUpdatedNoRSVP, ok := resNoRSVP[0].Args["notUpdated"].(map[string]any)
	if !ok || notUpdatedNoRSVP[eventID] == nil {
		t.Fatalf("Expected RSVP patch without mayRSVP to fail, got: %v", resNoRSVP[0].Args)
	}
	errObj2 := notUpdatedNoRSVP[eventID].(map[string]any)
	if errObj2["type"] != "forbidden" {
		t.Errorf("Expected SetError type 'forbidden', got %v", errObj2["type"])
	}
}

// TestITIP_MailboxDelivery_And_Processing verifies that when iTIP messages arrive via email (RFC 6047),
// ProcessMailboxITIP correctly parses the MIME text/calendar part, enforces sender identity binding,
// imports or updates the calendar event, and marks the email message with the $itip keyword.
func TestITIP_MailboxDelivery_And_Processing(t *testing.T) {
	spectest.Require(t, "RFC6047", "3", spectest.MUST,
		"iTIP processing enforces identity binding between envelope sender and iCalendar actors.")

	const organizer = "organizer@example.com"
	const attendee = "attendee@example.com"
	const stranger = "stranger@example.com"

	attID := jmapauth.AccountIDForSubject(attendee)
	attCtx := jmapauth.ContextWithAccountID(context.Background(), attID)

	mailBackend, cleanupMail := imapsmtp.NewEmbeddedBackend(attendee)
	defer cleanupMail()
	_, calBackend, _, _, _, cleanupNC := nextcloud.NewEmbeddedBackend(attendee)
	defer cleanupNC()

	srv := jmap.NewServer(nil,
		jmap.WithMailBackend(mailBackend),
		jmap.WithBlobBackend(mailBackend),
		jmap.WithCalendarsBackend(calBackend),
	)

	// 1. Deliver legitimate REQUEST email from organizer to attendee
	rawMIME := []byte(strings.Join([]string{
		"From: organizer@example.com",
		"To: attendee@example.com",
		"Subject: Invitation: Architecture Sync",
		"MIME-Version: 1.0",
		"Content-Type: text/calendar; charset=utf-8; method=REQUEST",
		"",
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"METHOD:REQUEST",
		"BEGIN:VEVENT",
		"UID:mbox-delivery-req-1@example.com",
		"SEQUENCE:1",
		"SUMMARY:Architecture Sync",
		"DTSTART:20261120T100000Z",
		"DURATION:PT1H",
		"ORGANIZER:mailto:organizer@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:attendee@example.com",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n"))

	blob, err := mailBackend.PutBlob(attCtx, attID, "message/rfc822", rawMIME)
	if err != nil {
		t.Fatalf("UploadBlob failed: %v", err)
	}

	em, err := mailBackend.CreateEmail(attCtx, &jmapmail.Email{
		BlobID: jmapcore.Id(blob.ID),
		From: []jmapmail.EmailAddress{
			{Name: "Organizer", Email: organizer},
		},
		To: []jmapmail.EmailAddress{
			{Name: "Attendee", Email: attendee},
		},
		Subject: "Invitation: Architecture Sync",
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	// 2. Trigger mailbox iTIP processing
	srv.ProcessMailboxITIP(attCtx)

	// 3. Verify event was auto-imported into attendee's calendar
	events, err := calBackend.GetAllCalendarEvents(attCtx)
	if err != nil {
		t.Fatalf("GetAllCalendarEvents failed: %v", err)
	}
	var importedEv *jmapcalendar.CalendarEvent
	for _, e := range events {
		if e.UID == "mbox-delivery-req-1@example.com" {
			importedEv = e
			break
		}
	}
	if importedEv == nil || importedEv.Title != "Architecture Sync" {
		t.Fatalf("expected imported event with title 'Architecture Sync', got %+v", importedEv)
	}

	// 4. Verify email is tagged with $itip marker
	emails, _, err := mailBackend.GetEmails(attCtx, []jmapcore.Id{em.ID})
	if err != nil || len(emails) == 0 || emails[0] == nil {
		t.Fatalf("GetEmails failed: %v", err)
	}
	if !emails[0].Keywords["$itip"] {
		t.Errorf("expected email to be marked with $itip keyword")
	}

	// 5. Deliver spoofed REQUEST email (sender is stranger claiming to be organizer)
	spoofedMIME := []byte(strings.Join([]string{
		"From: stranger@example.com",
		"To: attendee@example.com",
		"Subject: Invitation: Spoofed Tech Briefing",
		"MIME-Version: 1.0",
		"Content-Type: text/calendar; charset=utf-8; method=REQUEST",
		"",
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"METHOD:REQUEST",
		"BEGIN:VEVENT",
		"UID:spoofed-mbox-req-1@example.com",
		"SEQUENCE:1",
		"SUMMARY:Spoofed Tech Briefing",
		"ORGANIZER:mailto:organizer@example.com",
		"ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:attendee@example.com",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\r\n"))

	spBlob, err := mailBackend.PutBlob(attCtx, attID, "message/rfc822", spoofedMIME)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	_, err = mailBackend.CreateEmail(attCtx, &jmapmail.Email{
		BlobID: jmapcore.Id(spBlob.ID),
		From: []jmapmail.EmailAddress{
			{Name: "Stranger", Email: stranger},
		},
		To: []jmapmail.EmailAddress{
			{Name: "Attendee", Email: attendee},
		},
		Subject: "Invitation: Spoofed Tech Briefing",
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	// Trigger processing again
	srv.ProcessMailboxITIP(attCtx)

	// Verify spoofed event was NOT created on calendar
	afterEvents, _ := calBackend.GetAllCalendarEvents(attCtx)
	for _, e := range afterEvents {
		if e.UID == "spoofed-mbox-req-1@example.com" {
			t.Fatalf("spoofed email must NOT create calendar event!")
		}
	}
}
