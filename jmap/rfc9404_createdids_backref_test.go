package jmap_test

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9404_Section4_1_CreatedIdsBackReference verifies that a successful
// Blob/upload records an entry in the request-scoped createdIds map so a later
// method call can back-reference "#creationId" (RFC 9404 Section 4.1).
func TestRFC9404_Section4_1_CreatedIdsBackReference(t *testing.T) {
	spectest.Require(t, "RFC9404", "4.1", spectest.MUST,
		"For each successful upload, servers MUST add an entry to the")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI, jmap.MailCapabilityURI}

	raw := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: Backref\r\n\r\nBody\r\n")
	r := postJMAP(t, ts.URL, using, []any{
		[]any{"Blob/upload", map[string]any{"accountId": "primary", "create": map[string]any{
			"b1": map[string]any{"type": "message/rfc822", "data": []any{
				map[string]any{"data:asBase64": base64.StdEncoding.EncodeToString(raw)},
			}},
		}}, "u1"},
		[]any{"Email/import", map[string]any{"accountId": "primary", "create": map[string]any{
			"i1": map[string]any{"blobId": "#b1", "mailboxIds": map[string]any{"mb-inbox": true}},
		}}, "i1"},
	})
	if r.MethodResponses[0].Name != "Blob/upload" {
		t.Fatalf("upload error: %v", r.MethodResponses[0].Args)
	}
	mr := r.MethodResponses[1]
	if mr.Name != "Email/import" {
		t.Fatalf("import error: %v", mr.Args)
	}
	if created, _ := mr.Args["created"].(map[string]any); created["i1"] == nil {
		t.Errorf("back-reference #b1 should resolve to the uploaded blob, got %v (notCreated %v)", mr.Args["created"], mr.Args["notCreated"])
	}
}
