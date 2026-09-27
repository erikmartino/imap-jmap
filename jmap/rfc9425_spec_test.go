package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9425_SpecConventions verifies BCP 14 normative keyword conventions and reference metadata.
func TestRFC9425_SpecConventions(t *testing.T) {
	spectest.Require(t, "RFC9425", "", spectest.MAY, "and how to provide feedback on it may be obtained at")
	spectest.Require(t, "RFC9425", "", spectest.MUST, "Code Components extracted from this document must")
	spectest.Require(t, "RFC9425", "1", spectest.SHOULD, "should be handled by other means")
	spectest.Require(t, "RFC9425", "1.1", spectest.MUST, "The key words \"MUST\", \"MUST NOT\", \"REQUIRED\", \"SHALL\", \"SHALL NOT\",")
	spectest.Require(t, "RFC9425", "1.1", spectest.SHOULD, "\"SHOULD\", \"SHOULD NOT\", \"RECOMMENDED\", \"NOT RECOMMENDED\", \"MAY\", and")
	spectest.Require(t, "RFC9425", "1.1", spectest.MAY, "\"OPTIONAL\" in this document are to be interpreted as described in")
	spectest.Require(t, "RFC9425", "2119", spectest.MAY, "May 2017, <https://www")
}

// TestRFC9425_Quota_TypesAndScope_CapabilitiesFiltering verifies that servers filter out types for unrequested
// capabilities and omit Quota objects when no recognized types remain per RFC 9425 §4.1.
func TestRFC9425_Quota_TypesAndScope_CapabilitiesFiltering(t *testing.T) {
	spectest.Require(t, "RFC9425", "4.1", spectest.MAY, "Objects in scope may not be created or")
	spectest.Require(t, "RFC9425", "4.1", spectest.MUST, "The server MUST filter out any types for which the client did not")
	spectest.Require(t, "RFC9425", "4.1", spectest.MUST, "Further, the server MUST NOT return Quota objects for")
	spectest.Require(t, "RFC9425", "4.1", spectest.MAY, "The Quota object MAY contain the following fields:")
	spectest.Require(t, "RFC9425", "4.1", spectest.SHOULD, "If set, it SHOULD be lower than the \"softLimit\" (if present and")
	spectest.Require(t, "RFC9425", "4.1", spectest.SHOULD, "If set, it SHOULD be higher")
	spectest.Require(t, "RFC9425", "4.1", spectest.MUST, "description MUST be encoded in UTF-8 [RFC3629] as described in")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Client requests Core + Quota + Mail -> returns Quota objects with types: ["Email"]
	usingWithMail := []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI, jmap.MailCapabilityURI}
	rWithMail := postJMAP(t, ts.URL, usingWithMail, []any{
		[]any{"Quota/get", map[string]any{"accountId": "primary"}, "c1"},
	})
	listWithMail, ok := rWithMail.MethodResponses[0].Args["list"].([]any)
	if !ok || len(listWithMail) == 0 {
		t.Fatalf("expected quotas returned when Mail capability is used, got %v", rWithMail.MethodResponses[0].Args)
	}
	q1 := listWithMail[0].(map[string]any)
	typesArr, ok := q1["types"].([]any)
	if !ok || len(typesArr) == 0 || typesArr[0] != "Email" {
		t.Errorf("expected types to contain 'Email', got %v", q1["types"])
	}

	// 2. Client requests Core + Quota WITHOUT Mail capability:
	// Server MUST filter out Email from types, and because no recognized types remain,
	// MUST NOT return Quota objects in list.
	usingWithoutMail := []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI}
	rWithoutMail := postJMAP(t, ts.URL, usingWithoutMail, []any{
		[]any{"Quota/get", map[string]any{"accountId": "primary"}, "c2"},
		[]any{"Quota/get", map[string]any{"accountId": "primary", "ids": []any{"quota-octets"}}, "c3"},
	})
	listWithoutMail, _ := rWithoutMail.MethodResponses[0].Args["list"].([]any)
	if len(listWithoutMail) != 0 {
		t.Errorf("server MUST NOT return Quota objects when types are filtered out, got %v", listWithoutMail)
	}
	// For explicit ids fetch without capability, ID must be returned in notFound
	notFound, _ := rWithoutMail.MethodResponses[1].Args["notFound"].([]any)
	if len(notFound) != 1 || notFound[0] != "quota-octets" {
		t.Errorf("expected quota-octets in notFound when capability not requested, got %v", notFound)
	}
}

// TestRFC9425_QuotaGet_NullIds verifies that ids may be null to fetch all quotas per RFC 9425 §4.2.
func TestRFC9425_QuotaGet_NullIds(t *testing.T) {
	spectest.Require(t, "RFC9425", "4.2", spectest.MAY, "_id_'s argument may be \"null\" to fetch all quotas of the account at")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI, jmap.MailCapabilityURI}
	r := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/get", map[string]any{
			"accountId": "primary",
			"ids":       nil,
		}, "c1"},
	})
	list, ok := r.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list) < 2 {
		t.Fatalf("expected all quotas returned when ids is null, got %v", r.MethodResponses[0].Args)
	}
}

