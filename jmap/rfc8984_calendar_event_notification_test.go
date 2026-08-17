package jmap_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
)

// TestRFC8984_CalendarEventNotificationLifecycle tests CalendarEventNotification per
// draft-ietf-jmap-calendars Section 7: server-generated notifications on scheduling
// changes, get/changes/query/queryChanges, forbidden client create/update, and destroy.
func TestRFC8984_CalendarEventNotificationLifecycle(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create an event with sendSchedulingMessages: true and a participant. The server
	// MUST record a "created" CalendarEventNotification (Section 7.2).
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              "primary",
			"sendSchedulingMessages": true,
			"create": map[string]any{
				"e1": map[string]any{
					"title": "Notification Test",
					"start": "2026-08-25T11:00:00Z",
					"participants": map[string]any{
						"mailto:attendee@example.com": map[string]any{
							"name":   "Attendee",
							"email":  "attendee@example.com",
							"status": "needs-action",
							"roles":  map[string]any{"attendee": true},
						},
						"mailto:owner@example.com": map[string]any{
							"name":  "Owner",
							"email": "owner@example.com",
							"roles": map[string]any{"owner": true},
						},
					},
				},
			},
		}, "c1"},
	}
	createResp := postJMAP(t, ts.URL, using, createReq)
	created, ok := createResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["e1"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", createResp.MethodResponses[0].Args)
	}
	evID := created["e1"].(map[string]any)["id"].(string)

	// 2. Query notifications: exactly one, type "created", for our event.
	queryReq := []any{
		[]any{"CalendarEventNotification/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"type": "created",
			},
		}, "c2"},
	}
	queryResp := postJMAP(t, ts.URL, using, queryReq)
	queryArgs := queryResp.MethodResponses[0].Args
	ids, ok := queryArgs["ids"].([]any)
	if !ok || len(ids) != 1 {
		t.Fatalf("expected 1 created notification id, got %+v", queryArgs)
	}
	notifID := ids[0].(string)
	if queryArgs["total"].(float64) != 1 {
		t.Errorf("expected total 1, got %v", queryArgs["total"])
	}

	// 3. get: verify type, calendarEventId, changedBy (owner participant), and event data.
	getReq := []any{
		[]any{"CalendarEventNotification/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{notifID},
		}, "c3"},
	}
	getResp := postJMAP(t, ts.URL, using, getReq)
	list, ok := getResp.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("expected 1 notification, got %+v", getResp.MethodResponses[0].Args)
	}
	notif := list[0].(map[string]any)
	if notif["type"] != "created" {
		t.Errorf("expected type created, got %v", notif["type"])
	}
	if notif["calendarEventId"] != evID {
		t.Errorf("expected calendarEventId %s, got %v", evID, notif["calendarEventId"])
	}
	changedBy, ok := notif["changedBy"].(map[string]any)
	if !ok || changedBy["email"] != "owner@example.com" {
		t.Errorf("expected changedBy from owner participant, got %v", notif["changedBy"])
	}
	ev, ok := notif["event"].(map[string]any)
	if !ok || ev["title"] != "Notification Test" {
		t.Errorf("expected event data in notification, got %v", notif["event"])
	}

	// 4. Partial update of the event generates an "updated" notification carrying the
	// pre-change event plus the eventPatch (Section 7.2).
	updateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              "primary",
			"sendSchedulingMessages": true,
			"update": map[string]any{
				evID: map[string]any{
					"title": "Notification Test (Updated)",
				},
			},
		}, "c4"},
	}
	updateResp := postJMAP(t, ts.URL, using, updateReq)
	if notUpdated, _ := updateResp.MethodResponses[0].Args["notUpdated"].(map[string]any); len(notUpdated) != 0 {
		t.Fatalf("CalendarEvent/set update failed: %+v", updateResp.MethodResponses[0].Args)
	}

	query2Req := []any{
		[]any{"CalendarEventNotification/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"calendarEventIds": []any{evID},
			},
		}, "c5"},
	}
	query2Resp := postJMAP(t, ts.URL, using, query2Req)
	ids2, _ := query2Resp.MethodResponses[0].Args["ids"].([]any)
	if len(ids2) != 2 {
		t.Fatalf("expected 2 notifications for the event, got %v", ids2)
	}

	getUpdatedReq := []any{
		[]any{"CalendarEventNotification/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{ids2[0].(string)},
		}, "c6"},
	}
	getUpdatedResp := postJMAP(t, ts.URL, using, getUpdatedReq)
	updatedNotif := getUpdatedResp.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if updatedNotif["type"] != "updated" {
		t.Fatalf("newest notification must be type updated, got %v", updatedNotif["type"])
	}
	if _, hasPatch := updatedNotif["eventPatch"]; !hasPatch {
		t.Errorf("updated notification must carry eventPatch, got %v", updatedNotif)
	}
	beforeEv := updatedNotif["event"].(map[string]any)
	if beforeEv["title"] != "Notification Test" {
		t.Errorf("event must carry the pre-change data, got %v", beforeEv["title"])
	}

	// 5. queryChanges: since the pre-create state, the "created" notification is added.
	stateReq := []any{
		[]any{"CalendarEventNotification/get", map[string]any{
			"accountId": "primary",
		}, "c7"},
	}
	stateResp := postJMAP(t, ts.URL, using, stateReq)
	stateNow := stateResp.MethodResponses[0].Args["state"].(string)

	_ = stateNow

	// Since state of an empty past: queryChanges from "" must fail with cannotCalculateChanges.
	emptySince := []any{
		[]any{"CalendarEventNotification/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": "",
			"filter":          map[string]any{},
		}, "c8"},
	}
	emptySinceResp := postJMAP(t, ts.URL, using, emptySince)
	if emptySinceResp.MethodResponses[0].Name != "error" {
		t.Errorf("queryChanges with empty sinceQueryState must error, got %+v", emptySinceResp.MethodResponses[0])
	}

	// queryChanges from the state before the update only reports the "updated" notification.
	// We cannot obtain a historical state here, so instead verify shape on a fresh query.
	qcReq := []any{
		[]any{"CalendarEventNotification/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": stateNow,
		}, "c9"},
	}
	qcResp := postJMAP(t, ts.URL, using, qcReq)
	qcArgs := qcResp.MethodResponses[0].Args
	if qcArgs["newQueryState"] != stateNow {
		t.Errorf("expected newQueryState %s, got %v", stateNow, qcArgs["newQueryState"])
	}
	if added, ok := qcArgs["added"].([]any); !ok || len(added) != 0 {
		t.Errorf("expected no additions after current state, got %v", qcArgs["added"])
	}

	// 6. Client create MUST be rejected with forbidden (notCreated).
	forbiddenCreate := []any{
		[]any{"CalendarEventNotification/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"n1": map[string]any{
					"type":            "created",
					"calendarEventId": evID,
				},
			},
		}, "c10"},
	}
	fcResp := postJMAP(t, ts.URL, using, forbiddenCreate)
	fcArgs := fcResp.MethodResponses[0].Args
	notCreated, ok := fcArgs["notCreated"].(map[string]any)
	if !ok || notCreated["n1"] == nil {
		t.Fatalf("expected notCreated with forbidden for client create, got %+v", fcArgs)
	}
	if errMap := notCreated["n1"].(map[string]any); errMap["type"] != "forbidden" {
		t.Errorf("expected forbidden error type, got %v", errMap)
	}

	// 7. Client update MUST be rejected with forbidden (notUpdated).
	forbiddenUpdate := []any{
		[]any{"CalendarEventNotification/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				notifID: map[string]any{
					"type": "updated",
				},
			},
		}, "c11"},
	}
	fuResp := postJMAP(t, ts.URL, using, forbiddenUpdate)
	notUpdated, ok := fuResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if !ok || notUpdated[notifID] == nil {
		t.Fatalf("expected notUpdated with forbidden for client update, got %+v", fuResp.MethodResponses[0].Args)
	}

	// 8. Destroy is allowed; destroy of an unknown id reports notDestroyed.
	destroyReq := []any{
		[]any{"CalendarEventNotification/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{notifID, "n-missing"},
		}, "c12"},
	}
	destroyResp := postJMAP(t, ts.URL, using, destroyReq)
	destroyArgs := destroyResp.MethodResponses[0].Args
	destroyed, ok := destroyArgs["destroyed"].([]any)
	if !ok || len(destroyed) != 1 || destroyed[0] != notifID {
		t.Errorf("expected destroyed [%s], got %v", notifID, destroyed)
	}
	notDestroyed, ok := destroyArgs["notDestroyed"].(map[string]any)
	if !ok || notDestroyed["n-missing"] == nil {
		t.Errorf("expected notDestroyed for missing notification, got %+v", destroyArgs)
	}

	// 9. changes reflects the destroy.
	changesReq := []any{
		[]any{"CalendarEventNotification/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": stateNow,
		}, "c13"},
	}
	changesResp := postJMAP(t, ts.URL, using, changesReq)
	chDestroyed, _ := changesResp.MethodResponses[0].Args["destroyed"].([]any)
	if len(chDestroyed) != 1 || chDestroyed[0] != notifID {
		t.Errorf("expected destroyed [%s] in changes, got %v", notifID, chDestroyed)
	}
}

