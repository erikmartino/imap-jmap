package jmap_test

import (
	"strings"
	"testing"

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

	mandatoryFields := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:",
		"BEGIN:VEVENT",
		"UID:evt-5545-spec",
		"DTSTAMP:",
		"SUMMARY:RFC 5545 Compliance Check",
		"DESCRIPTION:Testing core iCalendar properties",
		"ORGANIZER:mailto:organizer@example.com",
		"ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=REQ-PARTICIPANT;PARTSTAT=ACCEPTED;CN=Attendee Name:mailto:attendee@example.com",
		"END:VEVENT",
		"END:VCALENDAR",
	}

	for _, field := range mandatoryFields {
		if !strings.Contains(reqICS, field) {
			t.Errorf("Expected mandatory RFC 5545 field %q in iCalendar output:\n%s", field, reqICS)
		}
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
