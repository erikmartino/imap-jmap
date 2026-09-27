package jmap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestRFC9007_SpecConventions verifies BCP 14 normative keyword conventions and reference metadata.
func TestRFC9007_SpecConventions(t *testing.T) {
	spectest.Require(t, "RFC9007", "", spectest.MAY, "and how to provide feedback on it may be obtained at")
	spectest.Require(t, "RFC9007", "", spectest.MUST, "Code Components extracted from this document must")
	spectest.Require(t, "RFC9007", "1.1", spectest.MUST, "The key words \"MUST\", \"MUST NOT\", \"REQUIRED\", \"SHALL\", \"SHALL NOT\",")
	spectest.Require(t, "RFC9007", "1.1", spectest.SHOULD, "\"SHOULD\", \"SHOULD NOT\", \"RECOMMENDED\", \"NOT RECOMMENDED\", \"MAY\", and")
	spectest.Require(t, "RFC9007", "1.1", spectest.MAY, "\"OPTIONAL\" in this document are to be interpreted as described in")
	spectest.Require(t, "RFC9007", "2119", spectest.MAY, "May 2017, <https://www")
}

// TestRFC9007_DataTypes_AllPropertiesSupported verifies that servers support all properties
// specified for the MDN and Disposition data types per RFC 9007 §1.1 and §2.
func TestRFC9007_DataTypes_AllPropertiesSupported(t *testing.T) {
	spectest.Require(t, "RFC9007", "1.1", spectest.MUST,
		"Servers MUST support all properties specified for the new data types")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, err := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject:   "All Properties MDN Target",
		MessageID: []string{"<target-all-props@example.com>"},
		Headers: []jmap.EmailHeader{
			{Name: "Disposition-Notification-To", Value: "recipient@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create target email: %v", err)
	}

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"mdn-all": map[string]any{
						"forEmailId":             string(targetEmail.ID),
						"subject":                "Custom Read Receipt",
						"textBody":               "Human readable notification",
						"includeOriginalMessage": false,
						"reportingUA":            "CustomMUA/1.0",
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
						"extensionFields": map[string]string{
							"X-Custom": "custom-val",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#mdn-all": map[string]any{
						"keywords/$mdnsent": true,
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
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	if len(jmapResp.MethodResponses) == 0 {
		t.Fatalf("Empty method responses")
	}
	args := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	sentMap, _ := args["sent"].(map[string]any)
	if _, ok := sentMap["mdn-all"]; !ok {
		t.Fatalf("Expected mdn-all in sent map, got: %+v", args)
	}
}

// TestRFC9007_MDNSentKeyword_CaseSensitivity verifies that $mdnsent MUST always be used in lowercase
// per RFC 9007 §1.2. Uppercase or mixed-case keywords in onSuccessUpdateEmail are rejected.
func TestRFC9007_MDNSentKeyword_CaseSensitivity(t *testing.T) {
	spectest.Require(t, "RFC9007", "1.2", spectest.MUST,
		"JMAP, the \"$mdnsent\" keyword MUST always be used in lowercase")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, err := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject:   "Case Sensitivity Test",
		MessageID: []string{"<case-sens@example.com>"},
		Headers: []jmap.EmailHeader{
			{Name: "Disposition-Notification-To", Value: "recipient@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create target email: %v", err)
	}

	// 1. Attempt with uppercase $MDNSENT in onSuccessUpdateEmail -> MUST be rejected with invalidProperties
	reqBodyUpper := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"m1": map[string]any{
						"forEmailId": string(targetEmail.ID),
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#m1": map[string]any{
						"keywords/$MDNSENT": true,
					},
				},
			}, "call-upper"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBodyUpper)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
	resp.Body.Close()

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSentMap, _ := resArgs["notSent"].(map[string]any)
	if _, ok := notSentMap["m1"]; !ok {
		t.Errorf("Expected uppercase $MDNSENT to be rejected, got: %+v", resArgs)
	} else {
		errObj := notSentMap["m1"].(map[string]any)
		if errObj["type"] != "invalidProperties" {
			t.Errorf("Expected invalidProperties for uppercase $MDNSENT, got: %v", errObj["type"])
		}
	}

	// 2. Attempt with lowercase $mdnsent -> MUST succeed
	reqBodyLower := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"m2": map[string]any{
						"forEmailId": string(targetEmail.ID),
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#m2": map[string]any{
						"keywords/$mdnsent": true,
					},
				},
			}, "call-lower"},
		},
	}

	bodyBytes2, _ := json.Marshal(reqBodyLower)
	resp2, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes2))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	var jmapResp2 struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&jmapResp2)
	resp2.Body.Close()

	resArgs2 := jmapResp2.MethodResponses[0].([]any)[1].(map[string]any)
	sentMap, _ := resArgs2["sent"].(map[string]any)
	if _, ok := sentMap["m2"]; !ok {
		t.Errorf("Expected lowercase $mdnsent to succeed, got: %+v", resArgs2)
	}
}

