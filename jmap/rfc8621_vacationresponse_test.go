package jmap_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8621_VacationResponseCapability verifies the vacation-response capability is advertised
// at the session and account level (RFC 8621 Section 8).
func TestRFC8621_VacationResponseCapability(t *testing.T) {
	spectest.Require(t, "RFC8621", "8", spectest.MUST,
		"The server advertises the urn:ietf:params:jmap:vacationresponse capability.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("fetch session: %v", err)
	}
	defer resp.Body.Close()
	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if _, ok := session.Capabilities[jmap.VacationResponseCapabilityURI]; !ok {
		t.Errorf("session capabilities missing %q", jmap.VacationResponseCapabilityURI)
	}
	acc, ok := session.Accounts[jmap.AccountIDForSubject(testUsername)]
	if !ok {
		t.Fatalf("primary account missing")
	}
	if _, ok := acc.AccountCapabilities[jmap.VacationResponseCapabilityURI]; !ok {
		t.Errorf("account capabilities missing %q", jmap.VacationResponseCapabilityURI)
	}
	if session.PrimaryAccounts[jmap.VacationResponseCapabilityURI] == "" {
		t.Errorf("primaryAccounts missing %q", jmap.VacationResponseCapabilityURI)
	}
}

// TestRFC8621_VacationResponseGetSet exercises the VacationResponse singleton (RFC 8621 Section 8):
// get returns the "singleton" object, set updates it (partial patch), get reflects the update, and
// create/destroy are rejected with the "singleton" SetError.
func TestRFC8621_VacationResponseGetSet(t *testing.T) {
	spectest.Require(t, "RFC8621", "8.1", spectest.MUST,
		"VacationResponse is a singleton whose id is \"singleton\".")
	spectest.Require(t, "RFC8621", "8.2", spectest.MUST,
		"VacationResponse cannot be created or destroyed; attempts return a singleton SetError.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.VacationResponseCapabilityURI}

	// 1. get returns the singleton, disabled by default.
	getResp := postJMAP(t, ts.URL, using, []any{
		[]any{"VacationResponse/get", map[string]any{"accountId": "primary"}, "c1"},
	})
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 VacationResponse (singleton), got %d: %+v", len(list), getResp.MethodResponses[0].Args)
	}
	vr := list[0].(map[string]any)
	if vr["id"] != "singleton" {
		t.Errorf("expected id \"singleton\", got %v", vr["id"])
	}
	if vr["isEnabled"] != false {
		t.Errorf("expected isEnabled false by default, got %v", vr["isEnabled"])
	}

	// 2. set: enable with a subject and body.
	setResp := postJMAP(t, ts.URL, using, []any{
		[]any{"VacationResponse/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				"singleton": map[string]any{
					"isEnabled": true,
					"subject":   "Out of office",
					"textBody":  "Back next week.",
					"fromDate":  "2026-09-01T00:00:00Z",
					"toDate":    "2026-09-08T00:00:00Z",
				},
			},
		}, "c2"},
	})
	if nu, _ := setResp.MethodResponses[0].Args["notUpdated"].(map[string]any); len(nu) != 0 {
		t.Fatalf("update failed: %+v", setResp.MethodResponses[0].Args)
	}
	upd, _ := setResp.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := upd["singleton"]; !ok {
		t.Fatalf("expected singleton in updated: %+v", setResp.MethodResponses[0].Args)
	}

	// 3. get reflects the update.
	getResp2 := postJMAP(t, ts.URL, using, []any{
		[]any{"VacationResponse/get", map[string]any{"accountId": "primary", "ids": []string{"singleton"}}, "c3"},
	})
	vr2 := getResp2.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if vr2["isEnabled"] != true || vr2["subject"] != "Out of office" || vr2["textBody"] != "Back next week." {
		t.Errorf("update not reflected: %+v", vr2)
	}

	// 4. partial update preserves unaddressed properties (RFC 8620 Section 5.3).
	postJMAP(t, ts.URL, using, []any{
		[]any{"VacationResponse/set", map[string]any{
			"accountId": "primary",
			"update":    map[string]any{"singleton": map[string]any{"isEnabled": false}},
		}, "c4"},
	})
	getResp3 := postJMAP(t, ts.URL, using, []any{
		[]any{"VacationResponse/get", map[string]any{"accountId": "primary"}, "c5"},
	})
	vr3 := getResp3.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if vr3["isEnabled"] != false {
		t.Errorf("expected isEnabled false after partial update, got %v", vr3["isEnabled"])
	}
	if vr3["subject"] != "Out of office" {
		t.Errorf("subject must survive a partial update, got %v", vr3["subject"])
	}

	// 5. create and destroy are rejected with the "singleton" SetError.
	rejResp := postJMAP(t, ts.URL, using, []any{
		[]any{"VacationResponse/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"new": map[string]any{"isEnabled": true}},
			"destroy":   []any{"singleton"},
		}, "c6"},
	})
	nc, _ := rejResp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if nc["new"] == nil || nc["new"].(map[string]any)["type"] != "singleton" {
		t.Errorf("expected singleton SetError for create, got %+v", nc)
	}
	nd, _ := rejResp.MethodResponses[0].Args["notDestroyed"].(map[string]any)
	if nd["singleton"] == nil || nd["singleton"].(map[string]any)["type"] != "singleton" {
		t.Errorf("expected singleton SetError for destroy, got %+v", nd)
	}
}

// TestRFC8621_VacationResponseIfInState verifies ifInState state-mismatch handling.
func TestRFC8621_VacationResponseIfInState(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.VacationResponseCapabilityURI}

	resp := postJMAP(t, ts.URL, using, []any{
		[]any{"VacationResponse/set", map[string]any{
			"accountId": "primary",
			"ifInState": "wrong-state",
			"update":    map[string]any{"singleton": map[string]any{"isEnabled": true}},
		}, "c1"},
	})
	if resp.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error, got %+v", resp.MethodResponses[0])
	}
	if resp.MethodResponses[0].Args["type"] != "stateMismatch" {
		t.Errorf("expected stateMismatch, got %v", resp.MethodResponses[0].Args["type"])
	}
}
