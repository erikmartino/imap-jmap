package jmap_test

import (
	"bytes"
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
	_, _ = blobBackend.PutBlob(nil, "primary", "text/plain", []byte("Hello RFC 9404"))

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
