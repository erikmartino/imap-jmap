package jmap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_Section1_6_ObjectIdentifiers tests Id validation per RFC 8620 Section 1.6 & 1.7.5.
func TestRFC8620_Section1_6_ObjectIdentifiers(t *testing.T) {
	validIDs := []jmap.Id{"a", "123", "user_123-abc", "A_B_C"}
	for _, id := range validIDs {
		if !id.Validate() {
			t.Errorf("Expected Id %q to be valid", id)
		}
	}

	invalidIDs := []jmap.Id{"", "id with space", "id#hash", "id/slash"}
	for _, id := range invalidIDs {
		if id.Validate() {
			t.Errorf("Expected Id %q to be invalid", id)
		}
	}
}

// TestRFC8620_Section2_1_DiscoveringTheJMAPSessionResource tests session discovery per RFC 8620 Section 2.1.
func TestRFC8620_Section2_1_DiscoveringTheJMAPSessionResource(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("Failed to execute GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 OK, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %q", resp.Header.Get("Content-Type"))
	}
}

// TestRFC8620_Section2_2_TheJMAPSessionObject tests JMAP Session object structure per RFC 8620 Section 2.2.
func TestRFC8620_Section2_2_TheJMAPSessionObject(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("Failed GET: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode Session JSON: %v", err)
	}

	if session.Username == "" {
		t.Error("Session username should not be empty")
	}
	if session.APIURL == "" {
		t.Error("Session apiUrl should not be empty")
	}
	if _, ok := session.Capabilities[jmap.CoreCapabilityURI]; !ok {
		t.Errorf("Capabilities must include %q", jmap.CoreCapabilityURI)
	}
}

// TestRFC8620_Section3_1_StructureOfAJMAPRequest tests JMAP Request object per RFC 8620 Section 3.1.
func TestRFC8620_Section3_1_StructureOfAJMAPRequest(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"hello": "world"}, "c1"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 OK, got %d", resp.StatusCode)
	}
}

// TestRFC8620_Section3_2_InvocationArrays tests Invocation marshal/unmarshal per RFC 8620 Section 3.2.
func TestRFC8620_Section3_2_InvocationArrays(t *testing.T) {
	inv := jmap.Invocation{
		Name:         "Core/echo",
		Args:         map[string]any{"foo": "bar"},
		ClientCallID: "c1",
	}

	data, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("Failed to marshal Invocation: %v", err)
	}

	var decoded jmap.Invocation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Invocation: %v", err)
	}

	if decoded.Name != inv.Name || decoded.ClientCallID != inv.ClientCallID {
		t.Errorf("Mismatch in Invocation: got %+v, want %+v", decoded, inv)
	}
}

// TestRFC8620_Section3_3_ResultReference tests result reference evaluation per RFC 8620 Section 3.3.
func TestRFC8620_Section3_3_ResultReference(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"greeting": "hello"}, "c1"},
			[]any{"Core/echo", map[string]any{
				"echoed": map[string]any{
					"#resultOf": "c1",
					"#name":     "Core/echo",
					"#path":     "/greeting",
				},
			}, "c2"},
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

	if len(jmapResp.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses, got %d", len(jmapResp.MethodResponses))
	}

	secondResp := jmapResp.MethodResponses[1]
	if secondResp.ClientCallID != "c2" {
		t.Errorf("Expected clientCallId c2, got %q", secondResp.ClientCallID)
	}
	if echoed, ok := secondResp.Args["echoed"].(string); !ok || echoed != "hello" {
		t.Errorf("Expected result reference resolution to 'hello', got %v", secondResp.Args["echoed"])
	}
}

// TestRFC8620_Section3_4_ProcessingARequest tests standard request processing per RFC 8620 Section 3.4.
func TestRFC8620_Section3_4_ProcessingARequest(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"ping": "pong"}, "call-1"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed POST /jmap: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected HTTP 200 OK, got %d", resp.StatusCode)
	}
}