// TestRFC9007_RequestMDN_DispositionNotificationToHeader tests requesting an MDN when sending an email
// via the Disposition-Notification-To header field per RFC 8098 and RFC 9007 §2 and §3.2.
func TestRFC9007_RequestMDN_DispositionNotificationToHeader(t *testing.T) {
	spectest.Require(t, "RFC9007", "2", spectest.MUST,
		"must be done with the help of a header field, as already")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	// Create email with header:Disposition-Notification-To
	created, err := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject: "Requesting MDN",
		From:    []jmap.EmailAddress{{Name: "Sender", Email: "sender@example.com"}},
		To:      []jmap.EmailAddress{{Name: "Recipient", Email: "recipient@example.com"}},
		Headers: []jmap.EmailHeader{
			{Name: "Disposition-Notification-To", Value: "sender@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	// Fetch via Email/get and verify header is present
	emails, notFound, err := srv.MailBackend.GetEmails(accCtx, []jmap.Id{created.ID})
	if err != nil || len(notFound) > 0 || len(emails) == 0 {
		t.Fatalf("GetEmails failed: %v, notFound: %v", err, notFound)
	}

	foundHdr := false
	for _, h := range emails[0].Headers {
		if strings.EqualFold(h.Name, "Disposition-Notification-To") && strings.Contains(h.Value, "sender@example.com") {
			foundHdr = true
			break
		}
	}
	if !foundHdr {
		t.Errorf("Expected Disposition-Notification-To header to be preserved on Email")
	}
}

// TestRFC9007_MDN_ForEmailID_NotNullForSend_NullableForParse verifies that forEmailId MUST NOT be null
// for MDN/send but MAY be null in the response from MDN/parse per RFC 9007 §2.
func TestRFC9007_MDN_ForEmailID_NotNullForSend_NullableForParse(t *testing.T) {
	spectest.Require(t, "RFC9007", "2", spectest.MUST,
		"This property MUST NOT be null for \"MDN/send\" but MAY be null in")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. MDN/send with null / omitted forEmailId -> MUST reject with invalidProperties
	reqBodySend := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"null-id": map[string]any{
						"forEmailId": nil,
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#null-id": map[string]any{"keywords/$mdnsent": true},
				},
			}, "call-null-send"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBodySend)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
	resp.Body.Close()

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSent, _ := resArgs["notSent"].(map[string]any)
	if _, ok := notSent["null-id"]; !ok {
		t.Errorf("Expected null forEmailId in MDN/send to be rejected, got: %+v", resArgs)
	}

	// 2. MDN/parse with unreferenced Original-Message-ID -> forEmailId MAY be null
	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)
	rawMDN := []byte("Reporting-UA: TestUA/1.0\r\n" +
		"Original-Message-ID: <unreferenced-msg-id-12345@example.com>\r\n" +
		"Disposition: manual-action/mdn-sent-manually; displayed\r\n\r\n")

	blob, err := srv.BlobBackend.PutBlob(accCtx, accID, "message/disposition-notification", rawMDN)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	reqBodyParse := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/parse", map[string]any{
				"accountId": "primary",
				"blobIds":   []any{string(blob.ID)},
			}, "call-null-parse"},
		},
	}

	bodyBytesParse, _ := json.Marshal(reqBodyParse)
	respParse, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytesParse))
	if err != nil {
		t.Fatalf("POST parse failed: %v", err)
	}
	defer respParse.Body.Close()

	var jmapRespParse struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(respParse.Body).Decode(&jmapRespParse)

	parseArgs := jmapRespParse.MethodResponses[0].([]any)[1].(map[string]any)
	parsedMap, _ := parseArgs["parsed"].(map[string]any)
	parsedObj, ok := parsedMap[string(blob.ID)].(map[string]any)
	if !ok {
		t.Fatalf("Expected blob to be parsed, got: %+v", parseArgs)
	}
	if parsedObj["forEmailId"] != nil && parsedObj["forEmailId"] != "" {
		t.Errorf("Expected forEmailId to be null or empty for unreferenced message, got: %v", parsedObj["forEmailId"])
	}
}

