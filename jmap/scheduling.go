package jmap

import (
	"context"
	"strings"
)

// This file implements the iTIP scheduling dispatch rules of JMAP for Calendars
// (draft-ietf-jmap-calendars-27 Section 5.9.2): when a CalendarEvent/set carries
// sendSchedulingMessages, the server sends the appropriate iMIP (RFC 6047) email
// carrying an iTIP (RFC 5546) message after a successful create/update/destroy.
//
//   - REQUEST (Section 5.9.2.1): the origin sends to every current participant
//     EXCEPT the calendar owner when the event is created or a non per-user
//     property changes. With hideAttendees, each recipient sees only themselves.
//   - CANCEL  (Section 5.9.2.2): the origin sends to every participant except the
//     owner when the event is destroyed (or a participant/instance is removed).
//   - REPLY   (Section 5.9.2.3): when the server is NOT the origin, a REPLY is sent
//     to the organizer for each of the user's participants whose participationStatus
//     changes to a value other than "needs-action" (the RSVP flow).

// normalizeCalendarAddress strips an optional "mailto:" scheme and lowercases the
// address so owner/organizer/recipient comparisons are scheme- and case-insensitive.
func normalizeCalendarAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if i := strings.Index(strings.ToLower(addr), "mailto:"); i == 0 {
		addr = addr[len("mailto:"):]
	}
	return strings.ToLower(strings.TrimSpace(addr))
}

// isOwnerParticipant reports whether a participant holds the "owner" role (the
// organizer of the event), tolerating both the modern roles map and the legacy
// single role field.
func isOwnerParticipant(p *JSCalendarParticipant) bool {
	if p == nil {
		return false
	}
	return (p.Roles != nil && p.Roles["owner"]) || p.Role == "owner"
}

// participantAddress returns the best scheduling (imip) address for a participant:
// its sendTo imip method, else its email, else the participants map key.
func participantAddress(key string, p *JSCalendarParticipant) string {
	if p != nil {
		if p.SendTo != nil {
			if v, ok := p.SendTo["imip"]; ok && v != "" {
				return normalizeCalendarAddress(v)
			}
		}
		if p.Email != "" {
			return normalizeCalendarAddress(p.Email)
		}
	}
	return normalizeCalendarAddress(key)
}

// organizerAddress returns the calendar address of the event's organizer: the
// replyTo imip method if present, else the address of the first owner-role
// participant. It is "" when the event carries no organizer information.
func organizerAddress(ev *CalendarEvent) string {
	if ev == nil {
		return ""
	}
	if ev.ReplyTo != nil {
		if v, ok := ev.ReplyTo["imip"]; ok && v != "" {
			return normalizeCalendarAddress(v)
		}
		// Any replyTo method is better than nothing.
		for _, v := range ev.ReplyTo {
			if v != "" {
				return normalizeCalendarAddress(v)
			}
		}
	}
	for key, p := range ev.Participants {
		if isOwnerParticipant(p) {
			return participantAddress(key, p)
		}
	}
	return ""
}

// schedulingRecipients returns the addresses that MUST receive a REQUEST/CANCEL:
// every participant except owner-role participants and the organizer address
// (draft-ietf-jmap-calendars-27 Section 5.9.2.1). The map is keyed by the
// participants map key so callers can build a per-recipient hideAttendees view.
func schedulingRecipients(ev *CalendarEvent) map[string]string {
	out := make(map[string]string)
	if ev == nil {
		return out
	}
	organizer := organizerAddress(ev)
	for key, p := range ev.Participants {
		if isOwnerParticipant(p) {
			continue
		}
		addr := participantAddress(key, p)
		if addr == "" || addr == organizer {
			continue
		}
		out[key] = addr
	}
	return out
}

// eventForRecipient returns the event to encode in a REQUEST for a single
// recipient. When hideAttendees is set, only the owner(s) and the recipient
// appear in the participant list (Section 5.9.2.1): "the recipient MUST be the
// only attendee in the message; all others are omitted."
func eventForRecipient(ev *CalendarEvent, recipientKey string) *CalendarEvent {
	if ev == nil || !ev.HideAttendees {
		return ev
	}
	clone := *ev
	clone.Participants = make(map[string]*JSCalendarParticipant, 2)
	for key, p := range ev.Participants {
		if key == recipientKey || isOwnerParticipant(p) {
			clone.Participants[key] = p
		}
	}
	return &clone
}

