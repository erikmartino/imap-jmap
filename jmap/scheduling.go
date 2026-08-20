package jmap

import (
	"context"
	"encoding/json"
	"fmt"
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
func sendSchedulingEmail(ctx context.Context, mailBackend MailBackend, subject, toAddr, ics, method string) error {
	if mailBackend == nil || toAddr == "" || ics == "" {
		return fmt.Errorf("missing mailBackend, toAddr, or ics data")
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
	if err != nil || saved == nil {
		return err
	}
	_, err = mailBackend.CreateSubmission(ctx, &EmailSubmission{
		EmailID:  saved.ID,
		ThreadID: saved.ThreadID,
	})
	return err
}

// cloneEventForDelivery deep-copies an event for delivery into another account's
// calendar: the stable uid is preserved (the cross-account correlation key), but the
// origin's server-assigned id, calendar membership, and timestamps are cleared so the
// recipient's backend assigns its own and the copy lands in the recipient's default
// calendar.
func cloneEventForDelivery(ev *CalendarEvent) *CalendarEvent {
	data, err := json.Marshal(ev)
	if err != nil {
		return nil
	}
	var clone CalendarEvent
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil
	}
	clone.ID = ""
	clone.CalendarIDs = nil
	clone.Created = ""
	clone.Updated = ""
	return &clone
}

// findEventByUIDIn returns the event in the given (account) context whose iCalendar
// uid matches (RFC 5546 Section 2.1.5), or nil.
func findEventByUIDIn(ctx context.Context, calBackend CalendarsBackend, uid string) *CalendarEvent {
	if calBackend == nil || uid == "" {
		return nil
	}
	all, err := calBackend.GetAllCalendarEvents(ctx)
	if err != nil {
		return nil
	}
	for _, ev := range all {
		if ev != nil && ev.UID == uid {
			return ev
		}
	}
	return nil
}

// localAccountCtx resolves an address to a local account context, or (nil,false) when
// the address is external or unresolvable. This is how the server acts as the calendar
// agent for a participant that lives on this same server (same-server iTIP delivery).
func localAccountCtx(resolver AccountResolver, addr string) (context.Context, bool) {
	if resolver == nil || addr == "" {
		return nil, false
	}
	acctID, local := resolver.ResolveAccountID(context.Background(), addr)
	if !local || acctID == "" {
		return nil, false
	}
	ctx := ContextWithAccountID(context.Background(), acctID)
	ctx = ContextWithSubject(ctx, addr)
	return ctx, true
}

// deliverRequestLocal delivers a REQUEST into a local recipient's calendar: it creates
// the event (with the recipient's participation still pending) the first time, and
// re-syncs the mutable details on a subsequent REQUEST. A CalendarEventNotification
// records the change as made by the organizer (draft-ietf-jmap-calendars-27 Section 7).
func deliverRequestLocal(calBackend CalendarsBackend, resolver AccountResolver, ev *CalendarEvent, recipientKey, recipientAddr string) bool {
	rcptCtx, ok := localAccountCtx(resolver, recipientAddr)
	if !ok || calBackend == nil {
		return false
	}
	view := eventForRecipient(ev, recipientKey)
	if existing := findEventByUIDIn(rcptCtx, calBackend, ev.UID); existing != nil {
		// A subsequent REQUEST re-syncs the mutable core details on the recipient's copy.
		patch := map[string]any{"title": view.Title, "start": view.Start}
		if view.Duration != "" {
			patch["duration"] = view.Duration
		}
		_, err := calBackend.UpdateCalendarEvent(rcptCtx, existing.ID, patch)
		return err == nil
	}
	copyEv := cloneEventForDelivery(view)
	if copyEv == nil {
		return false
	}
	created, err := calBackend.CreateCalendarEvent(rcptCtx, copyEv)
	if err != nil || created == nil {
		return false
	}
	_, _ = calBackend.CreateCalendarEventNotification(rcptCtx, &CalendarEventNotification{
		Type:            "created",
		CalendarEventID: created.ID,
		ChangedBy:       notificationChangedBy(ev),
		Event:           created,
	})
	return true
}

// deliverReplyLocal applies an attendee's REPLY into a local organizer's copy of the
// event (matched by uid), updating that participant's participationStatus and recording
// a CalendarEventNotification (draft-ietf-jmap-calendars-27 Section 5.9.2.3 / Section 7).
func deliverReplyLocal(calBackend CalendarsBackend, resolver AccountResolver, ev *CalendarEvent, attendeeAddr, status string) bool {
	orgCtx, ok := localAccountCtx(resolver, organizerAddress(ev))
	if !ok || calBackend == nil {
		return false
	}
	orgEvent := findEventByUIDIn(orgCtx, calBackend, ev.UID)
	if orgEvent == nil {
		return false
	}
	patch := map[string]any{
		"participants/" + attendeeAddr + "/participationStatus": status,
		"participants/" + attendeeAddr + "/scheduleStatus":      "2.0;delivered",
	}
	if _, err := calBackend.UpdateCalendarEvent(orgCtx, orgEvent.ID, patch); err != nil {
		return false
	}
	replyEmail := attendeeAddr
	_, _ = calBackend.CreateCalendarEventNotification(orgCtx, &CalendarEventNotification{
		Type:            "updated",
		CalendarEventID: orgEvent.ID,
		ChangedBy:       CalendarEventNotificationPerson{Email: &replyEmail, CalendarAddress: &replyEmail},
		Event:           orgEvent,
		EventPatch:      patch,
	})
	return true
}

// deliverCancelLocal marks a local recipient's copy of the event cancelled when the
// organizer destroys it (draft-ietf-jmap-calendars-27 Section 5.9.2.2).
func deliverCancelLocal(calBackend CalendarsBackend, resolver AccountResolver, ev *CalendarEvent, recipientAddr string) bool {
	rcptCtx, ok := localAccountCtx(resolver, recipientAddr)
	if !ok || calBackend == nil {
		return false
	}
	existing := findEventByUIDIn(rcptCtx, calBackend, ev.UID)
	if existing == nil {
		return false
	}
	_, err := calBackend.UpdateCalendarEvent(rcptCtx, existing.ID, map[string]any{"status": "cancelled"})
	return err == nil
}

// dispatchITIPRequests sends a METHOD:REQUEST to every scheduling recipient of the event
// (draft-ietf-jmap-calendars-27 Section 5.9.2.1): the calendar owner/organizer is never a
// recipient, and hideAttendees is honoured. Recipients local to this server also receive
// the event directly in their calendar (same-server iTIP delivery); external recipients
// get an iMIP email. It also records the per-participant scheduleStatus (SEC-7 / RFC 6638 Section 3.2.14).
func dispatchITIPRequests(ctx context.Context, mailBackend MailBackend, calBackend CalendarsBackend, resolver AccountResolver, ev *CalendarEvent, subjectPrefix, organizerEmail string) {
	if ev == nil {
		return
	}
	statusPatches := make(map[string]any)
	for key, addr := range schedulingRecipients(ev) {
		deliveredLocal := deliverRequestLocal(calBackend, resolver, ev, key, addr)
		sentEmail := false
		if mailBackend != nil {
			if reqICS, err := BuildITIPRequest(eventForRecipient(ev, key), organizerEmail); err == nil {
				if err := sendSchedulingEmail(ctx, mailBackend, subjectPrefix+ev.Title, addr, reqICS, "REQUEST"); err == nil {
					sentEmail = true
				}
			}
		}
		if deliveredLocal {
			statusPatches["participants/"+key+"/scheduleStatus"] = "2.0;delivered"
		} else if sentEmail {
			statusPatches["participants/"+key+"/scheduleStatus"] = "1.1;sent"
		} else {
			statusPatches["participants/"+key+"/scheduleStatus"] = "5.1;failed"
		}
	}
	if len(statusPatches) > 0 && calBackend != nil && ev.ID != "" {
		updated, _ := calBackend.UpdateCalendarEvent(ctx, ev.ID, statusPatches)
		if updated != nil && ev.Participants != nil {
			for k, p := range updated.Participants {
				if ev.Participants[k] != nil && p != nil {
					ev.Participants[k].ScheduleStatus = p.ScheduleStatus
				}
			}
		}
	}
}

// dispatchITIPCancels sends a METHOD:CANCEL to every scheduling recipient of the event
// (draft-ietf-jmap-calendars-27 Section 5.9.2.2), cancelling local recipients' copies and
// emailing external recipients.
func dispatchITIPCancels(ctx context.Context, mailBackend MailBackend, calBackend CalendarsBackend, resolver AccountResolver, ev *CalendarEvent, organizerEmail string) {
	if ev == nil {
		return
	}
	cancelICS, icsErr := BuildITIPCancel(ev, organizerEmail)
	for _, addr := range schedulingRecipients(ev) {
		deliverCancelLocal(calBackend, resolver, ev, addr)
		if mailBackend != nil && icsErr == nil {
			_ = sendSchedulingEmail(ctx, mailBackend, "Cancelled: "+ev.Title, addr, cancelICS, "CANCEL")
		}
	}
}

// dispatchITIPRepliesForPatch implements the RSVP flow (draft-ietf-jmap-calendars-27
// Section 5.9.2.3): when the update patch changes a participant's participationStatus
// to a value other than "needs-action", the server (not being the origin) sends a
// METHOD:REPLY to the organizer on that participant's behalf — reflected directly into a
// local organizer's copy, or emailed to an external organizer. It returns true when at
// least one REPLY was produced, so the caller can skip the origin REQUEST path — a bare
// RSVP is a reply, not a re-invitation.
func dispatchITIPRepliesForPatch(ctx context.Context, mailBackend MailBackend, calBackend CalendarsBackend, resolver AccountResolver, ev *CalendarEvent, patch map[string]any) bool {
	if ev == nil || len(patch) == 0 {
		return false
	}
	organizer := organizerAddress(ev)
	if organizer == "" {
		return false
	}
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
		if attendee == organizer {
			continue
		}
		deliveredLocal := deliverReplyLocal(calBackend, resolver, ev, attendee, status)
		if deliveredLocal {
			patch["participants/"+partKey+"/scheduleStatus"] = "2.0;delivered"
		} else if mailBackend != nil {
			if replyICS, err := BuildITIPReply(ev, attendee, status); err == nil {
				if err := sendSchedulingEmail(ctx, mailBackend, "Re: "+ev.Title, organizer, replyICS, "REPLY"); err == nil {
					patch["participants/"+partKey+"/scheduleStatus"] = "1.1;sent"
				} else {
					patch["participants/"+partKey+"/scheduleStatus"] = "5.1;failed"
				}
			}
		}
		sentAny = true
	}
	return sentAny
}