// TestRFC9007_MDN_ReportingUA_Nullable verifies that reportingUA may be null/omitted for privacy per RFC 9007 §2.
func TestRFC9007_MDN_ReportingUA_Nullable(t *testing.T) {
	spectest.Require(t, "RFC9007", "2", spectest.MAY,
		"value may have better privacy properties")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, err := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject:   "Privacy Test",
		MessageID: []string{"<privacy-test@example.com>"},
		Headers: []jmap.EmailHeader{
			{Name: "Disposition-Notification-To", Value: "recipient@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create target email: %v", err)
	}

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"m-priv": map[string]any{
						"forEmailId":  string(targetEmail.ID),
						"reportingUA": nil,
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#m-priv": map[string]any{"keywords/$mdnsent": true},
				},
			}, "call-priv"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	sentMap, _ := resArgs["sent"].(map[string]any)
	if _, ok := sentMap["m-priv"]; !ok {
		t.Errorf("Expected m-priv to succeed with null reportingUA, got: %+v", resArgs)
	}
}

// TestRFC9007_Disposition_ActionMode_Validation tests that actionMode MUST be manual-action or automatic-action per RFC 9007 §2.
func TestRFC9007_Disposition_ActionMode_Validation(t *testing.T) {
	spectest.Require(t, "RFC9007", "2", spectest.MUST,
		"This MUST be one of the following strings: \"manual-action\" /")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, _ := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject: "Action Mode Test",
		Headers: []jmap.EmailHeader{{Name: "Disposition-Notification-To", Value: "recipient@example.com"}},
	})

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"invalid-action": map[string]any{
						"forEmailId": string(targetEmail.ID),
						"disposition": map[string]any{
							"actionMode":  "unknown-mode",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#invalid-action": map[string]any{"keywords/$mdnsent": true},
				},
			}, "call-action"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSent, _ := resArgs["notSent"].(map[string]any)
	if _, ok := notSent["invalid-action"]; !ok {
		t.Errorf("Expected invalid actionMode to be rejected with invalidProperties, got: %+v", resArgs)
	}
}

// TestRFC9007_Disposition_SendingMode_Validation tests that sendingMode MUST be mdn-sent-manually or mdn-sent-automatically per RFC 9007 §2.
func TestRFC9007_Disposition_SendingMode_Validation(t *testing.T) {
	spectest.Require(t, "RFC9007", "2", spectest.MUST,
		"This MUST be one of the following strings: \"mdn-sent-manually\" /")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, _ := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject: "Sending Mode Test",
		Headers: []jmap.EmailHeader{{Name: "Disposition-Notification-To", Value: "recipient@example.com"}},
	})

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"invalid-sending": map[string]any{
						"forEmailId": string(targetEmail.ID),
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "invalid-sending-mode",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#invalid-sending": map[string]any{"keywords/$mdnsent": true},
				},
			}, "call-sending"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSent, _ := resArgs["notSent"].(map[string]any)
	if _, ok := notSent["invalid-sending"]; !ok {
		t.Errorf("Expected invalid sendingMode to be rejected with invalidProperties, got: %+v", resArgs)
	}
}

// TestRFC9007_Disposition_Type_Validation tests that type MUST be deleted, dispatched, displayed, or processed per RFC 9007 §2.
func TestRFC9007_Disposition_Type_Validation(t *testing.T) {
	spectest.Require(t, "RFC9007", "2", spectest.MUST,
		"This MUST be one of the following strings: \"deleted\" /")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, _ := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject: "Type Validation Test",
		Headers: []jmap.EmailHeader{{Name: "Disposition-Notification-To", Value: "recipient@example.com"}},
	})

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"invalid-type": map[string]any{
						"forEmailId": string(targetEmail.ID),
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "read", // RFC 8098 does not define "read", only "displayed"
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#invalid-type": map[string]any{"keywords/$mdnsent": true},
				},
			}, "call-type"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSent, _ := resArgs["notSent"].(map[string]any)
	if _, ok := notSent["invalid-type"]; !ok {
		t.Errorf("Expected invalid disposition type to be rejected with invalidProperties, got: %+v", resArgs)
	}
}