// TestRFC8620_Section3_5_StructureOfAJMAPResponse tests JMAP Response object per RFC 8620 Section 3.5.
func TestRFC8620_Section3_5_StructureOfAJMAPResponse(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"a": 1}, "c1"},
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
		t.Fatalf("Failed to decode Response JSON: %v", err)
	}

	if jmapResp.SessionState == "" {
		t.Error("Expected non-empty sessionState in Response")
	}
	if len(jmapResp.MethodResponses) != 1 {
		t.Errorf("Expected 1 method response, got %d", len(jmapResp.MethodResponses))
	}
}

// TestRFC8620_Section3_6_1_RequestErrors_InvalidJSON tests invalidJSON error per RFC 8620 Section 3.6.1.
func TestRFC8620_Section3_6_1_RequestErrors_InvalidJSON(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader([]byte("{invalid-json")))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected HTTP 400 Bad Request, got %d", resp.StatusCode)
	}

	var reqErr jmap.RequestError
	if err := json.NewDecoder(resp.Body).Decode(&reqErr); err != nil {
		t.Fatalf("Failed to decode RequestError: %v", err)
	}

	if reqErr.Type != jmap.ErrorInvalidJSON {
		t.Errorf("Expected error type %q, got %q", jmap.ErrorInvalidJSON, reqErr.Type)
	}
}

// TestRFC8620_Section3_6_1_RequestErrors_UnknownCapability tests unknownCapability error per RFC 8620 Section 3.6.1.
func TestRFC8620_Section3_6_1_RequestErrors_UnknownCapability(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using":       []string{"urn:ietf:params:jmap:unknown"},
		"methodCalls": []any{},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected HTTP 400 Bad Request, got %d", resp.StatusCode)
	}

	var reqErr jmap.RequestError
	if err := json.NewDecoder(resp.Body).Decode(&reqErr); err != nil {
		t.Fatalf("Failed to decode RequestError: %v", err)
	}

	if reqErr.Type != jmap.ErrorUnknownCapability {
		t.Errorf("Expected error type %q, got %q", jmap.ErrorUnknownCapability, reqErr.Type)
	}
}

// TestRFC8620_Section3_6_2_MethodErrors_UnknownMethod tests unknownMethod error per RFC 8620 Section 3.6.2.
func TestRFC8620_Section3_6_2_MethodErrors_UnknownMethod(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"NonExistent/method", map[string]any{}, "c1"},
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

	if len(jmapResp.MethodResponses) != 1 {
		t.Fatalf("Expected 1 method response, got %d", len(jmapResp.MethodResponses))
	}

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "error" {
		t.Errorf("Expected response name 'error', got %q", methodResp.Name)
	}

	errType, _ := methodResp.Args["type"].(string)
	if errType != jmap.MethodErrorUnknownMethod {
		t.Errorf("Expected method error type %q, got %q", jmap.MethodErrorUnknownMethod, errType)
	}
}

