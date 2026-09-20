package jmapcalendar

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmapprincipals"
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
	addr = strings.ReplaceAll(strings.ReplaceAll(addr, "\r", ""), "\n", "")
	if i := strings.Index(strings.ToLower(addr), "mailto:"); i == 0 {
		addr = addr[len("mailto:"):]
	}
	return strings.ToLower(addr)
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

// organizerAddress extracts the normalized organizer email from the event.
func organizerAddress(ev *CalendarEvent) string {
	if ev == nil {
		return ""
	}
	if ev.OrganizerCalendarAddress != "" {
		return normalizeCalendarAddress(ev.OrganizerCalendarAddress)
	}
	for _, p := range ev.Participants {
		if p == nil {
			continue
		}
		if (p.Roles != nil && p.Roles["owner"]) || p.Role == "owner" {
			if p.CalendarAddress != "" {
				return normalizeCalendarAddress(p.CalendarAddress)
			}
			if p.Email != "" {
				return normalizeCalendarAddress(p.Email)
			}
		}
	}
	return ""
}

// participantAddress returns the best email/calendar-address for a participant.
func participantAddress(key string, p *JSCalendarParticipant) string {
	if p != nil {
		if p.CalendarAddress != "" {
			return normalizeCalendarAddress(p.CalendarAddress)
		}
		if p.Email != "" {
			return normalizeCalendarAddress(p.Email)
		}
	}
	return normalizeCalendarAddress(key)
}

// schedulingRecipients returns the map of participantKey -> normalizedEmail for all
// participants who should receive scheduling messages (excluding the organizer/owner).
func schedulingRecipients(ev *CalendarEvent) map[string]string {
	recipients := make(map[string]string)
	if ev == nil {
		return recipients
	}
	org := organizerAddress(ev)
	for key, p := range ev.Participants {
		if p == nil {
			continue
		}
		addr := participantAddress(key, p)
		if addr == "" {
			continue
		}
		// The organizer/owner never receives their own outgoing request/cancel.
		if org != "" && addr == org {
			continue
		}
		if (p.Roles != nil && p.Roles["owner"]) || p.Role == "owner" {
			continue
		}
		recipients[key] = addr
	}
	return recipients
}

// eventForRecipient returns a copy of the event tailored for the recipient: when
// hideAttendees is true, all other participants are stripped so the recipient sees
// only themselves and the organizer (draft-ietf-jmap-calendars-27 Section 5.9.2.1).
func eventForRecipient(ev *CalendarEvent, recipientKey string) *CalendarEvent {
	if ev == nil || !ev.HideAttendees || len(ev.Participants) <= 1 {
		return ev
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return ev
	}
	var clone CalendarEvent
	if err := json.Unmarshal(b, &clone); err != nil {
		return ev
	}
	filtered := make(map[string]*JSCalendarParticipant)
	for k, p := range clone.Participants {
		if p == nil {
			continue
		}
		if k == recipientKey || (p.Roles != nil && p.Roles["owner"]) || p.Role == "owner" {
			filtered[k] = p
		}
	}
	clone.Participants = filtered
	return &clone
}