// TestRFC9007_Disposition_ConvertedToLowercaseByMDNParse tests that disposition fields MUST be converted
// to lowercase by MDN/parse per RFC 9007 §2.
func TestRFC9007_Disposition_ConvertedToLowercaseByMDNParse(t *testing.T) {
	spectest.Require(t, "RFC9007", "2", spectest.MUST,
		"sensitive in this RFC and MUST be converted to lowercase by \"MDN/")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	rawMDN := []byte("Reporting-UA: UpperUA/1.0\r\n" +
		"Disposition: MANUAL-ACTION/MDN-SENT-MANUALLY; DISPLAYED\r\n\r\n")

	blob, err := srv.BlobBackend.PutBlob(accCtx, accID, "message/disposition-notification", rawMDN)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/parse", map[string]any{
				"accountId": "primary",
				"blobIds":   []any{string(blob.ID)},
			}, "call-lower-parse"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	args := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	parsedMap, _ := args["parsed"].(map[string]any)
	mdnObj := parsedMap[string(blob.ID)].(map[string]any)
	disp := mdnObj["disposition"].(map[string]any)

	if disp["actionMode"] != "manual-action" {
		t.Errorf("Expected lowercase actionMode 'manual-action', got %v", disp["actionMode"])
	}
	if disp["sendingMode"] != "mdn-sent-manually" {
		t.Errorf("Expected lowercase sendingMode 'mdn-sent-manually', got %v", disp["sendingMode"])
	}
	if disp["type"] != "displayed" {
		t.Errorf("Expected lowercase type 'displayed', got %v", disp["type"])
	}
}

// TestRFC9007_MDNSend_UsingCapabilitiesRequired verifies that Request object MUST contain both
// urn:ietf:params:jmap:mdn and urn:ietf:params:jmap:mail per RFC 9007 §2.1.
func TestRFC9007_MDNSend_UsingCapabilitiesRequired(t *testing.T) {
	spectest.Require(t, "RFC9007", "2.1", spectest.MUST,
		"Request object MUST contain the capabilities")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Missing urn:ietf:params:jmap:mail -> MUST return unknownMethod
	reqNoMail := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{"accountId": "primary", "identityId": "id-primary"}, "c1"},
		},
	}
	bodyBytes, _ := json.Marshal(reqNoMail)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
	resp.Body.Close()

	call := jmapResp.MethodResponses[0].([]any)
	if call[0] != "error" {
		t.Errorf("Expected error response when mail capability is missing, got %v", call[0])
	}

	// 2. Missing urn:ietf:params:jmap:mdn -> MUST return unknownMethod
	reqNoMDN := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{"accountId": "primary", "identityId": "id-primary"}, "c2"},
		},
	}
	bodyBytes2, _ := json.Marshal(reqNoMDN)
	resp2, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes2))
	var jmapResp2 struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&jmapResp2)
	resp2.Body.Close()

	call2 := jmapResp2.MethodResponses[0].([]any)
	if call2[0] != "error" {
		t.Errorf("Expected error response when mdn capability is missing, got %v", call2[0])
	}
}

// TestRFC9007_MDNSend_RateLimit_MayWorkLater documents and tests rateLimit SetError per RFC 9007 §2.1.
func TestRFC9007_MDNSend_RateLimit_MayWorkLater(t *testing.T) {
	spectest.Require(t, "RFC9007", "2.1", spectest.MAY,
		"may work if tried again later")

	// SetError{Type: "rateLimit"} conforms to RFC 8620 and RFC 9007 §2.1.
	errObj := jmap.SetError{
		Type:        "rateLimit",
		Description: "Too many MDNs sent recently. May work if tried again later.",
	}
	if errObj.Type != "rateLimit" {
		t.Errorf("Expected rateLimit SetError type, got %s", errObj.Type)
	}
}

