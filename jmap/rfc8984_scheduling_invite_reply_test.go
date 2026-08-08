package jmap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/spectest"
)

// schedulingTestServer returns a JMAP server backed by fresh calendar + mail backends,
// plus the mail backend so tests can inspect the iMIP messages the server dispatched.
func schedulingTestServer(t *testing.T) (*httptest.Server, *memory.MemoryBackend) {
	t.Helper()
	calBackend := memory.NewMemoryCalendarsBackend()
	mailBackend := memory.NewMemoryBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend), jmap.WithMailBackend(mailBackend))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, mailBackend
}

// schedulingEmails returns only the iMIP scheduling messages the server dispatched
// (those carrying a text/calendar body part), ignoring any seeded sample mail — a real
// client likewise recognises a scheduling message by its text/calendar content type.
func schedulingEmails(t *testing.T, mb *memory.MemoryBackend) []*jmap.Email {
	t.Helper()
	emails, err := mb.GetAllEmails(context.Background())
	if err != nil {
		t.Fatalf("GetAllEmails: %v", err)
	}
	out := make([]*jmap.Email, 0, len(emails))
	for _, em := range emails {
		if len(em.TextBody) > 0 && strings.Contains(em.TextBody[0].Type, "text/calendar") {
			out = append(out, em)
		}
	}
	return out
}

func emailBody(em *jmap.Email) string {
	if len(em.BodyValues) == 0 {
		return ""
	}
	if v, ok := em.BodyValues["1"]; ok {
		return v.Value
	}
	for _, v := range em.BodyValues {
		return v.Value
	}
	return ""
}

func setCalendarEvent(t *testing.T, tsURL string, args map[string]any) jmap.Response {
	t.Helper()
	req := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"CalendarEvent/set", args, "c1"},
		},
	}
	body, _ := json.Marshal(req)
	resp, err := authedPost(tsURL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	defer resp.Body.Close()
	var out jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

// TestRFC8984_SchedulingRequestExcludesOwner verifies that a REQUEST (iMIP invitation)
// is sent to every participant EXCEPT the calendar owner/organizer
// (draft-ietf-jmap-calendars-27 Section 5.9.2.1).
func TestRFC8984_SchedulingRequestExcludesOwner(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.9.2.1", spectest.MUST,
		"On create, the origin sends a REQUEST to every current participant except the calendar owner.")
	spectest.Require(t, "RFC6047", "2.4", spectest.MUST,
		"The iMIP body part is text/calendar with a method parameter matching the iCalendar METHOD.")

	ts, mailBackend := schedulingTestServer(t)

	setCalendarEvent(t, ts.URL, map[string]any{
		"accountId":              "primary",
		"sendSchedulingMessages": true,
		"create": map[string]any{
			"e1": map[string]any{
				"title": "Owner Excluded Sync",
				"start": "2026-09-10T10:00:00Z",
				"participants": map[string]any{
					"user@example.com": map[string]any{
						"email": "user@example.com",
						"roles": map[string]any{"owner": true},
					},
					"bob@example.com": map[string]any{
						"email":               "bob@example.com",
						"roles":               map[string]any{"attendee": true},
						"participationStatus": "needs-action",
					},
				},
			},
		},
	})

	emails := schedulingEmails(t, mailBackend)
	toBob, toOwner := 0, 0
	for _, em := range emails {
		for _, addr := range em.To {
			switch addr.Email {
			case "bob@example.com":
				toBob++
				if len(em.TextBody) == 0 || em.TextBody[0].Type != "text/calendar; method=REQUEST" {
					t.Errorf("REQUEST body part type = %v, want text/calendar; method=REQUEST", em.TextBody)
				}
			case "user@example.com":
				toOwner++
			}
		}
	}
	if toBob != 1 {
		t.Errorf("attendee bob should get exactly one REQUEST, got %d", toBob)
	}
	if toOwner != 0 {
		t.Errorf("calendar owner must NOT receive a REQUEST, got %d", toOwner)
	}
}

// TestRFC8984_SchedulingNoSupportedScheduleMethods verifies that requesting scheduling
// messages when the server has no way to send them is rejected with the
// noSupportedScheduleMethods SetError (draft-ietf-jmap-calendars-27 Section 5.9).
func TestRFC8984_SchedulingNoSupportedScheduleMethods(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.9", spectest.MUST,
		"noSupportedScheduleMethods is returned when scheduling is requested but no schedule method is available.")

	// A calendar backend but NO mail backend: there is no method to send iTIP.
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := setCalendarEvent(t, ts.URL, map[string]any{
		"accountId":              "primary",
		"sendSchedulingMessages": true,
		"create": map[string]any{
			"e1": map[string]any{
				"title": "No Transport",
				"start": "2026-09-18T10:00:00Z",
				"participants": map[string]any{
					"bob@example.com": map[string]any{"email": "bob@example.com"},
				},
			},
		},
	})
	notCreated, ok := resp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok || notCreated["e1"] == nil {
		t.Fatalf("expected e1 in notCreated, got %+v", resp.MethodResponses[0].Args)
	}
	errObj, _ := notCreated["e1"].(map[string]any)
	if errObj["type"] != "noSupportedScheduleMethods" {
		t.Errorf("SetError type = %v, want noSupportedScheduleMethods", errObj["type"])
	}
}

