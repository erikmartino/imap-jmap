package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/nextcloud"
	"imap-jmap/jmap/spectest"
)

func setupSharingTestServer(t *testing.T, users ...string) (*httptest.Server, func()) {
	t.Helper()
	if len(users) == 0 {
		users = []string{"owner@example.com", "sharee@example.com"}
	}
	_, calBackend, contactsBackend, fileNodeBackend, principalsBackend, cleanup := nextcloud.NewEmbeddedBackend(users...)

	srv := jmap.NewServer(nil,
		jmap.WithCalendarsBackend(calBackend),
		jmap.WithPrincipalsBackend(principalsBackend),
		jmap.WithContactsBackend(contactsBackend),
		jmap.WithFileNodeBackend(fileNodeBackend),
	)
	ts := httptest.NewServer(srv.Handler())
	return ts, func() {
		ts.Close()
		cleanup()
	}
}

func callJMAPSharing(t *testing.T, tsURL, user string, calls []any) []jmap.Invocation {
	t.Helper()
	reqBody := map[string]any{
		"using": []string{
			jmap.CoreCapabilityURI,
			jmap.CalendarsCapabilityURI,
			jmap.SharingCapabilityURI,
			jmap.PrincipalsCapabilityURI,
		},
		"methodCalls": calls,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Marshal request: %v", err)
	}
	httpReq, err := http.NewRequest("POST", tsURL+"/jmap", bytes.NewReader(raw))
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

// TestRFC9670_CapabilityAdvertisement verifies that urn:ietf:params:jmap:sharing is advertised.
func TestRFC9670_CapabilityAdvertisement(t *testing.T) {
	spectest.Require(t, "RFC9670", "1.4.1", spectest.MUST,
		"The urn:ietf:params:jmap:sharing capability URI MUST be advertised in the accountCapabilities for accounts that support sharing.")

	ts, cleanup := setupSharingTestServer(t, "owner@example.com")
	defer cleanup()

	req, err := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer owner@example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /.well-known/jmap: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Decode session: %v", err)
	}

	if _, ok := session.Capabilities[jmap.SharingCapabilityURI]; !ok {
		t.Errorf("Expected %q in session.Capabilities", jmap.SharingCapabilityURI)
	}

	ownerAccID := jmap.AccountIDForSubject("owner@example.com")
	acc, ok := session.Accounts[ownerAccID]
	if !ok {
		t.Fatalf("Account %q not found in session", ownerAccID)
	}
	if _, ok := acc.AccountCapabilities[jmap.SharingCapabilityURI]; !ok {
		t.Errorf("Expected %q in accountCapabilities for %q", jmap.SharingCapabilityURI, ownerAccID)
	}
}

// TestRFC9670_ShareNotificationGet verifies the ShareNotification data model and ShareNotification/get.
func TestRFC9670_ShareNotificationGet(t *testing.T) {
	spectest.Require(t, "RFC9670", "2", spectest.MUST,
		"A ShareNotification object represents a change to the sharing status of an object.")
	spectest.Require(t, "RFC9670", "3.1", spectest.MUST,
		"ShareNotification/get returns requested properties for share notifications.")

	ownerUser := "owner@example.com"
	shareeUser := "sharee@example.com"
	ownerID := jmap.AccountIDForSubject(ownerUser)
	shareeID := jmap.AccountIDForSubject(shareeUser)

	ts, cleanup := setupSharingTestServer(t, ownerUser, shareeUser)
	defer cleanup()

	// 1. Owner creates a calendar
	resCreate := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"create": map[string]any{
				"cal1": map[string]any{
					"name": "Project Roadmap",
				},
			},
		}, "c1"},
	})
	createdCals, ok := resCreate[0].Args["created"].(map[string]any)
	if !ok || createdCals["cal1"] == nil {
		t.Fatalf("Failed to create calendar: %v", resCreate[0].Args)
	}
	calID := createdCals["cal1"].(map[string]any)["id"].(string)

	// 2. Owner shares the calendar with sharee
	resShare := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				calID: map[string]any{
					"shareWith": map[string]any{
						shareeID: map[string]any{
							"mayReadItems": true,
							"mayWriteAll":  true,
						},
					},
				},
			},
		}, "c2"},
	})
	updatedCals, _ := resShare[0].Args["updated"].(map[string]any)
	if _, ok := updatedCals[calID]; !ok {
		t.Fatalf("Expected calendar updated with shareWith: %v", resShare[0].Args)
	}

	// 3. Sharee queries share notifications via ShareNotification/get
	resGet := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/get", map[string]any{
			"accountId": shareeID,
		}, "c3"},
	})
	if resGet[0].Name != "ShareNotification/get" {
		t.Fatalf("Expected ShareNotification/get, got %s", resGet[0].Name)
	}
	list, ok := resGet[0].Args["list"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("Expected 1 notification in list, got %v", resGet[0].Args)
	}

	notif := list[0].(map[string]any)
	if notif["objectType"] != "Calendar" {
		t.Errorf("Expected objectType 'Calendar', got %v", notif["objectType"])
	}
	if notif["objectAccountId"] != ownerID {
		t.Errorf("Expected objectAccountId %q, got %v", ownerID, notif["objectAccountId"])
	}
	if notif["objectId"] != calID {
		t.Errorf("Expected objectId %q, got %v", calID, notif["objectId"])
	}

	changedBy, _ := notif["changedBy"].(map[string]any)
	if changedBy["email"] != ownerUser {
		t.Errorf("Expected changedBy.email %q, got %v", ownerUser, changedBy["email"])
	}

	newRights, _ := notif["newRights"].(map[string]any)
	if newRights["mayReadItems"] != true || newRights["mayWriteAll"] != true {
		t.Errorf("Expected mayReadItems and mayWriteAll to be true, got %v", newRights)
	}
}

