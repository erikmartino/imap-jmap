package jmap_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
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

// TestRFC8621_EmailCreateGeneratesMessageID verifies RFC 8621 Section 4.6:
// "If no Message-ID header field is included, the server MUST generate and set a Message-ID header field in conformance with [RFC5322], Section 3.6.4."
func TestRFC8621_EmailCreateGeneratesMessageID(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"included, the server MUST generate and set a Message-ID header field")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"msg1": map[string]any{
					"subject":    "Auto Message-ID Test",
					"from":       []any{map[string]any{"email": "alice@custom-domain.org"}},
					"to":         []any{map[string]any{"email": "bob@example.com"}},
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "Hello World"}},
					"textBody":   []any{map[string]any{"partId": "1"}},
				},
			},
		}, "c1"},
	})

	created, ok := r.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["msg1"] == nil {
		t.Fatalf("expected created msg1, got %v", r.MethodResponses[0].Args)
	}
	msgID := created["msg1"].(map[string]any)["id"].(string)

	r2 := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{msgID},
			"properties": []any{"id", "messageId", "from", "subject"},
		}, "g1"},
	})

	list, _ := r2.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 email in list, got %v", list)
	}
	em := list[0].(map[string]any)
	msgIDs, ok := em["messageId"].([]any)
	if !ok || len(msgIDs) == 0 {
		t.Fatalf("expected server-generated messageId, got %v", em["messageId"])
	}
	generatedID, ok := msgIDs[0].(string)
	if !ok || generatedID == "" {
		t.Fatalf("expected non-empty messageId string, got %v", msgIDs[0])
	}
	// Per RFC 8621 Section 4.1.2: Message-ID value is WITHOUT enclosing angle brackets
	if strings.HasPrefix(generatedID, "<") || strings.HasSuffix(generatedID, ">") {
		t.Errorf("JMAP messageId property MUST NOT contain enclosing angle brackets, got %q", generatedID)
	}
	// Verify that with angle brackets, it conforms to RFC 5322 Section 3.6.4
	wrapped := "<" + generatedID + ">"
	if !jmap.HasValidMessageID([]byte("Message-ID: " + wrapped + "\r\n\r\n")) {
		t.Errorf("expected generated Message-ID to conform to RFC 5322 Section 3.6.4, got %q", wrapped)
	}
	// Verify domain is derived from sender rather than hardcoded example.com
	if !strings.HasSuffix(generatedID, "@custom-domain.org") {
		t.Errorf("expected Message-ID domain to match sender domain custom-domain.org, got %q", generatedID)
	}
}

// TestRFC8621_EmailCreateRejectsHeadersProperty verifies RFC 8621 Section 4.6:
// "The "headers" property MUST NOT be given on either the top-level Email or an EmailBodyPart"
func TestRFC8621_EmailCreateRejectsHeadersProperty(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"o The \"headers\" property MUST NOT be given on either the top-level")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"top": map[string]any{
					"subject":    "Test",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"headers":    []any{map[string]any{"name": "X-Custom", "value": "val"}},
				},
				"part": map[string]any{
					"subject":    "Test 2",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "Body"}},
					"textBody": []any{map[string]any{
						"partId":  "1",
						"headers": []any{map[string]any{"name": "X-Part", "value": "val"}},
					}},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	for _, key := range []string{"top", "part"} {
		errObj, ok := notCreated[key].(map[string]any)
		if !ok || errObj["type"] != "invalidProperties" {
			t.Errorf("expected %s rejected with invalidProperties, got %v", key, errObj)
		}
	}
}

