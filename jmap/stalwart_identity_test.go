package jmap_test

import (
	"context"
	"net/http/httptest"
	"reflect"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/managesieve"
	"imap-jmap/jmap/nextcloud"
	"imap-jmap/jmap/spectest"
)

// TestStalwart_ParticipantIdentity translates Stalwart's calendar identity integration
// test suite (tests/src/jmap/calendar/identity.rs) to Go.
func TestStalwart_ParticipantIdentity(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "3.1", spectest.MUST,
		"A ParticipantIdentity represents an identity for sending/receiving calendar scheduling messages.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "3.2", spectest.MUST,
		"ParticipantIdentity/get returns requested properties for identities.")
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "3.3", spectest.MUST,
		"ParticipantIdentity/set creates, updates, and destroys participant identities with onSuccessSetIsDefault.")

	user := "jdoe@example.com"
	gwBackend, _ := imapsmtp.NewEmbeddedBackend(user)
	_, cal, contacts, fb, principals, cleanupNC := nextcloud.NewEmbeddedBackend(user)
	defer cleanupNC()
	_, sieve, cleanupSieve := managesieve.NewEmbeddedBackend(user)
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

	accountID := jmap.AccountIDForSubject(user)
	ctx := jmap.ContextWithAccountID(context.Background(), accountID)

	// Seed initial identities "a" and "b" matching Stalwart's test setup
	cal.SetAllowedAddresses(user, []string{"jdoe@example.com", "john.doe@example.com"})
	_, err := cal.CreateParticipantIdentity(ctx, &jmap.ParticipantIdentity{
		ID:              "a",
		Name:            "John Doe",
		CalendarAddress: "mailto:jdoe@example.com",
		IsDefault:       true,
	})
	if err != nil {
		t.Fatalf("failed to seed identity a: %v", err)
	}
	_, err = cal.CreateParticipantIdentity(ctx, &jmap.ParticipantIdentity{
		ID:              "b",
		Name:            "John Doe",
		CalendarAddress: "mailto:john.doe@example.com",
		IsDefault:       false,
	})
	if err != nil {
		t.Fatalf("failed to seed identity b: %v", err)
	}

	// 1. Obtain all identities
	getResp1 := postJMAPAs(t, ts.URL, user, using, []any{
		[]any{"ParticipantIdentity/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "name", "calendarAddress", "isDefault"},
		}, "g1"},
	})
	list1, ok := getResp1.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list1) != 2 {
		t.Fatalf("expected 2 identities initially, got %+v", getResp1.MethodResponses[0].Args)
	}
	expected1 := []map[string]any{
		{
			"id":              "a",
			"name":            "John Doe",
			"calendarAddress": "mailto:jdoe@example.com",
			"isDefault":       true,
		},
		{
			"id":              "b",
			"name":            "John Doe",
			"calendarAddress": "mailto:john.doe@example.com",
			"isDefault":       false,
		},
	}
	for i, exp := range expected1 {
		act := list1[i].(map[string]any)
		for k, v := range exp {
			if act[k] != v {
				t.Fatalf("item %d property %s: expected %v, got %v", i, k, v, act[k])
			}
		}
	}

	// 2. Destroy identity b
	destroyResp := postJMAPAs(t, ts.URL, user, using, []any{
		[]any{"ParticipantIdentity/set", map[string]any{
			"accountId": "primary",
			"destroy":   []string{"b"},
		}, "d1"},
	})
	destroyed, ok := destroyResp.MethodResponses[0].Args["destroyed"].([]any)
	if !ok || len(destroyed) != 1 || destroyed[0] != "b" {
		t.Fatalf("expected destroyed ['b'], got %+v", destroyResp.MethodResponses[0].Args)
	}

	getResp2 := postJMAPAs(t, ts.URL, user, using, []any{
		[]any{"ParticipantIdentity/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "name", "calendarAddress", "isDefault"},
		}, "g2"},
	})
	list2, ok := getResp2.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list2) != 1 {
		t.Fatalf("expected 1 identity after destroy, got %+v", getResp2.MethodResponses[0].Args)
	}
	expected2 := map[string]any{
		"id":              "a",
		"name":            "John Doe",
		"calendarAddress": "mailto:jdoe@example.com",
		"isDefault":       true,
	}
	act2 := list2[0].(map[string]any)
	for k, v := range expected2 {
		if act2[k] != v {
			t.Fatalf("after destroy property %s: expected %v, got %v", k, v, act2[k])
		}
	}

	// 3. Creating a new identity with an unauthorized calendar address should fail
	badCreateResp := postJMAPAs(t, ts.URL, user, using, []any{
		[]any{"ParticipantIdentity/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"i0": map[string]any{
					"name":            "Work",
					"calendarAddress": "mailto:work@example.com",
				},
				"i1": map[string]any{
					"name":            "Work",
					"calendarAddress": "work@example.com",
				},
			},
			"onSuccessSetIsDefault": "#i0",
		}, "c_bad"},
	})
	notCreated, ok := badCreateResp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %+v", badCreateResp.MethodResponses[0].Args)
	}
	for _, cid := range []string{"i0", "i1"} {
		errObj, ok := notCreated[cid].(map[string]any)
		if !ok {
			t.Fatalf("expected %s in notCreated, got %+v", cid, notCreated)
		}
		if desc, _ := errObj["description"].(string); desc != "Calendar address not configured for this account." {
			t.Fatalf("expected description 'Calendar address not configured for this account.', got %v", desc)
		}
	}

	// 4. Create a new identity and set it as default
	goodCreateResp := postJMAPAs(t, ts.URL, user, using, []any{
		[]any{"ParticipantIdentity/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"i0": map[string]any{
					"name":            "Johnny B Goode",
					"calendarAddress": "mailto:john.doe@example.com",
				},
			},
			"onSuccessSetIsDefault": "#i0",
		}, "c_good"},
	})
	created, ok := goodCreateResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["i0"] == nil {
		t.Fatalf("expected i0 created, got %+v", goodCreateResp.MethodResponses[0].Args)
	}
	newID := created["i0"].(map[string]any)["id"].(string)
	if newID != "b" {
		t.Fatalf("expected sequential id 'b', got %s", newID)
	}

	getResp3 := postJMAPAs(t, ts.URL, user, using, []any{
		[]any{"ParticipantIdentity/get", map[string]any{
			"accountId":  "primary",
			"properties": []string{"id", "name", "calendarAddress", "isDefault"},
		}, "g3"},
	})
	list3, ok := getResp3.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list3) != 2 {
		t.Fatalf("expected 2 identities after create, got %+v", getResp3.MethodResponses[0].Args)
	}
	expected3 := []map[string]any{
		{
			"id":              "a",
			"name":            "John Doe",
			"calendarAddress": "mailto:jdoe@example.com",
			"isDefault":       false,
		},
		{
			"id":              "b",
			"name":            "Johnny B Goode",
			"calendarAddress": "mailto:john.doe@example.com",
			"isDefault":       true,
		},
	}
	for i, exp := range expected3 {
		act := list3[i].(map[string]any)
		for k, v := range exp {
			if !reflect.DeepEqual(act[k], v) {
				t.Fatalf("final item %d property %s: expected %v, got %v", i, k, v, act[k])
			}
		}
	}
}