// sendSchedulingEmail persists an iMIP email (RFC 6047) carrying an iTIP body part
// and submits it. The body part's Content-Type method parameter matches the
// iCalendar METHOD (RFC 6047 Section 2.4).
func sendSchedulingEmail(ctx context.Context, mailBackend MailBackend, subject, toAddr, ics, method string) {
	if mailBackend == nil || toAddr == "" || ics == "" {
		return
	}
	email := &Email{
		Subject: subject,
		To:      []EmailAddress{{Email: toAddr}},
		TextBody: []EmailBodyPart{{
			Type: "text/calendar; method=" + method,
			Size: uint64(len(ics)),
		}},
		BodyValues: map[string]EmailBodyValue{
			"1": {Value: ics},
		},
	}
	saved, err := mailBackend.CreateEmail(ctx, email)
	if err == nil && saved != nil {
		_, _ = mailBackend.CreateSubmission(ctx, &EmailSubmission{
			EmailID:  saved.ID,
			ThreadID: saved.ThreadID,
		})
	}
}

// dispatchITIPRequests sends a METHOD:REQUEST iMIP invitation to every scheduling
// recipient of the event (draft-ietf-jmap-calendars-27 Section 5.9.2.1). The
// calendar owner/organizer is never a recipient, and hideAttendees is honoured.
func dispatchITIPRequests(ctx context.Context, mailBackend MailBackend, ev *CalendarEvent, subjectPrefix, organizerEmail string) {
	if mailBackend == nil || ev == nil {
		return
	}
	for key, addr := range schedulingRecipients(ev) {
		reqICS, err := BuildITIPRequest(eventForRecipient(ev, key), organizerEmail)
		if err != nil {
			continue
		}
		sendSchedulingEmail(ctx, mailBackend, subjectPrefix+ev.Title, addr, reqICS, "REQUEST")
	}
}

// dispatchITIPCancels sends a METHOD:CANCEL iMIP notice to every scheduling
// recipient of the event (draft-ietf-jmap-calendars-27 Section 5.9.2.2).
func dispatchITIPCancels(ctx context.Context, mailBackend MailBackend, ev *CalendarEvent, organizerEmail string) {
	if mailBackend == nil || ev == nil {
		return
	}
	cancelICS, err := BuildITIPCancel(ev, organizerEmail)
	if err != nil {
		return
	}
	for _, addr := range schedulingRecipients(ev) {
		sendSchedulingEmail(ctx, mailBackend, "Cancelled: "+ev.Title, addr, cancelICS, "CANCEL")
	}
}

// dispatchITIPRepliesForPatch implements the RSVP flow (draft-ietf-jmap-calendars-27
// Section 5.9.2.3): when the update patch changes a participant's participationStatus
// to a value other than "needs-action", the server (not being the origin) sends a
// METHOD:REPLY to the organizer on that participant's behalf. It returns true when at
// least one REPLY was sent, so the caller can skip the origin REQUEST path — a bare
// RSVP is a reply, not a re-invitation.
func dispatchITIPRepliesForPatch(ctx context.Context, mailBackend MailBackend, ev *CalendarEvent, patch map[string]any) bool {
	if mailBackend == nil || ev == nil || len(patch) == 0 {
		return false
	}
	organizer := organizerAddress(ev)
	sentAny := false
	for path, val := range patch {
		if !strings.HasPrefix(path, "participants/") {
			continue
		}
		parts := strings.Split(path, "/")
		if len(parts) != 3 {
			continue
		}
		field := parts[2]
		if field != "participationStatus" && field != "status" {
			continue
		}
		status, ok := val.(string)
		if !ok || status == "" || strings.EqualFold(status, "needs-action") {
			continue
		}
		partKey := parts[1]
		attendee := partKey
		if p, ok := ev.Participants[partKey]; ok {
			attendee = participantAddress(partKey, p)
		} else {
			attendee = normalizeCalendarAddress(partKey)
		}
		// A participant cannot reply to itself: skip when this participant is the
		// organizer (the owner changing their own status is not a REPLY).
		if organizer == "" || attendee == organizer {
			continue
		}
		replyICS, err := BuildITIPReply(ev, attendee, status)
		if err != nil {
			continue
		}
		sendSchedulingEmail(ctx, mailBackend, "Re: "+ev.Title, organizer, replyICS, "REPLY")
		sentAny = true
	}
	return sentAny
}
