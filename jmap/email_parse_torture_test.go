package jmap_test

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

// TestEmailParse_MIMETorture exercises the full MIME torture test suite against JMAP Email/parse
// and ParseRFC822. It verifies that malformed, deeply nested, adversarial, and edge-case MIME
// structures degrade gracefully, adhere to resource limits, never panic, and satisfy RFC 8621.
func TestEmailParse_MIMETorture(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST,
		"Email/parse parses blobs into Email objects; error paths are handled.")
	spectest.Require(t, "RFC2045", "5", spectest.MUST,
		"MIME multipart messages are parsed into a nested body-part structure.")
	spectest.Require(t, "RFC5322", "3.6", spectest.MUST,
		"RFC 5322 messages are parsed into JMAP Email header/address/subject/date fields.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.BlobCapabilityURI}
	testDir := filepath.Join("testdata", "mime_torture")

	entries, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatalf("Failed to read testdata/mime_torture directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".eml") {
			continue
		}

		vectorName := entry.Name()
		t.Run(vectorName, func(t *testing.T) {
			rawPath := filepath.Join(testDir, vectorName)
			rawBytes, err := os.ReadFile(rawPath)
			if err != nil {
				t.Fatalf("Failed to read vector file %s: %v", vectorName, err)
			}

			// 1. Direct unit-level parse via ParseRFC822 - MUST NOT PANIC
			em, parseErr := jmap.ParseRFC822(rawBytes)
			if parseErr == nil && em != nil {
				if em.Size != uint64(len(rawBytes)) {
					t.Errorf("[%s] Expected email size %d, got %d", vectorName, len(rawBytes), em.Size)
				}
				// Verify BodyValues map does not crash on iteration
				for pid, bv := range em.BodyValues {
					if pid == "" {
						t.Errorf("[%s] Empty part ID in BodyValues", vectorName)
					}
					_ = bv.Value
				}
			}

			// 2. Over-the-wire JMAP Email/parse via HTTP endpoint - MUST NOT PANIC
			blob, err := srv.BlobBackend.PutBlob(context.Background(), jmap.AccountIDForSubject(testUsername), "message/rfc822", rawBytes)
			if err != nil {
				t.Fatalf("[%s] Failed to upload test blob: %v", vectorName, err)
			}

			calls := []any{
				[]any{"Email/parse", map[string]any{
					"accountId":            "primary",
					"blobIds":              []any{string(blob.ID)},
					"fetchTextBodyValues":  true,
					"fetchHTMLBodyValues":  true,
					"fetchAllBodyValues":   true,
				}, "c1"},
			}
			res := postJMAP(t, ts.URL, using, calls)
			if len(res.MethodResponses) == 0 {
				t.Fatalf("[%s] Empty response from Email/parse", vectorName)
			}
			if res.MethodResponses[0].Name != "Email/parse" {
				t.Fatalf("[%s] Expected Email/parse response, got %s", vectorName, res.MethodResponses[0].Name)
			}

			respArgs := res.MethodResponses[0].Args
			parsedMap, _ := respArgs["parsed"].(map[string]any)
			notParsableList, _ := respArgs["notParsable"].([]any)

			pObj, hasParsed := parsedMap[string(blob.ID)].(map[string]any)
			isNotParsable := false
			for _, np := range notParsableList {
				if s, ok := np.(string); ok && s == string(blob.ID) {
					isNotParsable = true
					break
				}
			}

			if !hasParsed && !isNotParsable {
				t.Fatalf("[%s] Blob %s neither parsed nor listed in notParsable: %v", vectorName, blob.ID, respArgs)
			}

			// 3. Vector-specific behavioral verifications
			switch vectorName {
			case "crispin_torture.eml":
				if !hasParsed {
					t.Fatalf("[crispin_torture.eml] Expected successful parse, got notParsable")
				}
				subj, _ := pObj["subject"].(string)
				if !strings.Contains(subj, "Multi-media mail demonstration") {
					t.Errorf("[crispin_torture.eml] Expected subject to contain 'Multi-media mail demonstration', got %q", subj)
				}
				fromList, _ := pObj["from"].([]any)
				if len(fromList) == 0 {
					t.Errorf("[crispin_torture.eml] Expected From address list to be populated")
				} else if fromMap, ok := fromList[0].(map[string]any); ok {
					if fromMap["email"] != "mrc@CAC.Washington.EDU" {
						t.Errorf("[crispin_torture.eml] Expected From email mrc@CAC.Washington.EDU, got %v", fromMap["email"])
					}
				}

			case "rf_mime_torture.eml":
				if !hasParsed {
					t.Fatalf("[rf_mime_torture.eml] Expected successful parse, got notParsable")
				}
				subj, _ := pObj["subject"].(string)
				if !strings.Contains(subj, "Ryan Finnie's MIME Torture Test") {
					t.Errorf("[rf_mime_torture.eml] Expected subject to contain 'Ryan Finnie's MIME Torture Test', got %q", subj)
				}

			case "deep_nested_multiparts.eml":
				if !hasParsed {
					t.Fatalf("[deep_nested_multiparts.eml] Expected successful bounded parse, got notParsable")
				}
				subj, _ := pObj["subject"].(string)
				if !strings.Contains(subj, "Deeply Nested Multiparts") {
					t.Errorf("[deep_nested_multiparts.eml] Expected subject, got %q", subj)
				}

			case "mixed_cte.eml":
				if !hasParsed {
					t.Fatalf("[mixed_cte.eml] Expected successful parse, got notParsable")
				}
				// Verify textBody parts exist
				tb, _ := pObj["textBody"].([]any)
				if len(tb) == 0 {
					t.Errorf("[mixed_cte.eml] Expected textBody parts from mixed CTE parts")
				}

			case "header_folding_stress.eml":
				if !hasParsed {
					t.Fatalf("[header_folding_stress.eml] Expected successful parse, got notParsable")
				}

			case "header_injection.eml":
				if !hasParsed {
					t.Fatalf("[header_injection.eml] Expected successful parse, got notParsable")
				}
				// Verify Bcc was not injected into top-level bcc field if not a valid header
				bccList, _ := pObj["bcc"].([]any)
				for _, b := range bccList {
					if bm, ok := b.(map[string]any); ok && bm["email"] == "injected-victim@example.com" {
						t.Errorf("[header_injection.eml] Injected Bcc header leaked into recipient list")
					}
				}

			case "circular_recursive_rfc822.eml":
				if !hasParsed {
					t.Fatalf("[circular_recursive_rfc822.eml] Expected successful parse, got notParsable")
				}

			case "obsolete_syntax.eml":
				if !hasParsed {
					t.Fatalf("[obsolete_syntax.eml] Expected successful parse, got notParsable")
				}
				fromList, _ := pObj["from"].([]any)
				if len(fromList) == 0 {
					t.Errorf("[obsolete_syntax.eml] Expected From address to be parsed")
				}

			case "adversarial_payloads.eml":
				if !hasParsed {
					t.Fatalf("[adversarial_payloads.eml] Expected successful parse despite null bytes & long lines")
				}
			}
		})
	}
}