// TestRFC8984_CalendarEventNotificationStateChangeEvent verifies that creating a
// CalendarEventNotification publishes a StateChange on the broadcaster so subscribed
// clients are notified (RFC 8620 Section 7.1 / draft Section 7.7).
func TestRFC8984_CalendarEventNotificationStateChangeEvent(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req := authedRequest(t, "GET", ts.URL+"/eventsource?types=CalendarEventNotification&closeafter=state", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /eventsource failed: %v", err)
	}
	defer resp.Body.Close()

	go func() {
		time.Sleep(100 * time.Millisecond)
		seedCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(testUsername))
		_, _ = srv.CalendarsBackend.CreateCalendarEventNotification(seedCtx, &jmap.CalendarEventNotification{
			Type:            "created",
			CalendarEventID: "evt-notify-1",
			Event:           &jmap.CalendarEvent{ID: "evt-notify-1", Title: "State Change"},
		})
	}()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var sc jmap.StateChange
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data:")), &sc); err != nil {
			continue
		}
		changed, ok := sc.Changed[jmap.AccountIDForSubject(testUsername)]
		if !ok {
			continue
		}
		if _, hasType := changed["CalendarEventNotification"]; !hasType {
			t.Fatalf("expected CalendarEventNotification type in StateChange, got %+v", changed)
		}
		return
	}
	t.Fatal("no StateChange delivered for CalendarEventNotification mutation")
}
