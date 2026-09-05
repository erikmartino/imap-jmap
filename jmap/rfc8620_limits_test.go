package jmap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"

	"github.com/coder/websocket"
)

// TestRFC8620_Section3_6_1_MaxCallsInRequest tests that exceeding maxCallsInRequest returns
// urn:ietf:params:jmap:error:limit with limit="maxCallsInRequest" per RFC 8620 Section 3.6.1.
func TestRFC8620_Section3_6_1_MaxCallsInRequest(t *testing.T) {
	spectest.Require(t, "RFC 8620", "3.6.1", "MUST", "The request was not processed because it exceeds a server-defined limit... The problem details object MUST contain a property called limit")

	customLimits := jmap.CoreCapability{
		MaxSizeUpload:         jmap.DefaultMaxSizeUpload,
		MaxConcurrentUpload:   jmap.DefaultMaxConcurrentUpload,
		MaxSizeRequest:        jmap.DefaultMaxSizeRequest,
		MaxConcurrentRequests: jmap.DefaultMaxConcurrentRequests,
		MaxCallsInRequest:     2, // max 2 calls
		MaxObjectsInGet:       jmap.DefaultMaxObjectsInGet,
		MaxObjectsInSet:       jmap.DefaultMaxObjectsInSet,
		CollationAlgorithms:   []string{"i;ascii-casemap", "i;octet"},
	}

	srv := newTestServer(jmap.WithCoreCapability(customLimits))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Sending 2 calls (within limit) succeeds
	req2 := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"a": 1}, "c1"},
			[]any{"Core/echo", map[string]any{"b": 2}, "c2"},
		},
	}
	b2, _ := json.Marshal(req2)
	resp2, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(b2))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for 2 calls, got %d", resp2.StatusCode)
	}

	// 2. Sending 3 calls (exceeds limit 2) MUST be rejected with HTTP 400 and urn:ietf:params:jmap:error:limit
	req3 := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"a": 1}, "c1"},
			[]any{"Core/echo", map[string]any{"b": 2}, "c2"},
			[]any{"Core/echo", map[string]any{"c": 3}, "c3"},
		},
	}
	b3, _ := json.Marshal(req3)
	resp3, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(b3))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d", resp3.StatusCode)
	}
	if ct := resp3.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("expected Content-Type application/problem+json, got %q", ct)
	}

	var reqErr jmap.RequestError
	if err := json.NewDecoder(resp3.Body).Decode(&reqErr); err != nil {
		t.Fatalf("failed to decode RequestError: %v", err)
	}
	if reqErr.Type != jmap.ErrorLimit {
		t.Errorf("expected error type %q, got %q", jmap.ErrorLimit, reqErr.Type)
	}
	if reqErr.Limit != "maxCallsInRequest" {
		t.Errorf("expected limit %q, got %q", "maxCallsInRequest", reqErr.Limit)
	}
}

// TestRFC8620_Section3_6_1_MaxSizeRequest tests that exceeding maxSizeRequest returns
// HTTP 413 with urn:ietf:params:jmap:error:limit and limit="maxSizeRequest".
func TestRFC8620_Section3_6_1_MaxSizeRequest(t *testing.T) {
	spectest.Require(t, "RFC 8620", "3.6.1", "MUST", "The HTTP status code MUST be 400, or 413 if the limit exceeded is maxSizeRequest")

	customLimits := jmap.CoreCapability{
		MaxSizeUpload:         jmap.DefaultMaxSizeUpload,
		MaxConcurrentUpload:   jmap.DefaultMaxConcurrentUpload,
		MaxSizeRequest:        300, // max 300 octets
		MaxConcurrentRequests: jmap.DefaultMaxConcurrentRequests,
		MaxCallsInRequest:     jmap.DefaultMaxCallsInRequest,
		MaxObjectsInGet:       jmap.DefaultMaxObjectsInGet,
		MaxObjectsInSet:       jmap.DefaultMaxObjectsInSet,
		CollationAlgorithms:   []string{"i;ascii-casemap", "i;octet"},
	}

	srv := newTestServer(jmap.WithCoreCapability(customLimits))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Within 300 bytes succeeds
	smallReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"v": "ok"}, "c1"},
		},
	}
	sb, _ := json.Marshal(smallReq)
	sResp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(sb))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer sResp.Body.Close()
	if sResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", sResp.StatusCode)
	}

	// 2. Over 300 bytes rejected with 413
	largeReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{
				"padding": string(make([]byte, 500)),
			}, "c1"},
		},
	}
	lb, _ := json.Marshal(largeReq)
	lResp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(lb))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer lResp.Body.Close()

	if lResp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 Payload Too Large, got %d", lResp.StatusCode)
	}
	if ct := lResp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("expected Content-Type application/problem+json, got %q", ct)
	}

	var reqErr jmap.RequestError
	if err := json.NewDecoder(lResp.Body).Decode(&reqErr); err != nil {
		t.Fatalf("failed to decode RequestError: %v", err)
	}
	if reqErr.Type != jmap.ErrorLimit {
		t.Errorf("expected error type %q, got %q", jmap.ErrorLimit, reqErr.Type)
	}
	if reqErr.Limit != "maxSizeRequest" {
		t.Errorf("expected limit %q, got %q", "maxSizeRequest", reqErr.Limit)
	}
}

