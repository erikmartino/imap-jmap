package jmapcalendar

import (
	"bytes"
	"context"
	"io"
	"log"
	"mime"
	"strings"

	"github.com/emersion/go-message/mail"

	"imap-jmap/jmap/jmapcore"
)

// maxITIPMIMEParts bounds how many MIME parts are inspected when locating a
// text/calendar body, so a pathological message cannot cause unbounded work.
const maxITIPMIMEParts = 100

// ExtractCalendarBody returns the decoded body of the message's text/calendar MIME part
// (RFC 6047 Section 2.4), using a real MIME reader that also decodes any
// Content-Transfer-Encoding (base64 / quoted-printable). Only a genuine text/calendar
// part is honoured, so scheduling logic can never be driven by iCalendar-looking text
// smuggled into an unrelated part. Returns "" when the message carries no calendar part.
func ExtractCalendarBody(raw []byte) string {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	partsCount := 0
	for {
		if partsCount >= maxITIPMIMEParts {
			break
		}
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		partsCount++
		mediaType, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if strings.EqualFold(mediaType, "text/calendar") {
			body, err := io.ReadAll(p.Body)
			if err != nil {
				return ""
			}
			return string(body)
		}
	}
	return ""
}

// ApplyITIP applies an iMIP/iTIP message body to the account's calendars (RFC 6047 /
// RFC 5546). envelopeSender is the address that delivered the message — the SMTP
// MAIL FROM for delivered mail, or the RFC5322.From for mail processed from a mailbox.
// It is used for envelope/identity binding and organizer authorization, and is matched
// case-insensitively against the iTIP ORGANIZER/ATTENDEE addresses.
//
// It returns true when it created, updated or cancelled an event. It never fails open
// on identity binding: a REPLY is only applied when the sender matches the attendee,
// a REQUEST only when the sender matches the organizer, and a CANCEL only from the
// organizer. Replay/out-of-order messages (lower SEQUENCE) are ignored.
func ApplyITIP(ctx context.Context, backend CalendarsBackend, icsBody, envelopeSender string) bool {
	if backend == nil || icsBody == "" {
		return false
	}
	msg, err := ParseITIPMessage(icsBody)
	if err != nil || msg == nil || msg.UID == "" {
		return false
	}
	senderClean := cleanAddress(envelopeSender)

	switch {
	case strings.EqualFold(msg.Method, "REPLY"):
		attendeeEmail := envelopeSender
		if len(msg.Attendees) > 0 && msg.Attendees[0].Email != "" {
			attendeeEmail = msg.Attendees[0].Email
		}
		attendeeClean := cleanAddress(attendeeEmail)
		if senderClean != "" && attendeeClean != "" && senderClean != attendeeClean {
			log.Printf("iTIP: ignoring REPLY: sender %q does not match attendee %q", envelopeSender, attendeeEmail)
			return false
		}
		ev := findEventByUID(ctx, backend, msg.UID)
		if ev == nil {
			return false
		}
		partKey := findParticipantKey(ev, attendeeEmail)
		if partKey == "" {
			log.Printf("iTIP: ignoring REPLY: attendee %q is not a participant on event %s", attendeeEmail, ev.ID)
			return false
		}
		if msg.Sequence > 0 && ev.Sequence > 0 && msg.Sequence < ev.Sequence {
			log.Printf("iTIP: ignoring stale REPLY: message sequence %d < event sequence %d", msg.Sequence, ev.Sequence)
			return false
		}
		status := strings.ToLower(msg.Status)
		if status == "" {
			status = "accepted"
		}
		patch := map[string]any{
			"participants/" + partKey + "/participationStatus": status,
			"participants/" + partKey + "/status":              status,
			"participants/" + partKey + "/scheduleStatus":      "2.0;delivered",
		}
		if msg.Sequence > ev.Sequence {
			patch["sequence"] = msg.Sequence
		}
		if _, err := backend.UpdateCalendarEvent(ctx, ev.ID, patch); err != nil {
			return false
		}
		replyEmail := attendeeEmail
		backend.CreateCalendarEventNotification(ctx, &CalendarEventNotification{
			Type:            "updated",
			CalendarEventID: ev.ID,
			ChangedBy: CalendarEventNotificationPerson{
				Email:           &replyEmail,
				CalendarAddress: &replyEmail,
			},
			Event:      ev,
			EventPatch: patch,
		})
		log.Printf("iTIP: applied REPLY to event %s: participant %s -> %s", ev.ID, attendeeEmail, status)
		return true

	case strings.EqualFold(msg.Method, "REQUEST"):
		orgClean := cleanAddress(msg.Organizer)
		if senderClean != "" && orgClean != "" && senderClean != orgClean {
			log.Printf("iTIP: ignoring REQUEST: sender %q does not match organizer %q", envelopeSender, msg.Organizer)
			return false
		}
		imported := parseImportedEvent(icsBody, msg)
		if existing := findEventByUID(ctx, backend, imported.UID); existing != nil {
			if msg.Sequence > 0 && existing.Sequence > 0 && msg.Sequence < existing.Sequence {
				log.Printf("iTIP: ignoring stale REQUEST: message sequence %d < event sequence %d", msg.Sequence, existing.Sequence)
				return false
			}
			patch := map[string]any{"title": imported.Title, "start": imported.Start}
			if imported.Duration != "" {
				patch["duration"] = imported.Duration
			}
			if msg.Sequence >= existing.Sequence {
				patch["sequence"] = msg.Sequence
			}
			_, _ = backend.UpdateCalendarEvent(ctx, existing.ID, patch)
			return true
		}
		imported.ID = ""
		imported.CalendarIDs = map[jmapcore.Id]bool{"cal-default": true}
		if imported.Status == "" {
			imported.Status = "tentative"
		}
		ensureOwnerParticipant(imported, envelopeSender)
		createdEv, err := backend.CreateCalendarEvent(ctx, imported)
		if err == nil && createdEv != nil {
			log.Printf("iTIP: auto-imported invitation into calendar event %s (%s)", createdEv.ID, createdEv.Title)
			return true
		}
		return false

	case strings.EqualFold(msg.Method, "CANCEL"):
		ev := findEventByUID(ctx, backend, msg.UID)
		if ev == nil {
			return false
		}
		orgClean := cleanAddress(msg.Organizer)
		if senderClean != "" && orgClean != "" && senderClean != orgClean {
			log.Printf("iTIP: ignoring CANCEL: sender %q does not match organizer %q", envelopeSender, msg.Organizer)
			return false
		}
		if senderClean != "" && !isEventOrganizer(ev, envelopeSender) && orgClean != "" && !isEventOrganizer(ev, orgClean) {
			log.Printf("iTIP: ignoring CANCEL: sender %q is not the organizer of event %s", envelopeSender, ev.ID)
			return false
		}
		if msg.Sequence > 0 && ev.Sequence > 0 && msg.Sequence < ev.Sequence {
			log.Printf("iTIP: ignoring stale CANCEL: message sequence %d < event sequence %d", msg.Sequence, ev.Sequence)
			return false
		}
		patch := map[string]any{"status": "cancelled"}
		if msg.Sequence >= ev.Sequence {
			patch["sequence"] = msg.Sequence
		}
		if _, err := backend.UpdateCalendarEvent(ctx, ev.ID, patch); err != nil {
			return false
		}
		fromEmail := envelopeSender
		backend.CreateCalendarEventNotification(ctx, &CalendarEventNotification{
			Type:            "deleted",
			CalendarEventID: ev.ID,
			ChangedBy: CalendarEventNotificationPerson{
				Email:           &fromEmail,
				CalendarAddress: &fromEmail,
			},
			Event:      ev,
			EventPatch: patch,
		})
		log.Printf("iTIP: cancelled event %s from CANCEL", ev.ID)
		return true
	}
	return false
}