// TestRFC9670_ShareNotificationChanges verifies ShareNotification/changes tracking.
func TestRFC9670_ShareNotificationChanges(t *testing.T) {
	spectest.Require(t, "RFC9670", "3.2", spectest.MUST,
		"ShareNotification/changes returns changes to share notifications since a specified state.")

	ownerUser := "owner@example.com"
	shareeUser := "sharee@example.com"
	ownerID := jmap.AccountIDForSubject(ownerUser)
	shareeID := jmap.AccountIDForSubject(shareeUser)

	ts, cleanup := setupSharingTestServer(t, ownerUser, shareeUser)
	defer cleanup()

	// 1. Initial changes query for sharee
	resInit := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/changes", map[string]any{
			"accountId":  shareeID,
			"sinceState": "",
		}, "c1"},
	})
	initState, _ := resInit[0].Args["newState"].(string)

	// 2. Owner creates and shares a calendar
	resCreate := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"create": map[string]any{
				"cal": map[string]any{"name": "Team Calendar"},
			},
		}, "c2"},
	})
	calID := resCreate[0].Args["created"].(map[string]any)["cal"].(map[string]any)["id"].(string)

	callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				calID: map[string]any{
					"shareWith": map[string]any{
						shareeID: map[string]any{"mayReadItems": true},
					},
				},
			},
		}, "c3"},
	})

	// 3. Sharee requests changes since initState
	resChanges := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/changes", map[string]any{
			"accountId":  shareeID,
			"sinceState": initState,
		}, "c4"},
	})
	if resChanges[0].Name != "ShareNotification/changes" {
		t.Fatalf("Expected ShareNotification/changes, got %s", resChanges[0].Name)
	}

	createdList, _ := resChanges[0].Args["created"].([]any)
	if len(createdList) != 1 {
		t.Fatalf("Expected 1 created share notification, got %v", resChanges[0].Args)
	}

	newState, _ := resChanges[0].Args["newState"].(string)
	if newState == initState {
		t.Errorf("Expected newState to differ from initState")
	}
}

