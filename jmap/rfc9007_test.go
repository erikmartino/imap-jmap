package jmap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC9007_SessionCapability tests urn:ietf:params:jmap:mdn capability declaration in JMAP session per RFC 9007 Section 2.
func TestRFC9007_SessionCapability(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("Failed to fetch session: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session JSON: %v", err)
	}

	if _, ok := session.Capabilities[jmap.MdnCapabilityURI]; !ok {
		t.Errorf("Expected session capabilities to contain %q", jmap.MdnCapabilityURI)
	}

	primaryAcc, ok := session.Accounts[jmap.AccountIDForSubject(testUsername)]
	if !ok {
		t.Fatalf("Primary account missing")
	}

	if _, ok := primaryAcc.AccountCapabilities[jmap.MdnCapabilityURI]; !ok {
		t.Errorf("Expected account capabilities to contain %q", jmap.MdnCapabilityURI)
	}
}

// TestRFC9007_MDNSend tests MDN/send method call per RFC 9007 Section 3.1.
func TestRFC9007_MDNSend(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Query an existing email to reference in the MDN
	qBody, _ := json.Marshal(map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{"accountId": "primary"}, "q1"},
		},
	})
	qResp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(qBody))
	if err != nil {
		t.Fatalf("POST /jmap Email/query failed: %v", err)
	}
	var qJmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(qResp.Body).Decode(&qJmapResp)
	qResp.Body.Close()

	qArgs := qJmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	ids := qArgs["ids"].([]any)
	if len(ids) == 0 {
		t.Fatalf("Expected at least 1 seeded email for MDN test")
	}
	targetEmailID := ids[0].(string)

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-1",
				"send": map[string]any{
					"mdn1": map[string]any{
						"forEmailId": targetEmailID,
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "MDN-sent-manually",
							"type":        "displayed",
						},
						"textBody": "I have read your email.",
					},
					"mdn2": map[string]any{
						"forEmailId": "invalid-email-id",
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "MDN-sent-manually",
							"type":        "displayed",
						},
					},
				},
			}, "call-1"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap MDN/send failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode JMAP response: %v", err)
	}

	if len(jmapResp.MethodResponses) == 0 {
		t.Fatalf("Empty method responses")
	}

	methodCall := jmapResp.MethodResponses[0].([]any)
	if methodCall[0] != "MDN/send" {
		t.Fatalf("Expected MDN/send method response, got %v", methodCall[0])
	}

	args := methodCall[1].(map[string]any)
	sentMap, _ := args["sent"].(map[string]any)
	notSentMap, _ := args["notSent"].(map[string]any)

	if _, ok := sentMap["mdn1"]; !ok {
		t.Errorf("Expected mdn1 to be in sent map, got %+v", sentMap)
	}

	if _, ok := notSentMap["mdn2"]; !ok {
		t.Errorf("Expected mdn2 to be in notSent map due to invalid email ID, got %+v", notSentMap)
	}
}

// TestRFC9007_MDNParse tests MDN/parse method call per RFC 9007 Section 3.2.
func TestRFC9007_MDNParse(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Query an existing email to retrieve its real blobId
	qBody, _ := json.Marshal(map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{"accountId": "primary"}, "q1"},
		},
	})
	qResp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(qBody))
	if err != nil {
		t.Fatalf("POST /jmap Email/query failed: %v", err)
	}
	var qJmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(qResp.Body).Decode(&qJmapResp)
	qResp.Body.Close()

	qArgs := qJmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	ids := qArgs["ids"].([]any)
	if len(ids) == 0 {
		t.Fatalf("Expected at least 1 seeded email for MDN test")
	}

	getBody, _ := json.Marshal(map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/get", map[string]any{"accountId": "primary", "ids": []any{ids[0]}}, "g1"},
		},
	})
	gResp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(getBody))
	if err != nil {
		t.Fatalf("POST /jmap Email/get failed: %v", err)
	}
	var gJmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(gResp.Body).Decode(&gJmapResp)
	gResp.Body.Close()

	gArgs := gJmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	list := gArgs["list"].([]any)
	firstEmail := list[0].(map[string]any)

	// Store a real RFC 8098 MDN blob
	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)
	rawMDN := []byte("From: recipient@example.com\r\n" +
		"To: sender@example.com\r\n" +
		"Subject: Read receipt for: World domination\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/report; report-type=disposition-notification; boundary=\"boundary42\"\r\n\r\n" +
		"--boundary42\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"This receipt shows that the email has been displayed.\r\n\r\n" +
		"--boundary42\r\n" +
		"Content-Type: message/disposition-notification\r\n\r\n" +
		"Reporting-UA: joes-pc.cs.example.com; Foomail 97.1\r\n" +
		"Final-Recipient: rfc822; recipient@example.com\r\n" +
		"Original-Message-ID: <199509192301.23456@example.org>\r\n" +
		"Disposition: manual-action/MDN-sent-manually; displayed\r\n\r\n" +
		"--boundary42--\r\n")

	mdnBlob, err := srv.BlobBackend.PutBlob(accCtx, accID, "multipart/report", rawMDN)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}
	targetBlobID := string(mdnBlob.ID)
	nonMDNBlobID := firstEmail["blobId"].(string)

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/parse", map[string]any{
				"accountId": "primary",
				"blobIds":   []any{targetBlobID, nonMDNBlobID, "missing-blob-xyz"},
			}, "call-1"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST /jmap MDN/parse failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode JMAP response: %v", err)
	}

	if len(jmapResp.MethodResponses) == 0 {
		t.Fatalf("Empty method responses")
	}

	methodCall := jmapResp.MethodResponses[0].([]any)
	if methodCall[0] != "MDN/parse" {
		t.Fatalf("Expected MDN/parse method response, got %v", methodCall[0])
	}

	args := methodCall[1].(map[string]any)
	parsedMap, _ := args["parsed"].(map[string]any)

	if _, ok := parsedMap[targetBlobID]; !ok {
		t.Errorf("Expected %s to be in parsed map, got %+v", targetBlobID, parsedMap)
	} else {
		parsedObj := parsedMap[targetBlobID].(map[string]any)
		if parsedObj["subject"] != "Read receipt for: World domination" {
			t.Errorf("Expected subject 'Read receipt for: World domination', got %v", parsedObj["subject"])
		}
		disp := parsedObj["disposition"].(map[string]any)
		if disp["type"] != "displayed" || disp["actionMode"] != "manual-action" || disp["sendingMode"] != "mdn-sent-manually" {
			t.Errorf("Unexpected disposition: %+v", disp)
		}
	}

	// Non-MDN blob must be reported in notParsable (RFC 9007 §2.2)
	notParsableRaw, _ := args["notParsable"].([]any)
	if len(notParsableRaw) != 1 || notParsableRaw[0] != nonMDNBlobID {
		t.Errorf("Expected %s in notParsable, got %+v", nonMDNBlobID, notParsableRaw)
	}

	// A blob id that does not exist must be reported in notFound,
	// not fabricated (RFC 9007 Section 3.3 example).
	notFoundRaw, _ := args["notFound"].([]any)
	if len(notFoundRaw) != 1 || notFoundRaw[0] != "missing-blob-xyz" {
		t.Errorf("Expected missing-blob-xyz in notFound, got %+v", notFoundRaw)
	}
}
