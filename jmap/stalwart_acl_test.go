package jmap_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/nextcloud"
	"imap-jmap/jmap/spectest"
)

// TestStalwart_CalendarACL translates Stalwart's calendar acl.rs integration test suite.
// It verifies:
//   - Calendar.shareWith and Calendar.myRights per draft-ietf-jmap-calendars §1.4.
//   - Cross-account access authorization and "forbidden" method error per RFC 8620 §3.6.2.
//   - ShareNotification generation, change tracking, and retrieval per RFC 9670 §2, §3, §4.
//   - Permission enforcement on shared calendar update and destroy.
//   - Permission enforcement on shared calendar event update and destroy.
//   - CalendarEvent/copy into a shared calendar.
//   - Per-user calendar rename and description overrides.
//   - Revoking sharing rights and deleting shared calendars.
func TestStalwart_CalendarACL(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "1.4", spectest.MUST,
		"Calendar shareWith defines access rights granted to users, and myRights reflects the caller's rights.")
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST,
		"forbidden error is returned when accessing an account or calling a method without permission.")
	spectest.Require(t, "RFC9670", "2", spectest.MUST,
		"A ShareNotification object represents a change to the sharing status of an object.")
	spectest.Require(t, "RFC9670", "3", spectest.MUST,
		"ShareNotification/get returns requested properties for share notifications.")
	spectest.Require(t, "RFC9670", "4", spectest.MUST,
		"ShareNotification/changes returns changes to share notifications since a specified state.")

	johnUser := "jdoe@example.com"
	janeUser := "jane.smith@example.com"
	johnID := jmap.AccountIDForSubject(johnUser)
	janeID := jmap.AccountIDForSubject(janeUser)

	_, calBackend, contactsBackend, fileNodeBackend, principalsBackend, cleanup := nextcloud.NewEmbeddedBackend(johnUser, janeUser)
	defer cleanup()

	srv := jmap.NewServer(nil,
		jmap.WithCalendarsBackend(calBackend),
		jmap.WithPrincipalsBackend(principalsBackend),
		jmap.WithContactsBackend(contactsBackend),
		jmap.WithFileNodeBackend(fileNodeBackend),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{
		jmap.CoreCapabilityURI,
		jmap.CalendarsCapabilityURI,
		jmap.SharingCapabilityURI,
		jmap.PrincipalsCapabilityURI,
	}

	callJMAP := func(user string, calls []any) []jmap.Invocation {
		t.Helper()
		reqBody := map[string]any{
			"using":       using,
			"methodCalls": calls,
		}
		raw, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("Marshal request: %v", err)
		}
		httpReq, err := http.NewRequest("POST", ts.URL+"/jmap", bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+user)

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			t.Fatalf("POST /jmap: %v", err)
		}
		defer resp.Body.Close()

		var res struct {
			MethodResponses []jmap.Invocation `json:"methodResponses"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			t.Fatalf("Decode response: %v", err)
		}
		return res.MethodResponses
	}

	// 1. Create test calendars and events
	resJohnCal := callJMAP(johnUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": johnID,
			"create": map[string]any{
				"c1": map[string]any{
					"name": "Test #1",
				},
			},
		}, "call-1"},
	})
	createdJohnCal, ok := resJohnCal[0].Args["created"].(map[string]any)
	if !ok || createdJohnCal["c1"] == nil {
		t.Fatalf("Failed to create John's calendar: %v", resJohnCal[0].Args)
	}
	johnCalMap := createdJohnCal["c1"].(map[string]any)
	johnCalendarID := johnCalMap["id"].(string)

	resJohnEv := callJMAP(johnUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": johnID,
			"create": map[string]any{
				"e1": map[string]any{
					"@type":    "Event",
					"uid":      "a8df6573-0474-496d-8496-033ad45d7fea",
					"updated":  "2020-01-02T18:23:04Z",
					"title":    "John's Simple Event",
					"start":    "2020-01-15T13:00:00",
					"timeZone": "America/New_York",
					"duration": "PT1H",
					"calendarIds": map[string]bool{
						johnCalendarID: true,
					},
				},
			},
		}, "call-2"},
	})
	createdJohnEv, ok := resJohnEv[0].Args["created"].(map[string]any)
	if !ok || createdJohnEv["e1"] == nil {
		t.Fatalf("Failed to create John's event: %v", resJohnEv[0].Args)
	}
	johnEvMap := createdJohnEv["e1"].(map[string]any)
	johnEventID := johnEvMap["id"].(string)

	resJaneCal := callJMAP(janeUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": janeID,
			"create": map[string]any{
				"c1": map[string]any{
					"name": "Test #1",
				},
			},
		}, "call-3"},
	})
	createdJaneCal, ok := resJaneCal[0].Args["created"].(map[string]any)
	if !ok || createdJaneCal["c1"] == nil {
		t.Fatalf("Failed to create Jane's calendar: %v", resJaneCal[0].Args)
	}
	janeCalMap := createdJaneCal["c1"].(map[string]any)
	janeCalendarID := janeCalMap["id"].(string)

	resJaneEv := callJMAP(janeUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": janeID,
			"create": map[string]any{
				"e1": map[string]any{
					"uid":      "a8df6575-0474-496d-8496-033ad45d7fea",
					"updated":  "2020-01-02T18:23:04Z",
					"title":    "Jane's Simple Event",
					"start":    "2020-01-15T13:00:00",
					"timeZone": "America/New_York",
					"duration": "PT1H",
					"calendarIds": map[string]bool{
						janeCalendarID: true,
					},
				},
			},
		}, "call-4"},
	})
	createdJaneEv, ok := resJaneEv[0].Args["created"].(map[string]any)
	if !ok || createdJaneEv["e1"] == nil {
		t.Fatalf("Failed to create Jane's event: %v", resJaneEv[0].Args)
	}
	janeEvMap := createdJaneEv["e1"].(map[string]any)
	janeEventID := janeEvMap["id"].(string)

	// 2. Verify myRights for John on his calendar
	resGetJohnCal := callJMAP(johnUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "name", "myRights", "shareWith"},
			"ids":        []string{johnCalendarID},
		}, "call-5"},
	})
	listJohnCal := resGetJohnCal[0].Args["list"].([]any)
	if len(listJohnCal) == 0 {
		t.Fatalf("Expected 1 calendar in John's list, got %v", listJohnCal)
	}
	cal0 := listJohnCal[0].(map[string]any)
	if cal0["id"] != johnCalendarID || cal0["name"] != "Test #1" {
		t.Errorf("Unexpected calendar data: %v", cal0)
	}
	rights0 := cal0["myRights"].(map[string]any)
	for _, prop := range []string{"mayReadItems", "mayWriteAll", "mayDelete", "mayShare", "mayWriteOwn", "mayReadFreeBusy", "mayUpdatePrivate", "mayRSVP"} {
		if val, _ := rights0[prop].(bool); !val {
			t.Errorf("Expected myRights.%s == true, got %v", prop, rights0[prop])
		}
	}
	shareWith0, ok := cal0["shareWith"].(map[string]any)
	if !ok || len(shareWith0) != 0 {
		t.Errorf("Expected empty shareWith {}, got %v", cal0["shareWith"])
	}

	// 3. Obtain initial share notifications state for Jane
	resJaneInitNotifs := callJMAP(janeUser, []any{
		[]any{"ShareNotification/get", map[string]any{
			"accountId": janeID,
			"ids":       []string{},
		}, "call-6"},
	})
	janeShareChangeID, _ := resJaneInitNotifs[0].Args["state"].(string)

	// 4. Make sure Jane has no access to John's account yet -> returns "forbidden"
	resForbidden := callJMAP(janeUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId": johnID,
			"ids":       []string{johnCalendarID},
		}, "call-7"},
	})
	if resForbidden[0].Name != "error" {
		t.Fatalf("Expected error response, got %s", resForbidden[0].Name)
	}
	errTyp, _ := resForbidden[0].Args["type"].(string)
	if errTyp != "forbidden" {
		t.Errorf("Expected error type 'forbidden', got %q", errTyp)
	}

	// 5. Share calendar with Jane
	resShare := callJMAP(johnUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": johnID,
			"update": map[string]any{
				johnCalendarID: map[string]any{
					"shareWith": map[string]any{
						janeID: map[string]any{
							"mayReadItems": true,
						},
					},
				},
			},
		}, "call-8"},
	})
	updatedShare, _ := resShare[0].Args["updated"].(map[string]any)
	if _, ok := updatedShare[johnCalendarID]; !ok {
		t.Fatalf("Expected calendar updated with shareWith: %v", resShare[0].Args)
	}

	resGetSharedJohnCal := callJMAP(johnUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "name", "shareWith"},
			"ids":        []string{johnCalendarID},
		}, "call-9"},
	})
	listJohnCal2 := resGetSharedJohnCal[0].Args["list"].([]any)
	cal2 := listJohnCal2[0].(map[string]any)
	sw2 := cal2["shareWith"].(map[string]any)
	janeRights, ok := sw2[janeID].(map[string]any)
	if !ok {
		t.Fatalf("Expected shareWith[%s], got %v", janeID, sw2)
	}
	if val, _ := janeRights["mayReadItems"].(bool); !val {
		t.Errorf("Expected mayReadItems == true")
	}
	for _, prop := range []string{"mayWriteAll", "mayDelete", "mayShare", "mayWriteOwn", "mayReadFreeBusy", "mayUpdatePrivate", "mayRSVP"} {
		if val, _ := janeRights[prop].(bool); val {
			t.Errorf("Expected %s == false, got true", prop)
		}
	}

	// 6. Verify Jane can access John's calendar and event
	resJaneGetCal := callJMAP(janeUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "name", "myRights"},
			"ids":        []string{johnCalendarID},
		}, "call-10"},
	})
	if resJaneGetCal[0].Name != "Calendar/get" {
		t.Fatalf("Expected Calendar/get response, got %s: %v", resJaneGetCal[0].Name, resJaneGetCal[0].Args)
	}
	janeListCal := resJaneGetCal[0].Args["list"].([]any)
	if len(janeListCal) == 0 {
		t.Fatalf("Expected Jane to see John's shared calendar")
	}
	janeCalView := janeListCal[0].(map[string]any)
	if janeCalView["id"] != johnCalendarID || janeCalView["name"] != "Test #1" {
		t.Errorf("Unexpected shared calendar view: %v", janeCalView)
	}
	janeViewRights := janeCalView["myRights"].(map[string]any)
	if val, _ := janeViewRights["mayReadItems"].(bool); !val {
		t.Errorf("Expected Jane myRights.mayReadItems == true")
	}
	for _, prop := range []string{"mayWriteAll", "mayDelete", "mayShare", "mayWriteOwn", "mayReadFreeBusy", "mayUpdatePrivate", "mayRSVP"} {
		if val, _ := janeViewRights[prop].(bool); val {
			t.Errorf("Expected Jane myRights.%s == false, got true", prop)
		}
	}

	resJaneGetEv := callJMAP(janeUser, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "title"},
			"ids":        []string{johnEventID},
		}, "call-11"},
	})
	if resJaneGetEv[0].Name != "CalendarEvent/get" {
		t.Fatalf("Expected CalendarEvent/get, got %s: %v", resJaneGetEv[0].Name, resJaneGetEv[0].Args)
	}
	janeEvList := resJaneGetEv[0].Args["list"].([]any)
	if len(janeEvList) == 0 {
		t.Fatalf("Expected Jane to see John's shared event")
	}
	evItem := janeEvList[0].(map[string]any)
	if evItem["id"] != johnEventID || evItem["title"] != "John's Simple Event" {
		t.Errorf("Unexpected event data: %v", evItem)
	}

	// 7. Verify Jane received a ShareNotification
	resJaneChanges1 := callJMAP(janeUser, []any{
		[]any{"ShareNotification/changes", map[string]any{
			"accountId":  janeID,
			"sinceState": janeShareChangeID,
		}, "call-12"},
	})
	janeShareChangeID, _ = resJaneChanges1[0].Args["newState"].(string)
	createdNotifs := resJaneChanges1[0].Args["created"].([]any)
	if len(createdNotifs) != 1 {
		t.Fatalf("Expected 1 created share notification, got %v", createdNotifs)
	}
	shareID := createdNotifs[0].(string)

	resJaneGetNotif := callJMAP(janeUser, []any{
		[]any{"ShareNotification/get", map[string]any{
			"accountId": janeID,
			"properties": []string{
				"id", "changedBy", "objectType", "objectAccountId", "objectId", "oldRights", "newRights", "name",
			},
			"ids": []string{shareID},
		}, "call-13"},
	})
	notifList := resJaneGetNotif[0].Args["list"].([]any)
	if len(notifList) == 0 {
		t.Fatalf("Expected notification in list")
	}
	notif0 := notifList[0].(map[string]any)
	if notif0["id"] != shareID {
		t.Errorf("Expected id == %s, got %v", shareID, notif0["id"])
	}
	changedBy := notif0["changedBy"].(map[string]any)
	if changedBy["principalId"] != johnID || changedBy["name"] != "John Doe" || changedBy["email"] != "jdoe@example.com" {
		t.Errorf("Unexpected changedBy: %v", changedBy)
	}
	if notif0["objectType"] != "Calendar" || notif0["objectAccountId"] != johnID || notif0["objectId"] != johnCalendarID {
		t.Errorf("Unexpected notification metadata: %v", notif0)
	}
	oldR0 := notif0["oldRights"].(map[string]any)
	for _, prop := range []string{"mayReadItems", "mayWriteAll", "mayDelete", "mayShare", "mayWriteOwn", "mayReadFreeBusy", "mayUpdatePrivate", "mayRSVP"} {
		if val, _ := oldR0[prop].(bool); val {
			t.Errorf("Expected oldRights.%s == false, got true", prop)
		}
	}
	newR0 := notif0["newRights"].(map[string]any)
	if val, _ := newR0["mayReadItems"].(bool); !val {
		t.Errorf("Expected newRights.mayReadItems == true")
	}
	for _, prop := range []string{"mayWriteAll", "mayDelete", "mayShare", "mayWriteOwn", "mayReadFreeBusy", "mayUpdatePrivate", "mayRSVP"} {
		if val, _ := newR0[prop].(bool); val {
			t.Errorf("Expected newRights.%s == false, got true", prop)
		}
	}
	if notif0["name"] != nil {
		t.Errorf("Expected name == nil, got %v", notif0["name"])
	}

	// 8. Updating and deleting should fail for Jane without write/delete rights
	resFailUpdateCal := callJMAP(janeUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": johnID,
			"update": map[string]any{
				johnCalendarID: map[string]any{},
			},
		}, "call-14"},
	})
	notUpdatedCal := resFailUpdateCal[0].Args["notUpdated"].(map[string]any)
	errObj1 := notUpdatedCal[johnCalendarID].(map[string]any)
	if errObj1["description"] != "You are not allowed to modify this calendar." {
		t.Errorf("Expected 'You are not allowed to modify this calendar.', got %v", errObj1["description"])
	}

	resFailDestroyCal := callJMAP(janeUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": johnID,
			"destroy":   []string{johnCalendarID},
		}, "call-15"},
	})
	notDestroyedCal := resFailDestroyCal[0].Args["notDestroyed"].(map[string]any)
	errObj2 := notDestroyedCal[johnCalendarID].(map[string]any)
	if errObj2["description"] != "You are not allowed to delete this calendar." {
		t.Errorf("Expected 'You are not allowed to delete this calendar.', got %v", errObj2["description"])
	}

	resFailUpdateEv := callJMAP(janeUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": johnID,
			"update": map[string]any{
				johnEventID: map[string]any{},
			},
		}, "call-16"},
	})
	notUpdatedEv := resFailUpdateEv[0].Args["notUpdated"].(map[string]any)
	errObj3 := notUpdatedEv[johnEventID].(map[string]any)
	desc3, _ := errObj3["description"].(string)
	if !strings.Contains(desc3, "You are not allowed to modify calendar") {
		t.Errorf("Expected description containing 'You are not allowed to modify calendar', got %v", desc3)
	}

	resFailDestroyEv := callJMAP(janeUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": johnID,
			"destroy":   []string{johnEventID},
		}, "call-17"},
	})
	notDestroyedEv := resFailDestroyEv[0].Args["notDestroyed"].(map[string]any)
	errObj4 := notDestroyedEv[johnEventID].(map[string]any)
	desc4, _ := errObj4["description"].(string)
	if !strings.Contains(desc4, "You are not allowed to remove events from calendar") {
		t.Errorf("Expected description containing 'You are not allowed to remove events from calendar', got %v", desc4)
	}

	// 9. Grant Jane write and delete access
	resGrantWrite := callJMAP(johnUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": johnID,
			"update": map[string]any{
				johnCalendarID: map[string]any{
					fmt.Sprintf("shareWith/%s/mayWriteAll", janeID): true,
					fmt.Sprintf("shareWith/%s/mayDelete", janeID):   true,
				},
			},
		}, "call-18"},
	})
	updGrant, _ := resGrantWrite[0].Args["updated"].(map[string]any)
	if _, ok := updGrant[johnCalendarID]; !ok {
		t.Fatalf("Expected calendar updated: %v", resGrantWrite[0].Args)
	}

	resJaneGetCalWithWrite := callJMAP(janeUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "name", "myRights"},
			"ids":        []string{johnCalendarID},
		}, "call-19"},
	})
	janeViewCal2 := resJaneGetCalWithWrite[0].Args["list"].([]any)[0].(map[string]any)
	r2 := janeViewCal2["myRights"].(map[string]any)
	for _, prop := range []string{"mayReadItems", "mayWriteAll", "mayDelete"} {
		if val, _ := r2[prop].(bool); !val {
			t.Errorf("Expected Jane myRights.%s == true, got false", prop)
		}
	}
	for _, prop := range []string{"mayShare", "mayWriteOwn", "mayReadFreeBusy", "mayUpdatePrivate", "mayRSVP"} {
		if val, _ := r2[prop].(bool); val {
			t.Errorf("Expected Jane myRights.%s == false, got true", prop)
		}
	}

	// 10. Verify Jane received a second ShareNotification with updated rights
	resJaneChanges2 := callJMAP(janeUser, []any{
		[]any{"ShareNotification/changes", map[string]any{
			"accountId":  janeID,
			"sinceState": janeShareChangeID,
		}, "call-20"},
	})
	janeShareChangeID, _ = resJaneChanges2[0].Args["newState"].(string)
	createdNotifs2 := resJaneChanges2[0].Args["created"].([]any)
	if len(createdNotifs2) != 1 {
		t.Fatalf("Expected 1 created notification, got %v", createdNotifs2)
	}
	shareID2 := createdNotifs2[0].(string)

	resJaneGetNotif2 := callJMAP(janeUser, []any{
		[]any{"ShareNotification/get", map[string]any{
			"accountId": janeID,
			"properties": []string{
				"id", "changedBy", "objectType", "objectAccountId", "objectId", "oldRights", "newRights", "name",
			},
			"ids": []string{shareID2},
		}, "call-21"},
	})
	notif2 := resJaneGetNotif2[0].Args["list"].([]any)[0].(map[string]any)
	oldR2 := notif2["oldRights"].(map[string]any)
	if val, _ := oldR2["mayReadItems"].(bool); !val {
		t.Errorf("Expected oldRights.mayReadItems == true")
	}
	if val, _ := oldR2["mayWriteAll"].(bool); val {
		t.Errorf("Expected oldRights.mayWriteAll == false")
	}
	newR2 := notif2["newRights"].(map[string]any)
	if val, _ := newR2["mayReadItems"].(bool); !val {
		t.Errorf("Expected newRights.mayReadItems == true")
	}
	if val, _ := newR2["mayWriteAll"].(bool); !val {
		t.Errorf("Expected newRights.mayWriteAll == true")
	}
	if val, _ := newR2["mayDelete"].(bool); !val {
		t.Errorf("Expected newRights.mayDelete == true")
	}

	// 11. Creating a calendar in a shared account should fail
	resFailCreateCal := callJMAP(janeUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": johnID,
			"create": map[string]any{
				"newcal": map[string]any{
					"name": "A new shared calendar",
				},
			},
		}, "call-22"},
	})
	notCreatedMap := resFailCreateCal[0].Args["notCreated"].(map[string]any)
	errObjCreate := notCreatedMap["newcal"].(map[string]any)
	if errObjCreate["description"] != "Cannot create calendars in a shared account." {
		t.Errorf("Expected 'Cannot create calendars in a shared account.', got %v", errObjCreate["description"])
	}

	// 12. Copy Jane's event into John's calendar
	resCopy := callJMAP(janeUser, []any{
		[]any{"CalendarEvent/copy", map[string]any{
			"fromAccountId": janeID,
			"accountId":     johnID,
			"create": map[string]any{
				janeEventID: map[string]any{
					"calendarIds": map[string]bool{
						johnCalendarID: true,
					},
				},
			},
		}, "call-23"},
	})
	copiedMap, ok := resCopy[0].Args["created"].(map[string]any)
	if !ok || copiedMap[janeEventID] == nil {
		t.Fatalf("Expected event copied: %v", resCopy[0].Args)
	}
	copiedEv := copiedMap[janeEventID].(map[string]any)
	johnCopiedEventID := copiedEv["id"].(string)

	resGetCopiedEv := callJMAP(janeUser, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "calendarIds", "title"},
			"ids":        []string{johnCopiedEventID},
		}, "call-24"},
	})
	evCopiedList := resGetCopiedEv[0].Args["list"].([]any)
	if len(evCopiedList) == 0 {
		t.Fatalf("Expected copied event in list")
	}
	copiedView := evCopiedList[0].(map[string]any)
	if copiedView["id"] != johnCopiedEventID || copiedView["title"] != "Jane's Simple Event" {
		t.Errorf("Unexpected copied event view: %v", copiedView)
	}

	// 13. Destroy the copied event
	resDestroyCopied := callJMAP(janeUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": johnID,
			"destroy":   []string{johnCopiedEventID},
		}, "call-25"},
	})
	destroyedList := resDestroyCopied[0].Args["destroyed"].([]any)
	if len(destroyedList) != 1 || destroyedList[0] != johnCopiedEventID {
		t.Errorf("Expected destroyed == [%s], got %v", johnCopiedEventID, destroyedList)
	}

	// 14. Update John's event
	resUpdateEv := callJMAP(janeUser, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": johnID,
			"update": map[string]any{
				johnEventID: map[string]any{
					"title": "John's Updated Event",
				},
			},
		}, "call-26"},
	})
	updEvMap, _ := resUpdateEv[0].Args["updated"].(map[string]any)
	if _, ok := updEvMap[johnEventID]; !ok {
		t.Fatalf("Expected event updated: %v", resUpdateEv[0].Args)
	}

	resGetUpdatedEv := callJMAP(janeUser, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "title"},
			"ids":        []string{johnEventID},
		}, "call-27"},
	})
	updatedEvView := resGetUpdatedEv[0].Args["list"].([]any)[0].(map[string]any)
	if updatedEvView["id"] != johnEventID || updatedEvView["title"] != "John's Updated Event" {
		t.Errorf("Expected title 'John's Updated Event', got %v", updatedEvView)
	}

	// 15. Update John's calendar name and description (per-user override)
	resUpdateCalName := callJMAP(janeUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": johnID,
			"update": map[string]any{
				johnCalendarID: map[string]any{
					"name":        "Jane's version of John's Calendar",
					"description": "This is John's calendar, but Jane can edit it now",
				},
			},
		}, "call-28"},
	})
	updCalMap, _ := resUpdateCalName[0].Args["updated"].(map[string]any)
	if _, ok := updCalMap[johnCalendarID]; !ok {
		t.Fatalf("Expected calendar updated: %v", resUpdateCalName[0].Args)
	}

	// Jane should see her overridden name and description
	resJaneGetOverriddenCal := callJMAP(janeUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "name", "description"},
			"ids":        []string{johnCalendarID},
		}, "call-29"},
	})
	janeCalOverridden := resJaneGetOverriddenCal[0].Args["list"].([]any)[0].(map[string]any)
	if janeCalOverridden["id"] != johnCalendarID ||
		janeCalOverridden["name"] != "Jane's version of John's Calendar" ||
		janeCalOverridden["description"] != "This is John's calendar, but Jane can edit it now" {
		t.Errorf("Unexpected Jane view of calendar: %v", janeCalOverridden)
	}

	// John should still see the original name and null description
	resJohnGetOriginalCal := callJMAP(johnUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "name", "description"},
			"ids":        []string{johnCalendarID},
		}, "call-30"},
	})
	johnCalOriginal := resJohnGetOriginalCal[0].Args["list"].([]any)[0].(map[string]any)
	if johnCalOriginal["id"] != johnCalendarID ||
		johnCalOriginal["name"] != "Test #1" ||
		johnCalOriginal["description"] != nil {
		t.Errorf("Unexpected John view of calendar: %v", johnCalOriginal)
	}

	// 16. Revoke Jane's access
	resRevoke := callJMAP(johnUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": johnID,
			"update": map[string]any{
				johnCalendarID: map[string]any{
					fmt.Sprintf("shareWith/%s", janeID): nil,
				},
			},
		}, "call-31"},
	})
	updRevoke, _ := resRevoke[0].Args["updated"].(map[string]any)
	if _, ok := updRevoke[johnCalendarID]; !ok {
		t.Fatalf("Expected calendar revoked: %v", resRevoke[0].Args)
	}

	resGetRevokedCal := callJMAP(johnUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId":  johnID,
			"properties": []string{"id", "name", "shareWith"},
			"ids":        []string{johnCalendarID},
		}, "call-32"},
	})
	revokedCalView := resGetRevokedCal[0].Args["list"].([]any)[0].(map[string]any)
	revokedSW := revokedCalView["shareWith"].(map[string]any)
	if len(revokedSW) != 0 {
		t.Errorf("Expected shareWith == {}, got %v", revokedSW)
	}

	// 17. Verify Jane can no longer access the calendar -> forbidden
	resForbiddenAfterRevoke := callJMAP(janeUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId": johnID,
			"ids":       []string{johnCalendarID},
		}, "call-33"},
	})
	if resForbiddenAfterRevoke[0].Name != "error" || resForbiddenAfterRevoke[0].Args["type"] != "forbidden" {
		t.Errorf("Expected 'forbidden' error after revoke, got %v", resForbiddenAfterRevoke[0])
	}

	// 18. Verify Jane received a ShareNotification with revoked rights
	resJaneChanges3 := callJMAP(janeUser, []any{
		[]any{"ShareNotification/changes", map[string]any{
			"accountId":  janeID,
			"sinceState": janeShareChangeID,
		}, "call-34"},
	})
	createdNotifs3 := resJaneChanges3[0].Args["created"].([]any)
	if len(createdNotifs3) != 1 {
		t.Fatalf("Expected 1 created notification, got %v", createdNotifs3)
	}
	shareID3 := createdNotifs3[0].(string)

	resJaneGetNotif3 := callJMAP(janeUser, []any{
		[]any{"ShareNotification/get", map[string]any{
			"accountId": janeID,
			"properties": []string{
				"id", "changedBy", "objectType", "objectAccountId", "objectId", "oldRights", "newRights", "name",
			},
			"ids": []string{shareID3},
		}, "call-35"},
	})
	notif3 := resJaneGetNotif3[0].Args["list"].([]any)[0].(map[string]any)
	oldR3 := notif3["oldRights"].(map[string]any)
	if val, _ := oldR3["mayReadItems"].(bool); !val {
		t.Errorf("Expected oldRights.mayReadItems == true")
	}
	if val, _ := oldR3["mayWriteAll"].(bool); !val {
		t.Errorf("Expected oldRights.mayWriteAll == true")
	}
	if val, _ := oldR3["mayDelete"].(bool); !val {
		t.Errorf("Expected oldRights.mayDelete == true")
	}
	newR3 := notif3["newRights"].(map[string]any)
	for _, prop := range []string{"mayReadItems", "mayWriteAll", "mayDelete", "mayShare", "mayWriteOwn", "mayReadFreeBusy", "mayUpdatePrivate", "mayRSVP"} {
		if val, _ := newR3[prop].(bool); val {
			t.Errorf("Expected newRights.%s == false, got true", prop)
		}
	}

	// 19. Grant Jane delete access once again
	resGrantDelete := callJMAP(johnUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": johnID,
			"update": map[string]any{
				johnCalendarID: map[string]any{
					fmt.Sprintf("shareWith/%s/mayReadItems", janeID): true,
					fmt.Sprintf("shareWith/%s/mayDelete", janeID):   true,
				},
			},
		}, "call-36"},
	})
	updDel, _ := resGrantDelete[0].Args["updated"].(map[string]any)
	if _, ok := updDel[johnCalendarID]; !ok {
		t.Fatalf("Expected calendar updated: %v", resGrantDelete[0].Args)
	}

	// 20. Verify Jane can delete the calendar with onDestroyRemoveEvents
	resJaneDestroyCal := callJMAP(janeUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId":             johnID,
			"destroy":               []string{johnCalendarID},
			"onDestroyRemoveEvents": true,
		}, "call-37"},
	})
	destroyedByJane := resJaneDestroyCal[0].Args["destroyed"].([]any)
	if len(destroyedByJane) != 1 || destroyedByJane[0] != johnCalendarID {
		t.Errorf("Expected calendar destroyed by Jane: %v", resJaneDestroyCal[0].Args)
	}

	// 21. Destroy all remaining calendars
	_ = callJMAP(johnUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId":             johnID,
			"destroy":               []string{johnCalendarID},
			"onDestroyRemoveEvents": true,
		}, "clean-john"},
	})
	_ = callJMAP(janeUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId":             janeID,
			"destroy":               []string{janeCalendarID},
			"onDestroyRemoveEvents": true,
		}, "clean-jane"},
	})
}