// TestRFC8620_Section5_1_MaxObjectsInGet tests that requesting more than maxObjectsInGet ids
// returns a requestTooLarge method error per RFC 8620 Section 2.2 and 5.1.
func TestRFC8620_Section5_1_MaxObjectsInGet(t *testing.T) {
	spectest.Require(t, "RFC 8620", "5.1", "MUST", "If more than maxObjectsInGet ids are requested, the server MUST return a requestTooLarge method error")

	customLimits := jmap.CoreCapability{
		MaxSizeUpload:         jmap.DefaultMaxSizeUpload,
		MaxConcurrentUpload:   jmap.DefaultMaxConcurrentUpload,
		MaxSizeRequest:        jmap.DefaultMaxSizeRequest,
		MaxConcurrentRequests: jmap.DefaultMaxConcurrentRequests,
		MaxCallsInRequest:     jmap.DefaultMaxCallsInRequest,
		MaxObjectsInGet:       2, // max 2 objects in get
		MaxObjectsInSet:       jmap.DefaultMaxObjectsInSet,
		CollationAlgorithms:   []string{"i;ascii-casemap", "i;octet"},
	}

	srv := newTestServer(jmap.WithCoreCapability(customLimits))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Requesting 2 IDs succeeds
	callsOk := []any{
		[]any{"Mailbox/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{"mb-inbox", "mb-sent"},
		}, "c1"},
	}
	rOk := postJMAP(t, ts.URL, using, callsOk)
	if len(rOk.MethodResponses) != 1 || rOk.MethodResponses[0].Name != "Mailbox/get" {
		t.Fatalf("expected successful Mailbox/get response, got %v", rOk.MethodResponses)
	}

	// 2. Requesting 3 IDs (exceeds maxObjectsInGet: 2) MUST return requestTooLarge method error
	callsTooLarge := []any{
		[]any{"Mailbox/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{"mb-inbox", "mb-sent", "mb-trash"},
		}, "c2"},
	}
	rErr := postJMAP(t, ts.URL, using, callsTooLarge)
	if len(rErr.MethodResponses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(rErr.MethodResponses))
	}
	resp := rErr.MethodResponses[0]
	if resp.Name != "error" {
		t.Fatalf("expected method response 'error', got %q", resp.Name)
	}
	errType, _ := resp.Args["type"].(string)
	if errType != jmap.MethodErrorRequestTooLarge {
		t.Errorf("expected error type %q, got %q", jmap.MethodErrorRequestTooLarge, errType)
	}
}

