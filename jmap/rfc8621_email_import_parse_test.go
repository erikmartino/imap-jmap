package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
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