// sendSchedulingEmail persists an iMIP email (RFC 6047) carrying an iTIP body part
// and submits it. The body part's Content-Type method parameter matches the
// iCalendar METHOD (RFC 6047 Section 2.4).
func sendSchedulingEmail(ctx context.Context, mailBackend jmapmail.MailBackend, subject, fromAddr, toAddr, ics, method string) error {
	if mailBackend == nil || toAddr == "" || ics == "" {
		return fmt.Errorf("missing mailBackend, toAddr, or ics data")
	}
	subject = strings.ReplaceAll(strings.ReplaceAll(subject, "\r", ""), "\n", " ")
	fromAddr = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fromAddr, "\r", ""), "\n", ""))
	toAddr = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(toAddr, "\r", ""), "\n", ""))
	if fromAddr == "" {
		fromAddr = "calendar@example.com"
	}
	p1 := "1"
	email := &jmapmail.Email{
		MailboxIDs: map[jmapcore.Id]bool{"mb-sent": true},
		Subject:    subject,
		From:       []jmapmail.EmailAddress{{Email: fromAddr}},
		To:         []jmapmail.EmailAddress{{Email: toAddr}},
		BodyStructure: jmapmail.EmailBodyPart{
			PartID: &p1,
			Type:   "text/calendar; method=" + method,
			Size:   uint64(len(ics)),
		},
		TextBody: []jmapmail.EmailBodyPart{{
			PartID: &p1,
			Type:   "text/calendar; method=" + method,
			Size:   uint64(len(ics)),
		}},
		BodyValues: map[string]jmapmail.EmailBodyValue{
			"1": {Value: ics},
		},
	}
	saved, err := mailBackend.CreateEmail(ctx, email)
	if err != nil || saved == nil {
		return err
	}
	_, err = mailBackend.CreateSubmission(ctx, &jmapmail.EmailSubmission{
		EmailID:  saved.ID,
		ThreadID: saved.ThreadID,
		Envelope: &jmapmail.SubmissionEnvelope{
			MailFrom: jmapmail.SubmissionAddress{Email: fromAddr},
			RcptTo:   []jmapmail.SubmissionAddress{{Email: toAddr}},
		},
	})
	return err
}

// cloneEventForDelivery deep-copies an event for delivery into another account's
// calendar: the stable uid is preserved (the cross-account correlation key), but the
// origin's server-assigned id, calendar membership, and timestamps are cleared so the
// recipient's backend assigns its own and the copy lands in the recipient's default
// calendar.
func cloneEventForDelivery(ev *CalendarEvent) *CalendarEvent {
	if ev == nil {
		return nil
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return nil
	}
	var copyEv CalendarEvent
	if err := json.Unmarshal(b, &copyEv); err != nil {
		return nil
	}
	copyEv.ID = ""
	copyEv.CalendarIDs = nil
	copyEv.Created = ""
	copyEv.Updated = ""
	copyEv.IsOrigin = false
	return &copyEv
}

// findEventByUIDIn finds a CalendarEvent with matching uid in the target account.
func findEventByUIDIn(ctx context.Context, backend CalendarsBackend, uid string) *CalendarEvent {
	if backend == nil || uid == "" {
		return nil
	}
	events, err := backend.GetAllCalendarEvents(ctx)
	if err != nil {
		return nil
	}
	for _, e := range events {
		if e != nil && e.UID == uid {
			return e
		}
	}
	return nil
}

// localAccountCtx resolves an address to a local account context, or (nil,false) when
// the address is external or unresolvable. This is how the server acts as the calendar
// agent for a participant that lives on this same server (same-server iTIP delivery).
func localAccountCtx(resolver jmapauth.AccountResolver, addr string) (context.Context, bool) {
	if resolver == nil || addr == "" {
		return nil, false
	}
	acctID, local := resolver.ResolveAccountID(context.Background(), addr)
	if !local || acctID == "" {
		return nil, false
	}
	ctx := ContextWithAccountID(context.Background(), acctID)
	ctx = ContextWithSubject(ctx, addr)
	ctx = ContextWithCredentials(ctx, addr, addr)
	return ctx, true
}

