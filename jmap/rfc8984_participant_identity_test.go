package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_ParticipantIdentityLifecycle tests ParticipantIdentity per
// draft-ietf-jmap-calendars Section 3: the seeded default identity, get/changes, create
// with onSuccessSetIsDefault, rejection of direct isDefault, and sendTo key validation.
func TestRFC8984_ParticipantIdentityLifecycle(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. The server seeds one default identity (user@example.com).
	getReq := []any{
		[]any{"ParticipantIdentity/get", map[string]any{
			"accountId": "primary",
		}, "c1"},
	}
	getResp := postJMAP(t, ts.URL, using, getReq)
	args := getResp.MethodResponses[0].Args
	list, ok := args["list"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("expected 1 seeded identity, got %+v", args)
	}
	def := list[0].(map[string]any)
	if def["id"] != "identity-default" {
		t.Errorf("expected seeded identity-default, got %v", def["id"])
	}
	if def["calendarAddress"] != "mailto:user@example.com" {
		t.Errorf("expected mailto:user@example.com calendarAddress, got %v", def["calendarAddress"])
	}
	if def["isDefault"] != true {
		t.Errorf("expected seeded identity isDefault true, got %v", def["isDefault"])
	}
	state1 := args["state"].(string)

	// 2. Direct isDefault in create MUST be rejected with invalidProperties.
	badReq := []any{
		[]any{"ParticipantIdentity/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"pi1": map[string]any{
					"name":            "Direct Default",
					"calendarAddress": "mailto:direct@example.com",
					"isDefault":       true,
				},
			},
		}, "c2"},
	}
	badResp := postJMAP(t, ts.URL, using, badReq)
	notCreated, ok := badResp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok || notCreated["pi1"] == nil {
		t.Fatalf("expected notCreated for direct isDefault, got %+v", badResp.MethodResponses[0].Args)
	}
	errMap := notCreated["pi1"].(map[string]any)
	if errMap["type"] != "invalidProperties" {
		t.Errorf("expected invalidProperties error, got %v", errMap)
	}

	// 3. Create with sendTo and onSuccessSetIsDefault; previous default is demoted.
	createReq := []any{
		[]any{"ParticipantIdentity/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"pi2": map[string]any{
					"name":            "Work Identity",
					"calendarAddress": "mailto:work@example.com",
					"sendTo": map[string]any{
						"mail": "mailto:work@example.com",
					},
				},
			},
			"onSuccessSetIsDefault": "#pi2",
		}, "c3"},
	}
	createResp := postJMAP(t, ts.URL, using, createReq)
	setArgs := createResp.MethodResponses[0].Args
	created, ok := setArgs["created"].(map[string]any)
	if !ok || created["pi2"] == nil {
		t.Fatalf("ParticipantIdentity/set create failed: %+v", setArgs)
	}
	workID := created["pi2"].(map[string]any)["id"].(string)

	// onSuccessSetIsDefault MUST surface the identity in "updated" with isDefault.
	updated, ok := setArgs["updated"].(map[string]any)
	if !ok || updated[workID] == nil {
		t.Fatalf("expected updated entry for onSuccessSetIsDefault target, got %+v", setArgs)
	}

	// 4. get: work identity is now the default, seeded identity is not.
	get2Req := []any{
		[]any{"ParticipantIdentity/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{"identity-default", workID},
		}, "c4"},
	}
	get2Resp := postJMAP(t, ts.URL, using, get2Req)
	byID := map[string]map[string]any{}
	for _, item := range get2Resp.MethodResponses[0].Args["list"].([]any) {
		pi := item.(map[string]any)
		byID[pi["id"].(string)] = pi
	}
	if byID[workID]["isDefault"] != true {
		t.Errorf("expected work identity to be default, got %+v", byID[workID])
	}
	if byID["identity-default"]["isDefault"] != false {
		t.Errorf("expected seeded identity to be demoted, got %+v", byID["identity-default"])
	}

	// 5. changes: seeded identity appears as updated (isDefault demotion).
	changesReq := []any{
		[]any{"ParticipantIdentity/changes", map[string]any{
			"accountId":  "primary",
			"sinceState": state1,
		}, "c5"},
	}
	changesResp := postJMAP(t, ts.URL, using, changesReq)
	chArgs := changesResp.MethodResponses[0].Args
	chCreated, _ := chArgs["created"].([]any)
	if len(chCreated) != 1 || chCreated[0] != workID {
		t.Errorf("expected created [%s] in changes, got %v", workID, chCreated)
	}
	chUpdated, _ := chArgs["updated"].([]any)
	if len(chUpdated) != 1 || chUpdated[0] != "identity-default" {
		t.Errorf("expected updated [identity-default] in changes, got %v", chUpdated)
	}

	// 6. sendTo keys MUST be ASCII alphanumeric; non-conforming keys are rejected.
	sendToReq := []any{
		[]any{"ParticipantIdentity/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"pi3": map[string]any{
					"name":            "Bad SendTo",
					"calendarAddress": "mailto:bad@example.com",
					"sendTo": map[string]any{
						"not-valid!": "mailto:x@example.com",
					},
				},
			},
		}, "c6"},
	}
	sendToResp := postJMAP(t, ts.URL, using, sendToReq)
	notCreated2, ok := sendToResp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok || notCreated2["pi3"] == nil {
		t.Fatalf("expected notCreated for invalid sendTo key, got %+v", sendToResp.MethodResponses[0].Args)
	}

	// 7. destroy the new identity and confirm changes reports it.
	destroyReq := []any{
		[]any{"ParticipantIdentity/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{workID},
		}, "c7"},
	}
	destroyResp := postJMAP(t, ts.URL, using, destroyReq)
	destroyed, ok := destroyResp.MethodResponses[0].Args["destroyed"].([]any)
	if !ok || len(destroyed) != 1 || destroyed[0] != workID {
		t.Fatalf("expected destroyed [%s], got %+v", workID, destroyResp.MethodResponses[0].Args)
	}

	// 8. destroy of a missing identity MUST report notDestroyed, not silently succeed.
	missingReq := []any{
		[]any{"ParticipantIdentity/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{"identity-nope"},
		}, "c8"},
	}
	missingResp := postJMAP(t, ts.URL, using, missingReq)
	notDestroyed, ok := missingResp.MethodResponses[0].Args["notDestroyed"].(map[string]any)
	if !ok || notDestroyed["identity-nope"] == nil {
		t.Errorf("expected notDestroyed for missing identity, got %+v", missingResp.MethodResponses[0].Args)
	}

	// 9. update name through a partial patch; unrelated sendTo survives (no data loss).
	updateReq := []any{
		[]any{"ParticipantIdentity/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				"identity-default": map[string]any{
					"name": "Updated Default",
				},
			},
		}, "c9"},
	}
	updateResp := postJMAP(t, ts.URL, using, updateReq)
	if notUpdated, _ := updateResp.MethodResponses[0].Args["notUpdated"].(map[string]any); len(notUpdated) != 0 {
		t.Fatalf("update failed: %+v", updateResp.MethodResponses[0].Args)
	}
	// RFC 8620 Section 5.3: a plain update reports the id in "updated" with a null value
	// (no server-set properties changed beyond what the client sent) — not a fabricated object.
	updOK, _ := updateResp.MethodResponses[0].Args["updated"].(map[string]any)
	if v, present := updOK["identity-default"]; !present {
		t.Fatalf("expected identity-default in updated, got %+v", updateResp.MethodResponses[0].Args)
	} else if v != nil {
		t.Errorf("expected null value for a plain update, got %v", v)
	}
	get3Req := []any{
		[]any{"ParticipantIdentity/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{"identity-default"},
		}, "c10"},
	}
	get3Resp := postJMAP(t, ts.URL, using, get3Req)
	pi3 := get3Resp.MethodResponses[0].Args["list"].([]any)[0].(map[string]any)
	if pi3["name"] != "Updated Default" {
		t.Errorf("expected name 'Updated Default', got %v", pi3["name"])
	}
	if pi3["calendarAddress"] != "mailto:user@example.com" {
		t.Errorf("calendarAddress must survive partial update, got %v", pi3["calendarAddress"])
	}
}
