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

// TestRFC8621_EmailSetEmptyFieldsNull verifies RFC 8620 Section 5.3: the set-method
// arguments created/updated/destroyed/notCreated/notUpdated/notDestroyed are typed
// "Id[...]|null" and MUST serialize as JSON null (not "{}"/"[]") when there are no
// records. Clients (e.g. Bulwark webmail's createDraft) check `if (result.notCreated)`
// to detect a failed save, so an empty object is indistinguishable from an error.
func TestRFC8621_EmailSetEmptyFieldsNull(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// postRaw gives the raw JSON body so we can assert the wire representation.
	r := postRaw(t, ts.URL, map[string]any{
		"using":      []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"createdIds": map[string]string{},
		"methodCalls": []any{
			[]any{"Email/set", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"d1": map[string]any{
						"subject":    "Null fields",
						"mailboxIds": map[string]any{"mb-inbox": true},
						"bodyValues": map[string]any{"1": map[string]any{"value": "x"}},
						"textBody":   []any{map[string]any{"partId": "1"}},
					},
				},
			}, "c1"},
			[]any{"Email/set", map[string]any{
				"accountId": "primary",
				"destroy":   []any{"does-not-exist"},
			}, "c2"},
		},
	})

	first := r.MethodResponses[0].Args
	// created holds d1, so it must be a non-null object.
	if created, ok := first["created"].(map[string]any); !ok || created["d1"] == nil {
		t.Errorf("first Email/set: expected created[d1] object, got %v", first["created"])
	}
	// The remaining set arguments have no records and MUST be JSON null.
	for _, key := range []string{"updated", "notCreated", "notUpdated", "notDestroyed", "destroyed"} {
		v, present := first[key]
		if !present {
			t.Errorf("first Email/set: expected %q present (null when empty), got absent", key)
		}
		if v != nil {
			t.Errorf("first Email/set: expected %q to be JSON null, got %v", key, v)
		}
	}

	second := r.MethodResponses[1].Args
	for _, key := range []string{"updated", "created", "destroyed", "notUpdated", "notCreated"} {
		v, present := second[key]
		if !present {
			t.Errorf("second Email/set: expected %q present (null when empty), got absent", key)
		}
		if v != nil {
			t.Errorf("second Email/set: expected %q to be JSON null, got %v", key, v)
		}
	}
	if v, present := second["notDestroyed"]; !present {
		t.Errorf("second Email/set: expected notDestroyed present, got absent")
	} else if nd, ok := v.(map[string]any); !ok || nd["does-not-exist"] == nil {
		t.Errorf("second Email/set: expected notDestroyed[does-not-exist] error object, got %v", v)
	}
}

// TestRFC8621_EmailCreateImplicitBodyPartType verifies that Email/set accepts textBody/
// htmlBody body parts referenced by partId WITHOUT an explicit "type" property. Per RFC
// 8621 Section 4.2.2 the "type" property of a part defaults to the implicit MIME type
// (text/plain when no Content-Type is present), so a draft sent by mainstream clients
// (e.g. Bulwark webmail's createDraft) that omits "type" MUST NOT be rejected with
// invalidProperties. Explicitly conflicting types (e.g. text/html listed in textBody)
// MUST still be rejected.
func TestRFC8621_EmailCreateImplicitBodyPartType(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"plain": map[string]any{
					"subject":    "Implicit plain draft",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"keywords":   map[string]any{"$draft": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "Hello world"}},
					"textBody":   []any{map[string]any{"partId": "1"}},
				},
				"html": map[string]any{
					"subject":    "Implicit html draft",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"keywords":   map[string]any{"$draft": true},
					"bodyValues": map[string]any{"h": map[string]any{"value": "<p>Hi</p>"}},
					"htmlBody":   []any{map[string]any{"partId": "h"}},
				},
				"explicitconflict": map[string]any{
					"subject":    "Wrong type in textBody",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "x"}},
					"textBody":   []any{map[string]any{"partId": "1", "type": "text/html"}},
				},
			},
		}, "c1"},
	})
	args := r.MethodResponses[0].Args

	created, _ := args["created"].(map[string]any)
	for _, key := range []string{"plain", "html"} {
		if created[key] == nil {
			t.Errorf("expected %q (implicit body part type) to be created, got created=%v notCreated=%v", key, created, args["notCreated"])
		}
	}

	notCreated, _ := args["notCreated"].(map[string]any)
	errObj, ok := notCreated["explicitconflict"].(map[string]any)
	if !ok {
		t.Fatalf("expected explicitconflict in notCreated, got %v", notCreated)
	}
	if errObj["type"] != "invalidProperties" {
		t.Errorf("expected notCreated[explicitconflict].type=invalidProperties, got %v", errObj["type"])
	}
}