// deliverRequestLocal delivers a REQUEST into a local recipient's calendar: it creates
// the event (with the recipient's participation still pending) the first time, and
// re-syncs the mutable details on a subsequent REQUEST. A CalendarEventNotification
// records the change as made by the organizer (draft-ietf-jmap-calendars-27 Section 7).
func deliverRequestLocal(calBackend CalendarsBackend, resolver jmapauth.AccountResolver, ev *CalendarEvent, recipientKey, recipientAddr string) bool {
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
func deliverReplyLocal(calBackend CalendarsBackend, resolver jmapauth.AccountResolver, ev *CalendarEvent, attendeeAddr, status string) bool {
	orgCtx, ok := localAccountCtx(resolver, organizerAddress(ev))
	if !ok || calBackend == nil {
		return false
	}
	orgEvent := findEventByUIDIn(orgCtx, calBackend, ev.UID)
	if orgEvent == nil {
		return false
	}
	var pKey string
	for k, p := range orgEvent.Participants {
		if p != nil && (strings.EqualFold(p.CalendarAddress, attendeeAddr) ||
			strings.EqualFold(p.CalendarAddress, "mailto:"+attendeeAddr) ||
			strings.EqualFold("mailto:"+p.Email, attendeeAddr) ||
			strings.EqualFold(p.Email, attendeeAddr)) {
			pKey = k
			break
		}
	}
	if pKey == "" {
		pKey = attendeeAddr
	}
	patch := map[string]any{
		"participants/" + pKey + "/participationStatus": status,
		"participants/" + pKey + "/scheduleStatus":      "2.0;delivered",
	}
	if _, err := calBackend.UpdateCalendarEvent(orgCtx, orgEvent.ID, patch); err != nil {
		return false
	}
	replyEmail := strings.TrimPrefix(attendeeAddr, "mailto:")
	name := replyEmail
	if pKey != "" && orgEvent.Participants[pKey] != nil && orgEvent.Participants[pKey].Name != "" {
		name = orgEvent.Participants[pKey].Name
	}
	pID := AccountIDForSubject(replyEmail)
	_, _ = calBackend.CreateCalendarEventNotification(orgCtx, &CalendarEventNotification{
		Type:            "updated",
		CalendarEventID: orgEvent.ID,
		ChangedBy: CalendarEventNotificationPerson{
			Name:            name,
			Email:           &replyEmail,
			PrincipalID:     &pID,
			CalendarAddress: &attendeeAddr,
		},
		Event:      orgEvent,
		EventPatch: patch,
	})
	return true
}

// deliverCancelLocal marks a local recipient's copy of the event cancelled when the
// organizer destroys it (draft-ietf-jmap-calendars-27 Section 5.9.2.2).
func deliverCancelLocal(calBackend CalendarsBackend, resolver jmapauth.AccountResolver, ev *CalendarEvent, recipientAddr string) bool {
	rcptCtx, ok := localAccountCtx(resolver, recipientAddr)
	if !ok || calBackend == nil {
		return false
	}
	existing := findEventByUIDIn(rcptCtx, calBackend, ev.UID)
	if existing == nil {
		return false
	}
	updatedEv, err := calBackend.UpdateCalendarEvent(rcptCtx, existing.ID, map[string]any{"status": "cancelled"})
	if err == nil {
		changedBy := notificationChangedBy(ev)
		_, _ = calBackend.CreateCalendarEventNotification(rcptCtx, &CalendarEventNotification{
			Type:            "updated",
			CalendarEventID: existing.ID,
			ChangedBy:       changedBy,
			Event:           updatedEv,
		})
	}
	return err == nil
}

// ExpandGroupRecipients expands any group recipient into individual member addresses
// so invites and updates reach all members per draft-ietf-jmap-calendars-27 Section 6.
func ExpandGroupRecipients(ctx context.Context, principalsBackend jmapprincipals.PrincipalsBackend, recipients map[string]string) map[string]string {
	if principalsBackend == nil || len(recipients) == 0 {
		return recipients
	}
	allPrincipals, err := principalsBackend.GetAllPrincipals(ctx)
	if err != nil || len(allPrincipals) == 0 {
		return recipients
	}

	principalByID := make(map[jmapcore.Id]*jmapprincipals.Principal, len(allPrincipals))
	principalByEmail := make(map[string]*jmapprincipals.Principal, len(allPrincipals))
	for _, p := range allPrincipals {
		if p != nil {
			principalByID[p.ID] = p
			if p.Email != "" {
				principalByEmail[normalizeCalendarAddress(p.Email)] = p
			}
		}
	}

	expanded := make(map[string]string)
	for key, addr := range recipients {
		normAddr := normalizeCalendarAddress(addr)
		p, isPrincipal := principalByEmail[normAddr]
		if !isPrincipal {
			p = principalByID[jmapcore.Id(key)]
		}

		if p != nil && p.Type == "group" && len(p.Members) > 0 {
			for memberID := range p.Members {
				if member, ok := principalByID[jmapcore.Id(memberID)]; ok && member != nil && member.Email != "" {
					expanded[string(member.ID)] = normalizeCalendarAddress(member.Email)
				}
			}
		} else {
			expanded[key] = addr
		}
	}
	return expanded
}

// dispatchITIPRequests sends a METHOD:REQUEST to every scheduling recipient of the event
// (draft-ietf-jmap-calendars-27 Section 5.9.2.1): the calendar owner/organizer is never a
// recipient, and hideAttendees is honoured. Recipients local to this server also receive
// the event directly in their calendar (same-server iTIP delivery); external recipients
// get an iMIP email. It also records the per-participant scheduleStatus (SEC-7 / RFC 6638 Section 3.2.14).
func dispatchITIPRequests(ctx context.Context, mailBackend jmapmail.MailBackend, calBackend CalendarsBackend, principalsBackend jmapprincipals.PrincipalsBackend, resolver jmapauth.AccountResolver, ev *CalendarEvent, subjectPrefix, organizerEmail string) {
	if ev == nil {
		return
	}
	if organizerEmail == "" {
		organizerEmail = organizerAddress(ev)
	}
	recipients := expandGroupRecipients(ctx, principalsBackend, schedulingRecipients(ev))
	statusPatches := make(map[string]any)
	for key, addr := range recipients {
		deliveredLocal := deliverRequestLocal(calBackend, resolver, ev, key, addr)
		sentEmail := false
		if mailBackend != nil {
			if reqICS, err := BuildITIPRequest(eventForRecipient(ev, key), organizerEmail); err == nil {
				if err := sendSchedulingEmail(ctx, mailBackend, subjectPrefix+ev.Title, organizerEmail, addr, reqICS, "REQUEST"); err == nil {
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
func dispatchITIPCancels(ctx context.Context, mailBackend jmapmail.MailBackend, calBackend CalendarsBackend, principalsBackend jmapprincipals.PrincipalsBackend, resolver jmapauth.AccountResolver, ev *CalendarEvent, organizerEmail string) {
	if ev == nil {
		return
	}
	if organizerEmail == "" {
		organizerEmail = organizerAddress(ev)
	}
	recipients := expandGroupRecipients(ctx, principalsBackend, schedulingRecipients(ev))
	cancelICS, icsErr := BuildITIPCancel(ev, organizerEmail)
	for key, addr := range recipients {
		if p, ok := ev.Participants[key]; ok && p != nil {
			if strings.EqualFold(p.ParticipationStatus, "declined") || strings.EqualFold(p.Status, "declined") {
				continue
			}
		}
		deliverCancelLocal(calBackend, resolver, ev, addr)
		if mailBackend != nil && icsErr == nil {
			_ = sendSchedulingEmail(ctx, mailBackend, "Cancelled: "+ev.Title, organizerEmail, addr, cancelICS, "CANCEL")
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
func dispatchITIPRepliesForPatch(ctx context.Context, mailBackend jmapmail.MailBackend, calBackend CalendarsBackend, resolver jmapauth.AccountResolver, ev *CalendarEvent, patch map[string]any) bool {
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
				if err := sendSchedulingEmail(ctx, mailBackend, "Re: "+ev.Title, attendee, organizer, replyICS, "REPLY"); err == nil {
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
