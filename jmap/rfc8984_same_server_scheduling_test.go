package jmap_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// postAs performs an authenticated JMAP request as an arbitrary local user (the memory
// auth backend accepts username == password), so a single server can be driven as two
// different accounts within one test.
func postAs(t *testing.T, url, user string, using []string, calls []any) jmap.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"using": using, "methodCalls": calls})
	req, err := http.NewRequest(http.MethodPost, url+"/jmap", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+user)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST as %s: %v", user, err)
	}
	defer resp.Body.Close()
	var out jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

// TestRFC8984_SameServerInviteAcceptRoundTrip is the protocol-level mirror of the Bulwark
// e2e: Alice invites Bob (both local); the server delivers the invitation into Bob's
// calendar with participationStatus needs-action; Bob accepts; Alice's copy reflects the
// acceptance. This exercises same-server implicit iTIP scheduling
// (draft-ietf-jmap-calendars-27 Section 5.9.2.1 REQUEST and Section 5.9.2.3 REPLY).
func TestRFC8984_SameServerInviteAcceptRoundTrip(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.9.2.1", spectest.MUST,
		"An invited local participant receives the event in their calendar (participation pending).")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.9.2.3", spectest.MUST,
		"When the invited participant accepts, the organizer's copy reflects the acceptance.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const alice = "alice-rt@example.com"
	const bob = "bob-rt@example.com"
	cal := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI, jmap.MailCapabilityURI}
	aliceAcct := jmap.AccountIDForSubject(alice)
	bobAcct := jmap.AccountIDForSubject(bob)

	// 1. Alice creates the event and invites Bob, requesting scheduling messages.
	createResp := postAs(t, ts.URL, alice, cal, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              aliceAcct,
			"sendSchedulingMessages": true,
			"create": map[string]any{
				"e1": map[string]any{
					"title":    "Project Kickoff",
					"start":    "2026-10-01T15:00:00Z",
					"duration": "PT1H",
					"replyTo":  map[string]any{"imip": "mailto:" + alice},
					"participants": map[string]any{
						alice: map[string]any{"email": alice, "roles": map[string]any{"owner": true}},
						bob:   map[string]any{"email": bob, "roles": map[string]any{"attendee": true}, "participationStatus": "needs-action"},
					},
				},
			},
		}, "c1"},
	})
	created, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["e1"] == nil {
		t.Fatalf("Alice create failed: %+v", createResp.MethodResponses[0].Args)
	}
	aliceEventID := created["e1"].(map[string]any)["id"].(string)
	uid, _ := created["e1"].(map[string]any)["uid"].(string)
	if uid == "" {
		t.Fatalf("created event has no uid")
	}

	// 2. Bob sees the invitation in his own calendar, participation still pending.
	bobEvents := calendarQueryGet(t, ts.URL, bob, bobAcct, cal)
	bobEvent := findEventByTitle(bobEvents, "Project Kickoff")
	if bobEvent == nil {
		t.Fatalf("Bob did not receive the invitation in his calendar; events=%v", bobEvents)
	}
	if got := participationStatus(bobEvent, bob); got != "needs-action" {
		t.Errorf("Bob's invitation participationStatus = %q, want needs-action", got)
	}
	if bobEvent["uid"] != uid {
		t.Errorf("Bob's copy uid = %v, want %q (same-event correlation)", bobEvent["uid"], uid)
	}
	bobEventID := bobEvent["id"].(string)

	// 3. Bob accepts (RSVP) with scheduling on.
	acceptResp := postAs(t, ts.URL, bob, cal, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              bobAcct,
			"sendSchedulingMessages": true,
			"update": map[string]any{
				bobEventID: map[string]any{
					"participants/" + bob + "/participationStatus": "accepted",
				},
			},
		}, "c1"},
	})
	if updated, ok := acceptResp.MethodResponses[0].Args["updated"].(map[string]any); !ok {
		t.Fatalf("Bob accept failed: %+v", acceptResp.MethodResponses[0].Args)
	} else if _, present := updated[bobEventID]; !present {
		t.Fatalf("Bob accept did not update his event: %+v", acceptResp.MethodResponses[0].Args)
	}

	// 4. Alice's copy reflects Bob's acceptance.
	aliceEvents := postGetEvents(t, ts.URL, alice, aliceAcct, cal, []string{aliceEventID})
	aliceEvent := firstEvent(aliceEvents)
	if aliceEvent == nil {
		t.Fatalf("Alice's event vanished")
	}
	if got := participationStatus(aliceEvent, bob); got != "accepted" {
		t.Errorf("Alice sees Bob's participationStatus = %q, want accepted", got)
	}
}

// --- small helpers scoped to this scenario ---------------------------------

func calendarQueryGet(t *testing.T, url, user, accountID string, using []string) []map[string]any {
	t.Helper()
	q := postAs(t, url, user, using, []any{
		[]any{"CalendarEvent/query", map[string]any{"accountId": accountID}, "q"},
	})
	ids := toStringSlice(q.MethodResponses[0].Args["ids"])
	return postGetEvents(t, url, user, accountID, using, ids)
}

func postGetEvents(t *testing.T, url, user, accountID string, using []string, ids []string) []map[string]any {
	t.Helper()
	if len(ids) == 0 {
		return nil
	}
	idsAny := make([]any, len(ids))
	for i, id := range ids {
		idsAny[i] = id
	}
	g := postAs(t, url, user, using, []any{
		[]any{"CalendarEvent/get", map[string]any{"accountId": accountID, "ids": idsAny}, "g"},
	})
	list, _ := g.MethodResponses[0].Args["list"].([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func toStringSlice(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func findEventByTitle(events []map[string]any, title string) map[string]any {
	for _, ev := range events {
		if ev["title"] == title {
			return ev
		}
	}
	return nil
}

func firstEvent(events []map[string]any) map[string]any {
	if len(events) == 0 {
		return nil
	}
	return events[0]
}

func participationStatus(event map[string]any, participantKey string) string {
	participants, _ := event["participants"].(map[string]any)
	p, _ := participants[participantKey].(map[string]any)
	s, _ := p["participationStatus"].(string)
	return s
}
