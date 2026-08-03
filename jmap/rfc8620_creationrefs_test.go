package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// postRaw posts an arbitrary JSON request body and decodes the response.
func postRaw(t *testing.T, url string, payload map[string]any) jmap.Response {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := http.Post(url+"/jmap", "application/json", bytes.NewReader(body))
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

// TestRFC8620_CreationRefsMailboxParentChild verifies Mailbox/set resolves in-call
// "#creationId" references per RFC 8620 Section 5.3, including a forward reference (the
// child listed before its parent), and that update keys and destroy ids referring to
// creations in the same call resolve to the server-assigned ids.
func TestRFC8620_CreationRefsMailboxParentChild(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	post := func(calls []any) jmap.Response {
		return postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, calls)
	}

	r1 := post([]any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				// Forward reference: the child is listed before its parent.
				"child":  map[string]any{"name": "Child", "parentId": "#parent"},
				"parent": map[string]any{"name": "Parent"},
			},
			"update": map[string]any{
				"#child": map[string]any{"name": "Child Renamed"},
			},
			"destroy": []any{"#child"},
		}, "c1"},
	})
	args := r1.MethodResponses[0].Args
	created, _ := args["created"].(map[string]any)
	childObj, childOK := created["child"].(map[string]any)
	parentObj, parentOK := created["parent"].(map[string]any)
	if !childOK || !parentOK {
		t.Fatalf("both parent and child MUST be created despite the forward reference, got %v", created)
	}
	childID, _ := childObj["id"].(string)
	parentID, _ := parentObj["id"].(string)
	if childID == "" || parentID == "" {
		t.Fatalf("created mailboxes have no ids: %v", created)
	}
	// The created child object reports the resolved real parent id.
	if childObj["parentId"] != parentID {
		t.Errorf("expected created child parentId %q (resolved), got %v", parentID, childObj["parentId"])
	}
	notCreated, _ := args["notCreated"].(map[string]any)
	if len(notCreated) != 0 {
		t.Errorf("no notCreated expected, got %v", notCreated)
	}

	// The update key "#child" must resolve to the real id in the response.
	updated, _ := args["updated"].(map[string]any)
	if _, ok := updated[childID]; !ok {
		t.Errorf("expected updated keyed by real child id %q, got %v", childID, updated)
	}

	// Destroy via a creation reference resolves to the real id.
	destroyed, _ := args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != childID {
		t.Errorf("expected destroyed [%q], got %v", childID, destroyed)
	}

	// The parent survived; the child is gone.
	r2 := post([]any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary", "ids": []any{parentID, childID}}, "c2"},
	})
	list, _ := r2.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != parentID {
		t.Errorf("expected only the parent mailbox %q to remain, got %v", parentID, list)
	}
}

// TestRFC8620_CreationRefsCycleAndMissing verifies Mailbox/set rejects cyclic and dangling
// "#creationId" references via notCreated (RFC 8620 Section 5.3), while unrelated creates
// in the same batch still succeed.
func TestRFC8620_CreationRefsCycleAndMissing(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	post := func(calls []any) jmap.Response {
		return postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, calls)
	}

	r := post([]any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"a":     map[string]any{"name": "A", "parentId": "#b"},
				"b":     map[string]any{"name": "B", "parentId": "#a"},
				"ghost": map[string]any{"name": "Ghost", "parentId": "#never-created"},
				"ok":    map[string]any{"name": "Fine"},
			},
		}, "c1"},
	})
	args := r.MethodResponses[0].Args
	created, _ := args["created"].(map[string]any)
	if created["ok"] == nil {
		t.Errorf("independent create must succeed, got %v", created)
	}
	notCreated, _ := args["notCreated"].(map[string]any)
	for _, key := range []string{"a", "b", "ghost"} {
		errObj, ok := notCreated[key].(map[string]any)
		if !ok {
			t.Errorf("expected %q in notCreated for unresolvable reference, got %v", key, notCreated)
			continue
		}
		if errObj["type"] != "invalidProperties" {
			t.Errorf("expected notCreated[%q].type=invalidProperties, got %v", key, errObj["type"])
		}
	}
	if len(notCreated) != 3 {
		t.Errorf("expected exactly 3 notCreated entries, got %v", notCreated)
	}
}

