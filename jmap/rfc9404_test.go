package jmap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC9404_Section2_Capability tests urn:ietf:params:jmap:blob capability discovery per RFC 9404 Section 2.
func TestRFC9404_Section2_Capability(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}

	capRaw, ok := session.Capabilities[jmap.BlobCapabilityURI]
	if !ok {
		t.Fatalf("Capability %q missing in Session", jmap.BlobCapabilityURI)
	}

	capBytes, _ := json.Marshal(capRaw)
	var blobCap jmap.BlobCapability
	_ = json.Unmarshal(capBytes, &blobCap)

	if len(blobCap.SupportedAlgorithms) == 0 || blobCap.SupportedAlgorithms[0] != "sha-256" {
		t.Errorf("Expected supportedAlgorithms ['sha-256'], got %v", blobCap.SupportedAlgorithms)
	}
}

// TestRFC9404_Section4_BlobGet tests Blob/get method per RFC 9404 Section 4.
func TestRFC9404_Section4_BlobGet(t *testing.T) {
	blobBackend := memory.NewMemoryBlobBackend()
	_, _ = blobBackend.PutBlob(context.TODO(), "primary", "text/plain", []byte("Hello RFC 9404"))

	srv := jmap.NewServer(nil, jmap.WithBlobBackend(blobBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Upload a blob first via HTTP upload
	uploadResp, err := http.Post(ts.URL+"/upload/primary/", "text/plain", bytes.NewReader([]byte("Test Data")))
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	defer uploadResp.Body.Close()

	var uploadedBlob jmap.Blob
	_ = json.NewDecoder(uploadResp.Body).Decode(&uploadedBlob)

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI},
		"methodCalls": []any{
			[]any{"Blob/get", map[string]any{
				"accountId": "primary",
				"ids":       []string{uploadedBlob.ID, "non-existent-blob"},
			}, "c1"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Blob/get" {
		t.Fatalf("Expected method response 'Blob/get', got %q", methodResp.Name)
	}

	listRaw, ok := methodResp.Args["list"].([]any)
	if !ok || len(listRaw) != 1 {
		t.Fatalf("Expected 1 blob in list, got %v", methodResp.Args["list"])
	}

	notFoundRaw, ok := methodResp.Args["notFound"].([]any)
	if !ok || len(notFoundRaw) != 1 {
		t.Errorf("Expected 1 notFound ID, got %v", methodResp.Args["notFound"])
	}
}

// TestRFC9404_Section5_BlobUpload tests Blob/upload method per RFC 9404 Section 5.
func TestRFC9404_Section5_BlobUpload(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI},
		"methodCalls": []any{
			[]any{"Blob/upload", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"b1": map[string]any{
						"data": "SGVsbG8gV29ybGQ=", // Base64 "Hello World"
						"type": "text/plain",
					},
				},
			}, "c1"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Blob/upload" {
		t.Fatalf("Expected method response 'Blob/upload', got %q", methodResp.Name)
	}

	created, ok := methodResp.Args["created"].(map[string]any)
	if !ok || created["b1"] == nil {
		t.Errorf("Expected created blob for 'b1', got %v", methodResp.Args["created"])
	}
}