// TestRFC8621_EmailCreateReconstructsBodyStructure verifies that creating an Email
// from textBody/htmlBody arrays plus bodyValues (RFC 8621 Section 4.6) makes the server
// reconstruct bodyStructure, textBody/htmlBody, and preview so clients can render the
// message without a blob fetch. A missing bodyStructure/type is what a real server would
// never return, and clients (e.g. Bulwark webmail) display "(No body content available)"
// when bodyStructure is empty.
func TestRFC8621_EmailCreateReconstructsBodyStructure(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"text": map[string]any{
					"subject":    "Reconstructed text",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "Created via JMAP, viewed via Bulwark"}},
					"textBody":   []any{map[string]any{"partId": "1"}},
				},
				"html": map[string]any{
					"subject":    "Reconstructed html",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"h": map[string]any{"value": "<p>Hi</p>"}},
					"htmlBody":   []any{map[string]any{"partId": "h"}},
				},
			},
		}, "c1"},
	})

	created, _ := r.MethodResponses[0].Args["created"].(map[string]any)
	textID := created["text"].(map[string]any)["id"].(string)
	htmlID := created["html"].(map[string]any)["id"].(string)

	r2 := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{textID, htmlID},
			"properties": []any{"subject", "bodyStructure", "textBody", "htmlBody", "preview", "bodyValues"},
		}, "g"},
	})
	args := r2.MethodResponses[0].Args

	list, _ := args["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected 2 emails in Email/get list, got %v", list)
	}

	bySubject := map[string]map[string]any{}
	for _, raw := range list {
		em, _ := raw.(map[string]any)
		bySubject[em["subject"].(string)] = em
	}

	textEM := bySubject["Reconstructed text"]
	bs, _ := textEM["bodyStructure"].(map[string]any)
	if bs == nil || bs["type"] != "text/plain" || bs["partId"] != "1" {
		t.Errorf("expected text email bodyStructure {partId:1,type:text/plain}, got %v", textEM["bodyStructure"])
	}
	if size, _ := bs["size"].(float64); size != float64(len("Created via JMAP, viewed via Bulwark")) {
		t.Errorf("expected bodyStructure size %d, got %v", len("Created via JMAP, viewed via Bulwark"), bs["size"])
	}
	if tb, _ := textEM["textBody"].([]any); len(tb) != 1 {
		t.Errorf("expected textBody with 1 part, got %v", textEM["textBody"])
	}
	if textEM["preview"] != "Created via JMAP, viewed via Bulwark" {
		t.Errorf("expected preview to be the body text, got %q", textEM["preview"])
	}
	if bv, _ := textEM["bodyValues"].(map[string]any); bv == nil {
		t.Errorf("expected bodyValues to survive create/get round-trip")
	}

	htmlEM := bySubject["Reconstructed html"]
	hbs, _ := htmlEM["bodyStructure"].(map[string]any)
	if hbs == nil || hbs["type"] != "text/html" || hbs["partId"] != "h" {
		t.Errorf("expected html email bodyStructure {partId:h,type:text/html}, got %v", htmlEM["bodyStructure"])
	}
	if hb, _ := htmlEM["htmlBody"].([]any); len(hb) != 1 {
		t.Errorf("expected htmlBody with 1 part, got %v", htmlEM["htmlBody"])
	}
	if htmlEM["preview"] != "Hi" {
		t.Errorf("expected html preview to be stripped plain text 'Hi', got %q", htmlEM["preview"])
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
