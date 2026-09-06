package jmap_test

import (
	"strings"
	"testing"

	"github.com/emersion/go-ical"

	"imap-jmap/jmap"
)

// TestRFC5545_Section3_1_LineEndings verifies CRLF (\r\n) line endings per RFC 5545 Section 3.1.
func TestRFC5545_Section3_1_LineEndings(t *testing.T) {
	ev := &jmap.CalendarEvent{
		ID:    "evt-crlf-1",
		Title: "Line Ending Test",
		Start: "2026-09-01T10:00:00Z",
	}

	ics, err := jmap.BuildITIPRequest(ev, "org@example.com")
	if err != nil {
		t.Fatalf("BuildITIPRequest failed: %v", err)
	}

	if !strings.Contains(ics, "\r\n") {
		t.Errorf("Expected CRLF (\\r\\n) line endings in iCalendar string per RFC 5545 Section 3.1")
	}
}

// TestRFC5545_Section3_8_4_AttendeeOrganizer verifies ATTENDEE and ORGANIZER parameter formatting per RFC 5545 Sections 3.8.4.1 & 3.8.4.3.
func TestRFC5545_Section3_8_4_AttendeeOrganizer(t *testing.T) {
	ev := &jmap.CalendarEvent{
		ID:          "evt-5545-spec",
		Title:       "RFC 5545 Compliance Check",
		Description: "Testing core iCalendar properties",
		Start:       "2026-09-01T15:00:00Z",
		Participants: map[string]*jmap.JSCalendarParticipant{
			"attendee@example.com": {
				Name:   "Attendee Name",
				Email:  "attendee@example.com",
				Status: "accepted",
			},
		},
	}

	reqICS, err := jmap.BuildITIPRequest(ev, "organizer@example.com")
	if err != nil {
		t.Fatalf("BuildITIPRequest failed: %v", err)
	}

	dec := ical.NewDecoder(strings.NewReader(reqICS))
	cal, err := dec.Decode()
	if err != nil {
		t.Fatalf("Decode iCalendar failed: %v", err)
	}
	events := cal.Events()
	if len(events) == 0 {
		t.Fatalf("No VEVENT found in iCalendar output")
	}
	vevent := events[0]
	org := vevent.Props.Get(ical.PropOrganizer)
	if org == nil || org.Value != "mailto:organizer@example.com" {
		t.Errorf("Expected ORGANIZER mailto:organizer@example.com, got %v", org)
	}
	att := vevent.Props.Get(ical.PropAttendee)
	if att == nil {
		t.Fatalf("Expected ATTENDEE property in VEVENT")
	}
	if att.Value != "mailto:attendee@example.com" {
		t.Errorf("Expected ATTENDEE value mailto:attendee@example.com, got %q", att.Value)
	}
	if att.Params.Get("CUTYPE") != "INDIVIDUAL" {
		t.Errorf("Expected CUTYPE=INDIVIDUAL, got %q", att.Params.Get("CUTYPE"))
	}
	if att.Params.Get("ROLE") != "REQ-PARTICIPANT" {
		t.Errorf("Expected ROLE=REQ-PARTICIPANT, got %q", att.Params.Get("ROLE"))
	}
	if att.Params.Get("PARTSTAT") != "ACCEPTED" {
		t.Errorf("Expected PARTSTAT=ACCEPTED, got %q", att.Params.Get("PARTSTAT"))
	}
	if att.Params.Get("CN") != "Attendee Name" {
		t.Errorf("Expected CN='Attendee Name', got %q", att.Params.Get("CN"))
	}
}

// TestRFC5545_Section3_3_DateTimeFormatting verifies UTC date-time string formatting per RFC 5545 Section 3.3.5.
func TestRFC5545_Section3_3_DateTimeFormatting(t *testing.T) {
	ev := &jmap.CalendarEvent{
		ID:    "evt-dt-1",
		Title: "DateTime Check",
		Start: "2026-12-25T18:30:00Z",
	}

	ics, err := jmap.BuildITIPRequest(ev, "org@example.com")
	if err != nil {
		t.Fatalf("BuildITIPRequest failed: %v", err)
	}

	if !strings.Contains(ics, "DTSTART:20261225T183000Z") {
		t.Errorf("Expected DTSTART:20261225T183000Z in iCalendar output, got:\n%s", ics)
	}
}
