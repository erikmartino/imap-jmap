package jmap_test

import (
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC5546_ITIPRoundTripFullFidelity builds a rich CalendarEvent into an iTIP REQUEST
// and parses it back, asserting the properties a real client depends on survive the
// JSCalendar↔iCalendar conversion (RFC 8984 ⇄ RFC 5545): timezone, duration, status,
// privacy, free/busy, priority, colour, location, categories, recurrence, participants
// (roles/PARTSTAT/RSVP), the organizer, and alerts.
func TestRFC5546_ITIPRoundTripFullFidelity(t *testing.T) {
	spectest.Require(t, "RFC5546", "3.2.2", spectest.MUST,
		"A REQUEST carries the full event definition (recurrence, participants, alarms, location).")
	spectest.Require(t, "RFC5545", "3.6.1", spectest.MUST,
		"VEVENT properties round-trip to and from JSCalendar without loss of the core fields.")

	src := &jmap.CalendarEvent{
		UID:            "roundtrip-uid@example.com",
		Title:          "Quarterly Planning",
		Description:    "Agenda and goals",
		Start:          "2026-09-01T10:00:00",
		Duration:       "PT1H30M",
		TimeZone:       "Europe/Paris",
		Status:         "confirmed",
		Privacy:        "private",
		FreeBusyStatus: "busy",
		Priority:       5,
		Color:          "#ff0000",
		Sequence:       2,
		Locations:      map[string]*jmap.JSCalendarLocation{"l1": {Type: "Location", Name: "HQ Room 1"}},
		Categories:     map[string]bool{"work": true},
		RecurrenceRules: []*jmap.JSCalendarRecurrenceRule{{
			Type: "RecurrenceRule", Frequency: "weekly", Interval: 2, Count: 5,
			ByDay: []*jmap.NDay{{Day: "mo"}, {Day: "we"}},
		}},
		Participants: map[string]*jmap.JSCalendarParticipant{
			"alice@example.com": {Email: "alice@example.com", Roles: map[string]bool{"owner": true}},
			"bob@example.com": {
				Email: "bob@example.com", Name: "Bob", Roles: map[string]bool{"attendee": true},
				ParticipationStatus: "accepted", ExpectReply: true,
			},
		},
		Alerts: map[string]*jmap.JSCalendarAlert{
			"a1": {Type: "Alert", Action: "display", Description: "Reminder",
				Trigger: map[string]any{"@type": "OffsetTrigger", "offset": "-PT15M"}},
		},
	}

	ics, err := jmap.BuildITIPRequest(src, "alice@example.com")
	if err != nil {
		t.Fatalf("BuildITIPRequest: %v", err)
	}

	events, err := jmap.ParseICalendar([]byte(ics))
	if err != nil {
		t.Fatalf("ParseICalendar: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]

	if got.UID != src.UID {
		t.Errorf("uid = %q, want %q", got.UID, src.UID)
	}
	if got.Title != "Quarterly Planning" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Description != "Agenda and goals" {
		t.Errorf("description = %q", got.Description)
	}
	if got.Start != "2026-09-01T10:00:00" {
		t.Errorf("start = %q, want floating 2026-09-01T10:00:00", got.Start)
	}
	if got.TimeZone != "Europe/Paris" {
		t.Errorf("timeZone = %q", got.TimeZone)
	}
	if got.Duration != "PT1H30M" {
		t.Errorf("duration = %q", got.Duration)
	}
	if got.Status != "confirmed" {
		t.Errorf("status = %q", got.Status)
	}
	if got.Privacy != "private" {
		t.Errorf("privacy = %q", got.Privacy)
	}
	if got.FreeBusyStatus != "busy" {
		t.Errorf("freeBusyStatus = %q", got.FreeBusyStatus)
	}
	if got.Priority != 5 {
		t.Errorf("priority = %d", got.Priority)
	}
	if got.Color != "#ff0000" {
		t.Errorf("color = %q", got.Color)
	}
	if got.Sequence != 2 {
		t.Errorf("sequence = %d", got.Sequence)
	}
	if !got.Categories["work"] {
		t.Errorf("categories missing 'work': %v", got.Categories)
	}
	locOK := false
	for _, l := range got.Locations {
		if l.Name == "HQ Room 1" {
			locOK = true
		}
	}
	if !locOK {
		t.Errorf("location not round-tripped: %v", got.Locations)
	}
	if len(got.RecurrenceRules) != 1 {
		t.Fatalf("expected 1 recurrenceRule, got %d", len(got.RecurrenceRules))
	}
	rr := got.RecurrenceRules[0]
	if rr.Frequency != "weekly" || rr.Interval != 2 || rr.Count != 5 {
		t.Errorf("rrule core = freq %q interval %d count %d", rr.Frequency, rr.Interval, rr.Count)
	}
	if len(rr.ByDay) != 2 {
		t.Errorf("rrule byDay = %v, want [mo we]", rr.ByDay)
	}
	// Organizer (owner) and attendee both survive.
	if p := got.Participants["alice@example.com"]; p == nil || !(p.Roles["owner"] || p.Role == "owner") {
		t.Errorf("organizer/owner not round-tripped: %v", got.Participants["alice@example.com"])
	}
	bob := got.Participants["bob@example.com"]
	if bob == nil {
		t.Fatalf("attendee bob not round-tripped")
	}
	if bob.ParticipationStatus != "accepted" {
		t.Errorf("bob participationStatus = %q", bob.ParticipationStatus)
	}
	if bob.Name != "Bob" {
		t.Errorf("bob CN = %q", bob.Name)
	}
	if !bob.ExpectReply {
		t.Errorf("bob RSVP/expectReply not round-tripped")
	}
	if !(bob.Roles["attendee"] || bob.Role == "attendee") {
		t.Errorf("bob role = %v", bob.Roles)
	}
	if len(got.Alerts) != 1 {
		t.Errorf("expected 1 alert, got %d", len(got.Alerts))
	}
	for _, a := range got.Alerts {
		trig, _ := a.Trigger.(map[string]any)
		if trig["offset"] != "-PT15M" {
			t.Errorf("alert offset = %v, want -PT15M", trig["offset"])
		}
	}
}

// TestRFC5545_TextEscapingRoundTrip verifies RFC 5545 Section 3.3.11 TEXT escaping:
// commas, semicolons, backslashes and newlines in a value survive build→parse intact.
func TestRFC5545_TextEscapingRoundTrip(t *testing.T) {
	spectest.Require(t, "RFC5545", "3.3.11", spectest.MUST,
		"TEXT values escape backslash, comma, semicolon and newline, and unescape on read.")

	title := `Q3, Plan; "review" \ done`
	desc := "line one\nline two; with, punctuation"
	src := &jmap.CalendarEvent{UID: "esc@example.com", Title: title, Description: desc, Start: "2026-09-01T10:00:00Z"}

	ics, err := jmap.BuildITIPRequest(src, "org@example.com")
	if err != nil {
		t.Fatalf("BuildITIPRequest: %v", err)
	}
	// The raw ICS must contain the escaped forms, never a bare separator that would
	// corrupt the line structure.
	if !strings.Contains(ics, `SUMMARY:Q3\, Plan\; "review" \\ done`) {
		t.Errorf("SUMMARY not escaped correctly:\n%s", ics)
	}
	if strings.Contains(ics, "DESCRIPTION:line one\nline two") {
		t.Errorf("DESCRIPTION contains a raw newline (must be \\n):\n%s", ics)
	}

	events, err := jmap.ParseICalendar([]byte(ics))
	if err != nil {
		t.Fatalf("ParseICalendar: %v", err)
	}
	if events[0].Title != title {
		t.Errorf("title round-trip = %q, want %q", events[0].Title, title)
	}
	if events[0].Description != desc {
		t.Errorf("description round-trip = %q, want %q", events[0].Description, desc)
	}
}