// TestRFC8984_SchedulingReplyOnRSVP verifies the RSVP flow: when a (non-owner) participant
// changes their own participationStatus to a value other than needs-action, the server sends
// an iTIP REPLY to the organizer, and does NOT re-send a REQUEST
// (draft-ietf-jmap-calendars-27 Section 5.9.2.3; RFC 5546 Section 3.2.3).
func TestRFC8984_SchedulingReplyOnRSVP(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.9.2.3", spectest.MUST,
		"When not the origin, a REPLY is sent to the organizer for each of the user's participants whose participationStatus changes to a value other than needs-action.")
	spectest.Require(t, "RFC5546", "3.2.3", spectest.MUST,
		"A REPLY carries the ORGANIZER being answered and the replying ATTENDEE with its PARTSTAT.")

	ts, mailBackend := schedulingTestServer(t)

	// Create the event WITHOUT scheduling so only the RSVP produces a message.
	createResp := setCalendarEvent(t, ts.URL, map[string]any{
		"accountId": "primary",
		"create": map[string]any{
			"e1": map[string]any{
				"title":   "Quarterly Review",
				"start":   "2026-09-12T09:00:00Z",
				"replyTo": map[string]any{"imip": "mailto:organizer@example.com"},
				"participants": map[string]any{
					"organizer@example.com": map[string]any{
						"email": "organizer@example.com",
						"roles": map[string]any{"owner": true},
					},
					"alice@example.com": map[string]any{
						"email":               "alice@example.com",
						"roles":               map[string]any{"attendee": true},
						"participationStatus": "needs-action",
					},
				},
			},
		},
	})
	created, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["e1"] == nil {
		t.Fatalf("create failed: %+v", createResp.MethodResponses[0].Args)
	}
	evID := created["e1"].(map[string]any)["id"].(string)
	uid, _ := created["e1"].(map[string]any)["uid"].(string)

	if got := len(schedulingEmails(t, mailBackend)); got != 0 {
		t.Fatalf("no scheduling messages expected before RSVP, got %d", got)
	}

	// Alice RSVPs "accepted" with scheduling on.
	setCalendarEvent(t, ts.URL, map[string]any{
		"accountId":              "primary",
		"sendSchedulingMessages": true,
		"update": map[string]any{
			evID: map[string]any{
				"participants/alice@example.com/participationStatus": "accepted",
			},
		},
	})

	emails := schedulingEmails(t, mailBackend)
	var reply *jmap.Email
	for _, em := range emails {
		if len(em.TextBody) > 0 && strings.Contains(em.TextBody[0].Type, "method=REPLY") {
			reply = em
		}
		for _, addr := range em.To {
			if addr.Email == "alice@example.com" {
				t.Errorf("an RSVP must not re-send a REQUEST to the attendee (alice)")
			}
		}
	}
	if reply == nil {
		t.Fatalf("expected an iTIP REPLY email after RSVP, got %d emails", len(emails))
	}
	if len(reply.To) != 1 || reply.To[0].Email != "organizer@example.com" {
		t.Errorf("REPLY must be addressed to the organizer, got %+v", reply.To)
	}
	body := emailBody(reply)
	for _, want := range []string{"METHOD:REPLY", "PARTSTAT=ACCEPTED", "ORGANIZER:mailto:organizer@example.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("REPLY body missing %q; body:\n%s", want, body)
		}
	}
	// The REPLY MUST correlate to the event via its uid (RFC 5546 Section 2.1.5).
	if uid != "" && !strings.Contains(body, "UID:"+uid) {
		t.Errorf("REPLY body must carry the event uid %q; body:\n%s", uid, body)
	}
}

// TestRFC8984_SchedulingHideAttendees verifies that when hideAttendees is set, each REQUEST
// contains only its recipient (plus the owner) — no attendee sees the others
// (draft-ietf-jmap-calendars-27 Section 5.9.2.1).
func TestRFC8984_SchedulingHideAttendees(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.9.2.1", spectest.MUST,
		"With hideAttendees, the recipient MUST be the only attendee in the REQUEST; all others are omitted.")

	ts, mailBackend := schedulingTestServer(t)

	setCalendarEvent(t, ts.URL, map[string]any{
		"accountId":              "primary",
		"sendSchedulingMessages": true,
		"create": map[string]any{
			"e1": map[string]any{
				"title":         "Confidential Briefing",
				"start":         "2026-09-15T14:00:00Z",
				"hideAttendees": true,
				"participants": map[string]any{
					"user@example.com": map[string]any{
						"email": "user@example.com",
						"roles": map[string]any{"owner": true},
					},
					"bob@example.com": map[string]any{
						"email": "bob@example.com",
						"roles": map[string]any{"attendee": true},
					},
					"carol@example.com": map[string]any{
						"email": "carol@example.com",
						"roles": map[string]any{"attendee": true},
					},
				},
			},
		},
	})

	seen := map[string]string{}
	for _, em := range schedulingEmails(t, mailBackend) {
		for _, addr := range em.To {
			seen[addr.Email] = emailBody(em)
		}
	}
	if _, ok := seen["bob@example.com"]; !ok {
		t.Fatalf("bob did not receive a REQUEST")
	}
	if _, ok := seen["carol@example.com"]; !ok {
		t.Fatalf("carol did not receive a REQUEST")
	}
	if strings.Contains(seen["bob@example.com"], "carol@example.com") {
		t.Errorf("hideAttendees leaked carol into bob's REQUEST:\n%s", seen["bob@example.com"])
	}
	if strings.Contains(seen["carol@example.com"], "bob@example.com") {
		t.Errorf("hideAttendees leaked bob into carol's REQUEST:\n%s", seen["carol@example.com"])
	}
}
