package jmap_test

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

func TestStalwart_CalendarEventInstances(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.1", spectest.MUST,
		"A CalendarEvent object represents a calendar event or task.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.2", spectest.MUST,
		"CalendarEvent/get returns the requested properties for the specified event IDs.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.4", spectest.MUST,
		"CalendarEvent/set creates, updates, and destroys CalendarEvent objects.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.9", spectest.MUST,
		"CalendarEvent/query returns event ids matching the specified filter conditions.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{
		jmap.CoreCapabilityURI,
		jmap.CalendarsCapabilityURI,
	}

	expandInstances := func(calendarID string) []map[string]any {
		queryResp := postJMAP(t, ts.URL, using, []any{
			[]any{"CalendarEvent/query", map[string]any{
				"accountId": "primary",
				"filter": map[string]any{
					"inCalendar": calendarID,
					"after":      "2007-03-01T00:00:00",
					"before":     "2007-05-01T00:00:00",
				},
				"sort": []any{
					map[string]any{"property": "start"},
				},
				"timeZone":          "US/Eastern",
				"expandRecurrences": true,
			}, "q1"},
		})
		idsAny, _ := queryResp.MethodResponses[0].Args["ids"].([]any)
		if len(idsAny) == 0 {
			return nil
		}
		var ids []string
		for _, id := range idsAny {
			ids = append(ids, id.(string))
		}

		getResp := postJMAP(t, ts.URL, using, []any{
			[]any{"CalendarEvent/get", map[string]any{
				"accountId": "primary",
				"properties": []string{
					"id",
					"baseEventId",
					"start",
					"duration",
					"title",
					"recurrenceId",
					"locations",
					"recurrenceRule",
					"recurrenceOverrides",
				},
				"ids": ids,
			}, "g1"},
		})
		listAny, _ := getResp.MethodResponses[0].Args["list"].([]any)
		var res []map[string]any
		for _, item := range listAny {
			if m, ok := item.(map[string]any); ok {
				res = append(res, m)
			}
		}
		return res
	}

	starts := func(instances []map[string]any) []string {
		var res []string
		for _, inst := range instances {
			if s, ok := inst["start"].(string); ok {
				res = append(res, s)
			}
		}
		return res
	}

	instanceForStart := func(instances []map[string]any, start string) map[string]any {
		for _, inst := range instances {
			if s, ok := inst["start"].(string); ok && s == start {
				return inst
			}
		}
		t.Fatalf("missing instance starting at %s in %+v", start, instances)
		return nil
	}

	instanceIDForStart := func(instances []map[string]any, start string) string {
		inst := instanceForStart(instances, start)
		return inst["id"].(string)
	}

	baseEvent := func(id string) map[string]any {
		getResp := postJMAP(t, ts.URL, using, []any{
			[]any{"CalendarEvent/get", map[string]any{
				"accountId":  "primary",
				"properties": []string{"id", "title", "locations", "recurrenceOverrides"},
				"ids":        []string{id},
			}, "g_base"},
		})
		listAny, _ := getResp.MethodResponses[0].Args["list"].([]any)
		if len(listAny) == 0 {
			t.Fatalf("base event %s not found", id)
		}
		return listAny[0].(map[string]any)
	}

	// 1. Create calendar
	calResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"cal1": map[string]any{
					"name":     "Recurring events",
					"timeZone": "US/Eastern",
				},
			},
		}, "c1"},
	})
	createdCals, _ := calResp.MethodResponses[0].Args["created"].(map[string]any)
	calendarID := createdCals["cal1"].(map[string]any)["id"].(string)

	// 2. Create events
	evResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{
					"@type":    "Event",
					"uid":      "recurring-instances@example.com",
					"title":    "Daily standup",
					"start":    "2007-03-05T12:00:00",
					"duration": "PT1H",
					"timeZone": "US/Eastern",
					"updated":  "2007-02-06T00:11:21Z",
					"recurrenceRule": map[string]any{
						"frequency": "daily",
						"count":     5,
					},
					"locations": map[string]any{
						"loc1": map[string]any{
							"@type": "Location",
							"name":  "Room A",
						},
					},
					"recurrenceOverrides": map[string]any{
						"2007-03-07T12:00:00": map[string]any{
							"title":    "Moved standup",
							"start":    "2007-03-07T15:00:00",
							"duration": "PT1H",
							"updated":  "2007-02-06T00:11:21Z",
						},
					},
					"calendarIds": map[string]bool{
						calendarID: true,
					},
				},
				"e2": map[string]any{
					"@type":       "Event",
					"uid":         "single-instance@example.com",
					"title":       "One off",
					"start":       "2007-03-12T09:00:00",
					"duration":    "PT2H",
					"timeZone":    "US/Eastern",
					"updated":     "2007-02-06T00:11:21Z",
					"calendarIds": map[string]bool{calendarID: true},
				},
			},
		}, "ce1"},
	})
	createdEvs, _ := evResp.MethodResponses[0].Args["created"].(map[string]any)
	recurringID := createdEvs["e1"].(map[string]any)["id"].(string)
	singleID := createdEvs["e2"].(map[string]any)["id"].(string)

	// 3. The overridden occurrence keeps its original recurrence id
	instances := expandInstances(calendarID)
	expectedStarts := []string{
		"2007-03-05T12:00:00",
		"2007-03-06T12:00:00",
		"2007-03-07T15:00:00",
		"2007-03-08T12:00:00",
		"2007-03-09T12:00:00",
		"2007-03-12T09:00:00",
	}
	if !reflect.DeepEqual(starts(instances), expectedStarts) {
		t.Fatalf("expected starts %+v, got %+v", expectedStarts, starts(instances))
	}
	overriddenInst := instanceForStart(instances, "2007-03-07T15:00:00")
	if overriddenInst["recurrenceId"] != "2007-03-07T12:00:00" {
		t.Fatalf("expected recurrenceId 2007-03-07T12:00:00, got %v", overriddenInst["recurrenceId"])
	}

	// 4. Synthetic instances return null recurrence properties when requested
	for _, st := range []string{
		"2007-03-05T12:00:00",
		"2007-03-06T12:00:00",
		"2007-03-07T15:00:00",
		"2007-03-08T12:00:00",
		"2007-03-09T12:00:00",
	} {
		inst := instanceForStart(instances, st)
		if inst["recurrenceRule"] != nil {
			t.Fatalf("expected nil recurrenceRule for %s, got %v", st, inst["recurrenceRule"])
		}
		if inst["recurrenceOverrides"] != nil {
			t.Fatalf("expected nil recurrenceOverrides for %s, got %v", st, inst["recurrenceOverrides"])
		}
	}

	// 5. Unknown instances are reported as not found
	unknownID := recurringID + "#2099-01-01T00:00:00"
	upUnknownResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				unknownID: map[string]any{"title": "Nope"},
			},
		}, "u_unknown"},
	})
	notUpdated, _ := upUnknownResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errObj, ok := notUpdated[unknownID].(map[string]any)
	if !ok || errObj["type"] != "notFound" {
		t.Fatalf("expected notFound for unknownID %s, got %+v", unknownID, notUpdated)
	}

	// 6. Updating an instance generated by the recurrence rule creates an override
	id06 := instanceIDForStart(instances, "2007-03-06T12:00:00")
	up06Resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				id06: map[string]any{
					"title":               "Standup with guests",
					"locations/loc1/name": "Room B",
				},
			},
		}, "u_06"},
	})
	updatedMap, _ := up06Resp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := updatedMap[id06]; !ok {
		t.Fatalf("expected id06 updated, got %+v", up06Resp.MethodResponses[0].Args)
	}

	instances = expandInstances(calendarID)
	inst06 := instanceForStart(instances, "2007-03-06T12:00:00")
	if inst06["title"] != "Standup with guests" || inst06["duration"] != "PT1H" {
		t.Fatalf("inst06 mismatch: %+v", inst06)
	}
	locs, _ := inst06["locations"].(map[string]any)
	loc1, _ := locs["loc1"].(map[string]any)
	if loc1["name"] != "Room B" {
		t.Fatalf("inst06 loc1 name mismatch: %+v", loc1)
	}
	if instanceForStart(instances, "2007-03-05T12:00:00")["title"] != "Daily standup" {
		t.Fatalf("inst05 title changed unexpectedly")
	}

	// 7. Updating an occurrence that is already overridden patches the existing override
	id07 := instanceIDForStart(instances, "2007-03-07T15:00:00")
	up07Resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				id07: map[string]any{
					"title": "Moved standup, renamed",
				},
			},
		}, "u_07"},
	})
	updatedMap, _ = up07Resp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := updatedMap[id07]; !ok {
		t.Fatalf("expected id07 updated, got %+v", up07Resp.MethodResponses[0].Args)
	}

	base := baseEvent(recurringID)
	if base["title"] != "Daily standup" {
		t.Fatalf("base title mismatch: %+v", base)
	}
	baseOverrides, _ := base["recurrenceOverrides"].(map[string]any)
	ov06, _ := baseOverrides["2007-03-06T12:00:00"].(map[string]any)
	if ov06 == nil || ov06["title"] != "Standup with guests" {
		t.Fatalf("base override 06 mismatch: %+v", ov06)
	}
	ov07, _ := baseOverrides["2007-03-07T12:00:00"].(map[string]any)
	if ov07 == nil || ov07["title"] != "Moved standup, renamed" || ov07["start"] != "2007-03-07T15:00:00" {
		t.Fatalf("base override 07 mismatch: %+v", ov07)
	}

	// 8. A rejected instance is not written, even when another instance of the same event is
	instances = expandInstances(calendarID)
	goodID := instanceIDForStart(instances, "2007-03-09T12:00:00")
	badID := instanceIDForStart(instances, "2007-03-08T12:00:00")
	mixResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				goodID: map[string]any{"title": "Applied"},
				badID:  map[string]any{"title": "Rejected", "utcStart": "2007-03-08T17:00:00Z"},
			},
		}, "u_mix"},
	})
	upMap, _ := mixResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := upMap[goodID]; !ok {
		t.Fatalf("expected goodID updated: %+v", mixResp.MethodResponses[0].Args)
	}
	notUpMap, _ := mixResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
	badErr, _ := notUpMap[badID].(map[string]any)
	if badErr == nil || badErr["type"] != "invalidProperties" {
		t.Fatalf("expected invalidProperties for badID: %+v", notUpMap)
	}

	base = baseEvent(recurringID)
	baseOverrides, _ = base["recurrenceOverrides"].(map[string]any)
	var overrideKeys []string
	for k := range baseOverrides {
		overrideKeys = append(overrideKeys, k)
	}
	sort.Strings(overrideKeys)
	expectedKeys := []string{"2007-03-06T12:00:00", "2007-03-07T12:00:00", "2007-03-09T12:00:00"}
	if !reflect.DeepEqual(overrideKeys, expectedKeys) {
		t.Fatalf("expected override keys %+v, got %+v", expectedKeys, overrideKeys)
	}

	// 9. Destroying an instance removes just that occurrence
	instances = expandInstances(calendarID)
	id08 := instanceIDForStart(instances, "2007-03-08T12:00:00")
	del08Resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{id08},
		}, "d_08"},
	})
	desList, _ := del08Resp.MethodResponses[0].Args["destroyed"].([]any)
	if len(desList) != 1 || desList[0] != id08 {
		t.Fatalf("expected destroyed %s, got %+v", id08, del08Resp.MethodResponses[0].Args)
	}
	instances = expandInstances(calendarID)
	expectedStarts = []string{
		"2007-03-05T12:00:00",
		"2007-03-06T12:00:00",
		"2007-03-07T15:00:00",
		"2007-03-09T12:00:00",
		"2007-03-12T09:00:00",
	}
	if !reflect.DeepEqual(starts(instances), expectedStarts) {
		t.Fatalf("expected starts %+v, got %+v", expectedStarts, starts(instances))
	}

	// 10. Destroying an overridden instance does not bring back the original occurrence
	instances = expandInstances(calendarID)
	id07_overridden := instanceIDForStart(instances, "2007-03-07T15:00:00")
	del07Resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{id07_overridden},
		}, "d_07"},
	})
	desList, _ = del07Resp.MethodResponses[0].Args["destroyed"].([]any)
	if len(desList) != 1 || desList[0] != id07_overridden {
		t.Fatalf("expected destroyed %s, got %+v", id07_overridden, del07Resp.MethodResponses[0].Args)
	}
	instances = expandInstances(calendarID)
	expectedStarts = []string{
		"2007-03-05T12:00:00",
		"2007-03-06T12:00:00",
		"2007-03-09T12:00:00",
		"2007-03-12T09:00:00",
	}
	if !reflect.DeepEqual(starts(instances), expectedStarts) {
		t.Fatalf("expected starts %+v, got %+v", expectedStarts, starts(instances))
	}

	// 11. Several instances of the same event may be changed in a single request
	instances = expandInstances(calendarID)
	updateID := instanceIDForStart(instances, "2007-03-05T12:00:00")
	destroyID := instanceIDForStart(instances, "2007-03-09T12:00:00")
	multiResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				updateID: map[string]any{
					"title": "First standup",
				},
			},
			"destroy": []string{destroyID},
		}, "m1"},
	})
	upMap, _ = multiResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := upMap[updateID]; !ok {
		t.Fatalf("expected updateID updated: %+v", multiResp.MethodResponses[0].Args)
	}
	desList, _ = multiResp.MethodResponses[0].Args["destroyed"].([]any)
	if len(desList) != 1 || desList[0] != destroyID {
		t.Fatalf("expected destroyID destroyed: %+v", multiResp.MethodResponses[0].Args)
	}
	instances = expandInstances(calendarID)
	expectedStarts = []string{
		"2007-03-05T12:00:00",
		"2007-03-06T12:00:00",
		"2007-03-12T09:00:00",
	}
	if !reflect.DeepEqual(starts(instances), expectedStarts) {
		t.Fatalf("expected starts %+v, got %+v", expectedStarts, starts(instances))
	}
	if instanceForStart(instances, "2007-03-05T12:00:00")["title"] != "First standup" {
		t.Fatalf("expected title First standup")
	}

	// 12. Excluding the occurrence the event starts on is allowed
	instances = expandInstances(calendarID)
	id05 := instanceIDForStart(instances, "2007-03-05T12:00:00")
	del05Resp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{id05},
		}, "d_05"},
	})
	desList, _ = del05Resp.MethodResponses[0].Args["destroyed"].([]any)
	if len(desList) != 1 || desList[0] != id05 {
		t.Fatalf("expected id05 destroyed: %+v", del05Resp.MethodResponses[0].Args)
	}
	instances = expandInstances(calendarID)
	expectedStarts = []string{"2007-03-06T12:00:00", "2007-03-12T09:00:00"}
	if !reflect.DeepEqual(starts(instances), expectedStarts) {
		t.Fatalf("expected starts %+v, got %+v", expectedStarts, starts(instances))
	}

	// 13. A base event and its instances cannot be modified in the same request
	id06 = instanceIDForStart(instances, "2007-03-06T12:00:00")
	conflictResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				recurringID: map[string]any{"title": "Renamed"},
				id06:        map[string]any{"title": "Renamed instance"},
			},
		}, "u_conflict"},
	})
	notUpConflict, _ := conflictResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
	for _, id := range []string{recurringID, id06} {
		e, _ := notUpConflict[id].(map[string]any)
		if e == nil || e["type"] != "invalidProperties" || e["description"] != "A base event and its instances cannot be modified in the same request." {
			t.Fatalf("expected conflict error for %s, got %+v", id, notUpConflict)
		}
	}

	// 14. Properties that are not per-occurrence are rejected
	for _, prop := range []map[string]any{
		{"calendarIds": map[string]bool{calendarID: true}},
		{"isDraft": true},
		{"utcStart": "2007-03-06T17:00:00Z"},
		{"utcEnd": "2007-03-06T18:00:00Z"},
		{"mayInviteSelf": true},
		{"useDefaultAlerts": true},
	} {
		propResp := postJMAP(t, ts.URL, using, []any{
			[]any{"CalendarEvent/set", map[string]any{
				"accountId": "primary",
				"update": map[string]any{
					id06: prop,
				},
			}, "u_bad_prop"},
		})
		nu, _ := propResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
		errObj, _ := nu[id06].(map[string]any)
		if errObj == nil || errObj["type"] != "invalidProperties" || errObj["description"] != "This property cannot be modified on a single occurrence." {
			t.Fatalf("expected invalidProperties for bad prop %+v, got %+v", prop, nu)
		}
	}

	// 15. Properties an occurrence inherits from the base event are ignored, not rejected
	ignoreResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				id06: map[string]any{
					"@type":                            "Event",
					"title":                            "Ignoring inherited properties",
					"uid":                              "somebody-elses-uid@example.com",
					"recurrenceRule":                   map[string]any{"frequency": "weekly"},
					"privacy":                          "private",
					"participants/xyz/calendarAddress": "mailto:nobody@example.com",
				},
			},
		}, "u_ignore"},
	})
	upMap, _ = ignoreResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := upMap[id06]; !ok {
		t.Fatalf("expected id06 updated, got %+v", ignoreResp.MethodResponses[0].Args)
	}

	baseGetResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "uid", "recurrenceRule", "privacy", "participants"},
			"ids":        []string{recurringID},
		}, "g_base_check"},
	})
	baseItem := baseGetResp.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if baseItem["uid"] != "recurring-instances@example.com" {
		t.Fatalf("base uid modified unexpectedly: %v", baseItem["uid"])
	}
	rruleObj, _ := baseItem["recurrenceRule"].(map[string]any)
	if rruleObj == nil || rruleObj["frequency"] != "daily" {
		t.Fatalf("base recurrenceRule modified unexpectedly: %v", rruleObj)
	}
	if baseItem["privacy"] != nil {
		t.Fatalf("base privacy modified unexpectedly: %v", baseItem["privacy"])
	}
	if baseItem["participants"] != nil {
		t.Fatalf("base participants modified unexpectedly: %v", baseItem["participants"])
	}
	if instanceForStart(expandInstances(calendarID), "2007-03-06T12:00:00")["title"] != "Ignoring inherited properties" {
		t.Fatalf("instance title was not updated")
	}

	// 16. Destroying an event that is also being updated through one of its instances fails
	willDestroyResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				id06: map[string]any{"title": "Renamed instance"},
			},
			"destroy": []string{recurringID},
		}, "u_will_destroy"},
	})
	nu, _ := willDestroyResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errObj, _ = nu[id06].(map[string]any)
	if errObj == nil || errObj["type"] != "willDestroy" {
		t.Fatalf("expected willDestroy for id06, got %+v", nu)
	}
	desList, _ = willDestroyResp.MethodResponses[0].Args["destroyed"].([]any)
	if len(desList) != 1 || desList[0] != recurringID {
		t.Fatalf("expected recurringID destroyed: %+v", willDestroyResp.MethodResponses[0].Args)
	}

	// 17. A synthetic id of a non-recurring event refers to the event itself
	instances = expandInstances(calendarID)
	id12 := instanceIDForStart(instances, "2007-03-12T09:00:00")
	if id12 == singleID {
		t.Fatalf("expected synthetic id to differ from singleID")
	}
	upSingleResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				id12: map[string]any{"title": "One off, renamed"},
			},
		}, "u_single"},
	})
	upMap, _ = upSingleResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := upMap[id12]; !ok {
		t.Fatalf("expected synthetic single updated, got %+v", upSingleResp.MethodResponses[0].Args)
	}

	singleBase := baseEvent(singleID)
	if singleBase["title"] != "One off, renamed" {
		t.Fatalf("expected singleBase title updated, got %v", singleBase["title"])
	}

	instances = expandInstances(calendarID)
	id12 = instanceIDForStart(instances, "2007-03-12T09:00:00")
	delSingleResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{id12},
		}, "d_single"},
	})
	desList, _ = delSingleResp.MethodResponses[0].Args["destroyed"].([]any)
	if len(desList) != 1 || desList[0] != id12 {
		t.Fatalf("expected synthetic single destroyed, got %+v", delSingleResp.MethodResponses[0].Args)
	}
	if len(expandInstances(calendarID)) != 0 {
		t.Fatalf("expected no instances left, got %+v", expandInstances(calendarID))
	}

	// 18. Instances of floating all-day events keep their local time and duration
	allDayResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"allday": map[string]any{
					"@type":           "Event",
					"uid":             "all-day-instances@example.com",
					"title":           "Spring break",
					"start":           "2007-04-02T00:00:00",
					"duration":        "P1D",
					"showWithoutTime": true,
					"updated":         "2007-02-06T00:11:21Z",
					"recurrenceRule": map[string]any{
						"frequency": "daily",
						"count":     3,
					},
					"calendarIds": map[string]bool{calendarID: true},
				},
			},
		}, "c_allday"},
	})
	cAllDayMap, _ := allDayResp.MethodResponses[0].Args["created"].(map[string]any)
	allDayID := cAllDayMap["allday"].(map[string]any)["id"].(string)

	instances = expandInstances(calendarID)
	expectedStarts = []string{
		"2007-04-02T00:00:00",
		"2007-04-03T00:00:00",
		"2007-04-04T00:00:00",
	}
	if !reflect.DeepEqual(starts(instances), expectedStarts) {
		t.Fatalf("expected all day starts %+v, got %+v", expectedStarts, starts(instances))
	}

	updateAllDayID := instanceIDForStart(instances, "2007-04-03T00:00:00")
	destroyAllDayID := instanceIDForStart(instances, "2007-04-04T00:00:00")
	upAllDayResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				updateAllDayID: map[string]any{
					"title": "Spring break, day two",
				},
			},
			"destroy": []string{destroyAllDayID},
		}, "m_allday"},
	})
	upMap, _ = upAllDayResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := upMap[updateAllDayID]; !ok {
		t.Fatalf("expected updateAllDayID updated, got %+v", upAllDayResp.MethodResponses[0].Args)
	}
	desList, _ = upAllDayResp.MethodResponses[0].Args["destroyed"].([]any)
	if len(desList) != 1 || desList[0] != destroyAllDayID {
		t.Fatalf("expected destroyAllDayID destroyed, got %+v", upAllDayResp.MethodResponses[0].Args)
	}

	instances = expandInstances(calendarID)
	expectedStarts = []string{
		"2007-04-02T00:00:00",
		"2007-04-03T00:00:00",
	}
	if !reflect.DeepEqual(starts(instances), expectedStarts) {
		t.Fatalf("expected starts %+v, got %+v", expectedStarts, starts(instances))
	}
	updatedDay2 := instanceForStart(instances, "2007-04-03T00:00:00")
	if updatedDay2["title"] != "Spring break, day two" || updatedDay2["duration"] != "P1D" {
		t.Fatalf("updatedDay2 mismatch: %+v", updatedDay2)
	}

	// Destroy allDay event
	postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{allDayID},
		}, "d_allday_master"},
	})

	// 19. Synthetic ids keep identifying the same occurrence across writes
	seriesCalResp := postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"scal": map[string]any{
					"name":     "Stable instance ids",
					"timeZone": "US/Eastern",
				},
			},
		}, "c_scal"},
	})
	cScalMap, _ := seriesCalResp.MethodResponses[0].Args["created"].(map[string]any)
	seriesCalendarID := cScalMap["scal"].(map[string]any)["id"].(string)

	seriesEvResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sev": map[string]any{
					"@type":    "Event",
					"uid":      "stable-instance-ids@example.com",
					"title":    "Weekly sync",
					"start":    "2007-04-02T09:00:00",
					"duration": "PT1H",
					"timeZone": "US/Eastern",
					"updated":  "2007-02-06T00:11:21Z",
					"recurrenceRule": map[string]any{
						"frequency": "weekly",
						"count":     5,
					},
					"calendarIds": map[string]bool{seriesCalendarID: true},
				},
			},
		}, "c_sev"},
	})
	cSevMap, _ := seriesEvResp.MethodResponses[0].Args["created"].(map[string]any)
	if cSevMap["sev"] == nil {
		t.Fatalf("series event creation failed: %+v", seriesEvResp.MethodResponses[0].Args)
	}

	instances = expandInstances(seriesCalendarID)
	var heldIDs []string
	for _, inst := range instances {
		heldIDs = append(heldIDs, inst["id"].(string))
	}
	expectedStarts = []string{
		"2007-04-02T09:00:00",
		"2007-04-09T09:00:00",
		"2007-04-16T09:00:00",
		"2007-04-23T09:00:00",
		"2007-04-30T09:00:00",
	}
	if !reflect.DeepEqual(starts(instances), expectedStarts) {
		t.Fatalf("expected weekly sync starts %+v, got %+v", expectedStarts, starts(instances))
	}

	movedID := instanceIDForStart(instances, "2007-04-09T09:00:00")
	moveResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				movedID: map[string]any{
					"title": "Weekly sync, moved",
					"start": "2007-04-09T14:00:00",
				},
			},
		}, "u_move"},
	})
	upMap, _ = moveResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := upMap[movedID]; !ok {
		t.Fatalf("expected movedID updated, got %+v", moveResp.MethodResponses[0].Args)
	}

	heldGetResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "start", "recurrenceId"},
			"ids":        heldIDs,
		}, "g_held"},
	})
	heldListAny, _ := heldGetResp.MethodResponses[0].Args["list"].([]any)
	var heldList []map[string]any
	for _, item := range heldListAny {
		heldList = append(heldList, item.(map[string]any))
	}
	heldStart := func(id string) string {
		for _, ev := range heldList {
			if ev["id"] == id {
				return ev["start"].(string)
			}
		}
		t.Fatalf("missing instance %s in heldList: %+v", id, heldList)
		return ""
	}
	var actualHeldStarts []string
	for _, id := range heldIDs {
		actualHeldStarts = append(actualHeldStarts, heldStart(id))
	}
	expectedHeldStarts := []string{
		"2007-04-02T09:00:00",
		"2007-04-09T14:00:00",
		"2007-04-16T09:00:00",
		"2007-04-23T09:00:00",
		"2007-04-30T09:00:00",
	}
	if !reflect.DeepEqual(actualHeldStarts, expectedHeldStarts) {
		t.Fatalf("expected actualHeldStarts %+v, got %+v", expectedHeldStarts, actualHeldStarts)
	}

	for _, ev := range heldList {
		if ev["id"] == heldIDs[1] {
			if ev["recurrenceId"] != "2007-04-09T09:00:00" {
				t.Fatalf("expected recurrenceId 2007-04-09T09:00:00, got %v", ev["recurrenceId"])
			}
		}
	}

	// Destroying an id held across a write removes the occurrence it was issued for
	destroyHeldResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{heldIDs[2]},
		}, "d_held2"},
	})
	desList, _ = destroyHeldResp.MethodResponses[0].Args["destroyed"].([]any)
	if len(desList) != 1 || desList[0] != heldIDs[2] {
		t.Fatalf("expected heldIDs[2] destroyed, got %+v", destroyHeldResp.MethodResponses[0].Args)
	}

	instances = expandInstances(seriesCalendarID)
	expectedStarts = []string{
		"2007-04-02T09:00:00",
		"2007-04-09T14:00:00",
		"2007-04-23T09:00:00",
		"2007-04-30T09:00:00",
	}
	if !reflect.DeepEqual(starts(instances), expectedStarts) {
		t.Fatalf("expected series starts %+v, got %+v", expectedStarts, starts(instances))
	}

	// 20. Clean up
	postJMAP(t, ts.URL, using, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId":             "primary",
			"destroy":               []string{calendarID, seriesCalendarID},
			"onDestroyRemoveEvents": true,
		}, "d_clean"},
	})
	_ = json.Marshal
}