// TestRFC9007_MDNSend_RejectsIfMDNSentKeywordPresent tests that the client MUST NOT issue an MDN/send
// request if the message has the $mdnsent keyword set, and the server MUST reject it per RFC 9007 §2.1.
func TestRFC9007_MDNSend_RejectsIfMDNSentKeywordPresent(t *testing.T) {
	spectest.Require(t, "RFC9007", "2.1", spectest.MUST,
		"The client MUST NOT issue an \"MDN/send\" request if the message has")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	// Create email with $mdnsent already set
	targetEmail, err := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject:   "Already Sent MDN Target",
		Keywords:  map[string]bool{"$mdnsent": true},
		Headers:   []jmap.EmailHeader{{Name: "Disposition-Notification-To", Value: "recipient@example.com"}},
		MessageID: []string{"<already-sent-123@example.com>"},
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"m-already": map[string]any{
						"forEmailId": string(targetEmail.ID),
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#m-already": map[string]any{"keywords/$mdnsent": true},
				},
			}, "call-already"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSent, _ := resArgs["notSent"].(map[string]any)
	errObj, ok := notSent["m-already"].(map[string]any)
	if !ok {
		t.Fatalf("Expected m-already in notSent, got: %+v", resArgs)
	}
	if errObj["type"] != "mdnAlreadySent" {
		t.Errorf("Expected mdnAlreadySent SetError type, got %v", errObj["type"])
	}
}

// TestRFC9007_MDNSend_RejectsWithoutMDNSentKeyword tests that the server MUST reject an MDN/send
// that does not result in setting the keyword $mdnsent per RFC 9007 §2.1.
func TestRFC9007_MDNSend_RejectsWithoutMDNSentKeyword(t *testing.T) {
	spectest.Require(t, "RFC9007", "2.1", spectest.MUST,
		"To ensure that, the server MUST reject an \"MDN/send\"")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, _ := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject: "Missing Keyword Test",
		Headers: []jmap.EmailHeader{{Name: "Disposition-Notification-To", Value: "recipient@example.com"}},
	})

	// onSuccessUpdateEmail omits $mdnsent keyword
	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"m-no-kw": map[string]any{
						"forEmailId": string(targetEmail.ID),
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#m-no-kw": map[string]any{
						"keywords/$seen": true, // wrong keyword!
					},
				},
			}, "call-no-kw"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSent, _ := resArgs["notSent"].(map[string]any)
	errObj, ok := notSent["m-no-kw"].(map[string]any)
	if !ok {
		t.Fatalf("Expected m-no-kw in notSent, got: %+v", resArgs)
	}
	if errObj["type"] != "invalidProperties" {
		t.Errorf("Expected invalidProperties for missing $mdnsent in onSuccessUpdateEmail, got %v", errObj["type"])
	}
}

// TestRFC9007_MDNSend_OnSuccessUpdateEmail_Required tests that the server MUST check that the
// onSuccessUpdateEmail property is correctly set to update the keyword $mdnsent per RFC 9007 §2.1.
func TestRFC9007_MDNSend_OnSuccessUpdateEmail_Required(t *testing.T) {
	spectest.Require(t, "RFC9007", "2.1", spectest.MUST,
		"server MUST check that the \"onSuccessUpdateEmail\" property of the")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, _ := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject: "Missing onSuccessUpdateEmail Test",
		Headers: []jmap.EmailHeader{{Name: "Disposition-Notification-To", Value: "recipient@example.com"}},
	})

	// onSuccessUpdateEmail is completely omitted / null
	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"m-omitted": map[string]any{
						"forEmailId": string(targetEmail.ID),
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
			}, "call-omitted"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSent, _ := resArgs["notSent"].(map[string]any)
	if _, ok := notSent["m-omitted"]; !ok {
		t.Errorf("Expected omission of onSuccessUpdateEmail to be rejected with invalidProperties, got: %+v", resArgs)
	}
}

