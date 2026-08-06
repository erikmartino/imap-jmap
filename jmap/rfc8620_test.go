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

	resp, err := authedGet(ts.URL + "/.well-known/jmap")
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

	resp, err := authedGet(ts.URL + "/.well-known/jmap")
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

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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

// TestRFC8620_Section3_3_ResultReference tests result reference evaluation per RFC 8620 Section 3.7.
func TestRFC8620_Section3_3_ResultReference(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"greeting": "hello"}, "c1"},
			[]any{"Core/echo", map[string]any{
				"#echoed": map[string]any{
					"resultOf": "c1",
					"name":     "Core/echo",
					"path":     "/greeting",
				},
			}, "c2"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader([]byte("{invalid-json")))
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

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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

// TestRFC9670_Section1_5_2_PrincipalsOwnerImpliedCapability tests RFC 9670 Section 1.5.2:
// "urn:ietf:params:jmap:principals:owner" never appears in session capabilities but its support
// is implied by "urn:ietf:params:jmap:principals". Clients (e.g. Bulwark webmail) include it in
// the request "using" array, which MUST be accepted rather than rejected as unknownCapability.
func TestRFC9670_Section1_5_2_PrincipalsOwnerImpliedCapability(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// The session must advertise urn:ietf:params:jmap:principals but MUST NOT advertise
	// urn:ietf:params:jmap:principals:owner in its capabilities object (RFC 9670 Section 1.5.2).
	sessResp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer sessResp.Body.Close()
	var sess jmap.Session
	if err := json.NewDecoder(sessResp.Body).Decode(&sess); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}
	if _, ok := sess.Capabilities[jmap.PrincipalsCapabilityURI]; !ok {
		t.Fatalf("Session must advertise %q", jmap.PrincipalsCapabilityURI)
	}
	if _, ok := sess.Capabilities[jmap.PrincipalsOwnerCapabilityURI]; ok {
		t.Errorf("Session capabilities must not advertise %q directly (RFC 9670 Section 1.5.2)", jmap.PrincipalsOwnerCapabilityURI)
	}

	// A request whose "using" includes the implied sub-capability must succeed.
	reqPayload := map[string]any{
		"using": []string{
			jmap.CoreCapabilityURI,
			jmap.MailCapabilityURI,
			jmap.PrincipalsCapabilityURI,
			jmap.PrincipalsOwnerCapabilityURI,
		},
		"methodCalls": []any{
			[]any{"Mailbox/get", map[string]any{}, "c1"},
		},
	}
	body, _ := json.Marshal(reqPayload)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected HTTP 200 for request with implied principals:owner capability, got %d", resp.StatusCode)
	}

	var apiResp struct {
		MethodResponses [][]any `json:"methodResponses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		t.Fatalf("Failed to decode API response: %v", err)
	}
	if len(apiResp.MethodResponses) != 1 {
		t.Fatalf("Expected 1 method response, got %d", len(apiResp.MethodResponses))
	}
	if name, _ := apiResp.MethodResponses[0][0].(string); name != "Mailbox/get" {
		t.Errorf("Expected Mailbox/get method response, got %q", name)
	}

	// Sanity check: an unrelated unknown capability is still rejected.
	reqPayload2 := map[string]any{
		"using":       []string{jmap.CoreCapabilityURI, "urn:ietf:params:jmap:does-not-exist"},
		"methodCalls": []any{},
	}
	body2, _ := json.Marshal(reqPayload2)
	resp2, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body2))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected HTTP 400 for unknown capability, got %d", resp2.StatusCode)
	}
	var reqErr jmap.RequestError
	if err := json.NewDecoder(resp2.Body).Decode(&reqErr); err != nil {
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

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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
				"#echoed": map[string]any{
					"resultOf": "non-existent-id",
					"name":     "Core/echo",
					"path":     "/foo",
				},
			}, "c1"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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

	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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
	req := authedRequest(t, http.MethodPost, ts.URL+"/upload/primary/", bytes.NewReader(blobData))
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

// TestRFC8620_Section3_7_ResultReferences tests chained result reference resolution
// (argument name prefixed with "#", ResultReference with resultOf/name/path) per RFC 8620
// Section 3.7, using the exact request shape real clients (e.g. Bulwark webmail) send:
// Email/query with an inMailbox filter, then Email/get whose "#ids" argument references
// "/ids" of the query response. The Email/get response MUST contain exactly the queried
// mailbox's emails — never a fallback to all emails in the account.
func TestRFC8620_Section3_7_ResultReferences(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}}, "q1"},
			[]any{"Email/get", map[string]any{
				"accountId": "primary",
				"#ids": map[string]any{
					"resultOf": "q1",
					"name":     "Email/query",
					"path":     "/ids",
				},
			}, "g1"},
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-sent"}}, "q2"},
			[]any{"Email/get", map[string]any{
				"accountId": "primary",
				"#ids": map[string]any{
					"resultOf": "q2",
					"name":     "Email/query",
					"path":     "/ids",
				},
			}, "g2"},
		},
	}

	body, _ := json.Marshal(reqPayload)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	if len(jmapResp.MethodResponses) != 4 {
		t.Fatalf("Expected 4 method responses, got %d", len(jmapResp.MethodResponses))
	}

	// Call 2: Email/get must return exactly the inbox query's ids (2 seeded emails).
	q1IDs := queryIDs(t, jmapResp.MethodResponses[0])
	if len(q1IDs) == 0 {
		t.Fatalf("Expected the inbox query to return emails, got %v", q1IDs)
	}
	g1 := jmapResp.MethodResponses[1]
	if g1.Name != "Email/get" {
		t.Fatalf("Expected response 'Email/get', got %q", g1.Name)
	}
	g1IDs := getListIDs(t, g1.Args["list"])
	if !equalStringSlices(g1IDs, q1IDs) {
		t.Errorf("Email/get via result reference returned %v, want exactly the query ids %v", g1IDs, q1IDs)
	}
	if notFound, _ := g1.Args["notFound"].([]any); len(notFound) != 0 {
		t.Errorf("Expected notFound to be empty, got %v", notFound)
	}

	// Call 4: Email/get for the (empty) sent mailbox must return an empty list. If the
	// result reference is not honored, the handler falls back to fetching all emails and
	// every mailbox would show the same content (the Bulwark symptom).
	g2 := jmapResp.MethodResponses[3]
	if g2.Name != "Email/get" {
		t.Fatalf("Expected response 'Email/get', got %q", g2.Name)
	}
	g2IDs := getListIDs(t, g2.Args["list"])
	if len(g2IDs) != 0 {
		t.Errorf("Email/get via result reference for empty mailbox returned %v, want an empty list", g2IDs)
	}
}

func queryIDs(t *testing.T, inv jmap.Invocation) []string {
	t.Helper()
	idsRaw, ok := inv.Args["ids"].([]any)
	if !ok {
		t.Fatalf("Expected query response to have ids array, got %v", inv.Args["ids"])
	}
	ids := make([]string, 0, len(idsRaw))
	for _, id := range idsRaw {
		s, _ := id.(string)
		ids = append(ids, s)
	}
	return ids
}

func getListIDs(t *testing.T, listRaw any) []string {
	t.Helper()
	list, ok := listRaw.([]any)
	if !ok {
		t.Fatalf("Expected get response to have list array, got %v", listRaw)
	}
	ids := make([]string, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := m["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRFC8620_Section3_7_ResultReference_StarPointer tests the "*" array mapping in JSON
// pointers per RFC 8620 Section 3.7: "/list/*/id" maps over every item of the list array and
// flattens the results into a single array.
func TestRFC8620_Section3_7_ResultReference_StarPointer(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Email/query", map[string]any{"accountId": "primary", "filter": map[string]any{"inMailbox": "mb-inbox"}}, "q1"},
			[]any{"Email/get", map[string]any{
				"accountId": "primary",
				"#ids": map[string]any{
					"resultOf": "q1",
					"name":     "Email/query",
					"path":     "/ids",
				},
			}, "g1"},
			[]any{"Core/echo", map[string]any{
				"#echoed": map[string]any{
					"resultOf": "g1",
					"name":     "Email/get",
					"path":     "/list/*/id",
				},
			}, "e1"},
		},
	}

	body, _ := json.Marshal(reqPayload)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode Response: %v", err)
	}

	if len(jmapResp.MethodResponses) != 3 {
		t.Fatalf("Expected 3 method responses, got %d", len(jmapResp.MethodResponses))
	}

	star := jmapResp.MethodResponses[2]
	if star.Name != "Core/echo" {
		t.Fatalf("Expected response 'Core/echo', got %q", star.Name)
	}
	want := queryIDs(t, jmapResp.MethodResponses[0])
	gotRaw, _ := star.Args["echoed"].([]any)
	got := make([]string, 0, len(gotRaw))
	for _, id := range gotRaw {
		s, _ := id.(string)
		got = append(got, s)
	}
	if !equalStringSlices(got, want) {
		t.Errorf("Star pointer /list/*/id returned %v, want %v", got, want)
	}
}

// TestRFC8620_Section3_7_ResultReference_Errors tests the failure modes of result references
// per RFC 8620 Section 3.7: a non-ResultReference value under a "#" argument yields
// invalidResultReference, and giving an argument in both normal and referenced form yields
// invalidArguments.
func TestRFC8620_Section3_7_ResultReference_Errors(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	cases := []struct {
		name     string
		args     map[string]any
		wantType string
	}{
		{
			name: "referenced argument value is not a ResultReference object",
			args: map[string]any{
				"#echoed": "not-a-reference",
			},
			wantType: jmap.MethodErrorInvalidResultReference,
		},
		{
			name: "referenced call does not exist",
			args: map[string]any{
				"#echoed": map[string]any{"resultOf": "missing", "name": "Core/echo", "path": "/x"},
			},
			wantType: jmap.MethodErrorInvalidResultReference,
		},
		{
			name: "response name does not match",
			args: map[string]any{
				"#echoed": map[string]any{"resultOf": "c1", "name": "Other/method", "path": "/x"},
			},
			wantType: jmap.MethodErrorInvalidResultReference,
		},
		{
			name: "argument given in both normal and referenced form",
			args: map[string]any{
				"echoed":  "plain",
				"#echoed": map[string]any{"resultOf": "c1", "name": "Core/echo", "path": "/greeting"},
			},
			wantType: jmap.MethodErrorInvalidArguments,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			methodCalls := []any{[]any{"Core/echo", map[string]any{"greeting": "hello"}, "c1"}}
			methodCalls = append(methodCalls, []any{"Core/echo", tc.args, "c2"})

			reqPayload := map[string]any{
				"using":       []string{jmap.CoreCapabilityURI},
				"methodCalls": methodCalls,
			}
			body, _ := json.Marshal(reqPayload)
			resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
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
			second := jmapResp.MethodResponses[1]
			if second.Name != "error" {
				t.Fatalf("Expected response name 'error', got %q", second.Name)
			}
			errType, _ := second.Args["type"].(string)
			if errType != tc.wantType {
				t.Errorf("Expected method error type %q, got %q", tc.wantType, errType)
			}
		})
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
	resp, err := authedGet(ts.URL + "/download/primary/" + blob.ID + "/test.txt")
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

	req := authedRequest(t, "GET", ts.URL+"/download/primary/"+blob.ID+"/test.txt", nil)
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

// TestRFC8620_Section5_1_GetProperties verifies the optional "properties" argument of every
// */get method per RFC 8620 Section 5.1: only the requested properties (plus id) are returned.
func TestRFC8620_Section5_1_GetProperties(t *testing.T) {
	get := func(ts *httptest.Server, using []string, method string, args map[string]any) []any {
		callArgs := map[string]any{"accountId": "primary"}
		for k, v := range args {
			callArgs[k] = v
		}
		reqPayload := map[string]any{
			"using": using,
			"methodCalls": []any{
				[]any{method, callArgs, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
			t.Fatalf("Failed to decode Response: %v", err)
		}
		list, _ := jr.MethodResponses[0].Args["list"].([]any)
		return list
	}

	// assertFiltered checks that each returned object has exactly id plus the requested
	// properties, and that the requested property is absent/present as expected.
	assertFiltered := func(t *testing.T, list []any, props ...string) map[string]any {
		t.Helper()
		if len(list) == 0 {
			t.Fatalf("Expected a non-empty list")
		}
		obj := list[0].(map[string]any)
		if len(obj) != len(props)+1 {
			t.Fatalf("Expected only id plus %v in object, got keys %v", props, obj)
		}
		if _, ok := obj["id"]; !ok {
			t.Fatalf("Expected id property, got %v", obj)
		}
		out := obj
		return out
	}

	t.Run("Mailbox", func(t *testing.T) {
		srv := newTestServer()
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		list := get(ts, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, "Mailbox/get", map[string]any{
			"ids":        []string{"mb-inbox"},
			"properties": []string{"name", "role"},
		})
		obj := assertFiltered(t, list, "name", "role")
		if obj["name"] != "Inbox" {
			t.Errorf("Expected name Inbox, got %v", obj["name"])
		}
	})

	t.Run("Email", func(t *testing.T) {
		srv := newTestServer()
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		list := get(ts, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, "Email/get", map[string]any{
			"ids":        []string{"email-1"},
			"properties": []string{"subject", "size"},
		})
		obj := assertFiltered(t, list, "subject", "size")
		if obj["subject"] != "Welcome to JMAP Server" {
			t.Errorf("Expected seeded subject, got %v", obj["subject"])
		}
	})

	t.Run("Thread", func(t *testing.T) {
		srv := newTestServer()
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		list := get(ts, []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}, "Thread/get", map[string]any{
			"ids":        []string{"thread-2"},
			"properties": []string{"emailIds"},
		})
		obj := assertFiltered(t, list, "emailIds")
		emailIDs, _ := obj["emailIds"].([]any)
		if len(emailIDs) != 1 || emailIDs[0] != "email-1" {
			t.Errorf("Expected emailIds [email-1], got %v", obj["emailIds"])
		}
	})

	t.Run("Quota", func(t *testing.T) {
		srv := newTestServer()
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		list := get(ts, []string{jmap.CoreCapabilityURI, jmap.QuotaCapabilityURI}, "Quota/get", map[string]any{
			"properties": []string{"name"},
		})
		obj := assertFiltered(t, list, "name")
		if _, ok := obj["used"]; ok {
			t.Errorf("Expected used property to be filtered out, got %v", obj)
		}
	})

	t.Run("Calendar", func(t *testing.T) {
		srv := newTestServer()
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		list := get(ts, []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}, "Calendar/get", map[string]any{
			"ids":        []string{"cal-default"},
			"properties": []string{"name"},
		})
		obj := assertFiltered(t, list, "name")
		if obj["name"] != "Personal Calendar" {
			t.Errorf("Expected default calendar name, got %v", obj["name"])
		}
	})

	t.Run("CalendarEvent", func(t *testing.T) {
		srv := newTestServer()
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		// Create an event first so there is something to fetch.
		reqPayload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
			"methodCalls": []any{
				[]any{"CalendarEvent/set", map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"ev1": map[string]any{"title": "Partial Fetch", "start": "2026-08-05T10:00:00Z", "duration": "PT1H"},
					},
				}, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		var jr jmap.Response
		json.NewDecoder(resp.Body).Decode(&jr)
		resp.Body.Close()
		evID := jr.MethodResponses[0].Args["created"].(map[string]any)["ev1"].(map[string]any)["id"].(string)

		list := get(ts, []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}, "CalendarEvent/get", map[string]any{
			"ids":        []string{evID},
			"properties": []string{"title", "start"},
		})
		obj := assertFiltered(t, list, "title", "start")
		if obj["title"] != "Partial Fetch" {
			t.Errorf("Expected event title, got %v", obj["title"])
		}
	})

	t.Run("AddressBook", func(t *testing.T) {
		srv := newTestServer()
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		list := get(ts, []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}, "AddressBook/get", map[string]any{
			"ids":        []string{"ab-default"},
			"properties": []string{"name"},
		})
		obj := assertFiltered(t, list, "name")
		if obj["name"] != "Personal Contacts" {
			t.Errorf("Expected default address book name, got %v", obj["name"])
		}
	})

	t.Run("Card", func(t *testing.T) {
		srv := newTestServer()
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		// Create a card, then fetch only its kind property.
		reqPayload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI},
			"methodCalls": []any{
				[]any{"Card/set", map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"c1": map[string]any{
							"addressBookIds": map[string]any{"ab-default": true},
							"name":           map[string]any{"full": "Partial Card"},
						},
					},
				}, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		var jr jmap.Response
		json.NewDecoder(resp.Body).Decode(&jr)
		resp.Body.Close()
		cardID := jr.MethodResponses[0].Args["created"].(map[string]any)["c1"].(map[string]any)["id"].(string)

		list := get(ts, []string{jmap.CoreCapabilityURI, jmap.ContactsCapabilityURI}, "Card/get", map[string]any{
			"ids":        []string{cardID},
			"properties": []string{"name"},
		})
		obj := assertFiltered(t, list, "name")
		if _, ok := obj["name"]; !ok {
			t.Errorf("Expected name property in response, got %v", obj)
		}
	})

	t.Run("SieveScript", func(t *testing.T) {
		srv := newTestServer()
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		reqPayload := map[string]any{
			"using": []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI},
			"methodCalls": []any{
				[]any{"SieveScript/set", map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"s1": map[string]any{
							"name":     "Partial",
							"content":  `require ["fileinto"]; if header :contains "X-Spam" "Yes" { fileinto "Junk"; }`,
							"isActive": true,
						},
					},
				}, "c1"},
			},
		}
		body, _ := json.Marshal(reqPayload)
		resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		var jr jmap.Response
		json.NewDecoder(resp.Body).Decode(&jr)
		resp.Body.Close()
		scriptID := jr.MethodResponses[0].Args["created"].(map[string]any)["s1"].(map[string]any)["id"].(string)

		list := get(ts, []string{jmap.CoreCapabilityURI, jmap.SieveCapabilityURI}, "SieveScript/get", map[string]any{
			"ids":        []string{scriptID},
			"properties": []string{"name", "isActive"},
		})
		obj := assertFiltered(t, list, "name", "isActive")
		if obj["name"] != "Partial" {
			t.Errorf("Expected script name Partial, got %v", obj["name"])
		}
	})
}
