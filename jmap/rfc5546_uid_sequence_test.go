package jmap_test

import (
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC5546_ITIPUsesEventUIDAndSequence verifies that iTIP messages carry the event's
// stable "uid" property (not the server-assigned JMAP id) and its SEQUENCE, the correlation
// keys a peer uses to match a version of a component (RFC 5546 Section 2.1.5).
func TestRFC5546_ITIPUsesEventUIDAndSequence(t *testing.T) {
	spectest.Require(t, "RFC5546", "2.1.5", spectest.MUST,
		"UID (with SEQUENCE) identifies a specific revision of a calendar component across systems.")

	ev := &jmap.CalendarEvent{
		ID:       "evt-42",
		UID:      "9c2b-uid@example.com",
		Title:    "Design Review",
		Start:    "2026-09-01T10:00:00Z",
		Sequence: 3,
		Participants: map[string]*jmap.JSCalendarParticipant{
			"attendee@example.com": {Email: "attendee@example.com"},
		},
	}

	req, err := jmap.BuildITIPRequest(ev, "organizer@example.com")
	if err != nil {
		t.Fatalf("BuildITIPRequest: %v", err)
	}
	if !strings.Contains(req, "UID:9c2b-uid@example.com") {
		t.Errorf("REQUEST must use the event uid, not the JMAP id; got:\n%s", req)
	}
	if strings.Contains(req, "UID:evt-42") {
		t.Errorf("REQUEST must NOT use the JMAP id as UID; got:\n%s", req)
	}
	if !strings.Contains(req, "SEQUENCE:3") {
		t.Errorf("REQUEST must carry SEQUENCE:3; got:\n%s", req)
	}

	// The parsed UID round-trips to the same correlation key.
	msg, err := jmap.ParseITIPMessage(req)
	if err != nil {
		t.Fatalf("ParseITIPMessage: %v", err)
	}
	if msg.UID != "9c2b-uid@example.com" {
		t.Errorf("parsed UID = %q, want the event uid", msg.UID)
	}

	cancel, err := jmap.BuildITIPCancel(ev, "organizer@example.com")
	if err != nil {
		t.Fatalf("BuildITIPCancel: %v", err)
	}
	if !strings.Contains(cancel, "UID:9c2b-uid@example.com") || !strings.Contains(cancel, "SEQUENCE:3") {
		t.Errorf("CANCEL must carry the event uid and SEQUENCE; got:\n%s", cancel)
	}

	reply, err := jmap.BuildITIPReply(ev, "attendee@example.com", "accepted")
	if err != nil {
		t.Fatalf("BuildITIPReply: %v", err)
	}
	if !strings.Contains(reply, "UID:9c2b-uid@example.com") {
		t.Errorf("REPLY must use the event uid; got:\n%s", reply)
	}
}