// TestRFC8620_CreationRefsCrossCall verifies the request-scoped createdIds map (RFC 8620
// Sections 3.3 & 5.3): a "#creationId" in a later method call resolves to the id assigned
// by an earlier /set in the same request, across different data types (Mailbox/set then
// Email/set), and the response echoes the createdIds map.
func TestRFC8620_CreationRefsCrossCall(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	post := func(calls []any) jmap.Response {
		return postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, calls)
	}
	// Explicit empty createdIds in the request: RFC 8620 Section 3.4 only echoes
	// createdIds when it was given in the request.
	postRawReq := func(calls []any) jmap.Response {
		return postRaw(t, ts.URL, map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
			"createdIds":  map[string]string{},
			"methodCalls": calls,
		})
	}

	r := postRawReq([]any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"m1": map[string]any{"name": "Labels"}},
		}, "c1"},
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e1": map[string]any{"subject": "Ref Email", "mailboxIds": map[string]any{"#m1": true}},
			},
		}, "c2"},
	})
	createdMB, _ := r.MethodResponses[0].Args["created"].(map[string]any)
	mbObj, _ := createdMB["m1"].(map[string]any)
	mbID, _ := mbObj["id"].(string)
	if mbID == "" {
		t.Fatalf("mailbox creation failed: %v", r.MethodResponses[0].Args)
	}
	createdEM, _ := r.MethodResponses[1].Args["created"].(map[string]any)
	emObj, ok := createdEM["e1"].(map[string]any)
	if !ok {
		t.Fatalf("email referencing #m1 must be created, got %v", r.MethodResponses[1].Args)
	}
	emID, _ := emObj["id"].(string)

	// The stored email must reference the real mailbox id, not the placeholder.
	r2 := post([]any{
		[]any{"Email/get", map[string]any{"accountId": "primary", "ids": []any{emID}, "properties": []any{"mailboxIds"}}, "c3"},
	})
	list, _ := r2.MethodResponses[0].Args["list"].([]any)
	mailboxIds, _ := list[0].(map[string]any)["mailboxIds"].(map[string]any)
	if len(mailboxIds) != 1 || mailboxIds[mbID] != true {
		t.Errorf("expected email in real mailbox %q, got %v", mbID, mailboxIds)
	}

	// Response echoes createdIds (request had none, so only new entries appear).
	if r.CreatedIds["m1"] != mbID {
		t.Errorf("expected response createdIds[m1]=%q, got %v", mbID, r.CreatedIds)
	}
	if r.CreatedIds["e1"] != emID {
		t.Errorf("expected response createdIds[e1]=%q, got %v", emID, r.CreatedIds)
	}
}

// TestRFC8620_CreationRefsUpdatePatch verifies "#creationId" references in an update patch
// value (Mailbox parentId) resolve to the real id.
func TestRFC8620_CreationRefsUpdatePatch(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	post := func(calls []any) jmap.Response {
		return postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, calls)
	}

	// Create and update in the SAME request: #p1 must resolve within the call (RFC 8620
	// Section 5.3).
	r := post([]any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"p1": map[string]any{"name": "New Parent"}},
			"update":    map[string]any{"mb-inbox": map[string]any{"parentId": "#p1"}},
		}, "c1"},
	})
	parentID, _ := r.MethodResponses[0].Args["created"].(map[string]any)["p1"].(map[string]any)["id"].(string)
	if _, ok := r.MethodResponses[0].Args["updated"].(map[string]any)["mb-inbox"]; !ok {
		t.Fatalf("expected mb-inbox updated, got %v", r.MethodResponses[0].Args)
	}

	r2 := post([]any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary", "ids": []any{"mb-inbox"}}, "c2"},
	})
	list, _ := r2.MethodResponses[0].Args["list"].([]any)
	inbox := list[0].(map[string]any)
	if inbox["parentId"] != parentID {
		t.Errorf("expected mb-inbox.parentId %q (resolved), got %v", parentID, inbox["parentId"])
	}
}

// TestRFC8620_CreationRefsCardAndEvent verifies cross-call references for Card
// addressBookIds and CalendarEvent calendarIds (RFC 9610 / RFC 8984 over RFC 8620
// Section 5.3).
func TestRFC8620_CreationRefsCardAndEvent(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Contacts: AddressBook/set then Card/set with addressBookIds "#ab1".
	rc := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}, []any{
		[]any{"AddressBook/set", map[string]any{"accountId": "primary", "create": map[string]any{"ab1": map[string]any{"name": "Work Contacts"}}}, "c1"},
		[]any{"Card/set", map[string]any{"accountId": "primary", "create": map[string]any{"card1": map[string]any{"name": map[string]any{"full": "Alice"}, "addressBookIds": map[string]any{"#ab1": true}}}}, "c2"},
	})
	abID, _ := rc.MethodResponses[0].Args["created"].(map[string]any)["ab1"].(map[string]any)["id"].(string)
	cardID, _ := rc.MethodResponses[1].Args["created"].(map[string]any)["card1"].(map[string]any)["id"].(string)
	if abID == "" || cardID == "" {
		t.Fatalf("addressbook/card creation failed: %v", rc.MethodResponses)
	}
	rc2 := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}, []any{
		[]any{"Card/get", map[string]any{"accountId": "primary", "ids": []any{cardID}, "properties": []any{"addressBookIds"}}, "c3"},
	})
	cardList, _ := rc2.MethodResponses[0].Args["list"].([]any)
	abIDs, _ := cardList[0].(map[string]any)["addressBookIds"].(map[string]any)
	if len(abIDs) != 1 || abIDs[abID] != true {
		t.Errorf("expected card in real addressbook %q, got %v", abID, abIDs)
	}

	// Calendars: Calendar/set then CalendarEvent/set with calendarIds "#cal1".
	rcal := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}, []any{
		[]any{"Calendar/set", map[string]any{"accountId": "primary", "create": map[string]any{"cal1": map[string]any{"name": "Team Cal"}}}, "c4"},
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"ev1": map[string]any{
					"title":       "Ref Meeting",
					"start":       "2026-08-05T10:00:00Z",
					"duration":    "PT1H",
					"calendarIds": map[string]any{"#cal1": true},
				},
			},
		}, "c5"},
	})
	calID, _ := rcal.MethodResponses[0].Args["created"].(map[string]any)["cal1"].(map[string]any)["id"].(string)
	evID, _ := rcal.MethodResponses[1].Args["created"].(map[string]any)["ev1"].(map[string]any)["id"].(string)
	if calID == "" || evID == "" {
		t.Fatalf("calendar/event creation failed: %v", rcal.MethodResponses)
	}
	rcal2 := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}, []any{
		[]any{"CalendarEvent/get", map[string]any{"accountId": "primary", "ids": []any{evID}, "properties": []any{"calendarIds"}}, "c6"},
	})
	evList, _ := rcal2.MethodResponses[0].Args["list"].([]any)
	calIDs, _ := evList[0].(map[string]any)["calendarIds"].(map[string]any)
	if len(calIDs) != 1 || calIDs[calID] != true {
		t.Errorf("expected event in real calendar %q, got %v", calID, calIDs)
	}
}

