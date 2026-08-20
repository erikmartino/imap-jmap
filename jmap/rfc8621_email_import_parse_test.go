package jmap_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC8621_Section4_8_EmailImportAndParseErrorPaths tests error paths for Email/import and Email/parse per RFC 8621 Section 4.8 & Section 4.9.
func TestRFC8621_Section4_8_EmailImportAndParseErrorPaths(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Create a valid raw MIME blob via BlobBackend
	rawMIME := []byte("From: alice@example.com\r\nTo: bob@example.com\r\nSubject: RFC Import Test\r\n\r\nHello RFC Import World!")
	blob, err := srv.BlobBackend.PutBlob(context.Background(), jmap.AccountIDForSubject(testUsername), "text/plain", rawMIME)
	if err != nil {
		t.Fatalf("Failed to upload blob: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.BlobCapabilityURI}

	// 2. Email/import success with client overrides (keywords, mailboxIds)
	calls1 := []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"imp1": map[string]any{
					"blobId":     string(blob.ID),
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"keywords":   map[string]bool{"$seen": true},
				},
			},
		}, "c1"},
	}
	res1 := postJMAP(t, ts.URL, using, calls1)
	if len(res1.MethodResponses) == 0 {
		t.Fatalf("Empty response for Email/import")
	}
	created, _ := res1.MethodResponses[0].Args["created"].(map[string]any)
	impObj, ok := created["imp1"].(map[string]any)
	if !ok {
		t.Fatalf("Failed to import email: %v", res1.MethodResponses[0].Args)
	}
	importedID, _ := impObj["id"].(string)
	if importedID == "" {
		t.Errorf("Imported Email ID is empty")
	}

	// 3. Email/import missing blobId -> notCreated with type blobNotFound
	calls2 := []any{
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"impBad": map[string]any{
					"blobId":     "non-existent-blob-999",
					"mailboxIds": map[string]bool{"mb-inbox": true},
				},
			},
		}, "c2"},
	}
	res2 := postJMAP(t, ts.URL, using, calls2)
	notCreated, _ := res2.MethodResponses[0].Args["notCreated"].(map[string]any)
	errObj, ok := notCreated["impBad"].(map[string]any)
	if !ok {
		t.Fatalf("Expected impBad in notCreated")
	}
	errType, _ := errObj["type"].(string)
	if errType != "notFound" && errType != "blobNotFound" && errType != "invalidProperties" {
		t.Errorf("Expected blobNotFound, notFound, or invalidProperties type, got %q", errType)
	}

	// 4. Email/parse with valid blobId
	calls3 := []any{
		[]any{"Email/parse", map[string]any{
			"accountId":  "primary",
			"blobIds":    []any{string(blob.ID)},
			"properties": []string{"subject", "from", "to"},
		}, "c3"},
	}
	res3 := postJMAP(t, ts.URL, using, calls3)
	if len(res3.MethodResponses) == 0 || res3.MethodResponses[0].Name != "Email/parse" {
		t.Fatalf("Expected Email/parse response, got %v", res3.MethodResponses)
	}
	parsed, _ := res3.MethodResponses[0].Args["parsed"].(map[string]any)
	pObj, ok := parsed[string(blob.ID)].(map[string]any)
	if !ok {
		t.Fatalf("Failed to parse blob %s: %v", blob.ID, res3.MethodResponses[0].Args)
	}
	subj, _ := pObj["subject"].(string)
	if subj != "RFC Import Test" {
		t.Errorf("Expected subject 'RFC Import Test', got %q", subj)
	}
}

// TestRFC8621_SEC6_DeepMultipartNesting tests that deeply nested MIME multipart messages
// are safely bounded by MaxMIMENestingDepth without stack overflow.
func TestRFC8621_SEC6_DeepMultipartNesting(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST,
		"Resource limits: Deeply nested multipart messages must be parsed with bounded recursion depth.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Construct a 20-level nested multipart/mixed MIME message (exceeding MaxMIMENestingDepth=10)
	var buf bytes.Buffer
	buf.WriteString("From: sender@example.com\r\n")
	buf.WriteString("To: user@example.com\r\n")
	buf.WriteString("Subject: Deeply Nested MIME\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")

	for i := 1; i <= 20; i++ {
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"bound%d\"\r\n\r\n", i))
		buf.WriteString(fmt.Sprintf("--bound%d\r\n", i))
	}
	buf.WriteString("Content-Type: text/plain\r\n\r\nInnermost leaf content\r\n")
	for i := 20; i >= 1; i-- {
		buf.WriteString(fmt.Sprintf("--bound%d--\r\n", i))
	}

	blob, err := srv.BlobBackend.PutBlob(seedCtx(), jmap.AccountIDForSubject(testUsername), "message/rfc822", buf.Bytes())
	if err != nil {
		t.Fatalf("Failed to upload blob: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}
	calls := []any{
		[]any{"Email/parse", map[string]any{
			"accountId": "primary",
			"blobIds":   []any{string(blob.ID)},
		}, "c1"},
	}
	res := postJMAP(t, ts.URL, using, calls)
	if len(res.MethodResponses) == 0 || res.MethodResponses[0].Name != "Email/parse" {
		t.Fatalf("Expected Email/parse response, got %v", res.MethodResponses)
	}
	parsed, _ := res.MethodResponses[0].Args["parsed"].(map[string]any)
	if parsed[string(blob.ID)] == nil {
		t.Fatalf("Expected successfully parsed email object for deeply nested MIME, got: %v", res.MethodResponses[0].Args)
	}
}

// TestRFC8621_SEC6_OversizedRawMessage tests that messages exceeding MaxEmailRawSize fail parsing cleanly.
func TestRFC8621_SEC6_OversizedRawMessage(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST,
		"Resource limits: Messages exceeding MaxEmailRawSize must fail parseRFC822WithAccount.")

	oversized := make([]byte, jmap.MaxEmailRawSize+1024)
	copy(oversized, []byte("From: sender@example.com\r\nSubject: Big\r\n\r\nBig body"))

	srv := newTestServer()
	blob, err := srv.BlobBackend.PutBlob(seedCtx(), jmap.AccountIDForSubject(testUsername), "message/rfc822", oversized)
	if err != nil {
		t.Fatalf("Failed to upload blob: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}
	calls := []any{
		[]any{"Email/parse", map[string]any{
			"accountId": "primary",
			"blobIds":   []any{string(blob.ID)},
		}, "c1"},
	}
	res := postJMAP(t, ts.URL, using, calls)
	if len(res.MethodResponses) == 0 || res.MethodResponses[0].Name != "Email/parse" {
		t.Fatalf("Expected Email/parse response, got %v", res.MethodResponses)
	}
	notParsable, _ := res.MethodResponses[0].Args["notParsable"].([]any)
	if len(notParsable) == 0 {
		t.Errorf("Expected oversized message to be reported in notParsable, got: %v", res.MethodResponses[0].Args)
	}
}
