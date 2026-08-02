package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC5546_BuildAndParseReply tests RFC 5546 iTIP METHOD:REPLY building & parsing.
func TestRFC5546_BuildAndParseReply(t *testing.T) {
	ev := &jmap.CalendarEvent{
		ID:    "evt-5546-1",
		Title: "RFC 5546 Review",
		Start: "2026-08-15T10:00:00Z",
		Participants: map[string]*jmap.JSCalendarParticipant{
			"attendee@example.com": {
				Name:  "Attendee",
				Email: "attendee@example.com",
				Role:  "attendee",
			},
		},
	}

	replyICS, err := jmap.BuildITIPReply(ev, "attendee@example.com", "accepted")
	if err != nil {
		t.Fatalf("BuildITIPReply failed: %v", err)
	}

	if !strings.Contains(replyICS, "METHOD:REPLY") {
		t.Errorf("Expected METHOD:REPLY in generated ICS")
	}
	if !strings.Contains(replyICS, "PARTSTAT=ACCEPTED") {
		t.Errorf("Expected PARTSTAT=ACCEPTED in generated ICS")
	}

	msg, err := jmap.ParseITIPMessage(replyICS)
	if err != nil {
		t.Fatalf("ParseITIPMessage failed: %v", err)
	}

	if msg.Method != "REPLY" {
		t.Errorf("Expected Method REPLY, got %s", msg.Method)
	}
	if msg.UID != "evt-5546-1" {
		t.Errorf("Expected UID evt-5546-1, got %s", msg.UID)
	}
	if msg.Status != "ACCEPTED" {
		t.Errorf("Expected Status ACCEPTED, got %s", msg.Status)
	}
}

// TestRFC5546_BuildRequestAndCancel tests RFC 5546 METHOD:REQUEST and METHOD:CANCEL generation.
func TestRFC5546_BuildRequestAndCancel(t *testing.T) {
	ev := &jmap.CalendarEvent{
		ID:          "evt-5546-2",
		Title:       "Architecture Sync",
		Description: "Spec validation review",
		Start:       "2026-08-16T14:00:00Z",
		Participants: map[string]*jmap.JSCalendarParticipant{
			"alice@example.com": {
				Name:   "Alice",
				Email:  "alice@example.com",
				Status: "needs-action",
			},
		},
	}

	// 1. Build REQUEST
	reqICS, err := jmap.BuildITIPRequest(ev, "organizer@example.com")
	if err != nil {
		t.Fatalf("BuildITIPRequest failed: %v", err)
	}
	if !strings.Contains(reqICS, "METHOD:REQUEST") {
		t.Errorf("Expected METHOD:REQUEST in generated ICS")
	}

	// 2. Build CANCEL
	cancelICS, err := jmap.BuildITIPCancel(ev, "organizer@example.com")
	if err != nil {
		t.Fatalf("BuildITIPCancel failed: %v", err)
	}
	if !strings.Contains(cancelICS, "METHOD:CANCEL") {
		t.Errorf("Expected METHOD:CANCEL in generated ICS")
	}
	if !strings.Contains(cancelICS, "STATUS:CANCELLED") {
		t.Errorf("Expected STATUS:CANCELLED in generated ICS")
	}
}

// TestRFC5546_CalendarEvent_ParseInvitation tests CalendarEvent/parseInvitation handler per RFC 5546.
func TestRFC5546_CalendarEvent_ParseInvitation(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	rawICS := `BEGIN:VCALENDAR
VERSION:2.0
METHOD:REQUEST
BEGIN:VEVENT
UID:evt-rfc5546-99
SUMMARY:Design Review
ORGANIZER:mailto:lead@example.com
DTSTART:20260825T110000Z
ATTENDEE;PARTSTAT=NEEDS-ACTION:mailto:dev@example.com
END:VEVENT
END:VCALENDAR`

	parseReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"CalendarEvent/parseInvitation",
				map[string]any{
					"accountId": "primary",
					"content":   rawICS,
				},
				"c1",
			},
		},
	}

	bodyBytes, _ := json.Marshal(parseReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST parseInvitation failed: %v", err)
	}
	defer resp.Body.Close()

	var parseResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&parseResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	parseArgs := parseResp.MethodResponses[0].Args
	if parseArgs["method"] != "REQUEST" {
		t.Errorf("Expected method REQUEST, got %v", parseArgs["method"])
	}
	if parseArgs["uid"] != "evt-rfc5546-99" {
		t.Errorf("Expected UID evt-rfc5546-99, got %v", parseArgs["uid"])
	}
}

// TestRFC5546_AddRefreshCounter tests RFC 5546 METHOD:ADD, METHOD:REFRESH, and METHOD:COUNTER methods.
func TestRFC5546_AddRefreshCounter(t *testing.T) {
	ev := &jmap.CalendarEvent{
		ID:    "evt-5546-arc",
		Title: "Team Retrospective",
		Start: "2026-09-10T16:00:00Z",
	}

	// 1. ADD
	addICS, err := jmap.BuildITIPAdd(ev, "organizer@example.com")
	if err != nil || !strings.Contains(addICS, "METHOD:ADD") {
		t.Errorf("BuildITIPAdd failed or missing METHOD:ADD: %v", err)
	}

	msgAdd, err := jmap.ParseITIPMessage(addICS)
	if err != nil || msgAdd.Method != "ADD" {
		t.Errorf("ParseITIPMessage for ADD failed: %v", err)
	}

	// 2. REFRESH
	refICS, err := jmap.BuildITIPRefresh("evt-5546-arc", "user@example.com")
	if err != nil || !strings.Contains(refICS, "METHOD:REFRESH") {
		t.Errorf("BuildITIPRefresh failed or missing METHOD:REFRESH: %v", err)
	}

	msgRef, err := jmap.ParseITIPMessage(refICS)
	if err != nil || msgRef.Method != "REFRESH" {
		t.Errorf("ParseITIPMessage for REFRESH failed: %v", err)
	}

	// 3. COUNTER
	cntICS, err := jmap.BuildITIPCounter(ev, "user@example.com", "2026-09-10T17:00:00Z")
	if err != nil || !strings.Contains(cntICS, "METHOD:COUNTER") {
		t.Errorf("BuildITIPCounter failed or missing METHOD:COUNTER: %v", err)
	}

	msgCnt, err := jmap.ParseITIPMessage(cntICS)
	if err != nil || msgCnt.Method != "COUNTER" {
		t.Errorf("ParseITIPMessage for COUNTER failed: %v", err)
	}
}