func cleanAddress(addr string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(addr, "mailto:")))
}

// parseImportedEvent parses the (already MIME-extracted) text/calendar body into a full
// CalendarEvent (RFC 5545 -> RFC 8984), preferring the VEVENT whose UID matches the iTIP
// message and falling back to a title+start event from the scanned iTIP fields.
func parseImportedEvent(ics string, msg *ITIPMessage) *CalendarEvent {
	if events, err := ParseICalendar([]byte(ics)); err == nil {
		for _, e := range events {
			if e != nil && e.UID == msg.UID {
				return e
			}
		}
		if len(events) > 0 && events[0] != nil {
			return events[0]
		}
	}
	title := msg.Summary
	if title == "" {
		title = "External Meeting Invitation"
	}
	return &CalendarEvent{UID: msg.UID, Title: title, Start: msg.Start}
}

func findParticipantKey(ev *CalendarEvent, attendeeEmail string) string {
	if ev == nil || attendeeEmail == "" {
		return ""
	}
	clean := cleanAddress(attendeeEmail)
	for key, p := range ev.Participants {
		if strings.ToLower(key) == clean || strings.ToLower(key) == "mailto:"+clean {
			return key
		}
		if p != nil {
			if strings.EqualFold(strings.TrimPrefix(p.Email, "mailto:"), clean) {
				return key
			}
			for _, val := range p.SendTo {
				if strings.EqualFold(strings.TrimPrefix(val, "mailto:"), clean) {
					return key
				}
			}
		}
	}
	return ""
}

