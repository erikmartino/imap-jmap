package jmap_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8621_MaxBodyValueBytesDoesNotSplitHTMLTag verifies that truncating an
// HTML body value with maxBodyValueBytes does not cut inside an HTML tag
// (RFC 8621 Section 4.2).
func TestRFC8621_MaxBodyValueBytesDoesNotSplitHTMLTag(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.2", spectest.SHOULD,
		"the server SHOULD NOT truncate inside an HTML tag, e")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	const html = `<p>Hello</p><a href="https://example.com/very/long/path">link</a>`
	create := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"h": map[string]any{
					"mailboxIds": map[string]any{"mb-inbox": true},
					"subject":    "truncation",
					"bodyValues": map[string]any{"h": map[string]any{"value": html}},
					"htmlBody":   []any{map[string]any{"partId": "h", "type": "text/html"}},
				},
			},
		}, "c1"},
	})
	id, _ := create.MethodResponses[0].Args["created"].(map[string]any)["h"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("seed failed: %+v", create.MethodResponses[0].Args)
	}

	// 20 bytes lands inside the <a href="..."> tag.
	r := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId":           "primary",
			"ids":                 []any{id},
			"fetchHTMLBodyValues": true,
			"maxBodyValueBytes":   20,
		}, "g"},
	})
	list, _ := r.MethodResponses[0].Args["list"].([]any)
	bv, _ := list[0].(map[string]any)["bodyValues"].(map[string]any)
	part, _ := bv["h"].(map[string]any)
	val, _ := part["value"].(string)

	if i := strings.LastIndex(val, "<"); i >= 0 && !strings.Contains(val[i:], ">") {
		t.Errorf("truncation split an HTML tag: %q", val)
	}
	if trunc, _ := part["isTruncated"].(bool); !trunc {
		t.Errorf("expected isTruncated=true, got %v", part["isTruncated"])
	}
}