// TestRFC9670_ShareNotificationSetDestroyOnly verifies that ShareNotification/set is destroy-only.
func TestRFC9670_ShareNotificationSetDestroyOnly(t *testing.T) {
	spectest.Require(t, "RFC9670", "3.3", spectest.MUST,
		"ShareNotification/set only supports destroying share notifications; creating or updating is rejected.")

	ownerUser := "owner@example.com"
	shareeUser := "sharee@example.com"
	ownerID := jmap.AccountIDForSubject(ownerUser)
	shareeID := jmap.AccountIDForSubject(shareeUser)

	ts, cleanup := setupSharingTestServer(t, ownerUser, shareeUser)
	defer cleanup()

	// 1. Owner shares calendar with sharee to produce a ShareNotification
	resCreate := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"create": map[string]any{
				"cal": map[string]any{"name": "Shared Docs"},
			},
		}, "c1"},
	})
	calID := resCreate[0].Args["created"].(map[string]any)["cal"].(map[string]any)["id"].(string)

	callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				calID: map[string]any{
					"shareWith": map[string]any{
						shareeID: map[string]any{"mayReadItems": true},
					},
				},
			},
		}, "c2"},
	})

	// Fetch notification ID
	resGet := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/get", map[string]any{
			"accountId": shareeID,
		}, "c3"},
	})
	notifs := resGet[0].Args["list"].([]any)
	if len(notifs) == 0 {
		t.Fatalf("Expected at least 1 notification, got %v", resGet[0].Args)
	}
	notifID := notifs[0].(map[string]any)["id"].(string)

	// 2. Reject create via ShareNotification/set
	resBadCreate := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/set", map[string]any{
			"accountId": shareeID,
			"create": map[string]any{
				"sn1": map[string]any{"objectType": "Calendar"},
			},
		}, "c4"},
	})
	notCreated, _ := resBadCreate[0].Args["notCreated"].(map[string]any)
	if notCreated["sn1"] == nil {
		t.Errorf("Expected create to be rejected in notCreated: %v", resBadCreate[0].Args)
	}

	// 3. Reject update via ShareNotification/set
	resBadUpdate := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/set", map[string]any{
			"accountId": shareeID,
			"update": map[string]any{
				notifID: map[string]any{"objectType": "Calendar"},
			},
		}, "c5"},
	})
	notUpdated, _ := resBadUpdate[0].Args["notUpdated"].(map[string]any)
	if notUpdated[notifID] == nil {
		t.Errorf("Expected update to be rejected in notUpdated: %v", resBadUpdate[0].Args)
	}

	// 4. Successfully destroy notification via ShareNotification/set
	resDestroy := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/set", map[string]any{
			"accountId": shareeID,
			"destroy":   []string{notifID},
		}, "c6"},
	})
	destroyed, _ := resDestroy[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != notifID {
		t.Fatalf("Expected notification %s to be destroyed: %v", notifID, resDestroy[0].Args)
	}

	// 5. Verify it is now in notFound
	resVerify := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/get", map[string]any{
			"accountId": shareeID,
			"ids":       []string{notifID},
		}, "c7"},
	})
	notFoundList, _ := resVerify[0].Args["notFound"].([]any)
	if len(notFoundList) != 1 || notFoundList[0] != notifID {
		t.Errorf("Expected notification %s in notFound, got: %v", notifID, resVerify[0].Args)
	}
}

// TestRFC9670_CrossAccountForbidden verifies cross-account access control and permission enforcement.
func TestRFC9670_CrossAccountForbidden(t *testing.T) {
	spectest.Require(t, "RFC9670", "5", spectest.MUST,
		"Cross-account access without appropriate sharing rights MUST be rejected with a forbidden error.")

	ownerUser := "owner@example.com"
	shareeUser := "sharee@example.com"
	ownerID := jmap.AccountIDForSubject(ownerUser)
	shareeID := jmap.AccountIDForSubject(shareeUser)

	ts, cleanup := setupSharingTestServer(t, ownerUser, shareeUser)
	defer cleanup()

	// 1. Owner creates calendar without sharing
	resCreate := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"create": map[string]any{
				"cal": map[string]any{"name": "Private Calendar"},
			},
		}, "c1"},
	})
	calID := resCreate[0].Args["created"].(map[string]any)["cal"].(map[string]any)["id"].(string)

	// 2. Sharee attempts to access owner's account directly -> returns forbidden error
	resForbidden := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId": ownerID,
			"ids":       []string{calID},
		}, "c2"},
	})
	if resForbidden[0].Name != "error" {
		t.Fatalf("Expected error method response, got %s", resForbidden[0].Name)
	}
	errType, _ := resForbidden[0].Args["type"].(string)
	if errType != "forbidden" {
		t.Errorf("Expected error type 'forbidden', got %q", errType)
	}

	// 3. Owner shares calendar with read-only rights (mayReadItems: true, mayDelete: false)
	callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				calID: map[string]any{
					"shareWith": map[string]any{
						shareeID: map[string]any{
							"mayReadItems": true,
							"mayDelete":    false,
						},
					},
				},
			},
		}, "c3"},
	})

	// 4. Now sharee can fetch the calendar
	resAllowed := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"Calendar/get", map[string]any{
			"accountId": ownerID,
			"ids":       []string{calID},
		}, "c4"},
	})
	if resAllowed[0].Name != "Calendar/get" {
		t.Fatalf("Expected Calendar/get, got %s", resAllowed[0].Name)
	}
	list := resAllowed[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("Expected 1 calendar in list, got %v", resAllowed[0].Args)
	}

	// 5. Sharee attempts to destroy the calendar -> rejected with forbidden SetError
	resBadDestroy := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"destroy":   []string{calID},
		}, "c5"},
	})
	notDestroyed, _ := resBadDestroy[0].Args["notDestroyed"].(map[string]any)
	if notDestroyed[calID] == nil {
		t.Fatalf("Expected calendar destroy to be rejected in notDestroyed: %v", resBadDestroy[0].Args)
	}
	errItem, _ := notDestroyed[calID].(map[string]any)
	if errItem["type"] != "forbidden" {
		t.Errorf("Expected notDestroyed error type 'forbidden', got %v", errItem["type"])
	}
}