// TestRFC8620_CreationRefsSubmission verifies EmailSubmission/set resolves "#creationId"
// references for emailId and identityId (RFC 8621 Section 7 over RFC 8620 Section 5.3).
func TestRFC8620_CreationRefsSubmission(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Identity/set", map[string]any{"accountId": "primary", "create": map[string]any{"i1": map[string]any{"name": "Work", "email": "work@example.com"}}}, "c1"},
		[]any{"Email/set", map[string]any{"accountId": "primary", "create": map[string]any{"e1": map[string]any{"subject": "Send Me", "mailboxIds": map[string]any{"mb-drafts": true}}}}, "c2"},
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"s1": map[string]any{"emailId": "#e1", "identityId": "#i1"}},
		}, "c3"},
	})
	idnID, _ := r.MethodResponses[0].Args["created"].(map[string]any)["i1"].(map[string]any)["id"].(string)
	emID, _ := r.MethodResponses[1].Args["created"].(map[string]any)["e1"].(map[string]any)["id"].(string)
	subID, _ := r.MethodResponses[2].Args["created"].(map[string]any)["s1"].(map[string]any)["id"].(string)
	if idnID == "" || emID == "" || subID == "" {
		t.Fatalf("identity/email/submission creation failed: %v", r.MethodResponses)
	}

	r2 := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"EmailSubmission/get", map[string]any{"accountId": "primary", "ids": []any{subID}, "properties": []any{"emailId", "identityId"}}, "c4"},
	})
	list, _ := r2.MethodResponses[0].Args["list"].([]any)
	sub := list[0].(map[string]any)
	if sub["emailId"] != emID {
		t.Errorf("expected submission.emailId %q (resolved), got %v", emID, sub["emailId"])
	}
	if sub["identityId"] != idnID {
		t.Errorf("expected submission.identityId %q (resolved), got %v", idnID, sub["identityId"])
	}
}

// TestRFC8620_CreationRefsRequestSeed verifies a request-supplied createdIds map is honored
// (RFC 8620 Section 3.3): seeded entries resolve like in-call creations, and the response
// echoes the original entries along with any new ones.
func TestRFC8620_CreationRefsRequestSeed(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	payload := map[string]any{
		"using":      []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"createdIds": map[string]string{"seed-mb": "mb-inbox"},
		"methodCalls": []any{
			[]any{"Email/set", map[string]any{
				"accountId": "primary",
				"create":    map[string]any{"e1": map[string]any{"subject": "Seeded", "mailboxIds": map[string]any{"#seed-mb": true}}},
			}, "c1"},
		},
	}
	r := postRaw(t, ts.URL, payload)
	emObj, ok := r.MethodResponses[0].Args["created"].(map[string]any)["e1"].(map[string]any)
	if !ok {
		t.Fatalf("email with seeded creation reference must be created, got %v", r.MethodResponses[0].Args)
	}
	emID, _ := emObj["id"].(string)

	r2 := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/get", map[string]any{"accountId": "primary", "ids": []any{emID}, "properties": []any{"mailboxIds"}}, "c2"},
	})
	list, _ := r2.MethodResponses[0].Args["list"].([]any)
	mailboxIds, _ := list[0].(map[string]any)["mailboxIds"].(map[string]any)
	if len(mailboxIds) != 1 || mailboxIds["mb-inbox"] != true {
		t.Errorf("expected email in seeded mailbox mb-inbox, got %v", mailboxIds)
	}

	// The response must echo the original seeded entry.
	if r.CreatedIds["seed-mb"] != "mb-inbox" {
		t.Errorf("expected response createdIds to echo seed-mb->mb-inbox, got %v", r.CreatedIds)
	}
	if r.CreatedIds["e1"] != emID {
		t.Errorf("expected response createdIds to include e1->%q, got %v", emID, r.CreatedIds)
	}
}