// TestRFC8620_Section5_3_MaxObjectsInSet tests that sending more create+update+destroy items
// than maxObjectsInSet returns a requestTooLarge method error per RFC 8620 Section 2.2 and 5.3.
func TestRFC8620_Section5_3_MaxObjectsInSet(t *testing.T) {
	spectest.Require(t, "RFC 8620", "5.3", "MUST", "The maximum number of objects the client may send to create, update, or destroy in a single /set invocation. If this limit is exceeded, the server MUST return a requestTooLarge method error")

	customLimits := jmap.CoreCapability{
		MaxSizeUpload:         jmap.DefaultMaxSizeUpload,
		MaxConcurrentUpload:   jmap.DefaultMaxConcurrentUpload,
		MaxSizeRequest:        jmap.DefaultMaxSizeRequest,
		MaxConcurrentRequests: jmap.DefaultMaxConcurrentRequests,
		MaxCallsInRequest:     jmap.DefaultMaxCallsInRequest,
		MaxObjectsInGet:       jmap.DefaultMaxObjectsInGet,
		MaxObjectsInSet:       2, // max 2 objects in set
		CollationAlgorithms:   []string{"i;ascii-casemap", "i;octet"},
	}

	srv := newTestServer(jmap.WithCoreCapability(customLimits))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Creating 2 mailboxes succeeds
	callsOk := []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"m1": map[string]any{"name": "Box1"},
				"m2": map[string]any{"name": "Box2"},
			},
		}, "c1"},
	}
	rOk := postJMAP(t, ts.URL, using, callsOk)
	if len(rOk.MethodResponses) != 1 || rOk.MethodResponses[0].Name != "Mailbox/set" {
		t.Fatalf("expected successful Mailbox/set, got %v", rOk.MethodResponses)
	}

	// 2. Creating 1 + updating 1 + destroying 1 = 3 objects (exceeds maxObjectsInSet: 2) -> requestTooLarge
	callsTooLarge := []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"m3": map[string]any{"name": "Box3"},
			},
			"update": map[string]any{
				"m1": map[string]any{"name": "Box1 Renamed"},
			},
			"destroy": []any{"m2"},
		}, "c2"},
	}
	rErr := postJMAP(t, ts.URL, using, callsTooLarge)
	if len(rErr.MethodResponses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(rErr.MethodResponses))
	}
	resp := rErr.MethodResponses[0]
	if resp.Name != "error" {
		t.Fatalf("expected method response 'error', got %q", resp.Name)
	}
	errType, _ := resp.Args["type"].(string)
	if errType != jmap.MethodErrorRequestTooLarge {
		t.Errorf("expected error type %q, got %q", jmap.MethodErrorRequestTooLarge, errType)
	}
}

// TestRFC8620_Section3_7_NestedResultReferences tests result references in nested objects,
// query filter conditions, and patch updates per RFC 8620 Section 3.7.
func TestRFC8620_Section3_7_NestedResultReferences(t *testing.T) {
	spectest.Require(t, "RFC 8620", "3.7", "MUST", "A result reference may also be used as the value of a property inside an object in an argument")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Seed an email
	em, err := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Subject:    "Patch Target",
	})
	if err != nil {
		t.Fatalf("failed to create email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Nested reference in query filter condition:
	// c1: Mailbox/query -> ids
	// c2: Email/query with nested #inMailbox inside filter.conditions[0]
	callsQuery := []any{
		[]any{"Mailbox/query", map[string]any{
			"accountId": "primary",
		}, "c1"},
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"operator": "AND",
				"conditions": []any{
					map[string]any{
						"#inMailbox": map[string]any{
							"resultOf": "c1",
							"name":     "Mailbox/query",
							"path":     "/ids/0",
						},
					},
				},
			},
		}, "c2"},
	}
	rQuery := postJMAP(t, ts.URL, using, callsQuery)
	if len(rQuery.MethodResponses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(rQuery.MethodResponses))
	}
	eqResp := rQuery.MethodResponses[1]
	if eqResp.Name != "Email/query" {
		t.Fatalf("expected Email/query response, got %q: %v", eqResp.Name, eqResp.Args)
	}
	idsList, _ := eqResp.Args["ids"].([]any)
	if len(idsList) == 0 {
		t.Errorf("expected Email/query to find seeded email in referenced mailbox, got 0")
	}

	// 2. Nested reference in patch update:
	// c3: Email/query -> get the email ID
	// c4: Email/set updating keywords/$flagged using a nested result reference pointing to /canCalculateChanges (bool)
	callsPatch := []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
		}, "c3"},
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				string(em.ID): map[string]any{
					"#keywords/$flagged": map[string]any{
						"resultOf": "c3",
						"name":     "Email/query",
						"path":     "/canCalculateChanges",
					},
				},
			},
		}, "c4"},
	}
	rPatch := postJMAP(t, ts.URL, using, callsPatch)
	if len(rPatch.MethodResponses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(rPatch.MethodResponses))
	}
	setResp := rPatch.MethodResponses[1]
	if setResp.Name != "Email/set" {
		t.Fatalf("expected Email/set response, got %q: %v", setResp.Name, setResp.Args)
	}
	updatedMap, _ := setResp.Args["updated"].(map[string]any)
	if _, ok := updatedMap[string(em.ID)]; !ok {
		t.Fatalf("email %s should have been updated via nested result reference patch, got: %v", em.ID, setResp.Args)
	}

	// 3. Duplicate property: given in both normal and # form returns invalidArguments
	callsDup := []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				string(em.ID): map[string]any{
					"keywords/$flagged": true,
					"#keywords/$flagged": map[string]any{
						"resultOf": "c3",
						"name":     "Email/query",
						"path":     "/canCalculateChanges",
					},
				},
			},
		}, "c5"},
	}
	rDup := postJMAP(t, ts.URL, using, callsDup)
	if len(rDup.MethodResponses) != 1 || rDup.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error response for duplicate property, got %v", rDup.MethodResponses)
	}
	dupErrType, _ := rDup.MethodResponses[0].Args["type"].(string)
	if dupErrType != jmap.MethodErrorInvalidArguments {
		t.Errorf("expected invalidArguments for duplicate property, got %q", dupErrType)
	}
}