// TestRFC9670_ShareNotificationCreatedAndQuery covers the server-set "created"
// property (RFC 9670 Section 2) and ShareNotification/query (Section 3.4): the
// after/before/objectType/objectAccountId filters, FilterOperator trees, created
// sorting, and rejection of unknown filters/sorts.
func TestRFC9670_ShareNotificationCreatedAndQuery(t *testing.T) {
	spectest.Require(t, "RFC9670", "2", spectest.MUST,
		"A ShareNotification has a server-set created UTCDate property.")
	spectest.Require(t, "RFC9670", "3.4", spectest.MUST,
		"ShareNotification/query is a standard /query supporting after/before/objectType/objectAccountId and created sort.")

	ownerUser := "owner@example.com"
	shareeUser := "sharee@example.com"
	ownerID := jmap.AccountIDForSubject(ownerUser)
	shareeID := jmap.AccountIDForSubject(shareeUser)

	ts, cleanup := setupSharingTestServer(t, ownerUser, shareeUser)
	defer cleanup()

	// Owner creates and shares two calendars with the sharee.
	for _, name := range []string{"Alpha", "Beta"} {
		res := callJMAPSharing(t, ts.URL, ownerUser, []any{
			[]any{"Calendar/set", map[string]any{
				"accountId": ownerID,
				"create":    map[string]any{"c": map[string]any{"name": name}},
			}, "c"},
		})
		created, _ := res[0].Args["created"].(map[string]any)
		calID := created["c"].(map[string]any)["id"].(string)
		callJMAPSharing(t, ts.URL, ownerUser, []any{
			[]any{"Calendar/set", map[string]any{
				"accountId": ownerID,
				"update": map[string]any{
					calID: map[string]any{"shareWith": map[string]any{shareeID: map[string]any{"mayReadItems": true}}},
				},
			}, "s"},
		})
	}

	// ShareNotification/get exposes a server-set RFC3339 created timestamp.
	resGet := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/get", map[string]any{"accountId": shareeID}, "g"},
	})
	list, _ := resGet[0].Args["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 share notifications, got %v", resGet[0].Args)
	}
	for _, item := range list {
		n := item.(map[string]any)
		created, _ := n["created"].(string)
		if created == "" {
			t.Fatalf("expected non-empty created, got %v", n)
		}
		if _, err := time.Parse(time.RFC3339, created); err != nil {
			t.Fatalf("created %q is not RFC3339: %v", created, err)
		}
	}

	query := func(filter map[string]any, extra map[string]any) jmap.Invocation {
		args := map[string]any{"accountId": shareeID}
		if filter != nil {
			args["filter"] = filter
		}
		for k, v := range extra {
			args[k] = v
		}
		res := callJMAPSharing(t, ts.URL, shareeUser, []any{[]any{"ShareNotification/query", args, "q"}})
		return res[0]
	}

	// Positive: objectType matches both.
	if inv := query(map[string]any{"objectType": "Calendar"}, nil); inv.Name != "ShareNotification/query" {
		t.Fatalf("query failed: %+v", inv.Args)
	} else if ids, _ := inv.Args["ids"].([]any); len(ids) != 2 {
		t.Fatalf("expected 2 ids for objectType Calendar, got %v", ids)
	}

	// Negative: a non-matching objectType matches none.
	if ids, _ := query(map[string]any{"objectType": "Mailbox"}, nil).Args["ids"].([]any); len(ids) != 0 {
		t.Fatalf("expected 0 ids for objectType Mailbox, got %v", ids)
	}

	// objectAccountId filter matches the owner's account.
	if ids, _ := query(map[string]any{"objectAccountId": ownerID}, nil).Args["ids"].([]any); len(ids) != 2 {
		t.Fatalf("expected 2 ids for objectAccountId, got %v", ids)
	}
	if ids, _ := query(map[string]any{"objectAccountId": "someone-else"}, nil).Args["ids"].([]any); len(ids) != 0 {
		t.Fatalf("expected 0 ids for non-matching objectAccountId, got %v", ids)
	}

	// after/before bound the creation date.
	if ids, _ := query(map[string]any{"after": "2000-01-01T00:00:00Z"}, nil).Args["ids"].([]any); len(ids) != 2 {
		t.Fatalf("expected 2 ids for after=2000, got %v", ids)
	}
	if ids, _ := query(map[string]any{"before": "2000-01-01T00:00:00Z"}, nil).Args["ids"].([]any); len(ids) != 0 {
		t.Fatalf("expected 0 ids for before=2000, got %v", ids)
	}

	// FilterOperator tree.
	if ids, _ := query(map[string]any{
		"operator":   "AND",
		"conditions": []any{map[string]any{"objectType": "Calendar"}, map[string]any{"objectAccountId": ownerID}},
	}, nil).Args["ids"].([]any); len(ids) != 2 {
		t.Fatalf("expected 2 ids for AND filter, got %v", ids)
	}

	// created sort + limit.
	if ids, _ := query(map[string]any{"objectType": "Calendar"}, map[string]any{
		"sort":  []any{map[string]any{"property": "created", "isAscending": true}},
		"limit": float64(1),
	}).Args["ids"].([]any); len(ids) != 1 {
		t.Fatalf("expected 1 id with limit, got %v", ids)
	}

	// Unknown filter and sort are rejected, never silently permissive.
	if inv := query(map[string]any{"bogus": "x"}, nil); inv.Name != "error" {
		t.Fatalf("expected error for unknown filter, got %+v", inv)
	}
	if inv := query(map[string]any{"objectType": "Calendar"}, map[string]any{
		"sort": []any{map[string]any{"property": "bogus"}},
	}); inv.Name != "error" {
		t.Fatalf("expected error for unknown sort, got %+v", inv)
	}
}