// TestRFC9425_QuotaChanges_UpdatedProperties verifies Quota/changes updatedProperties per RFC 9425 §4.3.
func TestRFC9425_QuotaChanges_UpdatedProperties(t *testing.T) {
	spectest.Require(t, "RFC9425", "4.3", spectest.MAY, "The updatedProperties array may be used")
	spectest.Require(t, "RFC9425", "4.3", spectest.MAY, "Servers MAY decide to add other properties to the list that they")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI, jmap.MailCapabilityURI}
	getRes := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/get", map[string]any{"accountId": "primary"}, "g1"},
	})
	initialState := getRes.MethodResponses[0].Args["state"].(string)

	changesRes := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": initialState,
		}, "ch1"},
	})
	args := changesRes.MethodResponses[0].Args
	if _, ok := args["updatedProperties"]; !ok {
		t.Errorf("expected updatedProperties in Quota/changes response, got %v", args)
	}
}

// TestRFC9425_QuotaQuery_FilterConditions verifies Quota/query filter condition handling per RFC 9425 §4.4.
func TestRFC9425_QuotaQuery_FilterConditions(t *testing.T) {
	spectest.Require(t, "RFC9425", "4.4", spectest.MAY, "may be included or omitted:")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI, jmap.MailCapabilityURI}

	// 1. Zero properties in filter condition matches all
	rEmptyFilter := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{},
		}, "q1"},
	})
	idsEmpty, _ := rEmptyFilter.MethodResponses[0].Args["ids"].([]any)
	if len(idsEmpty) < 2 {
		t.Errorf("empty filter must match all quota objects, got %v", idsEmpty)
	}

	// 2. Filter by type: "Email"
	rTypeFilter := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"type": "Email"},
		}, "q2"},
	})
	idsType, _ := rTypeFilter.MethodResponses[0].Args["ids"].([]any)
	if len(idsType) < 2 {
		t.Errorf("filter by type:Email must match quotas, got %v", idsType)
	}
}

// TestRFC9425_QuotaPush_StateChanges verifies push notifications for Quota state changes per RFC 9425 §6.
func TestRFC9425_QuotaPush_StateChanges(t *testing.T) {
	spectest.Require(t, "RFC9425", "6", spectest.MUST, "Servers MUST support the JMAP push mechanisms, as specified in")

	srv := newTestServer()
	accID := jmap.AccountIDForSubject(testUsername)
	sub := srv.Broadcaster.Subscribe(accID)
	defer srv.Broadcaster.Unsubscribe(sub)

	accCtx := jmap.ContextWithAccountID(context.Background(), accID)
	// Create an email to mutate storage and trigger Quota change
	_, err := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject:   "Quota Push Test",
		MessageID: []string{"<quota-push@example.com>"},
		BodyStructure: jmap.EmailBodyPart{
			Type: "text/plain",
		},
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	// Verify state change event has Quota
	receivedQuotaPush := false
	select {
	case stateEvt := <-sub:
		if stateEvt != nil && stateEvt.Changed != nil {
			for _, typeMap := range stateEvt.Changed {
				if _, ok := typeMap["Quota"]; ok {
					receivedQuotaPush = true
				}
			}
		}
	case <-time.After(2 * time.Second):
	}

	if !receivedQuotaPush {
		t.Errorf("expected Quota state in push StateChange notification")
	}
}

// TestRFC9425_Security_ScopeAndVisibility verifies quota scope and confidentiality rules per RFC 9425 §8.
func TestRFC9425_Security_ScopeAndVisibility(t *testing.T) {
	spectest.Require(t, "RFC9425", "8", spectest.SHOULD, "Implementors should be careful to make sure the implementation of the")
	spectest.Require(t, "RFC9425", "8", spectest.SHOULD, "considered confidential information and should not be divulged to")
	spectest.Require(t, "RFC9425", "8", spectest.MAY, "Also, revealing domain and global quota counts to all users may cause")
	spectest.Require(t, "RFC9425", "8", spectest.SHOULD, "list belonging to the server, so they shouldn't know how many users")
	spectest.Require(t, "RFC9425", "8", spectest.SHOULD, "with \"domain\" or \"global\" scope SHOULD only be visible to server")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI, jmap.MailCapabilityURI}
	r := postJMAP(t, ts.URL, using, []any{
		[]any{"Quota/get", map[string]any{"accountId": "primary"}, "g1"},
	})
	list, ok := r.MethodResponses[0].Args["list"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("expected quotas returned, got %v", r.MethodResponses[0].Args)
	}

	for _, item := range list {
		q := item.(map[string]any)
		if q["scope"] != "account" {
			t.Errorf("regular users must only receive account-scoped quotas, got scope %v for quota %v", q["scope"], q["id"])
		}
	}
}