// TestRFC8887_WebSocket_LimitEnforcement tests that WebSocket connections enforce
// maxCallsInRequest and maxSizeRequest per RFC 8887 Section 4.3.4.
func TestRFC8887_WebSocket_LimitEnforcement(t *testing.T) {
	customLimits := jmap.CoreCapability{
		MaxSizeUpload:         jmap.DefaultMaxSizeUpload,
		MaxConcurrentUpload:   jmap.DefaultMaxConcurrentUpload,
		MaxSizeRequest:        200, // max 200 octets
		MaxConcurrentRequests: jmap.DefaultMaxConcurrentRequests,
		MaxCallsInRequest:     2, // max 2 calls
		MaxObjectsInGet:       jmap.DefaultMaxObjectsInGet,
		MaxObjectsInSet:       jmap.DefaultMaxObjectsInSet,
		CollationAlgorithms:   []string{"i;ascii-casemap", "i;octet"},
	}

	srv := newTestServer(jmap.WithCoreCapability(customLimits))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/jmap/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"jmap"},
		HTTPHeader:   basicAuthHeader(),
	})
	if err != nil {
		t.Fatalf("WebSocket handshake failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// 1. Exceeding maxCallsInRequest over WebSocket
	threeCallsReq := map[string]any{
		"id":    "req-1",
		"using": []string{jmap.CoreCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"a": 1}, "c1"},
			[]any{"Core/echo", map[string]any{"b": 2}, "c2"},
			[]any{"Core/echo", map[string]any{"c": 3}, "c3"},
		},
	}
	tcData, _ := json.Marshal(threeCallsReq)
	if err := conn.Write(ctx, websocket.MessageText, tcData); err != nil {
		t.Fatalf("conn.Write failed: %v", err)
	}

	_, respBytes, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read failed: %v", err)
	}
	var wsErr map[string]any
	if err := json.Unmarshal(respBytes, &wsErr); err != nil {
		t.Fatalf("failed to unmarshal ws error: %v", err)
	}
	if wsErr["@type"] != "RequestError" {
		t.Errorf("expected @type RequestError, got %v", wsErr["@type"])
	}
	if wsErr["type"] != jmap.ErrorLimit {
		t.Errorf("expected type %q, got %v", jmap.ErrorLimit, wsErr["type"])
	}
	if wsErr["limit"] != "maxCallsInRequest" {
		t.Errorf("expected limit 'maxCallsInRequest', got %v", wsErr["limit"])
	}

	// 2. Exceeding maxSizeRequest over WebSocket
	largeReq := map[string]any{
		"id":      "req-2",
		"using":   []string{jmap.CoreCapabilityURI},
		"padding": fmt.Sprintf("%0300d", 1),
	}
	lData, _ := json.Marshal(largeReq)
	if err := conn.Write(ctx, websocket.MessageText, lData); err != nil {
		t.Fatalf("conn.Write failed: %v", err)
	}

	_, lRespBytes, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("conn.Read failed: %v", err)
	}
	var wsSizeErr map[string]any
	if err := json.Unmarshal(lRespBytes, &wsSizeErr); err != nil {
		t.Fatalf("failed to unmarshal ws size error: %v", err)
	}
	if wsSizeErr["@type"] != "RequestError" {
		t.Errorf("expected @type RequestError, got %v", wsSizeErr["@type"])
	}
	if wsSizeErr["type"] != jmap.ErrorLimit {
		t.Errorf("expected type %q, got %v", jmap.ErrorLimit, wsSizeErr["type"])
	}
	if wsSizeErr["limit"] != "maxSizeRequest" {
		t.Errorf("expected limit 'maxSizeRequest', got %v", wsSizeErr["limit"])
	}
}