// TestRFC8621_EmailCreateRejectsDuplicateHeaderProperties verifies RFC 8621 Section 4.6:
// "There MUST NOT be two properties that represent the same header"
func TestRFC8621_EmailCreateRejectsDuplicateHeaderProperties(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"o There MUST NOT be two properties that represent the same header")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"dup": map[string]any{
					"subject":        "Original Subject",
					"header:Subject": "Duplicate Header Subject",
					"mailboxIds":     map[string]any{"mb-inbox": true},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["dup"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected duplicate header rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreateRejectsForbiddenParsedForms verifies RFC 8621 Section 4.6:
// "Header fields MUST NOT be specified in parsed forms that are forbidden for that header field"
func TestRFC8621_EmailCreateRejectsForbiddenParsedForms(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"o Header fields MUST NOT be specified in parsed forms that are")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"badForm": map[string]any{
					"mailboxIds":                 map[string]any{"mb-inbox": true},
					"header:Subject:asAddresses": []any{map[string]any{"email": "alice@example.com"}},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["badForm"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected forbidden parsed form rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreateRejectsContentHeadersOnTopLevel verifies RFC 8621 Section 4.6:
// "Header fields beginning with \"Content-\" MUST NOT be specified on the Email object"
func TestRFC8621_EmailCreateRejectsContentHeadersOnTopLevel(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"o Header fields beginning with \"Content-\" MUST NOT be specified on")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"contentHdr": map[string]any{
					"subject":             "Content header on top level",
					"mailboxIds":          map[string]any{"mb-inbox": true},
					"header:Content-Type": "text/plain",
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["contentHdr"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected top-level Content-* header rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreateBodyStructureMutualExclusion verifies RFC 8621 Section 4.6:
// "If a \"bodyStructure\" property is given, there MUST NOT be any textBody, htmlBody, or attachments property."
func TestRFC8621_EmailCreateBodyStructureMutualExclusion(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"o If a \"bodyStructure\" property is given, there MUST NOT be")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"conflict": map[string]any{
					"subject":       "Conflict",
					"mailboxIds":    map[string]any{"mb-inbox": true},
					"bodyStructure": map[string]any{"partId": "1", "type": "text/plain"},
					"textBody":      []any{map[string]any{"partId": "1"}},
					"bodyValues":    map[string]any{"1": map[string]any{"value": "Hello"}},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["conflict"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected bodyStructure conflict rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreateBodyStructureSubPartsMultipartOnly verifies RFC 8621 Section 4.6:
// "If given, the \"bodyStructure\" EmailBodyPart MUST NOT contain a subParts property unless the type is multipart/*"
func TestRFC8621_EmailCreateBodyStructureSubPartsMultipartOnly(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"o If given, the \"bodyStructure\" EmailBodyPart MUST NOT contain a")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"invalidSubParts": map[string]any{
					"subject":    "Invalid subparts",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyStructure": map[string]any{
						"type":     "text/plain",
						"subParts": []any{map[string]any{"partId": "1"}},
					},
					"bodyValues": map[string]any{"1": map[string]any{"value": "Hello"}},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["invalidSubParts"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected non-multipart subParts rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreateTextBodySingleTextPlainPart verifies RFC 8621 Section 4.6:
// "If given, textBody MUST contain exactly one body part and it MUST have type text/plain"
func TestRFC8621_EmailCreateTextBodySingleTextPlainPart(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"o If given, textBody MUST contain exactly one body part and it MUST")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"twoParts": map[string]any{
					"subject":    "Two textBody parts",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{
						"1": map[string]any{"value": "Part 1"},
						"2": map[string]any{"value": "Part 2"},
					},
					"textBody": []any{
						map[string]any{"partId": "1"},
						map[string]any{"partId": "2"},
					},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["twoParts"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected multi-part textBody rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreateHtmlBodySingleTextHtmlPart verifies RFC 8621 Section 4.6:
// "If given, htmlBody MUST contain exactly one body part and it MUST have type text/html"
func TestRFC8621_EmailCreateHtmlBodySingleTextHtmlPart(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"o If given, htmlBody MUST contain exactly one body part and it MUST")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"wrongType": map[string]any{
					"subject":    "Wrong type for htmlBody",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "text"}},
					"htmlBody":   []any{map[string]any{"partId": "1", "type": "text/plain"}},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["wrongType"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected wrong-type htmlBody rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreatePartIdOrBlobIdMutualExclusion verifies RFC 8621 Section 4.6:
// "The client may specify a partId OR a blobId, but not both"
func TestRFC8621_EmailCreatePartIdOrBlobIdMutualExclusion(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MAY,
		"* The client may specify a partId OR a blobId, but not both")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"both": map[string]any{
					"subject":    "Both partId and blobId",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "Hello"}},
					"textBody": []any{
						map[string]any{"partId": "1", "blobId": "blob-123"},
					},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["both"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected both partId and blobId rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreatePartIdMustBeInBodyValues verifies RFC 8621 Section 4.6:
// "a partId is given, this partId MUST be present in the bodyValues map"
func TestRFC8621_EmailCreatePartIdMustBeInBodyValues(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"a partId is given, this partId MUST be present in the")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"missingBV": map[string]any{
					"subject":    "partId missing from bodyValues",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"textBody":   []any{map[string]any{"partId": "non-existent"}},
					"bodyValues": map[string]any{"other": map[string]any{"value": "abc"}},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["missingBV"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected missing bodyValues partId rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreatePartIdMustOmitCharsetAndSize verifies RFC 8621 Section 4.6:
// "The \"charset\" property MUST be omitted if a partId is given" and
// "The \"size\" property MUST be omitted if a partId is given"
func TestRFC8621_EmailCreatePartIdMustOmitCharsetAndSize(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"* The \"charset\" property MUST be omitted if a partId is given")
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"* The \"size\" property MUST be omitted if a partId is given")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"withCharset": map[string]any{
					"subject":    "Charset with partId",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "Hello"}},
					"textBody": []any{
						map[string]any{"partId": "1", "charset": "utf-8"},
					},
				},
				"withSize": map[string]any{
					"subject":    "Size with partId",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"2": map[string]any{"value": "World"}},
					"textBody": []any{
						map[string]any{"partId": "2", "size": float64(5)},
					},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	for _, key := range []string{"withCharset", "withSize"} {
		errObj, ok := notCreated[key].(map[string]any)
		if !ok || errObj["type"] != "invalidProperties" {
			t.Errorf("expected %s rejected with invalidProperties, got %v", key, errObj)
		}
	}
}

// TestRFC8621_EmailCreateRejectsContentTransferEncodingOnPart verifies RFC 8621 Section 4.6:
// "A Content-Transfer-Encoding header field MUST NOT be given"
func TestRFC8621_EmailCreateRejectsContentTransferEncodingOnPart(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"* A Content-Transfer-Encoding header field MUST NOT be given")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"withCTE": map[string]any{
					"subject":    "CTE on body part",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"bodyValues": map[string]any{"1": map[string]any{"value": "Hello"}},
					"textBody": []any{
						map[string]any{
							"partId":                           "1",
							"header:Content-Transfer-Encoding": "base64",
						},
					},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["withCTE"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected CTE on body part rejected with invalidProperties, got %v", errObj)
	}
}

// TestRFC8621_EmailCreateBodyValuesTruncatedOrEncodingProblem verifies RFC 8621 Section 4.6:
// "isTruncated and isEncodingProblem MUST be either false or omitted"
func TestRFC8621_EmailCreateBodyValuesTruncatedOrEncodingProblem(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.MUST,
		"MUST be either false or omitted")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"truncated": map[string]any{
					"subject":    "Truncated body value",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"textBody":   []any{map[string]any{"partId": "1"}},
					"bodyValues": map[string]any{"1": map[string]any{"value": "Hello", "isTruncated": true}},
				},
				"encodingProblem": map[string]any{
					"subject":    "Encoding problem body value",
					"mailboxIds": map[string]any{"mb-inbox": true},
					"textBody":   []any{map[string]any{"partId": "2"}},
					"bodyValues": map[string]any{"2": map[string]any{"value": "World", "isEncodingProblem": true}},
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	for _, key := range []string{"truncated", "encodingProblem"} {
		errObj, ok := notCreated[key].(map[string]any)
		if !ok || errObj["type"] != "invalidProperties" {
			t.Errorf("expected %s rejected with invalidProperties, got %v", key, errObj)
		}
	}
}

// TestRFC8621_EmailCreateRejectionWithInvalidProperties verifies RFC 8621 Section 4.6:
// "Creation attempts that violate any of this SHOULD be rejected with an \"invalidProperties\" error"
func TestRFC8621_EmailCreateRejectionWithInvalidProperties(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.6", spectest.SHOULD,
		"Creation attempts that violate any of this SHOULD be rejected with an")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	r := postJMAP(t, ts.URL, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"noMailboxes": map[string]any{
					"subject": "Missing mailboxIds",
				},
			},
		}, "c1"},
	})

	notCreated, ok := r.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok {
		t.Fatalf("expected notCreated map, got %v", r.MethodResponses[0].Args)
	}
	errObj, ok := notCreated["noMailboxes"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("expected missing mailboxIds rejected with invalidProperties, got %v", errObj)
	}
}