// TestRFC8620_Section3_6_2_MethodErrors_InvalidResultReference tests invalidResultReference error per RFC 8620 Section 3.6.2.
func TestRFC8620_Section3_6_2_MethodErrors_InvalidResultReference(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{
				"echoed": map[string]any{
					"#resultOf": "non-existent-id",
					"#path":     "/foo",
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
	if methodResp.Name != "error" {
		t.Errorf("Expected response name 'error', got %q", methodResp.Name)
	}

	errType, _ := methodResp.Args["type"].(string)
	if errType != jmap.MethodErrorInvalidResultReference {
		t.Errorf("Expected method error type %q, got %q", jmap.MethodErrorInvalidResultReference, errType)
	}
}

// TestRFC8620_Section3_8_1_CoreEcho tests the Core/echo standard method per RFC 8620 Section 3.8.1.
func TestRFC8620_Section3_8_1_CoreEcho(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	args := map[string]any{
		"string": "hello",
		"number": float64(42),
		"bool":   true,
	}

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", args, "echo-call-1"},
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

	if len(jmapResp.MethodResponses) != 1 {
		t.Fatalf("Expected 1 method response, got %d", len(jmapResp.MethodResponses))
	}

	methodResp := jmapResp.MethodResponses[0]
	if methodResp.Name != "Core/echo" {
		t.Errorf("Expected response name 'Core/echo', got %q", methodResp.Name)
	}
	if methodResp.ClientCallID != "echo-call-1" {
		t.Errorf("Expected clientCallId 'echo-call-1', got %q", methodResp.ClientCallID)
	}
	if methodResp.Args["string"] != "hello" || methodResp.Args["number"] != float64(42) {
		t.Errorf("Args mismatch in Core/echo response: %v", methodResp.Args)
	}
}

// TestRFC8620_Section6_1_UploadingBlobs tests blob uploading per RFC 8620 Section 6.1.
func TestRFC8620_Section6_1_UploadingBlobs(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	blobData := []byte("Hello, JMAP Blobs!")
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/upload/primary/", bytes.NewReader(blobData))
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed POST /upload/: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected HTTP 201 Created, got %d", resp.StatusCode)
	}

	var blob jmap.Blob
	if err := json.NewDecoder(resp.Body).Decode(&blob); err != nil {
		t.Fatalf("Failed to decode Blob JSON: %v", err)
	}

	if blob.ID == "" {
		t.Error("Expected non-empty blobId")
	}
	if blob.Size != int64(len(blobData)) {
		t.Errorf("Expected blob size %d, got %d", len(blobData), blob.Size)
	}
}

// TestRFC8620_Section3_7_ResultReferences tests chained result reference resolution (#resultOf, #name, #path) per RFC 8620 Section 3.7.
func TestRFC8620_Section3_7_ResultReferences(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Batch request: Call 1 runs Email/query -> returns email IDs list.
	// Call 2 runs Email/get passing #resultOf: "c1", #path: "/ids" into ids parameter.
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{"accountId": "primary"}, "c1"},
			[]any{"Email/get", map[string]any{
				"accountId": "primary",
				"#ids": map[string]any{
					"#resultOf": "c1",
					"#name":     "Email/query",
					"#path":     "/ids",
				},
			}, "c2"},
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

	if len(jmapResp.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses, got %d", len(jmapResp.MethodResponses))
	}

	call2 := jmapResp.MethodResponses[1]
	if call2.Name != "Email/get" {
		t.Errorf("Expected response 'Email/get', got %q", call2.Name)
	}

	list, ok := call2.Args["list"].([]any)
	if !ok || len(list) == 0 {
		t.Errorf("Expected non-empty email list resolved via chained result reference, got %v", call2.Args["list"])
	}
}


// TestRFC8620_Section6_2_DownloadingBlobs tests blob downloading per RFC 8620 Section 6.2.
func TestRFC8620_Section6_2_DownloadingBlobs(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Upload blob first
	blobData := []byte("Sample Blob Data for Download")
	blob, _ := srv.BlobBackend.PutBlob(context.Background(), "primary", "text/plain", blobData)

	// Download blob
	resp, err := http.Get(ts.URL + "/download/primary/" + blob.ID + "/test.txt")
	if err != nil {
		t.Fatalf("GET /download/ failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected HTTP 200 OK, got %d", resp.StatusCode)
	}

	downloaded, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read downloaded body: %v", err)
	}

	if string(downloaded) != string(blobData) {
		t.Errorf("Downloaded content mismatch: got %q, want %q", string(downloaded), string(blobData))
	}
}

// TestRFC8620_Section6_2_MayProvisions_RangeDownload tests optional HTTP Range header support (206 Partial Content) per RFC 8620 Section 6.2 MAY provisions.
func TestRFC8620_Section6_2_MayProvisions_RangeDownload(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	blobData := []byte("Sample Blob Data for Download")
	blob, _ := srv.BlobBackend.PutBlob(context.Background(), "primary", "text/plain", blobData)

	req, err := http.NewRequest("GET", ts.URL+"/download/primary/"+blob.ID+"/test.txt", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Range", "bytes=0-5")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /download/ with Range failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Errorf("Expected HTTP 206 Partial Content for Range request per RFC 8620 MAY provisions, got %d", resp.StatusCode)
	}

	downloaded, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read Range downloaded body: %v", err)
	}

	if string(downloaded) != "Sample" {
		t.Errorf("Expected range bytes 'Sample', got %q", string(downloaded))
	}
}