// TestRFC9670_ShareNotificationQueryChanges covers ShareNotification/queryChanges
// (RFC 9670 Section 3.5): destroying a notification is reported as removed.
func TestRFC9670_ShareNotificationQueryChanges(t *testing.T) {
	spectest.Require(t, "RFC9670", "3.5", spectest.MUST,
		"ShareNotification/queryChanges is a standard /queryChanges.")

	ownerUser := "owner@example.com"
	shareeUser := "sharee@example.com"
	ownerID := jmap.AccountIDForSubject(ownerUser)
	shareeID := jmap.AccountIDForSubject(shareeUser)

	ts, cleanup := setupSharingTestServer(t, ownerUser, shareeUser)
	defer cleanup()

	resCreate := callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"create":    map[string]any{"c": map[string]any{"name": "Roadmap"}},
		}, "c"},
	})
	created, _ := resCreate[0].Args["created"].(map[string]any)
	calID := created["c"].(map[string]any)["id"].(string)
	callJMAPSharing(t, ts.URL, ownerUser, []any{
		[]any{"Calendar/set", map[string]any{
			"accountId": ownerID,
			"update": map[string]any{
				calID: map[string]any{"shareWith": map[string]any{shareeID: map[string]any{"mayReadItems": true}}},
			},
		}, "s"},
	})

	resQuery := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/query", map[string]any{"accountId": shareeID, "filter": map[string]any{"objectType": "Calendar"}}, "q"},
	})
	queryState, _ := resQuery[0].Args["queryState"].(string)
	if queryState == "" {
		t.Fatalf("expected queryState, got %v", resQuery[0].Args)
	}
	ids, _ := resQuery[0].Args["ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("expected 1 notification, got %v", ids)
	}
	notifID := ids[0].(string)

	// Sharee destroys the notification.
	resDestroy := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/set", map[string]any{"accountId": shareeID, "destroy": []any{notifID}}, "d"},
	})
	destroyed, _ := resDestroy[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 {
		t.Fatalf("expected notification destroyed, got %v", resDestroy[0].Args)
	}

	resChanges := callJMAPSharing(t, ts.URL, shareeUser, []any{
		[]any{"ShareNotification/queryChanges", map[string]any{
			"accountId":       shareeID,
			"sinceQueryState": queryState,
			"filter":          map[string]any{"objectType": "Calendar"},
		}, "qc"},
	})
	if resChanges[0].Name != "ShareNotification/queryChanges" {
		t.Fatalf("expected queryChanges, got %+v", resChanges[0])
	}
	removed, _ := resChanges[0].Args["removed"].([]any)
	if len(removed) != 1 || removed[0] != notifID {
		t.Fatalf("expected %s removed, got %v", notifID, resChanges[0].Args)
	}
}