// TestRFC9404_Section4_3_BlobLookup tests Blob/lookup reverse references and notFound
// reporting per RFC 9404 Section 4.3.
func TestRFC9404_Section4_3_BlobLookup(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(methodCalls []any) map[string]any {
		reqPayload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI, jmap.MailCapabilityURI},
			"methodCalls": methodCalls,
		}
		body, _ := json.Marshal(reqPayload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
			t.Fatalf("Failed to decode Response: %v", err)
		}
		return jr.MethodResponses[len(jr.MethodResponses)-1].Args
	}

	// Upload a blob, then import it as an email so the blob is referenced by Email + Thread.
	uploadArgs := post([]any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"b1": map[string]any{
					"data": "UmV2ZXJzZSBMb29rdXAgVGVzdA==", // Base64 "Reverse Lookup Test"
					"type": "text/plain",
				},
			},
		}, "c1"},
	})
	blobID := uploadArgs["created"].(map[string]any)["b1"].(map[string]any)["id"].(string)
	if blobID == "" {
		t.Fatalf("Expected uploaded blob id, got %v", uploadArgs["created"])
	}

	importArgs := post([]any{
		[]any{"Blob/upload", map[string]any{"accountId": "primary", "create": map[string]any{}}, "c0"},
		[]any{"Email/import", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"i1": map[string]any{
					"blobId":     blobID,
					"mailboxIds": map[string]any{"mb-inbox": true},
				},
			},
		}, "c2"},
	})
	createdImport := importArgs["created"].(map[string]any)
	emailObj, ok := createdImport["i1"].(map[string]any)
	if !ok {
		t.Fatalf("Expected imported email, got %v", createdImport)
	}
	emailID := emailObj["id"].(string)
	threadID := emailObj["threadId"].(string)

	// Lookup: the uploaded blob is referenced by the imported Email and its Thread;
	// a missing blob id is reported in notFound.
	lookupArgs := post([]any{
		[]any{"Blob/lookup", map[string]any{
			"accountId": "primary",
			"typeNames": []string{"Mailbox", "Thread", "Email"},
			"ids":       []string{blobID, "missing-blob"},
		}, "c3"},
	})

	notFoundRaw, _ := lookupArgs["notFound"].([]any)
	if len(notFoundRaw) != 1 || notFoundRaw[0] != "missing-blob" {
		t.Errorf("Expected notFound [missing-blob], got %v", lookupArgs["notFound"])
	}

	listRaw, _ := lookupArgs["list"].([]any)
	if len(listRaw) != 1 {
		t.Fatalf("Expected 1 BlobInfo entry, got %v", lookupArgs["list"])
	}
	info := listRaw[0].(map[string]any)
	if info["id"] != blobID {
		t.Errorf("Expected BlobInfo id %s, got %v", blobID, info["id"])
	}
	matched, _ := info["matchedIds"].(map[string]any)
	emailMatches, _ := matched["Email"].([]any)
	if len(emailMatches) != 1 || emailMatches[0] != emailID {
		t.Errorf("Expected Email matchedIds [%s], got %v", emailID, matched["Email"])
	}
	threadMatches, _ := matched["Thread"].([]any)
	if len(threadMatches) != 1 || threadMatches[0] != threadID {
		t.Errorf("Expected Thread matchedIds [%s], got %v", threadID, matched["Thread"])
	}
	mailboxMatches, _ := matched["Mailbox"].([]any)
	if len(mailboxMatches) != 0 {
		t.Errorf("Expected empty Mailbox matchedIds, got %v", matched["Mailbox"])
	}

	// A blob with no references still yields an entry with empty arrays per type.
	upload2Args := post([]any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"b2": map[string]any{
					"data": "dW5yZWZlcmVuY2Vk", // Base64 "unreferenced"
					"type": "text/plain",
				},
			},
		}, "c6"},
	})
	unrefBlobID := upload2Args["created"].(map[string]any)["b2"].(map[string]any)["id"].(string)

	emptyArgs := post([]any{
		[]any{"Blob/lookup", map[string]any{
			"accountId": "primary",
			"typeNames": []string{"Email", "Mailbox"},
			"ids":       []string{unrefBlobID},
		}, "c4"},
	})
	if emptyRaw, _ := emptyArgs["notFound"].([]any); len(emptyRaw) != 0 {
		t.Errorf("Expected no notFound for existing blob, got %v", emptyArgs["notFound"])
	}
	emptyList, _ := emptyArgs["list"].([]any)
	if len(emptyList) != 1 {
		t.Fatalf("Expected 1 BlobInfo entry for existing blob, got %v", emptyArgs["list"])
	}
	emptyMatched := emptyList[0].(map[string]any)["matchedIds"].(map[string]any)
	if em, _ := emptyMatched["Email"].([]any); len(em) != 0 {
		t.Errorf("Expected empty Email matchedIds, got %v", emptyMatched["Email"])
	}
	if mb, _ := emptyMatched["Mailbox"].([]any); len(mb) != 0 {
		t.Errorf("Expected empty Mailbox matchedIds, got %v", emptyMatched["Mailbox"])
	}

	// Unknown type names must produce an unknownDataType error (RFC 9404 Section 6.2).
	badTypeArgs := post([]any{
		[]any{"Blob/lookup", map[string]any{
			"accountId": "primary",
			"typeNames": []string{"BogusType"},
			"ids":       []string{blobID},
		}, "c5"},
	})
	if badTypeArgs["type"] != "unknownDataType" {
		t.Errorf("Expected unknownDataType error, got %v", badTypeArgs)
	}
}

// TestRFC9404_Section4_BlobCopy tests Blob/copy method per RFC 9404 Section 4.
func TestRFC9404_Section4_BlobCopy(t *testing.T) {
	blobBackend := memory.NewMemoryBlobBackend()
	b1, _ := blobBackend.PutBlob(context.TODO(), "account-a", "text/plain", []byte("Copy Me"))

	srv := jmap.NewServer(nil, jmap.WithBlobBackend(blobBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.BlobCapabilityURI},
		"methodCalls": []any{
			[]any{"Blob/copy", map[string]any{
				"fromAccountId": "account-a",
				"accountId":     "account-b",
				"create": map[string]any{
					"c1": map[string]any{
						"blobId": b1.ID,
					},
					"c2": map[string]any{
						"blobId": "missing-blob-id",
					},
				},
			}, "call-1"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Blob/copy" {
		t.Fatalf("Expected method response 'Blob/copy', got %q", methodResp.Name)
	}

	copied, ok := methodResp.Args["copied"].(map[string]any)
	if !ok || copied["c1"] == nil {
		t.Errorf("Expected copied blob for key 'c1', got %v", methodResp.Args["copied"])
	}

	notCopied, ok := methodResp.Args["notCopied"].(map[string]any)
	if !ok || notCopied["c2"] == nil {
		t.Errorf("Expected notCopied entry for key 'c2', got %v", methodResp.Args["notCopied"])
	}
}
