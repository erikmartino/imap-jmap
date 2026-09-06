package jmap

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// ITIPMessage represents a parsed iTIP (RFC 5546) / iMIP (RFC 6047) scheduling message.
type ITIPMessage struct {
	Method    string         `json:"method"` // "REQUEST", "REPLY", "CANCEL"
	UID       string         `json:"uid"`
	Sequence  uint32         `json:"sequence"`
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

	partStat := strings.ToUpper(status)
	if partStat != "ACCEPTED" && partStat != "DECLINED" && partStat != "TENTATIVE" {
		partStat = "ACCEPTED"
	}

	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//IMAP-JMAP Server//NONSGML v1.0//EN")
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropMethod, "REPLY")

	comp := ical.NewComponent(ical.CompEvent)
	comp.Props.SetText(ical.PropUID, eventUID(event))
	comp.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())

	seqProp := ical.NewProp(ical.PropSequence)
	seqProp.Value = fmt.Sprintf("%d", event.Sequence)
	comp.Props.Set(seqProp)

	if event.Title != "" {
		comp.Props.SetText(ical.PropSummary, event.Title)
	}
	if event.Start != "" {
		addDateTimeProp(comp, ical.PropDateTimeStart, event.Start, event.TimeZone, event.ShowWithoutTime)
	}
	if org := organizerAddress(event); org != "" {
		comp.Props.Set(newRawProp(ical.PropOrganizer, "mailto:"+org))
	}

	attProp := ical.NewProp(ical.PropAttendee)
	attProp.Value = "mailto:" + attendeeEmail
	attProp.Params.Set("CUTYPE", "INDIVIDUAL")
	attProp.Params.Set("ROLE", "REQ-PARTICIPANT")
	attProp.Params.Set("PARTSTAT", partStat)
	comp.Props.Add(attProp)

	cal.Children = append(cal.Children, comp)

	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildITIPRequest generates an iCalendar RFC 5545 / RFC 5546 string for a METHOD:REQUEST.
func BuildITIPRequest(event *CalendarEvent, organizerEmail string) (string, error) {
	if event == nil {
		return "", fmt.Errorf("event cannot be nil")
	}
	cal := CalendarEventToICalendar(event, "REQUEST", organizerEmail, "", "")
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildITIPCancel generates an iCalendar RFC 5545 / RFC 5546 string for a METHOD:CANCEL notice.
func BuildITIPCancel(event *CalendarEvent, organizerEmail string) (string, error) {
	if event == nil {
		return "", fmt.Errorf("event cannot be nil")
	}
	cal := CalendarEventToICalendar(event, "CANCEL", organizerEmail, "", "CANCELLED")
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildITIPAdd generates an iCalendar RFC 5546 string for a METHOD:ADD request.
func BuildITIPAdd(event *CalendarEvent, organizerEmail string) (string, error) {
	if event == nil {
		return "", fmt.Errorf("event cannot be nil")
	}
	cal := CalendarEventToICalendar(event, "ADD", organizerEmail, "", "")
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildITIPRefresh generates an iCalendar RFC 5546 string for a METHOD:REFRESH request.
func BuildITIPRefresh(uid, attendeeEmail string) (string, error) {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//IMAP-JMAP Server//NONSGML v1.0//EN")
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropMethod, "REFRESH")

	comp := ical.NewComponent(ical.CompEvent)
	comp.Props.SetText(ical.PropUID, uid)
	comp.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())

	attProp := ical.NewProp(ical.PropAttendee)
	attProp.Value = "mailto:" + attendeeEmail
	comp.Props.Add(attProp)

	cal.Children = append(cal.Children, comp)

	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildITIPCounter generates an iCalendar RFC 5546 string for a METHOD:COUNTER proposal.
func BuildITIPCounter(event *CalendarEvent, attendeeEmail, proposedStart string) (string, error) {
	if event == nil {
		return "", fmt.Errorf("event cannot be nil")
	}
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//IMAP-JMAP Server//NONSGML v1.0//EN")
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropMethod, "COUNTER")

	comp := ical.NewComponent(ical.CompEvent)
	comp.Props.SetText(ical.PropUID, eventUID(event))
	comp.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())

	if event.Title != "" {
		comp.Props.SetText(ical.PropSummary, event.Title)
	}
	if proposedStart != "" {
		addDateTimeProp(comp, ical.PropDateTimeStart, proposedStart, event.TimeZone, event.ShowWithoutTime)
	}

	attProp := ical.NewProp(ical.PropAttendee)
	attProp.Value = "mailto:" + attendeeEmail
	comp.Props.Add(attProp)

	cal.Children = append(cal.Children, comp)

	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ParseITIPMessage parses an iCalendar RFC 5546 string and extracts key fields using go-ical.
func ParseITIPMessage(icsContent string) (*ITIPMessage, error) {
	cal, err := ical.NewDecoder(strings.NewReader(icsContent)).Decode()
	if err != nil {
		return nil, fmt.Errorf("invalid iTIP message: %w", err)
	}

	msg := &ITIPMessage{
		Method: "REQUEST",
	}
	if m := cal.Props.Get(ical.PropMethod); m != nil && m.Value != "" {
		msg.Method = strings.ToUpper(m.Value)
	}

	var evComp *ical.Component
	for _, child := range cal.Children {
		if child.Name == ical.CompEvent {
			evComp = child
			break
		}
	}
	if evComp == nil {
		return nil, fmt.Errorf("invalid iTIP message: missing VEVENT")
	}

	if uidProp := evComp.Props.Get(ical.PropUID); uidProp != nil && uidProp.Value != "" {
		msg.UID = uidProp.Value
	}
	if msg.UID == "" {
		return nil, fmt.Errorf("invalid iTIP message: missing UID")
	}

	if seqProp := evComp.Props.Get(ical.PropSequence); seqProp != nil {
		var seq uint32
		_, _ = fmt.Sscanf(seqProp.Value, "%d", &seq)
		msg.Sequence = seq
	}
	if sumProp := evComp.Props.Get(ical.PropSummary); sumProp != nil {
		if text, err := sumProp.Text(); err == nil {
			msg.Summary = text
		} else {
			msg.Summary = sumProp.Value
		}
	}
	if dtStartProp := evComp.Props.Get(ical.PropDateTimeStart); dtStartProp != nil {
		msg.Start = dtStartProp.Value
	}
	if dtEndProp := evComp.Props.Get(ical.PropDateTimeEnd); dtEndProp != nil {
		msg.End = dtEndProp.Value
	}
	if orgProp := evComp.Props.Get(ical.PropOrganizer); orgProp != nil {
		org := orgProp.Value
		if strings.HasPrefix(strings.ToLower(org), "mailto:") {
			org = org[7:]
		}
		msg.Organizer = org
	}
	for _, attProp := range evComp.Props[ical.PropAttendee] {
		addr := attProp.Value
		if strings.HasPrefix(strings.ToLower(addr), "mailto:") {
			addr = addr[7:]
		}
		if addr != "" {
			msg.Attendees = append(msg.Attendees, EmailAddress{Email: addr})
		}
		if partStat := attProp.Params.Get("PARTSTAT"); partStat != "" && msg.Status == "" {
			msg.Status = partStat
		}
	}

	return msg, nil
}
