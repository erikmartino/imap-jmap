package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestStalwart_CalendarEventLifecycleAndQueries translates tests/src/jmap/calendar/event.rs
// from Stalwart into a hermetic Go integration test. It covers:
// - Calendar creation with default alerts
// - Event creation, initial state tracking, and destruction
// - Event change tracking via CalendarEvent/changes
// - Verifying JMAP for Calendars properties (isDraft, isOrigin, mayInviteSelf, mayInviteOthers, hideAttendees, useDefaultAlerts, utcStart, utcEnd)
// - CalendarEvent/get with recurrenceOverridesBefore/After and reduceParticipants
// - CalendarEvent/get with empty properties array returning only ID
// - Rejection of events without calendars or with duplicate UIDs
// - JSON pointer and partial patches on events (keywords, calendarIds, recurrenceOverrides, participants, utcStart/utcEnd)
// - Querying with text, inCalendar, UID, before, after, timeZone
// - Recurrence expansion with expandRecurrences: true returning 7 occurrences in chronological order
// - CalendarEvent/parse via uploaded iCalendar blob
// - Organizer assignment rules (automatic from participants, explicit preservation, empty when no participants)
// - Unbounded yearly recurrences queryable across years
func TestStalwart_CalendarEventLifecycleAndQueries(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.1", spectest.MUST,
		"A CalendarEvent object represents a calendar event or task.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.2", spectest.MUST,
		"CalendarEvent/get returns the requested properties for the specified event IDs.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.3", spectest.MUST,
		"CalendarEvent/changes returns changes to calendar events since a specified state.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.4", spectest.MUST,
		"CalendarEvent/set creates, updates, and destroys CalendarEvent objects.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.9", spectest.MUST,
		"CalendarEvent/query returns event ids matching the specified filter conditions.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.12", spectest.MUST,
		"CalendarEvent/parse parses iCalendar blobs into CalendarEvent objects.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{
		jmap.CoreCapabilityURI,
		jmap.CalendarsCapabilityURI,
		jmap.BlobCapabilityURI,
	}

	// 1. Create test calendars
	createCalResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"cal1": map[string]any{
					"name":     "Holy Calendar, Batman!",
					"timeZone": "Europe/Vatican",
				},
				"cal2": map[string]any{
					"name": "Calendar with Alerts",
					"defaultAlertsWithTime": map[string]any{
						"abc": map[string]any{
							"action": "display",
							"trigger": map[string]any{
								"relativeTo": "start",
								"offset":     "PT15M",
							},
						},
					},
				},
			},
		}, "c1"},
	})

	createdCals, ok := createCalResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || createdCals["cal1"] == nil || createdCals["cal2"] == nil {
		t.Fatalf("Calendar/set failed: %+v", createCalResp.MethodResponses[0].Args)
	}
	cal1ID := createdCals["cal1"].(map[string]any)["id"].(string)
	cal2ID := createdCals["cal2"].(map[string]any)["id"].(string)

	// 2. Obtain initial event state
	stateResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{},
		}, "g0"},
	})
	initialState, _ := stateResp.MethodResponses[0].Args["state"].(string)

	// 3. Create test events
	event1Payload := map[string]any{
		"@type":            "Event",
		"uid":              "74855313FA803DA593CD579A@example.com",
		"title":            "Event #1",
		"description":      "Go Steelers!",
		"start":            "2006-01-02T10:00:00",
		"duration":         "PT1H",
		"timeZone":         "US/Eastern",
		"calendarIds":      map[string]bool{cal1ID: true},
		"isDraft":          true,
		"mayInviteSelf":    true,
		"mayInviteOthers":  true,
		"hideAttendees":    true,
	}

	event2Payload := map[string]any{
		"@type":    "Event",
		"uid":      "00959BC664CA650E933C892C@example.com",
		"title":    "Event #2",
		"start":    "2006-01-02T12:00:00",
		"duration": "PT1H",
		"timeZone": "US/Eastern",
		"recurrenceRule": map[string]any{
			"frequency": "daily",
			"count":     5,
		},
		"recurrenceOverrides": map[string]any{
			"2006-01-04T12:00:00": map[string]any{
				"title":    "Event #2 bis",
				"start":    "2006-01-04T14:00:00",
				"duration": "PT1H",
			},
			"2006-01-06T12:00:00": map[string]any{
				"title":    "Event #2 bis bis",
				"start":    "2006-01-06T14:00:00",
				"duration": "PT1H",
			},
		},
		"calendarIds":      map[string]bool{cal2ID: true},
		"useDefaultAlerts": true,
	}

	event3Payload := map[string]any{
		"@type":                    "Event",
		"uid":                      "DC6C50A017428C5216A2F1CD@example.com",
		"title":                    "Event #3",
		"start":                    "2006-01-04T10:00:00",
		"duration":                 "PT1H",
		"timeZone":                 "US/Eastern",
		"status":                   "tentative",
		"sequence":                 1,
		"organizerCalendarAddress": "mailto:cyrus@example.com",
		"participants": map[string]any{
			"3f5bc8c0-c722-5345-b7d9-5a899db08a30": map[string]any{
				"@type":               "Participant",
				"calendarAddress":     "mailto:cyrus@example.com",
				"participationStatus": "accepted",
				"roles": map[string]bool{
					"chair": true,
					"owner": true,
				},
			},
			"ec5e7db5-22a3-5ed5-89bf-c8894ab86805": map[string]any{
				"@type":               "Participant",
				"calendarAddress":     "mailto:lisa@example.com",
				"participationStatus": "needs-action",
			},
		},
		"calendarIds":      map[string]bool{cal1ID: true, cal2ID: true},
		"useDefaultAlerts": false,
	}

	event4Payload := map[string]any{
		"@type":       "Event",
		"uid":         "tmp-event@example.com",
		"title":       "Tmp Event",
		"description": "Tmp Event",
		"start":       "2006-01-02T10:00:00",
		"duration":    "PT1H",
		"timeZone":    "US/Eastern",
		"calendarIds": map[string]bool{cal1ID: true},
	}

	createEvResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": event1Payload,
				"e2": event2Payload,
				"e3": event3Payload,
				"e4": event4Payload,
			},
		}, "ce1"},
	})

	createdEvents, ok := createEvResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || createdEvents["e1"] == nil || createdEvents["e2"] == nil || createdEvents["e3"] == nil || createdEvents["e4"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", createEvResp.MethodResponses[0].Args)
	}
	ev1ID := createdEvents["e1"].(map[string]any)["id"].(string)
	ev2ID := createdEvents["e2"].(map[string]any)["id"].(string)
	ev3ID := createdEvents["e3"].(map[string]any)["id"].(string)
	ev4ID := createdEvents["e4"].(map[string]any)["id"].(string)

	// 4. Destroy tmp event
	destroyResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{ev4ID},
		}, "d1"},
	})
	destroyed, ok := destroyResp.MethodResponses[0].Args["destroyed"].([]any)
	if !ok || len(destroyed) != 1 || destroyed[0].(string) != ev4ID {
		t.Fatalf("destroy failed: %+v", destroyResp.MethodResponses[0].Args)
	}

	// 5. Validate changes
	changesResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": initialState,
		}, "ch1"},
	})
	changesArgs := changesResp.MethodResponses[0].Args
	chCreated, _ := changesArgs["created"].([]any)
	createdSet := make(map[string]bool)
	for _, item := range chCreated {
		createdSet[item.(string)] = true
	}
	if !createdSet[ev1ID] || !createdSet[ev2ID] || !createdSet[ev3ID] {
		t.Fatalf("expected created events in changes, got %+v", changesArgs)
	}

	// 6. Verify event contents
	getResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{ev1ID, ev2ID, ev3ID},
		}, "g1"},
	})
	list, ok := getResp.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list) != 3 {
		t.Fatalf("expected 3 events in list, got %+v", getResp.MethodResponses[0].Args)
	}

	ev1 := list[0].(map[string]any)
	ev2 := list[1].(map[string]any)
	ev3 := list[2].(map[string]any)

	if ev1["isDraft"] != true {
		t.Fatalf("expected ev1 isDraft true, got %v", ev1["isDraft"])
	}
	if ev1["isOrigin"] != true {
		t.Fatalf("expected ev1 isOrigin true, got %v", ev1["isOrigin"])
	}
	if ev2["isDraft"] != false {
		t.Fatalf("expected ev2 isDraft false, got %v", ev2["isDraft"])
	}
	if ev2["isOrigin"] != true {
		t.Fatalf("expected ev2 isOrigin true, got %v", ev2["isOrigin"])
	}
	alerts, _ := ev2["alerts"].(map[string]any)
	if len(alerts) == 0 {
		t.Fatalf("expected ev2 alerts to be populated from defaultAlertsWithTime, got %v", ev2["alerts"])
	}
	if ev3["isDraft"] != false {
		t.Fatalf("expected ev3 isDraft false, got %v", ev3["isDraft"])
	}
	if ev3["isOrigin"] != false {
		t.Fatalf("expected ev3 isOrigin false, got %v", ev3["isOrigin"])
	}

	// 7. Verify JMAP for Calendars properties
	propResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"properties": []string{
				"id", "baseEventId", "mayInviteSelf", "mayInviteOthers",
				"hideAttendees", "useDefaultAlerts", "utcStart", "utcEnd",
			},
			"ids": []string{ev1ID, ev2ID, ev3ID},
		}, "g2"},
	})
	propList := propResp.MethodResponses[0].Args["list"].([]any)
	p1 := propList[0].(map[string]any)
	p2 := propList[1].(map[string]any)
	p3 := propList[2].(map[string]any)

	if p1["baseEventId"] != nil || p1["mayInviteSelf"] != true || p1["mayInviteOthers"] != true || p1["hideAttendees"] != true || p1["useDefaultAlerts"] != false || p1["utcStart"] != "2006-01-02T15:00:00Z" || p1["utcEnd"] != "2006-01-02T16:00:00Z" {
		t.Fatalf("ev1 properties mismatch: %+v", p1)
	}
	if p2["baseEventId"] != nil || p2["mayInviteSelf"] != false || p2["mayInviteOthers"] != false || p2["hideAttendees"] != false || p2["useDefaultAlerts"] != true || p2["utcStart"] != "2006-01-02T17:00:00Z" || p2["utcEnd"] != "2006-01-02T18:00:00Z" {
		t.Fatalf("ev2 properties mismatch: %+v", p2)
	}
	if p3["baseEventId"] != nil || p3["mayInviteSelf"] != false || p3["mayInviteOthers"] != false || p3["hideAttendees"] != false || p3["useDefaultAlerts"] != false || p3["utcStart"] != "2006-01-04T15:00:00Z" || p3["utcEnd"] != "2006-01-04T16:00:00Z" {
		t.Fatalf("ev3 properties mismatch: %+v", p3)
	}

	// 8. Test /get parameters: recurrenceOverridesBefore/After and reduceParticipants
	filterGetResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":                 "primary",
			"properties":                []string{"id", "title", "recurrenceOverrides", "participants"},
			"ids":                       []string{ev2ID, ev3ID},
			"recurrenceOverridesBefore": "2006-01-07T00:00:00Z",
			"recurrenceOverridesAfter":  "2006-01-06T00:00:00Z",
			"reduceParticipants":        true,
		}, "g3"},
	})
	fList := filterGetResp.MethodResponses[0].Args["list"].([]any)
	fe2 := fList[0].(map[string]any)
	fe3 := fList[1].(map[string]any)

	fe2Overrides, _ := fe2["recurrenceOverrides"].(map[string]any)
	if len(fe2Overrides) != 1 || fe2Overrides["2006-01-06T12:00:00"] == nil {
		t.Fatalf("expected only 2006-01-06T12:00:00 override, got %+v", fe2Overrides)
	}
	fe3Participants, _ := fe3["participants"].(map[string]any)
	if len(fe3Participants) != 1 || fe3Participants["3f5bc8c0-c722-5345-b7d9-5a899db08a30"] == nil {
		t.Fatalf("expected only owner participant in fe3, got %+v", fe3Participants)
	}

	// 9. Test /get with empty properties array: returns only ID
	emptyPropResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{},
			"ids":        []string{ev2ID, ev3ID},
		}, "g4"},
	})
	epList := emptyPropResp.MethodResponses[0].Args["list"].([]any)
	if len(epList[0].(map[string]any)) != 1 || epList[0].(map[string]any)["id"] != ev2ID {
		t.Fatalf("expected only id for ev2, got %+v", epList[0])
	}
	if len(epList[1].(map[string]any)) != 1 || epList[1].(map[string]any)["id"] != ev3ID {
		t.Fatalf("expected only id for ev3, got %+v", epList[1])
	}

	// 10. Creating an event without calendar should fail
	noCalResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e5": map[string]any{
					"title":       "Event #5",
					"start":       "2006-01-22T10:00:00",
					"duration":    "PT1H",
					"timeZone":    "US/Eastern",
					"calendarIds": map[string]bool{},
				},
			},
		}, "no-cal"},
	})
	notCreated, _ := noCalResp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreated["e5"] == nil {
		t.Fatalf("expected e5 in notCreated, got %+v", noCalResp.MethodResponses[0].Args)
	}

	// 11. Creating an event with a duplicate UID should fail
	dupUidResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e5": map[string]any{
					"title":       "Event #5",
					"start":       "2006-01-22T10:00:00",
					"duration":    "PT1H",
					"timeZone":    "US/Eastern",
					"uid":         "00959BC664CA650E933C892C@example.com",
					"calendarIds": map[string]bool{cal1ID: true},
				},
			},
		}, "dup-uid"},
	})
	notCreatedDup, _ := dupUidResp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if notCreatedDup["e5"] == nil {
		t.Fatalf("expected e5 in notCreated due to duplicate UID, got %+v", dupUidResp.MethodResponses[0].Args)
	}

	// 12. Patching tests
	patchResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				ev1ID: map[string]any{
					"isDraft":                 false,
					"mayInviteSelf":           false,
					"mayInviteOthers":         false,
					"hideAttendees":           false,
					"useDefaultAlerts":        true,
					"description":             nil,
					"title":                   "Event one",
					"keywords":                map[string]bool{"work": true},
					"calendarIds/" + cal2ID:   true,
				},
				ev2ID: map[string]any{
					"calendarIds": map[string]bool{
						cal1ID: true,
						cal2ID: true,
					},
					"title":                                         "Event two",
					"useDefaultAlerts":                              false,
					"description":                                   "Updated description",
					"recurrenceOverrides/2006-01-04T12:00:00/title": "Event two overridden",
					"recurrenceOverrides/2006-01-06T12:00:00/title": "Event two overridden twice",
				},
				ev3ID: map[string]any{
					"calendarIds/" + cal2ID:                                          false,
					"title":                                                          "Event three",
					"utcStart":                                                       "2006-01-04T14:00:00Z",
					"utcEnd":                                                         "2006-01-04T16:00:00Z",
					"participants/3f5bc8c0-c722-5345-b7d9-5a899db08a30/roles/chair": false,
					"participants/3f5bc8c0-c722-5345-b7d9-5a899db08a30/roles/owner": true,
					"participants/ec5e7db5-22a3-5ed5-89bf-c8894ab86805":            nil,
					"participants/7f2bd210-6c66-5b64-8562-0176b74462b1": map[string]any{
						"@type":               "Participant",
						"calendarAddress":     "mailto:rupert@example.com",
						"participationStatus": "needs-action",
					},
				},
			},
		}, "u1"},
	})
	updatedMap, _ := patchResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok1 := updatedMap[ev1ID]; !ok1 {
		t.Fatalf("ev1 not updated: %+v", patchResp.MethodResponses[0].Args)
	}
	if _, ok2 := updatedMap[ev2ID]; !ok2 {
		t.Fatalf("ev2 not updated: %+v", patchResp.MethodResponses[0].Args)
	}
	if _, ok3 := updatedMap[ev3ID]; !ok3 {
		t.Fatalf("ev3 not updated: %+v", patchResp.MethodResponses[0].Args)
	}

	// 13. Verify patches
	patchedGetResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"properties": []string{
				"id", "calendarIds", "title", "start", "description", "keywords",
				"recurrenceOverrides", "participants", "mayInviteOthers",
				"mayInviteSelf", "hideAttendees", "useDefaultAlerts", "isDraft",
			},
			"ids": []string{ev1ID, ev2ID, ev3ID},
		}, "g5"},
	})
	patchedList := patchedGetResp.MethodResponses[0].Args["list"].([]any)
	pe1 := patchedList[0].(map[string]any)
	pe2 := patchedList[1].(map[string]any)
	pe3 := patchedList[2].(map[string]any)

	pe1Cals, _ := pe1["calendarIds"].(map[string]any)
	if pe1Cals == nil || !pe1Cals[cal1ID].(bool) || !pe1Cals[cal2ID].(bool) {
		t.Fatalf("expected pe1 in cal1 and cal2, got %+v", pe1Cals)
	}
	if pe1["title"] != "Event one" || pe1["description"] != nil {
		t.Fatalf("pe1 mismatch: %+v", pe1)
	}

	pe2Overrides := pe2["recurrenceOverrides"].(map[string]any)
	if ov1, ok := pe2Overrides["2006-01-04T12:00:00"].(map[string]any); !ok || ov1["title"] != "Event two overridden" {
		t.Fatalf("pe2 override 1 mismatch: %+v", ov1)
	}
	if ov2, ok := pe2Overrides["2006-01-06T12:00:00"].(map[string]any); !ok || ov2["title"] != "Event two overridden twice" {
		t.Fatalf("pe2 override 2 mismatch: %+v", ov2)
	}

	pe3Cals := pe3["calendarIds"].(map[string]any)
	if !pe3Cals[cal1ID].(bool) || pe3Cals[cal2ID] != nil {
		t.Fatalf("pe3 calendarIds mismatch: %+v", pe3Cals)
	}
	if pe3["title"] != "Event three" || pe3["start"] != "2006-01-04T09:00:00" {
		t.Fatalf("pe3 start/title mismatch: %+v", pe3)
	}
	pe3Participants := pe3["participants"].(map[string]any)
	if len(pe3Participants) != 2 || pe3Participants["ec5e7db5-22a3-5ed5-89bf-c8894ab86805"] != nil {
		t.Fatalf("pe3 participants mismatch: %+v", pe3Participants)
	}

	// 14. Query tests
	queryResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"text":       "Event one",
				"inCalendar": cal1ID,
				"uid":        "74855313FA803DA593CD579A@example.com",
				"after":      "2006-01-02T10:59:59",
				"before":     "2006-01-02T10:00:01",
			},
			"sort":     []any{"start"},
			"timeZone": "US/Eastern",
		}, "q1"},
	})
	qIDs, _ := queryResp.MethodResponses[0].Args["ids"].([]any)
	if len(qIDs) != 1 || qIDs[0].(string) != ev1ID {
		t.Fatalf("expected ev1ID from query, got %+v", qIDs)
	}

	// 15. Recurrence expansion tests
	expandResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"after":  "2006-01-01T00:00:00",
				"before": "2006-01-08T00:00:00",
			},
			"sort":              []any{"start"},
			"timeZone":          "US/Eastern",
			"expandRecurrences": true,
		}, "q-exp"},
	})
	expIDs, _ := expandResp.MethodResponses[0].Args["ids"].([]any)
	if len(expIDs) != 7 {
		t.Fatalf("expected 7 expanded occurrence IDs, got %d: %+v", len(expIDs), expIDs)
	}

	expGetResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"properties": []string{
				"id", "baseEventId", "start", "duration", "timeZone", "title", "recurrenceId",
			},
			"ids": expIDs,
		}, "g-exp"},
	})
	expList := expGetResp.MethodResponses[0].Args["list"].([]any)
	if len(expList) != 7 {
		t.Fatalf("expected 7 items in expList, got %d", len(expList))
	}

	// Verify occurrence 0: Event one
	inst0 := expList[0].(map[string]any)
	if inst0["title"] != "Event one" || inst0["start"] != "2006-01-02T10:00:00" || inst0["baseEventId"] != ev1ID {
		t.Fatalf("inst0 mismatch: %+v", inst0)
	}
	// Verify occurrence 1: Event two #1
	inst1 := expList[1].(map[string]any)
	if inst1["title"] != "Event two" || inst1["start"] != "2006-01-02T12:00:00" || inst1["baseEventId"] != ev2ID || inst1["recurrenceId"] != "2006-01-02T12:00:00" {
		t.Fatalf("inst1 mismatch: %+v", inst1)
	}
	// Verify occurrence 2: Event two #2
	inst2 := expList[2].(map[string]any)
	if inst2["title"] != "Event two" || inst2["start"] != "2006-01-03T12:00:00" || inst2["baseEventId"] != ev2ID || inst2["recurrenceId"] != "2006-01-03T12:00:00" {
		t.Fatalf("inst2 mismatch: %+v", inst2)
	}
	// Verify occurrence 3: Event three
	inst3 := expList[3].(map[string]any)
	if inst3["title"] != "Event three" || inst3["start"] != "2006-01-04T09:00:00" || inst3["duration"] != "PT2H" || inst3["baseEventId"] != ev3ID {
		t.Fatalf("inst3 mismatch: %+v", inst3)
	}
	// Verify occurrence 4: Event two overridden
	inst4 := expList[4].(map[string]any)
	if inst4["title"] != "Event two overridden" || inst4["start"] != "2006-01-04T14:00:00" || inst4["baseEventId"] != ev2ID || inst4["recurrenceId"] != "2006-01-04T12:00:00" {
		t.Fatalf("inst4 mismatch: %+v", inst4)
	}
	// Verify occurrence 5: Event two #4
	inst5 := expList[5].(map[string]any)
	if inst5["title"] != "Event two" || inst5["start"] != "2006-01-05T12:00:00" || inst5["baseEventId"] != ev2ID || inst5["recurrenceId"] != "2006-01-05T12:00:00" {
		t.Fatalf("inst5 mismatch: %+v", inst5)
	}
	// Verify occurrence 6: Event two overridden twice
	inst6 := expList[6].(map[string]any)
	if inst6["title"] != "Event two overridden twice" || inst6["start"] != "2006-01-06T14:00:00" || inst6["baseEventId"] != ev2ID || inst6["recurrenceId"] != "2006-01-06T12:00:00" {
		t.Fatalf("inst6 mismatch: %+v", inst6)
	}

	// 16. Parse tests via Blob/upload and CalendarEvent/parse
	rawICS := "BEGIN:VCALENDAR\r\n" +
		"PRODID:-//xyz Corp//NONSGML PDA Calendar Version 1.0//EN\r\n" +
		"VERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\n" +
		"DTSTAMP:19960704T120000Z\r\n" +
		"UID:uid1@example.com\r\n" +
		"ORGANIZER:mailto:jsmith@example.com\r\n" +
		"DTSTART:19960918T143000Z\r\n" +
		"DTEND:19960920T220000Z\r\n" +
		"STATUS:CONFIRMED\r\n" +
		"CATEGORIES:CONFERENCE\r\n" +
		"SUMMARY:Networld+Interop Conference\r\n" +
		"DESCRIPTION:Networld+Interop Conference and Exhibit\\nAtlanta World Congress Center\\n\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"

	parseResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"ical": map[string]any{
					"data": []any{
						map[string]any{
							"data:asText": rawICS,
						},
					},
				},
			},
		}, "S4"},
		[]any{"CalendarEvent/parse", map[string]any{
			"accountId": "primary",
			"blobIds":   []string{"#ical"},
		}, "G4"},
	})

	parseArgs := parseResp.MethodResponses[1].Args
	parsedMap, ok := parseArgs["parsed"].(map[string]any)
	if !ok || len(parsedMap) == 0 {
		t.Fatalf("CalendarEvent/parse failed: %+v", parseArgs)
	}
	var parsedEvents []any
	for _, v := range parsedMap {
		parsedEvents = v.([]any)
		break
	}
	if len(parsedEvents) == 0 {
		t.Fatalf("expected parsed events array, got %+v", parsedMap)
	}
	pe := parsedEvents[0].(map[string]any)
	if pe["title"] != "Networld+Interop Conference" || pe["uid"] != "uid1@example.com" {
		t.Fatalf("parsed event mismatch: %+v", pe)
	}

	// 17. Deletion tests
	delResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{ev2ID, ev3ID},
		}, "d2"},
	})
	delDestroyed, _ := delResp.MethodResponses[0].Args["destroyed"].([]any)
	if len(delDestroyed) != 2 {
		t.Fatalf("expected 2 destroyed events, got %+v", delResp.MethodResponses[0].Args)
	}

	// 18. Organizer assignment tests
	orgResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"auto": map[string]any{
					"@type":       "Event",
					"uid":         "organizer-auto@example.com",
					"title":       "Organizer auto",
					"start":       "2006-01-04T10:00:00",
					"duration":    "PT1H",
					"timeZone":    "US/Eastern",
					"calendarIds": map[string]bool{cal1ID: true},
					"participants": map[string]any{
						"p1": map[string]any{
							"@type":               "Participant",
							"calendarAddress":     "mailto:jdoe@example.com",
							"participationStatus": "accepted",
							"roles":               map[string]bool{"chair": true, "owner": true},
						},
						"p2": map[string]any{
							"@type":               "Participant",
							"calendarAddress":     "mailto:rupert@example.com",
							"participationStatus": "needs-action",
						},
					},
				},
				"explicit": map[string]any{
					"@type":                    "Event",
					"uid":                      "organizer-explicit@example.com",
					"title":                    "Organizer explicit",
					"start":                    "2006-01-04T10:00:00",
					"duration":                 "PT1H",
					"timeZone":                 "US/Eastern",
					"calendarIds":              map[string]bool{cal1ID: true},
					"organizerCalendarAddress": "mailto:cyrus@example.com",
					"participants": map[string]any{
						"p1": map[string]any{
							"@type":               "Participant",
							"calendarAddress":     "mailto:jdoe@example.com",
							"participationStatus": "accepted",
							"roles":               map[string]bool{"chair": true, "owner": true},
						},
					},
				},
				"none": map[string]any{
					"@type":       "Event",
					"uid":         "organizer-none@example.com",
					"title":       "Organizer none",
					"start":       "2006-01-04T10:00:00",
					"duration":    "PT1H",
					"timeZone":    "US/Eastern",
					"calendarIds": map[string]bool{cal1ID: true},
				},
			},
		}, "org-create"},
	})
	orgCreated := orgResp.MethodResponses[0].Args["created"].(map[string]any)
	autoID := orgCreated["auto"].(map[string]any)["id"].(string)
	explicitID := orgCreated["explicit"].(map[string]any)["id"].(string)
	noneID := orgCreated["none"].(map[string]any)["id"].(string)

	orgGetResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "organizerCalendarAddress"},
			"ids":        []string{autoID, explicitID, noneID},
		}, "org-get"},
	})
	orgList := orgGetResp.MethodResponses[0].Args["list"].([]any)
	oAuto := orgList[0].(map[string]any)
	oExplicit := orgList[1].(map[string]any)
	oNone := orgList[2].(map[string]any)

	// Server assigns organizer when participants present
	if oAuto["organizerCalendarAddress"] != "mailto:primary" && oAuto["organizerCalendarAddress"] != "mailto:jdoe@example.com" {
		t.Fatalf("expected assigned organizer for auto, got %v", oAuto["organizerCalendarAddress"])
	}
	// Explicit organizer is never overwritten
	if oExplicit["organizerCalendarAddress"] != "mailto:cyrus@example.com" {
		t.Fatalf("expected explicit organizer preserved, got %v", oExplicit["organizerCalendarAddress"])
	}
	// Event without participants has null organizer
	if oNone["organizerCalendarAddress"] != nil && oNone["organizerCalendarAddress"] != "" {
		t.Fatalf("expected nil organizer for none, got %v", oNone["organizerCalendarAddress"])
	}

	// Adding participants assigns organizer
	postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				noneID: map[string]any{
					"participants": map[string]any{
						"p1": map[string]any{
							"@type":               "Participant",
							"calendarAddress":     "mailto:primary",
							"participationStatus": "accepted",
							"roles":               map[string]bool{"owner": true},
						},
					},
				},
			},
		}, "none-update"},
	})

	noneGetResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "organizerCalendarAddress"},
			"ids":        []string{noneID},
		}, "none-get"},
	})
	noneList := noneGetResp.MethodResponses[0].Args["list"].([]any)
	oNoneUpdated := noneList[0].(map[string]any)
	if oNoneUpdated["organizerCalendarAddress"] != "mailto:primary" {
		t.Fatalf("expected organizer assigned after adding participants, got %v", oNoneUpdated["organizerCalendarAddress"])
	}

	// 19. Unbounded yearly recurrences queryable across years
	yearlyResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"yearly": map[string]any{
					"@type":       "Event",
					"uid":         "yearly-unbounded@example.com",
					"title":       "Unbounded yearly event",
					"start":       "2018-06-01T09:00:00",
					"duration":    "PT1H",
					"timeZone":    "Etc/UTC",
					"calendarIds": map[string]bool{cal1ID: true},
					"recurrenceRule": map[string]any{
						"@type":     "RecurrenceRule",
						"frequency": "yearly",
					},
				},
			},
		}, "yearly-create"},
	})
	yearlyCreated, ok := yearlyResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || yearlyCreated["yearly"] == nil {
		t.Fatalf("yearly event creation failed: %+v", yearlyResp.MethodResponses[0].Args)
	}
	yearlyID := yearlyCreated["yearly"].(map[string]any)["id"].(string)

	yearlyQueryResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"after":  "2027-06-01T00:00:00",
				"before": "2027-07-01T00:00:00",
			},
			"sort":     []any{"start"},
			"timeZone": "Etc/UTC",
		}, "yearly-query"},
	})
	yearlyIDs, _ := yearlyQueryResp.MethodResponses[0].Args["ids"].([]any)
	foundYearly := false
	for _, id := range yearlyIDs {
		if id.(string) == yearlyID {
			foundYearly = true
			break
		}
	}
	if !foundYearly {
		t.Fatalf("unbounded yearly event was not returned from June 2027 query: %+v", yearlyIDs)
	}

	// Destroy yearly event
	postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{yearlyID},
		}, "yearly-destroy"},
	})
}
