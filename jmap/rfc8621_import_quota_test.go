package jmap_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/spectest"
)

// TestRFC8621_EmailImportOverQuota verifies that an import that would take the
// account over its storage or message-count quota is rejected with an
// "overQuota" SetError (RFC 8621 Section 4.8).
func TestRFC8621_EmailImportOverQuota(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.8", spectest.SHOULD,
		"over quota, the import should be rejected with an \"overQuota\"")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}
	acct := jmap.AccountIDForSubject(testUsername)

	mb, ok := srv.MailBackend.(*imapsmtp.IMAPSMTPBackend)
	if !ok {
		t.Skipf("mail backend %T has no configurable quota", srv.MailBackend)
	}

	raw := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: big\r\n" +
		"Message-ID: <quota-import@example.com>\r\n\r\n" + strings.Repeat("body ", 200))
	blob, err := srv.BlobBackend.PutBlob(context.Background(), acct, "message/rfc822", raw)
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	importOne := func() map[string]any {
		r := postJMAP(t, ts.URL, using, []any{
			[]any{"Email/import", map[string]any{
				"accountId": "primary",
				"emails": map[string]any{
					"i1": map[string]any{
						"blobId":     string(blob.ID),
						"mailboxIds": map[string]any{"mb-inbox": true},
					},
				},
			}, "c1"},
		})
		notCreated, _ := r.MethodResponses[0].Args["notCreated"].(map[string]any)
		obj, _ := notCreated["i1"].(map[string]any)
		return obj
	}

	// Storage quota: a 10-octet limit cannot hold the message.
	mb.SetQuotaUsage(acct, 0, 0)
	mb.SetQuotaHardLimits(acct, 10, 0)
	if obj := importOne(); obj == nil || obj["type"] != "overQuota" {
		t.Errorf("storage over-quota import should be rejected with overQuota, got %v", obj)
	}

	// Message-count quota: one message allowed, already used.
	mb.SetQuotaUsage(acct, 0, 1)
	mb.SetQuotaHardLimits(acct, 0, 1)
	if obj := importOne(); obj == nil || obj["type"] != "overQuota" {
		t.Errorf("message-count over-quota import should be rejected with overQuota, got %v", obj)
	}
}
