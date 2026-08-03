package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8621_EmailSetErrorPaths verifies Email/set reports notCreated/notUpdated/
// notDestroyed with the exact SetError types RFC 8621 Section 4.6 and RFC 8620
// Section 5.3 require: invalid creates land in notCreated (invalidProperties), updates of
// missing ids land in notUpdated (notFound), and destroys of missing ids land in
// notDestroyed (notFound) instead of being silently dropped. A valid create in the same
// batch still succeeds (partial success).
func TestRFC8621_EmailSetErrorPaths(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	post := func(calls []any) jmap.Response {
		return postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, calls)
	}

	r := post([]any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"ok":   map[string]any{"subject": "Good", "mailboxIds": map[string]any{"mb-inbox": true}},
				"nomb": map[string]any{"subject": "No mailbox"},
				"hdr": map[string]any{
					"subject":    "Headers",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"headers":    []any{map[string]any{"name": "Subject", "value": "x"}},
				},
				"bs": map[string]any{
					"subject":       "Body conflict",
					"mailboxIds":    map[string]any{"mb-inbox": true},
					"bodyStructure": map[string]any{"type": "text/plain"},
					"textBody":      []any{map[string]any{"type": "text/plain"}},
				},
				"tb": map[string]any{
					"subject":    "Bad textBody",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"textBody":   []any{map[string]any{"type": "text/html"}},
				},
			},
			"update": map[string]any{
				"email-does-not-exist": map[string]any{"keywords": map[string]any{"$seen": true}},
			},
			"destroy": []any{"email-does-not-exist"},
		}, "c1"},
	})
	args := r.MethodResponses[0].Args

	created, _ := args["created"].(map[string]any)
	if created == nil || created["ok"] == nil {
		t.Fatalf("valid create in the same batch must succeed (partial success), got created=%v", args["created"])
	}

	notCreated, _ := args["notCreated"].(map[string]any)
	for _, key := range []string{"nomb", "hdr", "bs", "tb"} {
		errObj, ok := notCreated[key].(map[string]any)
		if !ok {
			t.Errorf("expected %q in notCreated, got %v", key, notCreated)
			continue
		}
		if errObj["type"] != "invalidProperties" {
			t.Errorf("expected notCreated[%q].type=invalidProperties, got %v", key, errObj["type"])
		}
	}
	if len(notCreated) != 4 {
		t.Errorf("expected exactly 4 notCreated entries, got %v", notCreated)
	}

	notUpdated, _ := args["notUpdated"].(map[string]any)
	updErr, ok := notUpdated["email-does-not-exist"].(map[string]any)
	if !ok {
		t.Fatalf("expected notUpdated for missing update id, got %v", args["notUpdated"])
	}
	if updErr["type"] != "notFound" {
		t.Errorf("expected notUpdated type notFound for missing id, got %v", updErr["type"])
	}

	destroyed, _ := args["destroyed"].([]any)
	if len(destroyed) != 0 {
		t.Errorf("missing id must not appear in destroyed, got %v", destroyed)
	}
	notDestroyed, _ := args["notDestroyed"].(map[string]any)
	delErr, ok := notDestroyed["email-does-not-exist"].(map[string]any)
	if !ok {
		t.Fatalf("expected notDestroyed for missing destroy id, got %v", args["notDestroyed"])
	}
	if delErr["type"] != "notFound" {
		t.Errorf("expected notDestroyed type notFound for missing id, got %v", delErr["type"])
	}

	createdID, _ := created["ok"].(map[string]any)["id"].(string)
	if createdID == "" {
		t.Fatal("created email has no id")
	}
}

// TestRFC8621_MailboxSetUpdateMissingNotFound verifies Mailbox/set reports a missing update
// id in notUpdated with type notFound (RFC 8620 Section 5.3), not invalidProperties.
func TestRFC8621_MailboxSetUpdateMissingNotFound(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update":    map[string]any{"mb-does-not-exist": map[string]any{"name": "Renamed"}},
		}, "c1"},
	})
	notUpdated, _ := r.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errObj, ok := notUpdated["mb-does-not-exist"].(map[string]any)
	if !ok {
		t.Fatalf("expected notUpdated for missing update id, got %v", r.MethodResponses[0].Args["notUpdated"])
	}
	if errObj["type"] != "notFound" {
		t.Errorf("expected notUpdated type notFound, got %v", errObj["type"])
	}
}

// TestRFC8621_IdentitySetUpdateMissingNotFound verifies Identity/set reports a missing
// update id in notUpdated with type notFound.
func TestRFC8621_IdentitySetUpdateMissingNotFound(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Identity/set", map[string]any{
			"accountId": "primary",
			"update":    map[string]any{"idn-does-not-exist": map[string]any{"name": "Renamed"}},
		}, "c1"},
	})
	notUpdated, _ := r.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errObj, ok := notUpdated["idn-does-not-exist"].(map[string]any)
	if !ok {
		t.Fatalf("expected notUpdated for missing update id, got %v", r.MethodResponses[0].Args["notUpdated"])
	}
	if errObj["type"] != "notFound" {
		t.Errorf("expected notUpdated type notFound, got %v", errObj["type"])
	}
}

// TestRFC9661_SieveScriptSetUpdateMissingNotFound verifies SieveScript/set reports a
// missing update id in notUpdated with type notFound, not invalidScript.
func TestRFC9661_SieveScriptSetUpdateMissingNotFound(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}, []any{
		[]any{"SieveScript/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{"sieve-does-not-exist": map[string]any{
				"content": `require ["fileinto"]; fileinto "INBOX.test";`,
			}},
		}, "c1"},
	})
	notUpdated, _ := r.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errObj, ok := notUpdated["sieve-does-not-exist"].(map[string]any)
	if !ok {
		t.Fatalf("expected notUpdated for missing update id, got %v", r.MethodResponses[0].Args["notUpdated"])
	}
	if errObj["type"] != "notFound" {
		t.Errorf("expected notUpdated type notFound, got %v", errObj["type"])
	}
}

// TestRFC9404_FileNodeSetUpdateMissingNotFound verifies FileNode/set reports a missing
// update id in notUpdated with type notFound, not invalidProperties.
func TestRFC9404_FileNodeSetUpdateMissingNotFound(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI}, []any{
		[]any{"FileNode/set", map[string]any{
			"accountId": "primary",
			"update":    map[string]any{"fn-does-not-exist": map[string]any{"name": "Renamed"}},
		}, "c1"},
	})
	notUpdated, _ := r.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errObj, ok := notUpdated["fn-does-not-exist"].(map[string]any)
	if !ok {
		t.Fatalf("expected notUpdated for missing update id, got %v", r.MethodResponses[0].Args["notUpdated"])
	}
	if errObj["type"] != "notFound" {
		t.Errorf("expected notUpdated type notFound, got %v", errObj["type"])
	}
}