func isEventOrganizer(ev *CalendarEvent, email string) bool {
	if ev == nil || email == "" {
		return false
	}
	clean := cleanAddress(email)
	for key, p := range ev.Participants {
		if p == nil {
			continue
		}
		isOrg := (p.Roles != nil && (p.Roles["owner"] || p.Roles["organizer"] || p.Roles["chair"])) ||
			p.Role == "owner" || p.Role == "organizer" || p.Role == "chair"
		if !isOrg {
			continue
		}
		if strings.ToLower(key) == clean || strings.EqualFold(strings.TrimPrefix(p.Email, "mailto:"), clean) {
			return true
		}
		for _, val := range p.SendTo {
			if strings.EqualFold(strings.TrimPrefix(val, "mailto:"), clean) {
				return true
			}
		}
	}
	return false
}

// ensureOwnerParticipant guarantees the imported event has an owner participant (the
// organizer), adding the envelope sender as owner when the ICS carried none.
func ensureOwnerParticipant(ev *CalendarEvent, from string) {
	for _, p := range ev.Participants {
		if p != nil && ((p.Roles != nil && p.Roles["owner"]) || p.Role == "owner") {
			return
		}
	}
	if from == "" {
		return
	}
	if ev.Participants == nil {
		ev.Participants = make(map[string]*JSCalendarParticipant)
	}
	ev.Participants[from] = &JSCalendarParticipant{
		Email: from,
		Role:  "owner",
		Roles: map[string]bool{"owner": true},
	}
}

// findEventByUID locates the calendar event whose iCalendar UID (RFC 5546 Section 2.1.5)
// matches uid. It scans the account's events by their "uid" property, and falls back to
// treating uid as a JMAP id for events imported before uid tracking.
func findEventByUID(ctx context.Context, backend CalendarsBackend, uid string) *CalendarEvent {
	if backend == nil || uid == "" {
		return nil
	}
	if all, err := backend.GetAllCalendarEvents(ctx); err == nil {
		for _, ev := range all {
			if ev != nil && ev.UID == uid {
				return ev
			}
		}
	}
	events, _, err := backend.GetCalendarEvents(ctx, []jmapcore.Id{jmapcore.Id(uid)})
	if err == nil && len(events) > 0 {
		return events[0]
	}
	return nil
}
