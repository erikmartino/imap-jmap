package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/managesieve"
	"imap-jmap/jmap/nextcloud"
	"imap-jmap/jmap/spectest"
)

func postJMAPAs(t *testing.T, url, user string, using []string, calls []any) jmap.Response {
	t.Helper()
	payload := map[string]any{"using": using, "methodCalls": calls}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", url+"/jmap", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, user)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()
	var jr jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}
	return jr
}

func TestStalwart_CalendarEventNotifications(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "7.1", spectest.MUST,
		"A CalendarEventNotification represents a change made to a calendar event by another user.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "7.2", spectest.MUST,
		"CalendarEventNotification/get returns notifications for calendar event changes.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "7.3", spectest.MUST,
		"CalendarEventNotification/changes returns changes to notifications since a state.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.9.2", spectest.MUST,
		"sendSchedulingMessages triggers iTIP scheduling request/reply/cancel dispatch.")

	usernames := []string{"jdoe@example.com", "jane.smith@example.com", "bill@example.com"}
	gwBackend, _ := imapsmtp.NewEmbeddedBackend(usernames...)
	_, cal, contacts, fb, principals, cleanupNC := nextcloud.NewEmbeddedBackend(usernames...)
	defer cleanupNC()
	_, sieve, cleanupSieve := managesieve.NewEmbeddedBackend(usernames...)
	defer cleanupSieve()
	imap := jmap.NewMemoryIMAPAccessBackend()
	memAuth := jmap.NewMemoryAuthBackend()
	memAuth.SetDisableSeeding(true)

	srv := jmap.NewServer(nil,
		jmap.WithMailBackend(gwBackend),
		jmap.WithBlobBackend(gwBackend),
		jmap.WithFileNodeBackend(fb),
		jmap.WithCalendarsBackend(cal),
		jmap.WithContactsBackend(contacts),
		jmap.WithPrincipalsBackend(principals),
		jmap.WithSieveBackend(sieve),
		jmap.WithIMAPAccessBackend(imap),
		jmap.WithAuthBackend(memAuth),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{
		jmap.CoreCapabilityURI,
		jmap.CalendarsCapabilityURI,
	}

	john := "jdoe@example.com"
	jane := "jane.smith@example.com"
	bill := "bill@example.com"

	johnID := jmap.AccountIDForSubject(john)
	janeID := jmap.AccountIDForSubject(jane)
	billID := jmap.AccountIDForSubject(bill)

	var johnChangeID, janeChangeID, billChangeID string

	// 1. Obtain share notification change ids for all accounts
	for _, pair := range []struct {
		user     string
		changeID *string
	}{
		{john, &johnChangeID},
		{jane, &janeChangeID},
		{bill, &billChangeID},
	} {
		resp := postJMAPAs(t, ts.URL, pair.user, using, []any{
			[]any{"CalendarEventNotification/get", map[string]any{
				"accountId":  "primary",
				"properties": []string{"id"},
				"ids":        []string{},
			}, "g_init"},
		})
		list, _ := resp.MethodResponses[0].Args["list"].([]any)
		if len(list) != 0 {
			t.Fatalf("expected empty notification list, got %v", list)
		}
		*pair.changeID = resp.MethodResponses[0].Args["state"].(string)

		chResp := postJMAPAs(t, ts.URL, pair.user, using, []any{
			[]any{"CalendarEventNotification/changes", map[string]any{
				"accountId":  "primary",
				"sinceState": *pair.changeID,
			}, "ch_init"},
		})
		chArgs := chResp.MethodResponses[0].Args
		created, _ := chArgs["created"].([]any)
		if len(created) != 0 {
			t.Fatalf("expected no changes initially, got %v", created)
		}
		if chArgs["newState"] != *pair.changeID {
			t.Fatalf("expected newState == changeID")
		}
	}

	// 2. Create test calendars for John
	johnCalResp := postJMAPAs(t, ts.URL, john, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"name": "Test Calendar",
				},
			},
		}, "c_john"},
	})
	c1Map, _ := johnCalResp.MethodResponses[0].Args["created"].(map[string]any)
	johnCalendarID := c1Map["c1"].(map[string]any)["id"].(string)

	// 3. Send invitation to Jane and Bill
	startTime := time.Now().Add(1 * time.Hour).UTC().Format("2006-01-02T15:04:05")
	eventPayload := map[string]any{
		"@type":           "Event",
		"uid":             "9263504FD3AD",
		"title":           "Lunch",
		"timeZone":        "Europe/London",
		"start":           startTime,
		"duration":        "PT1H",
		"freeBusyStatus":  "busy",
		"updated":         "2009-06-02T17:00:00Z",
		"sequence":        0,
		"calendarIds":     map[string]bool{johnCalendarID: true},
		"organizerCalendarAddress": "mailto:jdoe@example.com",
		"participants": map[string]any{
			"8584f8f9-5414-55e3-8a1c-ad6fc2f3ffb6": map[string]any{
				"@type":               "Participant",
				"name":                "John Doe",
				"calendarAddress":     "mailto:jdoe@example.com",
				"participationStatus": "accepted",
				"roles": map[string]bool{
					"chair": true,
					"owner": true,
				},
			},
			"a0171748-fe8d-57d8-879e-56036a5251d1": map[string]any{
				"@type":               "Participant",
				"name":                "Jane Smith",
				"calendarAddress":     "mailto:jane.smith@example.com",
				"participationStatus": "needs-action",
				"kind":                "individual",
			},
			"86720268-d67c-58c3-9217-03df7d7ee4d8": map[string]any{
				"@type":               "Participant",
				"name":                "Bill Foobar",
				"calendarAddress":     "mailto:bill@example.com",
				"participationStatus": "needs-action",
				"kind":                "individual",
			},
		},
	}

	johnEvResp := postJMAPAs(t, ts.URL, john, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              "primary",
			"sendSchedulingMessages": true,
			"create": map[string]any{
				"e1": eventPayload,
			},
		}, "ce_john"},
	})
	cEvMap, _ := johnEvResp.MethodResponses[0].Args["created"].(map[string]any)
	johnEventID := cEvMap["e1"].(map[string]any)["id"].(string)

	// 4. Verify Jane and Bill received the share notification
	var janeEventID, billEventID string
	for _, target := range []struct {
		user     string
		changeID *string
		eventID  *string
	}{
		{jane, &janeChangeID, &janeEventID},
		{bill, &billChangeID, &billEventID},
	} {
		chResp := postJMAPAs(t, ts.URL, target.user, using, []any{
			[]any{"CalendarEventNotification/changes", map[string]any{
				"accountId":  "primary",
				"sinceState": *target.changeID,
			}, "ch_rcpt"},
		})
		chArgs := chResp.MethodResponses[0].Args
		created, _ := chArgs["created"].([]any)
		if len(created) != 1 {
			t.Fatalf("expected 1 notification created for %s, got %v", target.user, created)
		}
		*target.changeID = chArgs["newState"].(string)
		notifID := created[0].(string)

		notifResp := postJMAPAs(t, ts.URL, target.user, using, []any{
			[]any{"CalendarEventNotification/get", map[string]any{
				"accountId": "primary",
				"properties": []string{
					"id", "created", "changedBy", "comment", "type",
					"calendarEventId", "isDraft", "event", "eventPatch",
				},
				"ids": []string{notifID},
			}, "g_notif"},
		})
		notifList := notifResp.MethodResponses[0].Args["list"].([]any)
		if len(notifList) != 1 {
			t.Fatalf("expected 1 notification in get for %s", target.user)
		}
		notif := notifList[0].(map[string]any)
		*target.eventID = notif["calendarEventId"].(string)
		if notif["type"] != "created" {
			t.Fatalf("expected type created, got %v", notif["type"])
		}
		changedBy, _ := notif["changedBy"].(map[string]any)
		if changedBy["email"] != "jdoe@example.com" {
			t.Fatalf("expected changedBy email jdoe@example.com, got %+v", changedBy)
		}
		if changedBy["principalId"] != string(johnID) {
			t.Fatalf("expected changedBy principalId %s, got %+v", johnID, changedBy)
		}
		if notif["isDraft"] != false {
			t.Fatalf("expected isDraft false")
		}

		// Verify event exists in recipient's calendar
		evGetResp := postJMAPAs(t, ts.URL, target.user, using, []any{
			[]any{"CalendarEvent/get", map[string]any{
				"accountId":  "primary",
				"properties": []string{"id", "title"},
				"ids":        []string{*target.eventID},
			}, "g_ev"},
		})
		evList := evGetResp.MethodResponses[0].Args["list"].([]any)
		if len(evList) != 1 {
			t.Fatalf("event not found for %s", target.user)
		}
		if evList[0].(map[string]any)["title"] != "Lunch" {
			t.Fatalf("expected title Lunch for %s", target.user)
		}
	}

	// 5. Jane and Bill accept the invitation
	upJaneResp := postJMAPAs(t, ts.URL, jane, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              "primary",
			"sendSchedulingMessages": true,
			"update": map[string]any{
				janeEventID: map[string]any{
					"participants/a0171748-fe8d-57d8-879e-56036a5251d1/participationStatus": "accepted",
				},
			},
		}, "u_jane"},
	})
	janeUpMap, _ := upJaneResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := janeUpMap[janeEventID]; !ok {
		t.Fatalf("jane update failed: %+v", upJaneResp.MethodResponses[0].Args)
	}

	upBillResp := postJMAPAs(t, ts.URL, bill, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              "primary",
			"sendSchedulingMessages": true,
			"update": map[string]any{
				billEventID: map[string]any{
					"participants/86720268-d67c-58c3-9217-03df7d7ee4d8/participationStatus": "accepted",
				},
			},
		}, "u_bill"},
	})
	billUpMap, _ := upBillResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := billUpMap[billEventID]; !ok {
		t.Fatalf("bill update failed: %+v", upBillResp.MethodResponses[0].Args)
	}

	// 6. Verify John received two share notifications
	johnChResp := postJMAPAs(t, ts.URL, john, using, []any{
		[]any{"CalendarEventNotification/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": johnChangeID,
		}, "ch_john"},
	})
	johnCreated, _ := johnChResp.MethodResponses[0].Args["created"].([]any)
	if len(johnCreated) != 2 {
		t.Fatalf("expected 2 notifications for john, got %v", johnCreated)
	}

	for _, nIDAny := range johnCreated {
		nID := nIDAny.(string)
		gnResp := postJMAPAs(t, ts.URL, john, using, []any{
			[]any{"CalendarEventNotification/get", map[string]any{
				"accountId": "primary",
				"properties": []string{
					"id", "changedBy", "comment", "type", "calendarEventId", "isDraft",
				},
				"ids": []string{nID},
			}, "g_jnotif"},
		})
		jnList := gnResp.MethodResponses[0].Args["list"].([]any)
		jn := jnList[0].(map[string]any)
		if jn["type"] != "updated" {
			t.Fatalf("expected type updated, got %v", jn["type"])
		}
		if jn["calendarEventId"] != johnEventID {
			t.Fatalf("expected calendarEventId %s, got %v", johnEventID, jn["calendarEventId"])
		}
		if jn["isDraft"] != false {
			t.Fatalf("expected isDraft false")
		}
	}

	// Verify the event was updated in John's calendar
	johnEvGet := postJMAPAs(t, ts.URL, john, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "title", "participants"},
			"ids":        []string{johnEventID},
		}, "g_john_ev"},
	})
	johnEvObj := johnEvGet.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	johnParts := johnEvObj["participants"].(map[string]any)
	janeP, _ := johnParts["a0171748-fe8d-57d8-879e-56036a5251d1"].(map[string]any)
	billP, _ := johnParts["86720268-d67c-58c3-9217-03df7d7ee4d8"].(map[string]any)
	if janeP == nil || janeP["participationStatus"] != "accepted" {
		t.Fatalf("expected jane accepted on john's event, got %+v", janeP)
	}
	if billP == nil || billP["participationStatus"] != "accepted" {
		t.Fatalf("expected bill accepted on john's event, got %+v", billP)
	}

	// 7. Jane later declines the invitation
	decJaneResp := postJMAPAs(t, ts.URL, jane, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              "primary",
			"sendSchedulingMessages": true,
			"update": map[string]any{
				janeEventID: map[string]any{
					"participants/a0171748-fe8d-57d8-879e-56036a5251d1/participationStatus": "declined",
				},
			},
		}, "u_jane_dec"},
	})
	if _, ok := decJaneResp.MethodResponses[0].Args["updated"].(map[string]any)[janeEventID]; !ok {
		t.Fatalf("jane decline failed")
	}

	johnEvGet2 := postJMAPAs(t, ts.URL, john, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "title", "participants"},
			"ids":        []string{johnEventID},
		}, "g_john_ev2"},
	})
	johnParts2 := johnEvGet2.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)["participants"].(map[string]any)
	janeP2, _ := johnParts2["a0171748-fe8d-57d8-879e-56036a5251d1"].(map[string]any)
	if janeP2 == nil || janeP2["participationStatus"] != "declined" {
		t.Fatalf("expected jane declined on john's event, got %+v", janeP2)
	}

	// 8. John deletes the event
	delJohnEv := postJMAPAs(t, ts.URL, john, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              "primary",
			"sendSchedulingMessages": true,
			"destroy":                []string{johnEventID},
		}, "d_john_ev"},
	})
	desJohnList, _ := delJohnEv.MethodResponses[0].Args["destroyed"].([]any)
	if len(desJohnList) != 1 || desJohnList[0] != johnEventID {
		t.Fatalf("expected johnEventID destroyed: %+v", delJohnEv.MethodResponses[0].Args)
	}

	// Verify that Jane (declined) receives no cancellation notification
	janeChResp2 := postJMAPAs(t, ts.URL, jane, using, []any{
		[]any{"CalendarEventNotification/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": janeChangeID,
		}, "ch_jane2"},
	})
	janeCh2, _ := janeChResp2.MethodResponses[0].Args["created"].([]any)
	if len(janeCh2) != 0 {
		t.Fatalf("expected no cancellation notification for declined Jane, got %v", janeCh2)
	}

	// Verify that Bill received the cancellation
	billChResp2 := postJMAPAs(t, ts.URL, bill, using, []any{
		[]any{"CalendarEventNotification/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": billChangeID,
		}, "ch_bill2"},
	})
	billCh2, _ := billChResp2.MethodResponses[0].Args["created"].([]any)
	if len(billCh2) != 1 {
		t.Fatalf("expected 1 cancellation notification for Bill, got %v", billCh2)
	}
	billNotifID := billCh2[0].(string)

	billNotifResp := postJMAPAs(t, ts.URL, bill, using, []any{
		[]any{"CalendarEventNotification/get", map[string]any{
			"accountId": "primary",
			"properties": []string{
				"id", "changedBy", "comment", "type", "calendarEventId", "isDraft",
			},
			"ids": []string{billNotifID},
		}, "g_bill_notif2"},
	})
	bNotifList := billNotifResp.MethodResponses[0].Args["list"].([]any)
	bNotif := bNotifList[0].(map[string]any)
	if bNotif["type"] != "updated" {
		t.Fatalf("expected type updated, got %v", bNotif["type"])
	}
	if bNotif["calendarEventId"] != billEventID {
		t.Fatalf("expected calendarEventId %s, got %v", billEventID, bNotif["calendarEventId"])
	}

	// Verify Bill's event was updated to status cancelled
	billEvGet := postJMAPAs(t, ts.URL, bill, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "title", "status"},
			"ids":        []string{billEventID},
		}, "g_bill_ev2"},
	})
	bEvObj := billEvGet.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if bEvObj["status"] != "cancelled" {
		t.Fatalf("expected status cancelled on bill's event, got %v", bEvObj["status"])
	}

	// 9. Scheduling messages are sent for a server-assigned organizer
	janeInitState := postJMAPAs(t, ts.URL, jane, using, []any{
		[]any{"CalendarEventNotification/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id"},
			"ids":        []string{},
		}, "g_jane_state"},
	}).MethodResponses[0].Args["state"].(string)

	eventNoOrg := map[string]any{
		"@type":          "Event",
		"uid":            "9263504FD3AE",
		"title":          "Brunch",
		"timeZone":       "Europe/London",
		"start":          startTime,
		"duration":       "PT1H",
		"freeBusyStatus": "busy",
		"updated":        "2009-06-02T17:00:00Z",
		"sequence":       0,
		"calendarIds":    map[string]bool{johnCalendarID: true},
		"participants": map[string]any{
			"8584f8f9-5414-55e3-8a1c-ad6fc2f3ffb6": map[string]any{
				"@type":               "Participant",
				"calendarAddress":     "mailto:jdoe@example.com",
				"participationStatus": "accepted",
				"roles": map[string]bool{
					"chair": true,
					"owner": true,
				},
			},
			"a0171748-fe8d-57d8-879e-56036a5251d1": map[string]any{
				"@type":               "Participant",
				"calendarAddress":     "mailto:jane.smith@example.com",
				"participationStatus": "needs-action",
				"kind":                "individual",
			},
		},
	}

	johnEvNoOrgResp := postJMAPAs(t, ts.URL, john, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              "primary",
			"sendSchedulingMessages": true,
			"create": map[string]any{
				"e2": eventNoOrg,
			},
		}, "c_no_org"},
	})
	johnEv2ID := johnEvNoOrgResp.MethodResponses[0].Args["created"].(map[string]any)["e2"].(map[string]any)["id"].(string)

	johnEv2Get := postJMAPAs(t, ts.URL, john, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "organizerCalendarAddress"},
			"ids":        []string{johnEv2ID},
		}, "g_j2"},
	})
	j2Obj := johnEv2Get.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if j2Obj["organizerCalendarAddress"] != "mailto:jdoe@example.com" {
		t.Fatalf("expected server assigned organizer mailto:jdoe@example.com, got %v", j2Obj["organizerCalendarAddress"])
	}

	janeChResp3 := postJMAPAs(t, ts.URL, jane, using, []any{
		[]any{"CalendarEventNotification/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": janeInitState,
		}, "ch_jane3"},
	})
	janeCh3, _ := janeChResp3.MethodResponses[0].Args["created"].([]any)
	if len(janeCh3) != 1 {
		t.Fatalf("expected 1 notification for Jane, got %v", janeCh3)
	}
	janeNotif2ID := janeCh3[0].(string)

	janeNotif2Resp := postJMAPAs(t, ts.URL, jane, using, []any{
		[]any{"CalendarEventNotification/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "calendarEventId"},
			"ids":        []string{janeNotif2ID},
		}, "g_jane_notif2"},
	})
	janeEv2ID := janeNotif2Resp.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)["calendarEventId"].(string)

	janeEv2Get := postJMAPAs(t, ts.URL, jane, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "title", "organizerCalendarAddress"},
			"ids":        []string{janeEv2ID},
		}, "g_jane_ev2"},
	})
	janeEv2Obj := janeEv2Get.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if janeEv2Obj["title"] != "Brunch" {
		t.Fatalf("expected title Brunch, got %v", janeEv2Obj["title"])
	}
	if janeEv2Obj["organizerCalendarAddress"] != "mailto:jdoe@example.com" {
		t.Fatalf("expected organizer mailto:jdoe@example.com, got %v", janeEv2Obj["organizerCalendarAddress"])
	}

	// An attendee replying to the invitation does not take over as organizer
	janeReplyResp := postJMAPAs(t, ts.URL, jane, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId":              "primary",
			"sendSchedulingMessages": true,
			"update": map[string]any{
				janeEv2ID: map[string]any{
					"participants/a0171748-fe8d-57d8-879e-56036a5251d1/participationStatus": "accepted",
				},
			},
		}, "u_jane_reply"},
	})
	if _, ok := janeReplyResp.MethodResponses[0].Args["updated"].(map[string]any)[janeEv2ID]; !ok {
		t.Fatalf("jane reply failed: %+v", janeReplyResp.MethodResponses[0].Args)
	}

	janeEv2GetAfter := postJMAPAs(t, ts.URL, jane, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "organizerCalendarAddress"},
			"ids":        []string{janeEv2ID},
		}, "g_jane_ev2_after"},
	})
	janeEv2AfterObj := janeEv2GetAfter.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if janeEv2AfterObj["organizerCalendarAddress"] != "mailto:jdoe@example.com" {
		t.Fatalf("expected organizer to remain mailto:jdoe@example.com, got %v", janeEv2AfterObj["organizerCalendarAddress"])
	}

	// 10. Clean up
	for _, u := range []string{john, jane, bill} {
		postJMAPAs(t, ts.URL, u, using, []any{
			[]any{"Calendar/set", map[string]any{
				"accountId":             "primary",
				"destroy":               []string{johnCalendarID},
				"onDestroyRemoveEvents": true,
			}, "d_cleanup"},
		})
	}
	_ = janeID
	_ = billID
}
