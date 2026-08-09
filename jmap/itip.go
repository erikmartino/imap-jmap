package jmap

import (
	"fmt"
	"strings"
	"time"
)

// ITIPMessage represents a parsed iTIP (RFC 5546) / iMIP (RFC 6047) scheduling message.
type ITIPMessage struct {
	Method    string         `json:"method"` // "REQUEST", "REPLY", "CANCEL"
	UID       string         `json:"uid"`
	Summary   string         `json:"summary"`
	Start     string         `json:"start"`
	End       string         `json:"end,omitempty"`
	Organizer string         `json:"organizer"`
	Attendees []EmailAddress `json:"attendees"`
	Status    string         `json:"status,omitempty"` // For REPLY: "ACCEPTED", "DECLINED", "TENTATIVE"
}

// eventUID returns the RFC 5545 UID for iTIP messages. It MUST be the event's stable
// "uid" property (the cross-system correlation key, RFC 5546 Section 2.1.5), not the
// server-assigned JMAP id; it falls back to the id only when no uid is set.
func eventUID(event *CalendarEvent) string {
	if event != nil && event.UID != "" {
		return event.UID
	}
	if event != nil {
		return string(event.ID)
	}
	return ""
}

// BuildITIPReply generates an iCalendar RFC 5545 / RFC 5546 string for a METHOD:REPLY
// (RFC 5546 Section 3.2.3). A REPLY carries the ORGANIZER being answered, the replying
// ATTENDEE with its PARTSTAT, and the UID/SEQUENCE correlation keys (Section 2.1.5).
func BuildITIPReply(event *CalendarEvent, attendeeEmail, status string) (string, error) {
	if event == nil {
		return "", fmt.Errorf("event cannot be nil")
	}

	uid := eventUID(event)

	partStat := strings.ToUpper(status)
	if partStat != "ACCEPTED" && partStat != "DECLINED" && partStat != "TENTATIVE" {
		partStat = "ACCEPTED"
	}

	nowStr := time.Now().UTC().Format("20060102T150405Z")

	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//IMAP-JMAP Server//NONSGML v1.0//EN\r\n")
	sb.WriteString("METHOD:REPLY\r\n")
	sb.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(&sb, "UID:%s\r\n", uid)
	fmt.Fprintf(&sb, "DTSTAMP:%s\r\n", nowStr)
	fmt.Fprintf(&sb, "SEQUENCE:%d\r\n", event.Sequence)
	if event.Title != "" {
		fmt.Fprintf(&sb, "SUMMARY:%s\r\n", event.Title)
	}
	if event.Start != "" {
		startClean := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(event.Start, "-", ""), ":", ""), ".000", "")
		fmt.Fprintf(&sb, "DTSTART:%s\r\n", startClean)
	}
	// A REPLY MUST identify the ORGANIZER whose request is being answered (RFC 5546
	// Section 3.2.3): derive it from the event's replyTo / owner participant.
	if org := organizerAddress(event); org != "" {
		fmt.Fprintf(&sb, "ORGANIZER:mailto:%s\r\n", org)
	}
	fmt.Fprintf(&sb, "ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=REQ-PARTICIPANT;PARTSTAT=%s:mailto:%s\r\n", partStat, attendeeEmail)
	sb.WriteString("END:VEVENT\r\n")
	sb.WriteString("END:VCALENDAR\r\n")

	return sb.String(), nil
}

// BuildITIPRequest generates an iCalendar RFC 5545 / RFC 5546 string for a METHOD:REQUEST.
func BuildITIPRequest(event *CalendarEvent, organizerEmail string) (string, error) {
	if event == nil {
		return "", fmt.Errorf("event cannot be nil")
	}

	// The full JSCalendar→iCalendar serializer emits the complete VEVENT (recurrence,
	// alarms, locations, full participant metadata, all-day/timezone handling) so the
	// invitation is lossless for real clients.
	return encodeICalendar(event, "REQUEST", organizerEmail, "", ""), nil
}

// BuildITIPCancel generates an iCalendar RFC 5545 / RFC 5546 string for a METHOD:CANCEL notice.
func BuildITIPCancel(event *CalendarEvent, organizerEmail string) (string, error) {
	if event == nil {
		return "", fmt.Errorf("event cannot be nil")
	}

	// A CANCEL obsoletes prior revisions: the full VEVENT is emitted with STATUS:CANCELLED
	// and the carried SEQUENCE (RFC 5546 Sections 2.1.5 / 3.2.5).
	return encodeICalendar(event, "CANCEL", organizerEmail, "", "CANCELLED"), nil
}