// TestEmailParse_AdversarialEdgeCases tests edge cases like zero-byte and whitespace-only payloads.
func TestEmailParse_AdversarialEdgeCases(t *testing.T) {
	spectest.Require(t, "RFC8621", "4.8", spectest.MUST,
		"Email/parse parses blobs into Email objects; error paths are handled.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.BlobCapabilityURI}

	testCases := []struct {
		name     string
		data     []byte
		mustFail bool
	}{
		{"EmptyBytes", []byte{}, true},
		{"WhitespaceOnly", []byte("   \r\n\t\r\n   \r\n"), true},
		{"NoHeaderSeparator", []byte("From: alice@example.com\r\nSubject: No Blank Line"), false},
		{"SingleChar", []byte("A"), true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := srv.BlobBackend.PutBlob(context.Background(), jmap.AccountIDForSubject(testUsername), "message/rfc822", tc.data)
			if err != nil {
				t.Fatalf("Failed to put blob: %v", err)
			}

			calls := []any{
				[]any{"Email/parse", map[string]any{
					"accountId": "primary",
					"blobIds":   []any{string(blob.ID)},
				}, "c1"},
			}
			res := postJMAP(t, ts.URL, using, calls)
			if len(res.MethodResponses) == 0 {
				t.Fatalf("Empty response")
			}
			respArgs := res.MethodResponses[0].Args
			parsedMap, _ := respArgs["parsed"].(map[string]any)
			notParsableList, _ := respArgs["notParsable"].([]any)

			_, isParsed := parsedMap[string(blob.ID)]
			isNotParsable := false
			for _, np := range notParsableList {
				if s, ok := np.(string); ok && s == string(blob.ID) {
					isNotParsable = true
					break
				}
			}

			if tc.mustFail && !isNotParsable {
				t.Errorf("[%s] Expected blob to be reported in notParsable, got parsed: %v", tc.name, isParsed)
			}
			if !tc.mustFail && !isParsed && !isNotParsable {
				t.Errorf("[%s] Blob neither parsed nor in notParsable", tc.name)
			}
		})
	}
}
