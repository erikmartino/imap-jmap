package jmap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9007_MDNParse_FullConformance tests comprehensive MDN MIME decoding
// and object property mapping per RFC 8098 and RFC 9007 §2 and §3.2.
func TestRFC9007_MDNParse_FullConformance(t *testing.T) {
	spectest.Require(t, "RFC9007", "4", spectest.MUST,
		"MDN/parse parses an MDN blob.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	// 1. Seed an email with known Message-ID to verify forEmailId correlation
	origMsgID := "<original-target-email-123@example.com>"
	targetEmail, err := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject:   "Target Subject For MDN",
		MessageID: []string{origMsgID},
		From:      []jmap.EmailAddress{{Name: "Sender", Email: "sender@example.com"}},
		To:        []jmap.EmailAddress{{Name: "Recipient", Email: "recipient@example.com"}},
	})
	if err != nil {
		t.Fatalf("Failed to create target email: %v", err)
	}

	// 2. Construct a multipart/report MDN message containing:
	// - Human-readable textBody
	// - message/disposition-notification with mixed case disposition, error, and extension fields
	rawMDN := []byte("From: recipient@example.com\r\n" +
		"To: sender@example.com\r\n" +
		"Subject: Read Receipt: Target Subject For MDN\r\n" +
		"Date: Sun, 06 Sep 2026 12:00:00 +0000\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/report; report-type=disposition-notification; boundary=\"report-bnd-1\"\r\n" +
		"\r\n" +
		"--report-bnd-1\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"The message was displayed on recipient's terminal.\r\n" +
		"\r\n" +
		"--report-bnd-1\r\n" +
		"Content-Type: message/disposition-notification\r\n" +
		"\r\n" +
		"Reporting-UA: ExampleMail/2.4 (Unix)\r\n" +
		"MDN-Gateway: smtp; gateway.example.com\r\n" +
		"Original-Recipient: rfc822; orig-recipient@example.com\r\n" +
		"Final-Recipient: rfc822; recipient@example.com\r\n" +
		"Original-Message-ID: " + origMsgID + "\r\n" +
		"Disposition: MANUAL-ACTION/MDN-sent-manually; DISPLAYED\r\n" +
		"Error: Screen resolution too small to display full body\r\n" +
		"X-Custom-MDN-Extension: custom-extension-value-42\r\n" +
		"\r\n" +
		"--report-bnd-1--\r\n")

	mdnBlob, err := srv.BlobBackend.PutBlob(accCtx, accID, "multipart/report", rawMDN)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	// 3. Construct an MDN with unreferenced Original-Message-ID to verify forEmailId is empty/null
	rawUnmatchedMDN := []byte("From: recipient@example.com\r\n" +
		"To: sender@example.com\r\n" +
		"Subject: Receipt for Unknown Message\r\n" +
		"Content-Type: multipart/report; report-type=disposition-notification; boundary=\"report-bnd-2\"\r\n" +
		"\r\n" +
		"--report-bnd-2\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Acknowledged.\r\n" +
		"\r\n" +
		"--report-bnd-2\r\n" +
		"Content-Type: message/disposition-notification\r\n" +
		"\r\n" +
		"Reporting-UA: UnmatchedUA/1.0\r\n" +
		"Final-Recipient: rfc822; recipient@example.com\r\n" +
		"Original-Message-ID: <non-existent-msg-id@example.org>\r\n" +
		"Disposition: automatic-action/MDN-sent-automatically; processed\r\n" +
		"\r\n" +
		"--report-bnd-2--\r\n")

	unmatchedBlob, err := srv.BlobBackend.PutBlob(accCtx, accID, "multipart/report", rawUnmatchedMDN)
	if err != nil {
		t.Fatalf("PutBlob unmatched failed: %v", err)
	}

	// 4. Construct a non-MDN blob (plain text message) to verify notParsable
	nonMDNBlob, err := srv.BlobBackend.PutBlob(accCtx, accID, "text/plain", []byte("Just a normal text file, not an MDN."))
	if err != nil {
		t.Fatalf("PutBlob nonMDN failed: %v", err)
	}

	// 5. Invoke MDN/parse with all three blobs plus a non-existent blob
	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/parse", map[string]any{
				"accountId": "primary",
				"blobIds": []any{
					string(mdnBlob.ID),
					string(unmatchedBlob.ID),
					string(nonMDNBlob.ID),
					"missing-blob-id-999",
				},
			}, "call-mdn-parse"},
		},
	}

	reqBytes, _ := json.Marshal(reqBody)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(reqBytes))
	if err != nil {
		t.Fatalf("POST /jmap MDN/parse failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)

	// Verify notFound contains missing-blob-id-999
	notFound, _ := resArgs["notFound"].([]any)
	if len(notFound) != 1 || notFound[0] != "missing-blob-id-999" {
		t.Errorf("Expected ['missing-blob-id-999'] in notFound, got: %v", notFound)
	}

	// Verify notParsable contains nonMDNBlob.ID
	notParsable, _ := resArgs["notParsable"].([]any)
	if len(notParsable) != 1 || notParsable[0] != string(nonMDNBlob.ID) {
		t.Errorf("Expected [%s] in notParsable, got: %v", nonMDNBlob.ID, notParsable)
	}

	// Verify parsed map
	parsed, _ := resArgs["parsed"].(map[string]any)
	if parsed == nil {
		t.Fatalf("parsed map is nil")
	}

	// Check matched MDN object
	matchedMDNRaw, ok := parsed[string(mdnBlob.ID)]
	if !ok {
		t.Fatalf("mdnBlob %s missing from parsed map: %v", mdnBlob.ID, parsed)
	}
	matchedMDN := matchedMDNRaw.(map[string]any)

	if matchedMDN["forEmailId"] != string(targetEmail.ID) {
		t.Errorf("expected forEmailId %q, got %v", targetEmail.ID, matchedMDN["forEmailId"])
	}
	if matchedMDN["subject"] != "Read Receipt: Target Subject For MDN" {
		t.Errorf("expected subject 'Read Receipt: Target Subject For MDN', got %v", matchedMDN["subject"])
	}
	if matchedMDN["textBody"] != "The message was displayed on recipient's terminal." {
		t.Errorf("expected textBody 'The message was displayed on recipient's terminal.', got %v", matchedMDN["textBody"])
	}
	if matchedMDN["reportingUA"] != "ExampleMail/2.4 (Unix)" {
		t.Errorf("expected reportingUA 'ExampleMail/2.4 (Unix)', got %v", matchedMDN["reportingUA"])
	}
	if matchedMDN["mdnGateway"] != "smtp; gateway.example.com" {
		t.Errorf("expected mdnGateway 'smtp; gateway.example.com', got %v", matchedMDN["mdnGateway"])
	}
	if matchedMDN["originalRecipient"] != "rfc822; orig-recipient@example.com" {
		t.Errorf("expected originalRecipient 'rfc822; orig-recipient@example.com', got %v", matchedMDN["originalRecipient"])
	}
	if matchedMDN["finalRecipient"] != "rfc822; recipient@example.com" {
		t.Errorf("expected finalRecipient 'rfc822; recipient@example.com', got %v", matchedMDN["finalRecipient"])
	}
	if matchedMDN["originalMessageId"] != origMsgID {
		t.Errorf("expected originalMessageId %q, got %v", origMsgID, matchedMDN["originalMessageId"])
	}

	// Verify disposition normalization (must be converted to lowercase per RFC 9007 §2)
	disp, _ := matchedMDN["disposition"].(map[string]any)
	if disp["actionMode"] != "manual-action" {
		t.Errorf("expected lowercase actionMode 'manual-action', got %v", disp["actionMode"])
	}
	if disp["sendingMode"] != "mdn-sent-manually" {
		t.Errorf("expected lowercase sendingMode 'mdn-sent-manually', got %v", disp["sendingMode"])
	}
	if disp["type"] != "displayed" {
		t.Errorf("expected lowercase type 'displayed', got %v", disp["type"])
	}

	// Verify error field
	errList, _ := matchedMDN["error"].([]any)
	if len(errList) != 1 || errList[0] != "Screen resolution too small to display full body" {
		t.Errorf("expected error field in MDN, got: %v", errList)
	}

	// Verify extensionFields
	extFields, _ := matchedMDN["extensionFields"].(map[string]any)
	val, ok := extFields["X-Custom-Mdn-Extension"]
	if !ok {
		val = extFields["X-Custom-MDN-Extension"]
	}
	if val != "custom-extension-value-42" {
		t.Errorf("expected extension field custom-extension-value-42, got: %v", extFields)
	}

	// Check unmatched MDN object: forEmailId should be empty/null because Original-Message-ID was not found
	unmatchedMDNRaw, ok := parsed[string(unmatchedBlob.ID)]
	if !ok {
		t.Fatalf("unmatchedBlob %s missing from parsed map", unmatchedBlob.ID)
	}
	unmatchedMDN := unmatchedMDNRaw.(map[string]any)
	if unmatchedMDN["forEmailId"] != nil && unmatchedMDN["forEmailId"] != "" {
		t.Errorf("expected forEmailId to be null/empty for unmatched message-id, got %v", unmatchedMDN["forEmailId"])
	}
	unmatchedDisp, _ := unmatchedMDN["disposition"].(map[string]any)
	if unmatchedDisp["type"] != "processed" {
		t.Errorf("expected disposition type 'processed', got %v", unmatchedDisp["type"])
	}
}

// TestRFC9007_MDNParse_DirectEntity tests parsing an MDN when the blob contains
// raw message/disposition-notification headers directly without multipart/report wrapping.
func TestRFC9007_MDNParse_DirectEntity(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	rawDirect := []byte("Reporting-UA: DirectClient/1.0\r\n" +
		"Final-Recipient: rfc822; direct@example.com\r\n" +
		"Original-Message-ID: <direct-123@example.com>\r\n" +
		"Disposition: automatic-action/mdn-sent-automatically; deleted\r\n" +
		"\r\n")

	blob, err := srv.BlobBackend.PutBlob(accCtx, accID, "message/disposition-notification", rawDirect)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/parse", map[string]any{
				"accountId": "primary",
				"blobIds":   []any{string(blob.ID)},
			}, "call-1"},
		},
	}

	reqBytes, _ := json.Marshal(reqBody)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(reqBytes))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	parsed, _ := resArgs["parsed"].(map[string]any)

	mdnObj, ok := parsed[string(blob.ID)].(map[string]any)
	if !ok {
		t.Fatalf("expected direct entity to parse successfully, got: %v", resArgs)
	}
	disp := mdnObj["disposition"].(map[string]any)
	if disp["type"] != "deleted" {
		t.Errorf("expected disposition type 'deleted', got %v", disp["type"])
	}
	if disp["actionMode"] != "automatic-action" {
		t.Errorf("expected actionMode 'automatic-action', got %v", disp["actionMode"])
	}
}