// BuildITIPAdd generates an iCalendar RFC 5546 string for a METHOD:ADD request.
func BuildITIPAdd(event *CalendarEvent, organizerEmail string) (string, error) {
	if event == nil {
		return "", fmt.Errorf("event cannot be nil")
	}
	ics, err := BuildITIPRequest(event, organizerEmail)
	if err != nil {
		return "", err
	}
	return strings.Replace(ics, "METHOD:REQUEST", "METHOD:ADD", 1), nil
}

// BuildITIPRefresh generates an iCalendar RFC 5546 string for a METHOD:REFRESH request.
func BuildITIPRefresh(uid, attendeeEmail string) (string, error) {
	nowStr := time.Now().UTC().Format("20060102T150405Z")
	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//IMAP-JMAP Server//NONSGML v1.0//EN\r\n")
	sb.WriteString("METHOD:REFRESH\r\n")
	sb.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(&sb, "UID:%s\r\n", uid)
	fmt.Fprintf(&sb, "DTSTAMP:%s\r\n", nowStr)
	fmt.Fprintf(&sb, "ATTENDEE:mailto:%s\r\n", attendeeEmail)
	sb.WriteString("END:VEVENT\r\n")
	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String(), nil
}

// BuildITIPCounter generates an iCalendar RFC 5546 string for a METHOD:COUNTER proposal.
func BuildITIPCounter(event *CalendarEvent, attendeeEmail, proposedStart string) (string, error) {
	if event == nil {
		return "", fmt.Errorf("event cannot be nil")
	}
	uid := eventUID(event)
	nowStr := time.Now().UTC().Format("20060102T150405Z")

	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//IMAP-JMAP Server//NONSGML v1.0//EN\r\n")
	sb.WriteString("METHOD:COUNTER\r\n")
	sb.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(&sb, "UID:%s\r\n", uid)
	fmt.Fprintf(&sb, "DTSTAMP:%s\r\n", nowStr)
	if event.Title != "" {
		fmt.Fprintf(&sb, "SUMMARY:%s\r\n", event.Title)
	}
	if proposedStart != "" {
		startClean := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(proposedStart, "-", ""), ":", ""), ".000", "")
		fmt.Fprintf(&sb, "DTSTART:%s\r\n", startClean)
	}
	fmt.Fprintf(&sb, "ATTENDEE:mailto:%s\r\n", attendeeEmail)
	sb.WriteString("END:VEVENT\r\n")
	sb.WriteString("END:VCALENDAR\r\n")

	return sb.String(), nil
}

// ParseITIPMessage parses an iCalendar RFC 5546 string and extracts key fields.
func ParseITIPMessage(icsContent string) (*ITIPMessage, error) {
	lines := strings.Split(icsContent, "\n")
	msg := &ITIPMessage{
		Method: "REQUEST",
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "METHOD:") {
			msg.Method = strings.TrimPrefix(line, "METHOD:")
		} else if strings.HasPrefix(line, "UID:") {
			msg.UID = strings.TrimPrefix(line, "UID:")
		} else if strings.HasPrefix(line, "SUMMARY:") {
			msg.Summary = strings.TrimPrefix(line, "SUMMARY:")
		} else if strings.HasPrefix(line, "DTSTART:") {
			msg.Start = strings.TrimPrefix(line, "DTSTART:")
		} else if strings.HasPrefix(line, "ORGANIZER:") {
			org := strings.TrimPrefix(line, "ORGANIZER:")
			if idx := strings.Index(org, "mailto:"); idx != -1 {
				msg.Organizer = org[idx+7:]
			} else {
				msg.Organizer = org
			}
		} else if strings.Contains(line, "ATTENDEE;") || strings.HasPrefix(line, "ATTENDEE:") {
			if idx := strings.Index(line, "mailto:"); idx != -1 {
				email := line[idx+7:]
				msg.Attendees = append(msg.Attendees, EmailAddress{Email: email})
			}
			if idx := strings.Index(line, "PARTSTAT="); idx != -1 {
				part := line[idx+9:]
				if endIdx := strings.IndexAny(part, ";:"); endIdx != -1 {
					msg.Status = part[:endIdx]
				} else {
					msg.Status = part
				}
			}
		}
	}

	if msg.UID == "" {
		return nil, fmt.Errorf("invalid iTIP message: missing UID")
	}

	return msg, nil
}