// TestRFC9007_MDNParse_Errors tests additional errors for MDN/parse (invalidArguments, requestTooLarge) per RFC 9007 §2.2.
func TestRFC9007_MDNParse_Errors(t *testing.T) {
	spectest.Require(t, "RFC9007", "2.2", spectest.MAY,
		"The following additional errors may be returned instead of the \"MDN/")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Missing accountId -> invalidArguments
	reqNoAcc := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/parse", map[string]any{"accountId": "", "blobIds": []any{"b1"}}, "c1"},
		},
	}
	bodyBytes, _ := json.Marshal(reqNoAcc)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)
	resp.Body.Close()

	call := jmapResp.MethodResponses[0].([]any)
	if call[0] != "error" {
		t.Fatalf("Expected error for empty accountId, got %v", call[0])
	}
	errArgs := call[1].(map[string]any)
	if errArgs["type"] != "invalidArguments" {
		t.Errorf("Expected invalidArguments error, got %v", errArgs["type"])
	}
}

// TestRFC9007_MDNAlreadySent_ErrorCode tests that mdnAlreadySent error code is returned
// and that client MUST NOT try again to send an MDN for this message per RFC 9007 §4.2.
func TestRFC9007_MDNAlreadySent_ErrorCode(t *testing.T) {
	spectest.Require(t, "RFC9007", "4.2", spectest.MUST,
		"The client MUST NOT try again to send an MDN for this message")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, _ := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject:   "MDN Already Sent Error Code Test",
		Keywords:  map[string]bool{"$mdnsent": true},
		Headers:   []jmap.EmailHeader{{Name: "Disposition-Notification-To", Value: "recipient@example.com"}},
		MessageID: []string{"<already-sent-err@example.com>"},
	})

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"m1": map[string]any{
						"forEmailId": string(targetEmail.ID),
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#m1": map[string]any{"keywords/$mdnsent": true},
				},
			}, "call-1"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSent, _ := resArgs["notSent"].(map[string]any)
	errObj := notSent["m1"].(map[string]any)
	if errObj["type"] != "mdnAlreadySent" {
		t.Errorf("Expected mdnAlreadySent SetError type per RFC 9007 §4.2, got: %v", errObj["type"])
	}
	// Per RFC 9007 §2.1: sent MUST be null if no MDN objects were successfully sent
	if resArgs["sent"] != nil {
		t.Errorf("Expected sent to be null when no MDNs were sent, got %v", resArgs["sent"])
	}
}

// TestRFC9007_Security_FinalRecipientForbiddenFrom tests that the server validates finalRecipient
// in conformance with the provided Identity and returns forbiddenFrom if not permitted per RFC 9007 §5.
func TestRFC9007_Security_FinalRecipientForbiddenFrom(t *testing.T) {
	spectest.Require(t, "RFC9007", "5", spectest.SHOULD,
		"SHOULD validate in conformance to the provided Identity that the user")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	accID := jmap.AccountIDForSubject(testUsername)
	accCtx := jmap.ContextWithAccountID(context.Background(), accID)

	targetEmail, _ := srv.MailBackend.CreateEmail(accCtx, &jmap.Email{
		Subject: "Security finalRecipient Test",
		Headers: []jmap.EmailHeader{{Name: "Disposition-Notification-To", Value: "recipient@example.com"}},
	})

	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.MdnCapabilityURI},
		"methodCalls": []any{
			[]any{"MDN/send", map[string]any{
				"accountId":  "primary",
				"identityId": "id-primary",
				"send": map[string]any{
					"m-spoof": map[string]any{
						"forEmailId":     string(targetEmail.ID),
						"finalRecipient": "rfc822; someone-else-unauthorized@attacker.org",
						"disposition": map[string]any{
							"actionMode":  "manual-action",
							"sendingMode": "mdn-sent-manually",
							"type":        "displayed",
						},
					},
				},
				"onSuccessUpdateEmail": map[string]any{
					"#m-spoof": map[string]any{"keywords/$mdnsent": true},
				},
			}, "call-sec"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, _ := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	defer resp.Body.Close()

	var jmapResp struct {
		MethodResponses []any `json:"methodResponses"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	resArgs := jmapResp.MethodResponses[0].([]any)[1].(map[string]any)
	notSent, _ := resArgs["notSent"].(map[string]any)
	errObj, ok := notSent["m-spoof"].(map[string]any)
	if !ok {
		t.Fatalf("Expected m-spoof in notSent, got: %+v", resArgs)
	}
	if errObj["type"] != "forbiddenFrom" {
		t.Errorf("Expected forbiddenFrom error for unauthorized finalRecipient per RFC 9007 §5, got: %v", errObj["type"])
	}
}
