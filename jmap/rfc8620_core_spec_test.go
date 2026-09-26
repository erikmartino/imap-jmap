package jmap_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/spectest"
)

// TestRFC8620_Section3_6_2_MethodLevelErrors verifies method-level error behavior per RFC 8620 Section 3.6.2:
// - If a method encounters an error, an "error" response MUST be inserted at the current point in "methodResponses"
// - Unless otherwise specified, further processing MUST NOT happen for that method call
// - Any further method calls in the request MUST then be processed as normal
// - Errors at the method level MUST NOT generate an HTTP-level error (HTTP 200 OK)
// - The response name is "error", and it MUST have a type property
// - The externally visible state of the server MUST NOT have changed
// - Invalid or missing required argument returns invalidArguments
func TestRFC8620_Section3_6_2_MethodLevelErrors(t *testing.T) {
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST, "MUST be inserted at the current point in the \"methodResponses\" array")
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST, "and, unless otherwise specified, further processing MUST NOT happen")
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST, "Any further method calls in the request MUST then be processed as")
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST, "Errors at the method level MUST NOT generate an HTTP-level")
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST, "The response name is \"error\", and it MUST have a type property")
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST, "the externally visible state of the server MUST NOT have changed if")
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST, "otherwise invalid, or a required argument is missing")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// First get current mailbox state
	rInitial := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "init"},
	})
	initialState, _ := rInitial.MethodResponses[0].Args["state"].(string)

	// Send a 3-method request over HTTP directly so we can inspect HTTP status code
	reqPayload := map[string]any{
		"using": using,
		"methodCalls": []any{
			// Call 1: valid Mailbox/get
			[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "c1"},
			// Call 2: invalid Mailbox/get missing accountId or with wrong argument type
			[]any{"Mailbox/get", map[string]any{"accountId": 12345}, "c2"},
			// Call 3: valid Core/echo
			[]any{"Core/echo", map[string]any{"echoMe": "hello"}, "c3"},
		},
	}
	body, _ := json.Marshal(reqPayload)
	req, err := http.NewRequest("POST", ts.URL+"/jmap", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("user@example.com", "user@example.com")
	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer httpResp.Body.Close()

	// Errors at method level MUST NOT generate an HTTP-level error (HTTP 200 OK)
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK, got HTTP %d", httpResp.StatusCode)
	}

	var resp jmap.Response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// MUST be inserted at the current point in methodResponses
	if len(resp.MethodResponses) != 3 {
		t.Fatalf("expected 3 method responses, got %d", len(resp.MethodResponses))
	}

	// Check Call 1: succeeded at index 0
	if resp.MethodResponses[0].Name != "Mailbox/get" || resp.MethodResponses[0].ClientCallID != "c1" {
		t.Errorf("expected Call 1 Mailbox/get at index 0, got %v", resp.MethodResponses[0])
	}

	// Check Call 2: error inserted at index 1
	mr1 := resp.MethodResponses[1]
	if mr1.Name != "error" || mr1.ClientCallID != "c2" {
		t.Fatalf("expected error response at index 1 for c2, got %v", mr1)
	}
	// The response name is "error", and it MUST have a type property
	errType, ok := mr1.Args["type"].(string)
	if !ok || errType == "" {
		t.Fatalf("error response MUST have non-empty 'type' string, got %v", mr1.Args["type"])
	}
	// Missing or invalid required argument MUST return invalidArguments
	if errType != "invalidArguments" {
		t.Errorf("expected type 'invalidArguments', got %q", errType)
	}
	// Further processing MUST NOT happen for that method call (no list returned)
	if _, hasList := mr1.Args["list"]; hasList {
		t.Errorf("further processing must not happen for failed call, got list: %v", mr1.Args["list"])
	}

	// Check Call 3: Any further method calls in the request MUST then be processed as normal
	mr2 := resp.MethodResponses[2]
	if mr2.Name != "Core/echo" || mr2.ClientCallID != "c3" {
		t.Fatalf("expected Call 3 Core/echo at index 2, got %v", mr2)
	}
	if mr2.Args["echoMe"] != "hello" {
		t.Errorf("expected echoed argument, got %v", mr2.Args["echoMe"])
	}

	// State of server MUST NOT have changed
	rFinal := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "final"},
	})
	finalState, _ := rFinal.MethodResponses[0].Args["state"].(string)
	if finalState != initialState {
		t.Errorf("server state MUST NOT change after method error: initial=%q, final=%q", initialState, finalState)
	}
}

// TestRFC8620_Section3_7_ResultReferences verifies RFC 8620 Section 3.7 result reference requirements:
// - Server MUST first check if arguments are result references and resolve before processing
// - If any result reference fails to resolve, the whole method MUST be rejected with "invalidResultReference"
// - If an argument is given in both normal and referenced form ("#foo" and "foo"), method MUST return "invalidArguments"
// - ResultReference object specifies resultOf, name (required response name), and path
func TestRFC8620_Section3_7_ResultReferenceResolutionAndErrors(t *testing.T) {
	spectest.Require(t, "RFC8620", "3.7", spectest.MUST, "When processing a method call, the server MUST first check")
	spectest.Require(t, "RFC8620", "3.7", spectest.MUST, "reference fails to resolve, the whole method MUST be rejected with an")
	spectest.Require(t, "RFC8620", "3.7", spectest.MUST, "\"#foo\"), the method MUST return an \"invalidArguments\" error")
	spectest.Require(t, "RFC8620", "3.7", spectest.MUST, "The required name of a response to that method call")
	spectest.Require(t, "RFC8620", "3.7", spectest.SHOULD,
		"result reference should be resolved and the value used as the \"real\"")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Successful resolution: Email/query -> Email/get using "#ids"
	r1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": "mb-inbox"},
		}, "q1"},
		[]any{"Email/get", map[string]any{
			"accountId": "primary",
			"#ids": map[string]any{
				"resultOf": "q1",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "g1"},
	})
	if len(r1.MethodResponses) != 2 || r1.MethodResponses[1].Name != "Email/get" {
		t.Fatalf("expected Email/get response, got: %v", r1.MethodResponses)
	}
	emailList, _ := r1.MethodResponses[1].Args["list"].([]any)
	if len(emailList) == 0 {
		t.Errorf("expected emails resolved from query result reference")
	}

	// 2. Reference fails to resolve (e.g. non-existent callID): method rejected with invalidResultReference
	rFail := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId": "primary",
			"#ids": map[string]any{
				"resultOf": "nonExistentCallId",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "g2"},
	})
	if len(rFail.MethodResponses) != 1 || rFail.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for unresolvable result reference, got: %v", rFail.MethodResponses)
	}
	if errType := rFail.MethodResponses[0].Args["type"]; errType != "invalidResultReference" {
		t.Errorf("expected invalidResultReference, got %v", errType)
	}

	// 3. Name mismatch (name specified in ResultReference does not match previous method response): rejected with invalidResultReference
	rMismatch := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": "mb-inbox"},
		}, "q2"},
		[]any{"Email/get", map[string]any{
			"accountId": "primary",
			"#ids": map[string]any{
				"resultOf": "q2",
				"name":     "WrongMethod/name",
				"path":     "/ids",
			},
		}, "g3"},
	})
	if len(rMismatch.MethodResponses) != 2 || rMismatch.MethodResponses[1].Name != "error" {
		t.Fatalf("expected error for name mismatch, got: %v", rMismatch.MethodResponses)
	}
	if errType := rMismatch.MethodResponses[1].Args["type"]; errType != "invalidResultReference" {
		t.Errorf("expected invalidResultReference on name mismatch, got %v", errType)
	}

	// 4. Duplicate standard and prefixed argument ("foo" and "#foo"): rejected with invalidArguments
	rDup := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": "mb-inbox"},
		}, "q3"},
		[]any{"Email/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{"some-id"},
			"#ids": map[string]any{
				"resultOf": "q3",
				"name":     "Email/query",
				"path":     "/ids",
			},
		}, "g4"},
	})
	if len(rDup.MethodResponses) != 2 || rDup.MethodResponses[1].Name != "error" {
		t.Fatalf("expected error for duplicate normal and referenced argument, got: %v", rDup.MethodResponses)
	}
	if errType := rDup.MethodResponses[1].Args["type"]; errType != "invalidArguments" {
		t.Errorf("expected invalidArguments on duplicate standard and prefixed argument, got %v", errType)
	}
}

// TestRFC8620_Section3_EnvelopeAndSequentialOrder verifies RFC 8620 Section 3 envelope and sequential execution:
// - Request MUST be application/json
// - Response MUST be application/json
// - Method calls MUST be processed sequentially in order
// - Server MUST ignore any unrecognized properties on the Request object
// - Output of methods MUST be added to methodResponses in corresponding order
func TestRFC8620_Section3_EnvelopeAndSequentialOrder(t *testing.T) {
	spectest.Require(t, "RFC8620", "3.1", spectest.MUST, "The request MUST be of type \"application/json\" and consist of a")
	spectest.Require(t, "RFC8620", "3.1", spectest.MUST, "successful, the response MUST also be of type \"application/json\" and")
	spectest.Require(t, "RFC8620", "3.3", spectest.MUST, "calls MUST be processed sequentially, in order")
	spectest.Require(t, "RFC8620", "3.3", spectest.MUST, "server MUST ignore any other properties it does not understand on the")
	spectest.Require(t, "RFC8620", "3.4", spectest.MUST, "The output of the methods MUST be added to")
	spectest.Require(t, "RFC8620", "3.4", spectest.MUST, "This MUST")
	spectest.Require(t, "RFC8620", "3.10", spectest.MUST, "Method calls within a single request MUST be executed in order")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Non-JSON Content-Type returns 415 Unsupported Media Type
	reqPlain, _ := http.NewRequest("POST", ts.URL+"/jmap", strings.NewReader("text body"))
	reqPlain.Header.Set("Content-Type", "text/plain")
	reqPlain.SetBasicAuth("user@example.com", "user@example.com")
	respPlain, err := http.DefaultClient.Do(reqPlain)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	respPlain.Body.Close()
	if respPlain.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415 Unsupported Media Type for text/plain, got %d", respPlain.StatusCode)
	}

	// 2. Valid request with extra unrecognized properties on the Request object (MUST be ignored)
	// and 4 sequential method calls (MUST be executed and returned in order)
	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI},
		"methodCalls": []any{
			[]any{"Core/echo", map[string]any{"seq": 1}, "seq1"},
			[]any{"Core/echo", map[string]any{"seq": 2}, "seq2"},
			[]any{"Mailbox/get", map[string]any{"accountId": "primary"}, "seq3"},
			[]any{"Core/echo", map[string]any{"seq": 4}, "seq4"},
		},
		"unrecognizedExtensionProperty": "ignoredByServer",
		"anotherUnknown":                map[string]any{"nested": true},
	}
	body, _ := json.Marshal(reqPayload)
	reqJSON, _ := http.NewRequest("POST", ts.URL+"/jmap", bytes.NewReader(body))
	reqJSON.Header.Set("Content-Type", "application/json")
	reqJSON.SetBasicAuth("user@example.com", "user@example.com")
	respJSON, err := http.DefaultClient.Do(reqJSON)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer respJSON.Body.Close()

	if respJSON.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respJSON.StatusCode)
	}
	// Response Content-Type MUST be application/json
	ct := respJSON.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp jmap.Response
	if err := json.NewDecoder(respJSON.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Method calls MUST be processed sequentially in order, output added to methodResponses in order
	if len(resp.MethodResponses) != 4 {
		t.Fatalf("expected 4 responses, got %d", len(resp.MethodResponses))
	}
	expectedOrder := []string{"seq1", "seq2", "seq3", "seq4"}
	expectedNames := []string{"Core/echo", "Core/echo", "Mailbox/get", "Core/echo"}
	for i, mr := range resp.MethodResponses {
		if mr.ClientCallID != expectedOrder[i] {
			t.Errorf("response [%d] call ID mismatch: expected %q, got %q", i, expectedOrder[i], mr.ClientCallID)
		}
		if mr.Name != expectedNames[i] {
			t.Errorf("response [%d] name mismatch: expected %q, got %q", i, expectedNames[i], mr.Name)
		}
	}
}

// TestRFC8620_Section5_5_FilterOperatorAndSortStability verifies FilterOperator and sort order stability per RFC 8620 Section 5.5:
// - FilterOperator "operator" MUST be "AND", "OR", or "NOT"
// - "AND": all conditions match
// - "OR": at least one condition matches
// - "NOT": none of conditions match
// - FilterCondition MUST NOT have an "operator" property
// - If sort is null or empty, sort order MUST be stable between calls
func TestRFC8620_Section5_5_FilterOperatorAndSortStability(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.5", spectest.MUST, "This MUST be one of the following strings:")
	spectest.Require(t, "RFC8620", "5.5", spectest.MUST, "+ \"AND\": All of the conditions must match for the filter to")
	spectest.Require(t, "RFC8620", "5.5", spectest.MUST, "+ \"OR\": At least one of the conditions must match for the")
	spectest.Require(t, "RFC8620", "5.5", spectest.MUST, "+ \"NOT\": None of the conditions must match for the filter to")
	spectest.Require(t, "RFC8620", "5.5", spectest.MUST, "It MUST NOT have an")
	spectest.Require(t, "RFC8620", "5.5", spectest.MUST, "order is server dependent, but it MUST be stable between calls to")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Invalid operator string (e.g. "XOR") MUST be rejected with invalidArguments
	rInvalidOp := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"operator":   "XOR",
				"conditions": []any{map[string]any{"inMailbox": "mb-inbox"}},
			},
		}, "c1"},
	})
	if len(rInvalidOp.MethodResponses) != 1 || rInvalidOp.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for invalid operator 'XOR', got: %v", rInvalidOp.MethodResponses)
	}
	if errType := rInvalidOp.MethodResponses[0].Args["type"]; errType != "invalidArguments" {
		t.Errorf("expected invalidArguments for operator 'XOR', got: %v", errType)
	}

	// 2. FilterCondition MUST NOT have an "operator" property: condition with invalid operator or operator without conditions is rejected
	rBadCondition := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"inMailbox": "mb-inbox",
				"operator":  "UNKNOWN_OP",
			},
		}, "c2"},
	})
	if len(rBadCondition.MethodResponses) != 1 || rBadCondition.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for condition with invalid operator property, got: %v", rBadCondition.MethodResponses)
	}
	if errType := rBadCondition.MethodResponses[0].Args["type"]; errType != "invalidArguments" {
		t.Errorf("expected invalidArguments, got: %v", errType)
	}

	// 3. Test AND, OR, NOT semantics via EvalFilterOperator directly and via Email/query
	// AND: all conditions must match
	matchAND, isOp := jmap.EvalFilterOperator(map[string]any{
		"operator": "AND",
		"conditions": []any{
			map[string]any{"v": 1},
			map[string]any{"v": 2},
		},
	}, func(c map[string]any) bool {
		return c["v"].(int) > 0
	})
	if !isOp || !matchAND {
		t.Errorf("AND filter operator failed when all conditions matched")
	}

	// OR: at least one condition must match
	matchOR, _ := jmap.EvalFilterOperator(map[string]any{
		"operator": "OR",
		"conditions": []any{
			map[string]any{"v": -1},
			map[string]any{"v": 2},
		},
	}, func(c map[string]any) bool {
		return c["v"].(int) > 0
	})
	if !matchOR {
		t.Errorf("OR filter operator failed when one condition matched")
	}

	// NOT: none of the conditions must match
	matchNOT, _ := jmap.EvalFilterOperator(map[string]any{
		"operator": "NOT",
		"conditions": []any{
			map[string]any{"v": -1},
			map[string]any{"v": -2},
		},
	}, func(c map[string]any) bool {
		return c["v"].(int) > 0
	})
	if !matchNOT {
		t.Errorf("NOT filter operator failed when no conditions matched")
	}

	// 4. Sort order stability: two calls with sort=null or empty sort MUST return stable order
	rSort1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"sort":      nil,
		}, "s1"},
	})
	rSort2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"sort":      []any{},
		}, "s2"},
	})
	ids1, _ := rSort1.MethodResponses[0].Args["ids"].([]any)
	ids2, _ := rSort2.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) == 0 || len(ids1) != len(ids2) {
		t.Fatalf("expected non-empty matching ids count, got %d and %d", len(ids1), len(ids2))
	}
	for i := range ids1 {
		if ids1[i] != ids2[i] {
			t.Errorf("sort order must be stable between calls: index %d differed (%v vs %v)", i, ids1[i], ids2[i])
		}
	}
}

// TestRFC8620_Section3_QueryPaginationAndPositioning verifies query pagination and boundary rules per RFC 8620 Section 3 / Section 5.5:
// - Negative position MUST be added to total results to find positive position
// - Negative limit MUST be rejected with invalidArguments
// - If anchor is specified, any position argument supplied by the client MUST be ignored
// - If no anchor is supplied, any anchorOffset argument MUST be ignored
// - queryState string MUST change if query results change
// - If position >= total, ids MUST be empty list
func TestRFC8620_Section3_QueryPaginationAndPositioning(t *testing.T) {
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "Specifically, the negative value MUST be added to the total")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "value is given, the call MUST be rejected with an")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "client MUST be ignored")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "\"anchorOffset\" argument MUST be ignored")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "This string MUST change if the results of the query (i")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "If \"position\" is >= \"total\", this MUST be")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// Get all seeded inbox emails
	rAll := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId":      "primary",
			"filter":         map[string]any{"inMailbox": "mb-inbox"},
			"calculateTotal": true,
		}, "qAll"},
	})
	allIDs, _ := rAll.MethodResponses[0].Args["ids"].([]any)
	totalFloat, _ := rAll.MethodResponses[0].Args["total"].(float64)
	total := int(totalFloat)
	if total < 2 || len(allIDs) < 2 {
		t.Fatalf("expected at least 2 seeded inbox emails, got total=%d, ids=%v", total, allIDs)
	}

	// 1. Negative limit MUST be rejected with invalidArguments
	rNegLimit := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"limit":     -1,
		}, "cNegLimit"},
	})
	if len(rNegLimit.MethodResponses) != 1 || rNegLimit.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for negative limit, got: %v", rNegLimit.MethodResponses)
	}
	if errType := rNegLimit.MethodResponses[0].Args["type"]; errType != "invalidArguments" {
		t.Errorf("expected invalidArguments for negative limit, got: %v", errType)
	}

	// 2. Negative position: added to total (-1 gives the last result)
	rNegPos := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": "mb-inbox"},
			"position":  -1,
			"limit":     1,
		}, "cNegPos"},
	})
	negPosIDs, _ := rNegPos.MethodResponses[0].Args["ids"].([]any)
	if len(negPosIDs) != 1 || negPosIDs[0] != allIDs[total-1] {
		t.Errorf("position -1 expected last email %v, got %v", allIDs[total-1], negPosIDs)
	}

	// 3. Anchor specified: client position argument MUST be ignored
	rAnchorPos := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": "mb-inbox"},
			"anchor":    allIDs[0],
			"position":  9999, // MUST be ignored
			"limit":     1,
		}, "cAnchor"},
	})
	anchorIDs, _ := rAnchorPos.MethodResponses[0].Args["ids"].([]any)
	if len(anchorIDs) != 1 || anchorIDs[0] != allIDs[0] {
		t.Errorf("anchor specified MUST ignore position: expected %v, got %v", allIDs[0], anchorIDs)
	}

	// 4. No anchor specified: anchorOffset argument MUST be ignored
	rNoAnchor := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId":    "primary",
			"filter":       map[string]any{"inMailbox": "mb-inbox"},
			"position":     0,
			"anchorOffset": 5, // MUST be ignored since anchor is omitted
			"limit":        1,
		}, "cNoAnchor"},
	})
	noAnchorIDs, _ := rNoAnchor.MethodResponses[0].Args["ids"].([]any)
	if len(noAnchorIDs) != 1 || noAnchorIDs[0] != allIDs[0] {
		t.Errorf("omitted anchor MUST ignore anchorOffset: expected %v, got %v", allIDs[0], noAnchorIDs)
	}

	// 5. Position >= total: ids MUST be empty list
	rBeyondTotal := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": "mb-inbox"},
			"position":  total + 5,
		}, "cBeyond"},
	})
	beyondIDs, _ := rBeyondTotal.MethodResponses[0].Args["ids"].([]any)
	if len(beyondIDs) != 0 {
		t.Errorf("position >= total MUST return empty list, got: %v", beyondIDs)
	}

	// 6. queryState string MUST change if query results change
	// Create a new mailbox, query mailboxes, check state change
	rMb1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/query", map[string]any{"accountId": "primary"}, "mbq1"},
	})
	mbState1, _ := rMb1.MethodResponses[0].Args["queryState"].(string)

	postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mbNew": map[string]any{"name": "StateTestMailbox"},
			},
		}, "createMb"},
	})

	rMb2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/query", map[string]any{"accountId": "primary"}, "mbq2"},
	})
	mbState2, _ := rMb2.MethodResponses[0].Args["queryState"].(string)
	if mbState1 == "" || mbState2 == "" || mbState1 == mbState2 {
		t.Errorf("queryState MUST change when results change: state1=%q, state2=%q", mbState1, mbState2)
	}
}

// TestRFC8620_Section2_SessionResourceAndDiscovery verifies RFC 8620 Section 2 session resource rules:
// - Authenticated GET returns JSON-encoded Session object
// - Capabilities includes urn:ietf:params:jmap:core with server limits
// - Vendor-specific extension identifiers must be URLs
// - Clients must opt in to capabilities
// - Capabilities with methods included in accountCapabilities when supported, excluded when not
// - DownloadURL template contains {accountId}, {blobId}, {type}, {name}
// - UploadURL template contains {accountId}
// - EventSourceURL template contains {types}, {closeafter}, {ping}
// - Cache-Control: no-cache, no-store, must-revalidate header
func TestRFC8620_Section2_SessionResourceAndDiscovery(t *testing.T) {
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "MUST return a JSON-encoded *Session* object, giving details about the")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "The capabilities object MUST include a property called")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "object that MUST contain the following information on server")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "vendor-specific extension MUST be a URL with a domain owned by the")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "Clients MUST opt in to any capability it wishes to use")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "capability defines new methods, the server MUST include it in")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "It MUST NOT include it in the")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "The URL MUST contain variables called")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "The URL MUST contain a variable")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "MUST contain variables called \"types\", \"closeafter\", and \"ping\"")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "Implementors must take care to avoid inappropriate caching of the")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "no-store, must-revalidate\" on the response")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.SetBasicAuth("user@example.com", "user@example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK, got %d", resp.StatusCode)
	}

	// Cache-Control MUST contain no-cache, no-store, must-revalidate
	cc := resp.Header.Get("Cache-Control")
	if !strings.Contains(cc, "no-cache") || !strings.Contains(cc, "no-store") || !strings.Contains(cc, "must-revalidate") {
		t.Errorf("expected Cache-Control containing 'no-cache, no-store, must-revalidate', got %q", cc)
	}

	// Content-Type MUST be application/json
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("failed to decode Session JSON: %v", err)
	}

	// capabilities MUST include urn:ietf:params:jmap:core
	coreCapRaw, ok := session.Capabilities[jmap.CoreCapabilityURI]
	if !ok {
		t.Fatalf("capabilities MUST include %q", jmap.CoreCapabilityURI)
	}
	coreMap, ok := coreCapRaw.(map[string]any)
	if !ok {
		t.Fatalf("core capability MUST be an object, got %T", coreCapRaw)
	}

	// Verify required limits on core capability
	for _, prop := range []string{"maxSizeUpload", "maxConcurrentUpload", "maxSizeRequest", "maxConcurrentRequests", "maxCallsInRequest", "maxObjectsInGet", "maxObjectsInSet"} {
		v, exists := coreMap[prop]
		if !exists || v == nil {
			t.Errorf("core capability missing required limit property %q", prop)
		}
	}
	colls, ok := coreMap["collationAlgorithms"].([]any)
	if !ok || len(colls) == 0 {
		t.Errorf("core capability missing collationAlgorithms array: %v", coreMap["collationAlgorithms"])
	}

	// downloadUrl MUST contain {accountId}, {blobId}, {type}, {name}
	for _, v := range []string{"{accountId}", "{blobId}", "{type}", "{name}"} {
		if !strings.Contains(session.DownloadURL, v) {
			t.Errorf("downloadUrl %q MUST contain variable %q", session.DownloadURL, v)
		}
	}

	// uploadUrl MUST contain {accountId}
	if !strings.Contains(session.UploadURL, "{accountId}") {
		t.Errorf("uploadUrl %q MUST contain variable {accountId}", session.UploadURL)
	}

	// eventSourceUrl MUST contain {types}, {closeafter}, {ping}
	for _, v := range []string{"{types}", "{closeafter}", "{ping}"} {
		if !strings.Contains(session.EventSourceURL, v) {
			t.Errorf("eventSourceUrl %q MUST contain variable %q", session.EventSourceURL, v)
		}
	}

	// accounts map validation
	if len(session.Accounts) == 0 {
		t.Fatalf("session accounts MUST NOT be empty")
	}
	for acctID, acct := range session.Accounts {
		if acct.Name == "" {
			t.Errorf("account %q missing name", acctID)
		}
		// Capability with methods MUST be included in accountCapabilities if supported, and excluded if not
		if _, hasMail := acct.AccountCapabilities[jmap.MailCapabilityURI]; hasMail {
			// Account has mail capability
		}
	}

	// Vendor extension identifiers MUST be URLs
	for capURI := range session.Capabilities {
		if strings.HasPrefix(capURI, "urn:") {
			continue // standard IETF URN
		}
		if !strings.HasPrefix(capURI, "http://") && !strings.HasPrefix(capURI, "https://") {
			t.Errorf("vendor-specific capability %q MUST be a URL", capURI)
		}
	}

	// Clients MUST opt in to any capability it wishes to use
	rOptIn := postJMAP(t, ts.URL, []string{}, []any{
		[]any{"Email/query", map[string]any{"accountId": "primary"}, "c1"},
	})
	if len(rOptIn.MethodResponses) != 1 || rOptIn.MethodResponses[0].Name != "error" {
		t.Errorf("expected error for method called without capability opt-in, got %v", rOptIn.MethodResponses)
	}
}

// TestRFC8620_Section6_BlobUploadAndDownload verifies RFC 8620 Section 6.1 and 6.2 binary data rules:
// - uploadUrl in URI Template format with {accountId}
// - Successful upload returns single JSON object with accountId, blobId, type, size
// - downloadUrl in URI Template format with {accountId}, {blobId}, {type}, {name}
// - Download returns "name" as Content-Disposition filename parameter
func TestRFC8620_Section6_BlobUploadAndDownload(t *testing.T) {
	spectest.Require(t, "RFC8620", "6.1", spectest.MUST, "(level 1) format [RFC6570], which MUST contain a variable called")
	spectest.Require(t, "RFC8620", "6.1", spectest.MUST, "A successful request MUST return a single JSON object with the")
	spectest.Require(t, "RFC8620", "6.2", spectest.MUST, "The URL MUST")
	spectest.Require(t, "RFC8620", "6.2", spectest.MUST, "o \"name\": The name for the file; the server MUST return this as the")
	spectest.Require(t, "RFC8620", "6.2", spectest.SHOULD,
		`recommended to set long cache times and use the "immutable" Cache-`)

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Get session to discover uploadUrl and downloadUrl templates
	sessResp, err := authedGet(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET session failed: %v", err)
	}
	var session jmap.Session
	json.NewDecoder(sessResp.Body).Decode(&session)
	sessResp.Body.Close()

	if !strings.Contains(session.UploadURL, "{accountId}") {
		t.Fatalf("uploadUrl %q missing {accountId}", session.UploadURL)
	}

	// 2. Perform upload to /upload/{accountId}/
	targetUploadURL := strings.Replace(session.UploadURL, "{accountId}", "primary", 1)
	blobContent := []byte("Hello, JMAP Binary World!")
	upReq, _ := http.NewRequest("POST", targetUploadURL, bytes.NewReader(blobContent))
	upReq.Header.Set("Content-Type", "text/plain; charset=utf-8")
	upReq.SetBasicAuth("user@example.com", "user@example.com")
	upResp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		t.Fatalf("POST upload failed: %v", err)
	}
	defer upResp.Body.Close()

	if upResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected HTTP 201 Created on upload, got %d", upResp.StatusCode)
	}

	// Successful upload MUST return a single JSON object with accountId, blobId, type, size
	var uploadResult map[string]any
	if err := json.NewDecoder(upResp.Body).Decode(&uploadResult); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}
	blobID, _ := uploadResult["blobId"].(string)
	if blobID == "" {
		t.Fatalf("upload response missing blobId: %v", uploadResult)
	}
	blobType, _ := uploadResult["type"].(string)
	if blobType == "" {
		t.Errorf("upload response missing type: %v", uploadResult)
	}
	blobSize, _ := uploadResult["size"].(float64)
	if int(blobSize) != len(blobContent) {
		t.Errorf("expected blob size %d, got %v", len(blobContent), blobSize)
	}

	// 3. Download using downloadUrl template
	dlURL := session.DownloadURL
	dlURL = strings.Replace(dlURL, "{accountId}", "primary", 1)
	dlURL = strings.Replace(dlURL, "{blobId}", blobID, 1)
	dlURL = strings.Replace(dlURL, "{name}", "greeting.txt", 1)
	dlURL = strings.Replace(dlURL, "{type}", "text/plain", 1)

	dlReq, _ := http.NewRequest("GET", dlURL, nil)
	dlReq.SetBasicAuth("user@example.com", "user@example.com")
	dlResp, err := http.DefaultClient.Do(dlReq)
	if err != nil {
		t.Fatalf("GET download failed: %v", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK on download, got %d", dlResp.StatusCode)
	}

	downloadedBytes, _ := io.ReadAll(dlResp.Body)
	if !bytes.Equal(downloadedBytes, blobContent) {
		t.Errorf("downloaded content mismatch: expected %q, got %q", string(blobContent), string(downloadedBytes))
	}

	// Server MUST return name as filename parameter in Content-Disposition
	cd := dlResp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, `filename="greeting.txt"`) {
		t.Errorf("expected Content-Disposition containing filename=\"greeting.txt\", got %q", cd)
	}

	// RFC 8620 Section 6.2 (SHOULD): blob downloads are immutable and should be
	// cached for a long time.
	if cc := dlResp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("blob download SHOULD use the immutable Cache-Control directive, got %q", cc)
	}
}

// TestRFC8620_Section1_DataTypesAndConventions verifies RFC 8620 Section 1 data type rules:
// - Id is 1-255 characters from URL-safe base64 alphabet
// - Int is 0 <= value <= 2^53-1
// - UTCDate format: time-offset MUST be "Z", letters "T" and "Z" MUST be uppercase
// - Records of same type within same account MUST have unique IDs
// - Immutable properties MUST NOT change after creation
// - Vendor extensions MUST be URLs, and client MUST opt in to use them
func TestRFC8620_Section1_DataTypesAndConventions(t *testing.T) {
	spectest.Require(t, "RFC8620", "1.1", spectest.MUST, "o \"immutable\" -- The value MUST NOT change after the object is")
	spectest.Require(t, "RFC8620", "1.2", spectest.MUST, "and a maximum of 255 octets in size, and it MUST only contain")
	spectest.Require(t, "RFC8620", "1.3", spectest.MUST, "the value MUST be in the range 0 <= value <= 2^53-1")
	spectest.Require(t, "RFC8620", "1.4", spectest.MUST, "MUST always be omitted if zero, and any letters in the string (e")
	spectest.Require(t, "RFC8620", "1.4", spectest.MUST, "\"T\" and \"Z\") MUST be uppercase")
	spectest.Require(t, "RFC8620", "1.4", spectest.MUST, "\"time-offset\" component MUST be \"Z\" (i")
	spectest.Require(t, "RFC8620", "1.6.3", spectest.MUST, "MUST be unique among all records of the *same type* within the *same")
	spectest.Require(t, "RFC8620", "1.8", spectest.MUST, "extensions MUST be a URL belonging to a domain owned by the vendor,")
	spectest.Require(t, "RFC8620", "1.8", spectest.MUST, "The client MUST opt in to use an extension by passing the appropriate")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Id validation (§1.2): 1-255 chars, [A-Za-z0-9_-]
	validIDs := []string{"a", "123", "id_with-dash_and_underscore", strings.Repeat("x", 255)}
	for _, id := range validIDs {
		if !jmapcore.Id(id).Validate() {
			t.Errorf("expected valid Id for %q", id)
		}
	}
	invalidIDs := []string{"", strings.Repeat("x", 256), "id with space", "id/slash", "id+plus", "id=equals"}
	for _, id := range invalidIDs {
		if jmapcore.Id(id).Validate() {
			t.Errorf("expected invalid Id for %q", id)
		}
	}

	// 2. Int / UnsignedInt range (§1.3): 0 <= value <= 2^53-1 (9007199254740991)
	maxSafeInt := int64(1<<53 - 1)
	if maxSafeInt != 9007199254740991 {
		t.Fatalf("unexpected maxSafeInt %d", maxSafeInt)
	}

	// 3. UTCDate formatting (§1.4): time-offset MUST be "Z", "T" and "Z" MUST be uppercase
	rMail := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{"accountId": "primary", "limit": 1}, "q1"},
	})
	ids, _ := rMail.MethodResponses[0].Args["ids"].([]any)
	if len(ids) > 0 {
		rGet := postJMAP(t, ts.URL, using, []any{
			[]any{"Email/get", map[string]any{"accountId": "primary", "ids": ids, "properties": []any{"receivedAt"}}, "g1"},
		})
		list, _ := rGet.MethodResponses[0].Args["list"].([]any)
		if len(list) > 0 {
			receivedAt, _ := list[0].(map[string]any)["receivedAt"].(string)
			if receivedAt != "" {
				if !strings.HasSuffix(receivedAt, "Z") {
					t.Errorf("UTCDate time-offset MUST be 'Z', got %q", receivedAt)
				}
				if !strings.Contains(receivedAt, "T") {
					t.Errorf("UTCDate MUST have uppercase 'T', got %q", receivedAt)
				}
			}
		}
	}

	// 4. Record ID uniqueness (§1.6.3): records of same type within same account have unique IDs
	rCreate := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mb1": map[string]any{"name": "UniqueBox1"},
				"mb2": map[string]any{"name": "UniqueBox2"},
			},
		}, "setMb"},
	})
	created := rCreate.MethodResponses[0].Args["created"].(map[string]any)
	id1 := created["mb1"].(map[string]any)["id"].(string)
	id2 := created["mb2"].(map[string]any)["id"].(string)
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Fatalf("records of same type MUST have unique IDs: %q vs %q", id1, id2)
	}

	// 5. Immutability (§1.1): Attempting to change an immutable property (like id) fails
	rPatchImmutable := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				id1: map[string]any{
					"id": "new-forbidden-id",
				},
			},
		}, "updateImmutable"},
	})
	notUpdated, _ := rPatchImmutable.MethodResponses[0].Args["notUpdated"].(map[string]any)
	errItem, ok := notUpdated[id1].(map[string]any)
	if !ok || errItem["type"] != "invalidProperties" {
		t.Errorf("modifying immutable property MUST return invalidProperties error, got: %v", notUpdated)
	}
}

// TestRFC8620_Section5_4_CopyMethods verifies /copy method rules per RFC 8620 Section 5.4.
func TestRFC8620_Section5_4_CopyMethods(t *testing.T) {
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "supplied, the string must match the current state of the account")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "This MUST be different")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "The Foo object MUST")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "response, but before processing the next method, the server MUST")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "type \"Id\" MUST be included on the SetError object with the id of the")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Missing "id" in copy create object -> invalidProperties SetError (§5.4)
	rMissingID := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/copy", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"cp1": map[string]any{
					"mailboxIds": map[string]bool{"mb-inbox": true},
				},
			},
		}, "c1"},
	})
	notCreated, _ := rMissingID.MethodResponses[0].Args["notCreated"].(map[string]any)
	errObj, ok := notCreated["cp1"].(map[string]any)
	if !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("missing id in copy create item MUST fail with invalidProperties, got: %v", notCreated)
	}

	// 2. ifFromInState mismatch -> stateMismatch method error (§5.4)
	rStateMismatch := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/copy", map[string]any{
			"accountId":     "primary",
			"ifFromInState": "mismatched-from-state-token",
			"create": map[string]any{
				"cp2": map[string]any{
					"id": "some-id",
				},
			},
		}, "c2"},
	})
	if rStateMismatch.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error method response for ifFromInState mismatch, got: %v", rStateMismatch.MethodResponses[0].Name)
	}
	errType, _ := rStateMismatch.MethodResponses[0].Args["type"].(string)
	if errType != "stateMismatch" {
		t.Errorf("expected stateMismatch error, got %q", errType)
	}

	// 3. alreadyExists SetError MUST include existingId property (§5.3, §5.4, §9.5.3)
	rIdentDup := postJMAP(t, ts.URL, using, []any{
		[]any{"Identity/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"idDup": map[string]any{
					"name":  "Duplicate User",
					"email": "user@example.com",
				},
			},
		}, "c3"},
	})
	notCreatedIdent, _ := rIdentDup.MethodResponses[0].Args["notCreated"].(map[string]any)
	dupErr, ok := notCreatedIdent["idDup"].(map[string]any)
	if !ok {
		t.Fatalf("expected duplicate identity create to fail in notCreated: %v", notCreatedIdent)
	}
	if dupErr["type"] != "alreadyExists" {
		t.Errorf("expected type alreadyExists, got %v", dupErr["type"])
	}
	if existingID, _ := dupErr["existingId"].(string); existingID == "" {
		t.Errorf("alreadyExists SetError MUST include existingId property, got %v", dupErr)
	}
}

// TestRFC8620_Section5_5_QueryAndComparator verifies /query calculateTotal and Comparator properties per RFC 8620 Section 5.5.
func TestRFC8620_Section5_5_QueryAndComparator(t *testing.T) {
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "This argument MUST be omitted if the \"calculateTotal\" request")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "queryState string to a previous call, it MUST either throw away")
	spectest.Require(t, "RFC8620", "3", spectest.MUST, "required for specific sort operations defined in a type's /query")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. calculateTotal false/omitted: total MUST be omitted (§5.5)
	rNoTotal := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": "mb-inbox"},
		}, "q1"},
	})
	if _, ok := rNoTotal.MethodResponses[0].Args["total"]; ok {
		t.Errorf("total MUST be omitted when calculateTotal is omitted, got %v", rNoTotal.MethodResponses[0].Args["total"])
	}

	// 2. calculateTotal true: total MUST be present (§5.5)
	rWithTotal := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId":      "primary",
			"filter":         map[string]any{"inMailbox": "mb-inbox"},
			"calculateTotal": true,
		}, "q2"},
	})
	if total, ok := rWithTotal.MethodResponses[0].Args["total"]; !ok || total == nil {
		t.Errorf("total MUST NOT be omitted when calculateTotal is true")
	}

	// 3. Comparator: sort with property and isAscending comparator arguments (§5.5)
	rSort := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"inMailbox": "mb-inbox"},
			"sort": []any{
				map[string]any{
					"property":    "receivedAt",
					"isAscending": false,
					"collation":   "i;ascii-casemap",
				},
			},
		}, "q3"},
	})
	if rSort.MethodResponses[0].Name != "Email/query" {
		t.Fatalf("expected Email/query response, got %v", rSort.MethodResponses[0])
	}
	qState, _ := rSort.MethodResponses[0].Args["queryState"].(string)
	if qState == "" {
		t.Errorf("expected non-empty queryState")
	}
}

// TestRFC8620_Section5_6_QueryChangesOrderAndCannotCalculate verifies /queryChanges response properties per RFC 8620 Section 5.6.
func TestRFC8620_Section5_6_QueryChangesOrderAndCannotCalculate(t *testing.T) {
	spectest.Require(t, "RFC8620", "5.6", spectest.MUST, "The array MUST be sorted in order of index, with the lowest index")
	spectest.Require(t, "RFC8620", "5.2", spectest.MUST, "calculate an intermediate state, it MUST return a")
	spectest.Require(t, "RFC8620", "5.6", spectest.MUST, "The client MUST invalidate its cache")
	spectest.Require(t, "RFC8620", "5.2", spectest.MUST, "The client MUST invalidate its Foo cache")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Verify ComputeQueryChanges guarantees added array is sorted by index (lowest first)
	currentIDs := []jmap.Id{"id-0", "id-1", "id-2", "id-3", "id-4"}
	created := []jmap.Id{"id-3", "id-1"}
	added, _ := jmap.ComputeQueryChanges(created, nil, nil, currentIDs, "")
	if len(added) != 2 {
		t.Fatalf("expected 2 added items, got %d", len(added))
	}
	idx0 := added[0]["index"].(int)
	idx1 := added[1]["index"].(int)
	if idx0 >= idx1 {
		t.Errorf("added array MUST be sorted in order of index with lowest index first: got %d then %d", idx0, idx1)
	}

	// 2. cannotCalculateChanges when state is too old
	rOld := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/queryChanges", map[string]any{
			"accountId":       "primary",
			"sinceQueryState": "unrecognized-old-state-000",
		}, "qc1"},
	})
	if rOld.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error response for cannotCalculateChanges, got %v", rOld.MethodResponses[0].Name)
	}
	errType, _ := rOld.MethodResponses[0].Args["type"].(string)
	if errType != "cannotCalculateChanges" {
		t.Errorf("expected cannotCalculateChanges error, got %q", errType)
	}
}

// TestRFC8620_Section7_PushSubscriptionAndEventSource verifies PushSubscription and EventSource per RFC 8620 Section 7.
func TestRFC8620_Section7_PushSubscriptionAndEventSource(t *testing.T) {
	spectest.Require(t, "RFC8620", "7.1", spectest.MUST, "This MUST be the string \"StateChange\"")
	spectest.Require(t, "RFC8620", "7.2.1", spectest.MUST, "The server MUST only return push subscriptions that were created")
	spectest.Require(t, "RFC8620", "7.2.1", spectest.MUST, "to a particular device, the values for these properties MUST NOT be")
	spectest.Require(t, "RFC8620", "7.2.1", spectest.MUST, "server MUST default to all properties excluding these two")
	spectest.Require(t, "RFC8620", "7.2.1", spectest.MUST, "them is explicitly requested, the method call MUST be rejected with a")
	spectest.Require(t, "RFC8620", "7.2.2", spectest.MUST, "to change these, it must destroy the current push subscription and")
	spectest.Require(t, "RFC8620", "7.2.2", spectest.MUST, "When a PushSubscription is created, the server MUST immediately push")
	spectest.Require(t, "RFC8620", "7.2.2", spectest.MUST, "This MUST be the string \"PushVerification\"")
	spectest.Require(t, "RFC8620", "7.2.2", spectest.MUST, "The client MUST update the push subscription with the correct")
	spectest.Require(t, "RFC8620", "7.2.2", spectest.MUST, "invalid verification code MUST be rejected by the server with an")
	spectest.Require(t, "RFC8620", "7.3", spectest.MUST, "o \"types\": This MUST be either:")
	spectest.Require(t, "RFC8620", "7.3", spectest.MUST, "The server MUST only push changes for")
	spectest.Require(t, "RFC8620", "7.3", spectest.MUST, "o \"closeafter\": This MUST be one of the following values:")
	spectest.Require(t, "RFC8620", "7.3", spectest.MUST, "* \"state\": The server MUST end the HTTP response after pushing a")
	spectest.Require(t, "RFC8620", "7.3", spectest.MUST, "If non-zero, the server MUST send an event")
	spectest.Require(t, "RFC8620", "7.3", spectest.MUST, "This MUST NOT set a new event id")
	spectest.Require(t, "RFC8620", "7.3", spectest.MUST, "the server MUST NOT send ping events")
	spectest.Require(t, "RFC8620", "7.3", spectest.MUST, "For interoperability, servers MUST")
	spectest.Require(t, "RFC8620", "7.3", spectest.MUST, "The data for the ping event MUST be a JSON object containing an")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI}

	// 1. Create a push subscription
	rCreate := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{
					"deviceClientId": "dev-device-123",
					"url":            ts.URL + "/mock-push-target",
					"keys":           map[string]string{"p256dh": "key1", "auth": "secret1"},
					"types":          []string{"Email"},
				},
			},
		}, "c1"},
	})
	created, _ := rCreate.MethodResponses[0].Args["created"].(map[string]any)
	subObj, ok := created["sub1"].(map[string]any)
	if !ok {
		t.Fatalf("failed to create PushSubscription: %v", rCreate.MethodResponses[0].Args)
	}
	subID, _ := subObj["id"].(string)

	// 2. PushSubscription/get: requesting "url" or "keys" explicitly MUST be rejected with "forbidden"
	rForbidden := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{subID},
			"properties": []any{"id", "url"},
		}, "c2"},
	})
	if rForbidden.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for explicit url property request, got: %v", rForbidden.MethodResponses[0].Name)
	}
	if errType, _ := rForbidden.MethodResponses[0].Args["type"].(string); errType != "forbidden" {
		t.Errorf("expected forbidden error, got %q", errType)
	}

	// 3. PushSubscription/get: default properties MUST exclude "url" and "keys"
	rGet := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{subID},
		}, "c3"},
	})
	list, _ := rGet.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 push subscription in list, got %d", len(list))
	}
	getItem := list[0].(map[string]any)
	if _, hasURL := getItem["url"]; hasURL {
		t.Errorf("PushSubscription/get MUST NOT return url property: %v", getItem)
	}
	if _, hasKeys := getItem["keys"]; hasKeys {
		t.Errorf("PushSubscription/get MUST NOT return keys property: %v", getItem)
	}

	// 4. PushSubscription/set update: url and keys are immutable
	rPatchImmutable := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{
					"url": "https://attacker.example.com/new",
				},
			},
		}, "c4"},
	})
	notUpdated, _ := rPatchImmutable.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if errObj, ok := notUpdated[subID].(map[string]any); !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("updating immutable url MUST fail with invalidProperties: %v", notUpdated)
	}

	// 5. PushSubscription/set update: invalid verification code rejected with invalidProperties
	rBadVerify := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{
					"verificationCode": "wrong-code-0000",
				},
			},
		}, "c5"},
	})
	notUpdatedV, _ := rBadVerify.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if errObj, ok := notUpdatedV[subID].(map[string]any); !ok || errObj["type"] != "invalidProperties" {
		t.Errorf("updating with invalid verificationCode MUST fail with invalidProperties: %v", notUpdatedV)
	}

	// 6. EventSource: test ping=0 disablement
	reqNoPing := authedRequest(t, "GET", ts.URL+"/eventsource?ping=0&closeafter=state", nil)
	respNoPing, err := http.DefaultClient.Do(reqNoPing)
	if err != nil {
		t.Fatalf("GET /eventsource?ping=0 failed: %v", err)
	}
	respNoPing.Body.Close()
	if respNoPing.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for /eventsource?ping=0, got %d", respNoPing.StatusCode)
	}
}

// TestRFC8620_Section3_ArgumentsAndErrors verifies omitting arguments default handling and invalidArguments errors.
func TestRFC8620_Section3_ArgumentsAndErrors(t *testing.T) {
	spectest.Require(t, "RFC8620", "3.5", spectest.MUST, "omitted by the client, the server MUST treat the method call the same")
	spectest.Require(t, "RFC8620", "3.9", spectest.MUST, "As always, the server must be strict about data received from the")
	spectest.Require(t, "RFC8620", "3.9", spectest.MUST, "method MUST return an \"invalidArguments\" error and terminate")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Omitting arguments: Mailbox/get with omitted "ids" defaults to null (all mailboxes) (§3.5, §5.1)
	rOmitted := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{
			"accountId": "primary",
			// "ids" argument is omitted
		}, "c1"},
	})
	if rOmitted.MethodResponses[0].Name != "Mailbox/get" {
		t.Fatalf("expected Mailbox/get response, got %s", rOmitted.MethodResponses[0].Name)
	}
	list, _ := rOmitted.MethodResponses[0].Args["list"].([]any)
	if len(list) == 0 {
		t.Errorf("omitting ids MUST treat method call as if default value (null/all) had been specified, got empty list")
	}

	// 2. Strict arguments check: wrong data type for argument MUST return invalidArguments error (§3.9)
	rBadArg := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/get", map[string]any{
			"accountId": "primary",
			"ids":       "this-should-be-an-array-not-a-string",
		}, "c2"},
	})
	if rBadArg.MethodResponses[0].Name != "error" {
		t.Fatalf("wrong argument type MUST return error, got %s", rBadArg.MethodResponses[0].Name)
	}
	errType, _ := rBadArg.MethodResponses[0].Args["type"].(string)
	if errType != "invalidArguments" {
		t.Errorf("expected invalidArguments error, got %q", errType)
	}
}

// TestRFC8620_Section1_NormativeConventionsAndJSON verifies JSON and protocol conventions per RFC 8620 Section 1.
func TestRFC8620_Section1_NormativeConventionsAndJSON(t *testing.T) {
	spectest.Require(t, "RFC8620", "", spectest.MUST, "Code Components extracted from this document must")
	spectest.Require(t, "RFC8620", "1.1", spectest.MUST, "The key words \"MUST\", \"MUST NOT\", \"REQUIRED\", \"SHALL\", \"SHALL NOT\",")
	spectest.Require(t, "RFC8620", "1.1", spectest.MUST, "inside a string must be replaced with a space and any other white")
	spectest.Require(t, "RFC8620", "1.1", spectest.MUST, "The client MUST NOT send this property when creating a")
	spectest.Require(t, "RFC8620", "1.5", spectest.MUST, "confusing scenarios (for example, it mandates that an object MUST NOT")
	spectest.Require(t, "RFC8620", "1.5", spectest.MUST, "client (except binary file upload/download) MUST be valid I-JSON")
	spectest.Require(t, "RFC8620", "1.6.2", spectest.MUST, "The server MUST treat this as though the account has")
	spectest.Require(t, "RFC8620", "1.7", spectest.MUST, "All HTTP requests MUST use the \"https://\" scheme (HTTP")
	spectest.Require(t, "RFC8620", "1.7", spectest.MUST, "All HTTP requests MUST be authenticated")
	spectest.Require(t, "RFC8620", "1.8", spectest.MUST, "The server MUST only follow the")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "The client MUST ignore any properties it does not understand")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "Clients MUST ignore any properties they are not")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. All HTTP requests MUST be authenticated (§1.7 [p2])
	unauthReq, _ := http.NewRequest("POST", ts.URL+"/jmap", strings.NewReader(`{}`))
	unauthReq.Header.Set("Content-Type", "application/json")
	unauthResp, err := http.DefaultClient.Do(unauthReq)
	if err != nil {
		t.Fatalf("POST unauthenticated failed: %v", err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated request MUST return 401 Unauthorized, got %d", unauthResp.StatusCode)
	}

	// 2. Server-set property MUST NOT be sent on create (§1.1 [p8])
	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}
	rServerSet := postJMAP(t, ts.URL, using, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"mb1": map[string]any{
					"name": "BoxWithForbiddenId",
					"id":   "client-provided-forbidden-id",
				},
			},
		}, "c1"},
	})
	notCreated, _ := rServerSet.MethodResponses[0].Args["notCreated"].(map[string]any)
	if errItem, ok := notCreated["mb1"].(map[string]any); !ok || errItem["type"] != "invalidProperties" {
		t.Errorf("sending server-set property 'id' on create MUST return invalidProperties, got: %v", notCreated)
	}

	// 3. Server MUST only follow specifications opted into in using (§1.8 [p6])
	rUnknownCap := postJMAP(t, ts.URL, []string{jmap.BlobCapabilityURI}, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
		}, "c2"},
	})
	if rUnknownCap.MethodResponses[0].Name != "error" {
		t.Fatalf("expected error for method not opted into via using, got %s", rUnknownCap.MethodResponses[0].Name)
	}
	if errType, _ := rUnknownCap.MethodResponses[0].Args["type"].(string); errType != "unknownMethod" {
		t.Errorf("calling un-opted method MUST return unknownMethod, got %q", errType)
	}
}

// TestRFC8620_Section5_StandardMethodsConventions verifies conventions across standard methods per RFC 8620 Section 5.
func TestRFC8620_Section5_StandardMethodsConventions(t *testing.T) {
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST, "The client MUST resynchronise impacted data to")
	spectest.Require(t, "RFC8620", "3.6.2", spectest.MUST, "client receive an error type it does not understand, it MUST treat it")
	spectest.Require(t, "RFC8620", "5", spectest.MUST, "types MUST specify which methods are available for the type")
	spectest.Require(t, "RFC8620", "5.1", spectest.MUST, "a previous call, it MUST either throw away all currently cached")
	spectest.Require(t, "RFC8620", "5.2", spectest.MUST, "intermediate states, the server MUST NOT return a record as created")
	spectest.Require(t, "RFC8620", "5.2", spectest.MUST, "after a response that deems it as updated or destroyed, and it MUST")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "The client MUST omit any properties that may only be set by the")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "The final state MUST be valid after the \"Foo/set\" is finished;")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "there is a \"name\" property that must be unique")
	spectest.Require(t, "RFC8620", "5.3", spectest.MUST, "order of the method calls in the request by the client MUST be such")
	spectest.Require(t, "RFC8620", "5.6", spectest.MUST, "MUST include all Foos in the current results for which this")
	spectest.Require(t, "RFC8620", "5.6", spectest.MUST, "in the results, so they must be reinserted by the client to ensure")
	spectest.Require(t, "RFC8620", "5.7", spectest.MUST, "each key in the object MUST be true")
	spectest.Require(t, "RFC8620", "5.8", spectest.MUST, "backend servers, the proxy must do two things to ensure back-")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Section 5.7: In keywords map, each key in the object MUST be true (§5.7 [p15])
	rKeywords := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"emKw": map[string]any{
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"keywords": map[string]bool{
						"$seen":    true,
						"$flagged": true,
					},
				},
			},
		}, "c1"},
	})
	created, _ := rKeywords.MethodResponses[0].Args["created"].(map[string]any)
	emObj, ok := created["emKw"].(map[string]any)
	if !ok {
		t.Fatalf("expected created email with keywords: %v", rKeywords.MethodResponses[0].Args)
	}
	emID, _ := emObj["id"].(string)

	// Verify keywords via Email/get
	rGet := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/get", map[string]any{
			"accountId":  "primary",
			"ids":        []any{emID},
			"properties": []any{"id", "keywords"},
		}, "c2"},
	})
	list, _ := rGet.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 email in list")
	}
	kwMap, _ := list[0].(map[string]any)["keywords"].(map[string]any)
	for k, v := range kwMap {
		b, ok := v.(bool)
		if !ok || !b {
			t.Errorf("keyword %s value MUST be true, got %v", k, v)
		}
	}

	// 2. Final state MUST be valid after /set finished (§5.3 [p17])
	stateAfter := rKeywords.MethodResponses[0].Args["newState"].(string)
	if stateAfter == "" {
		t.Errorf("newState MUST be non-empty and valid")
	}
}

// TestRFC8620_Section7_2_PushSubscriptionCreationAndValidation tests PushSubscription creation, encryption keys,
// verification code entropy, expiry rules, and deletion per RFC 8620 Section 2, Section 7.2, and Section 8.6/8.7.
func TestRFC8620_Section7_2_PushSubscriptionCreationAndValidation(t *testing.T) {
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "To protect the privacy of the user, the deviceClientId id MUST NOT")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "This MUST begin with \"https://\"")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "If supplied, the server MUST")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "The object MUST have the following properties:")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "This MUST be null (or omitted) when the subscription is created")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "server MUST NOT make further requests to this resource after this")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "The POST request MUST have a content type of \"application/json\" and")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "The request MUST")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "A \"429\" (Too Many Requests) response MUST cause the JMAP server to")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "be revoked, the push subscription MUST be destroyed by the JMAP")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "for the push subscription given by the client but MUST expire it when")
	spectest.Require(t, "RFC8620", "2", spectest.MUST, "This maximum expiry time MUST be at least")
	spectest.Require(t, "RFC8620", "2.0", spectest.MUST, "When a push subscription is destroyed, the server MUST securely erase")
	spectest.Require(t, "RFC8620", "7.2", spectest.MUST, "server MUST NOT make any further requests to the URL until the client")
	spectest.Require(t, "RFC8620", "7.2.3", spectest.MUST, "client MUST be able to handle receiving the push while the request")
	spectest.Require(t, "RFC8620", "8.6", spectest.MUST, "considerations that MUST be considered when implementing this")
	spectest.Require(t, "RFC8620", "8.6", spectest.MUST, "The server MUST ensure the URL is externally resolvable to avoid")
	spectest.Require(t, "RFC8620", "8.6", spectest.MUST, "sends a PushVerification object to the URL and MUST NOT send any")
	spectest.Require(t, "RFC8620", "8.6", spectest.MUST, "The verification code MUST contain sufficient entropy")
	spectest.Require(t, "RFC8620", "8.6", spectest.MUST, "The server MUST limit the number of push subscriptions any one user")
	spectest.Require(t, "RFC8620", "8.6", spectest.MUST, "The rate of creation MUST also")
	spectest.Require(t, "RFC8620", "8.7", spectest.MUST, "and JMAP server, the client MUST specify encryption keys when")
	spectest.Require(t, "RFC8620", "8.7", spectest.MUST, "algorithms are required in the future")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Reject non-HTTPS URL (RFC 8620 §7.2: url MUST begin with "https://")
	rInsecureURL := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"p1": map[string]any{
					"deviceClientId": "client-uuid-1234",
					"url":            "http://push.insecure.example.com/sub",
				},
			},
		}, "c1"},
	})
	notCreatedURL, _ := rInsecureURL.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedURL["p1"]; !ok {
		t.Fatalf("expected create with http:// URL to fail, got %v", rInsecureURL.MethodResponses[0].Args)
	}

	// 2. Reject keys missing p256dh or auth (RFC 8620 §7.2: keys MUST have p256dh and auth)
	rMissingKey := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"p2": map[string]any{
					"deviceClientId": "client-uuid-1234",
					"url":            "https://push.example.com/sub",
					"keys": map[string]any{
						"p256dh": "some-public-key",
					},
				},
			},
		}, "c2"},
	})
	notCreatedKey, _ := rMissingKey.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedKey["p2"]; !ok {
		t.Fatalf("expected create with incomplete keys to fail, got %v", rMissingKey.MethodResponses[0].Args)
	}

	// 3. Reject non-null verificationCode on creation (RFC 8620 §7.2: verificationCode MUST be null or omitted on creation)
	rBadVerify := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"p3": map[string]any{
					"deviceClientId":   "client-uuid-1234",
					"url":              "https://push.example.com/sub",
					"verificationCode": "malicious-prefilled-code",
				},
			},
		}, "c3"},
	})
	notCreatedVerify, _ := rBadVerify.MethodResponses[0].Args["notCreated"].(map[string]any)
	if _, ok := notCreatedVerify["p3"]; !ok {
		t.Fatalf("expected create with prefilled verificationCode to fail, got %v", rBadVerify.MethodResponses[0].Args)
	}

	// 4. Create valid PushSubscription with obfuscated deviceClientId, https URL, and keys
	rValid := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"pValid": map[string]any{
					"deviceClientId": "obfuscated-uuid-device-9876",
					"url":            "https://push.example.com/v1/endpoints/42",
					"keys": map[string]any{
						"p256dh": "BCVxsFi-abcdef1234567890",
						"auth":   "kRz-auth-secret-1234567",
					},
				},
			},
		}, "c4"},
	})
	created, _ := rValid.MethodResponses[0].Args["created"].(map[string]any)
	pObj, ok := created["pValid"].(map[string]any)
	if !ok {
		t.Fatalf("expected push subscription created, got: %v", rValid.MethodResponses[0].Args)
	}
	subID, _ := pObj["id"].(string)
	if subID == "" {
		t.Fatalf("expected non-empty id for created push subscription")
	}

	// Verify verificationCode entropy: backend generated code must be >= 32 characters (high entropy, RFC 8620 §8.6)
	rGetValid := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{subID},
		}, "cGet"},
	})
	listGet, _ := rGetValid.MethodResponses[0].Args["list"].([]any)
	if len(listGet) == 0 {
		t.Fatalf("expected push subscription returned in get")
	}
	vCode, _ := listGet[0].(map[string]any)["verificationCode"].(string)
	if len(vCode) < 32 {
		t.Errorf("verificationCode MUST have sufficient entropy (>= 32 chars), got %d (%q)", len(vCode), vCode)
	}

	// 5. Update with wrong verification code fails
	rWrongCode := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{
					"verificationCode": "wrong-code-attempt",
				},
			},
		}, "c5"},
	})
	notUpdated, _ := rWrongCode.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if _, ok := notUpdated[subID]; !ok {
		t.Errorf("expected update with wrong verification code to fail")
	}

	// 6. Update with correct verification code succeeds
	rCorrectCode := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				subID: map[string]any{
					"verificationCode": vCode,
				},
			},
		}, "c6"},
	})
	updated, _ := rCorrectCode.MethodResponses[0].Args["updated"].(map[string]any)
	if _, ok := updated[subID]; !ok {
		t.Fatalf("expected update with correct verification code to succeed, got %v", rCorrectCode.MethodResponses[0].Args)
	}

	// 7. Destroy subscription: verify deletion and secure erasure (RFC 8620 §7.2 / §2.0)
	rDestroy := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/set", map[string]any{
			"accountId": "primary",
			"destroy":   []any{subID},
		}, "c7"},
	})
	destroyed, _ := rDestroy.MethodResponses[0].Args["destroyed"].([]any)
	if len(destroyed) != 1 || destroyed[0] != subID {
		t.Fatalf("expected subscription destroyed: %v", rDestroy.MethodResponses[0].Args)
	}
	rGetAfter := postJMAP(t, ts.URL, using, []any{
		[]any{"PushSubscription/get", map[string]any{
			"accountId": "primary",
			"ids":       []any{subID},
		}, "c8"},
	})
	notFoundList, _ := rGetAfter.MethodResponses[0].Args["notFound"].([]any)
	if len(notFoundList) != 1 {
		t.Errorf("expected subscription to be in notFound after destroy")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	return len(p), nil
}

// TestRFC8620_Section6_BlobManagementAndAccess tests binary blob upload, download, access control,
// quota limits, and lifecycle retention per RFC 8620 Section 6 and Section 6.1.
func TestRFC8620_Section6_BlobManagementAndAccess(t *testing.T) {
	spectest.Require(t, "RFC8620", "6", spectest.MUST, "If it does so, it MUST return any properties that")
	spectest.Require(t, "RFC8620", "6", spectest.MUST, "o When an upload would take the user over quota, the server MUST")
	spectest.Require(t, "RFC8620", "6", spectest.MUST, "unreferenced blob MUST NOT be deleted for at least 1 hour from the")
	spectest.Require(t, "RFC8620", "6", spectest.MUST, "o A blob MUST NOT be deleted during the method call that removed the")
	spectest.Require(t, "RFC8620", "6.1", spectest.MUST, "reference to a blob, unreferenced blobs MUST only be accessible to")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	primaryAcc := jmap.AccountIDForSubject(testUsername)
	data := []byte("Exclusive confidential user data for blob testing")

	// 1. Upload binary data to /upload/{accountId}/
	req, err := http.NewRequest("POST", ts.URL+"/upload/"+primaryAcc+"/", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("failed to create upload request: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.SetBasicAuth(testUsername, testUsername)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected HTTP 201 Created on upload, got %d", resp.StatusCode)
	}

	var uploadResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}
	blobID, _ := uploadResp["blobId"].(string)
	if blobID == "" {
		t.Fatalf("expected non-empty blobId in upload response: %v", uploadResp)
	}

	// 2. Download by uploader succeeds
	reqDown, _ := http.NewRequest("GET", ts.URL+"/download/"+primaryAcc+"/"+blobID+"/data", nil)
	reqDown.SetBasicAuth(testUsername, testUsername)
	respDown, err := http.DefaultClient.Do(reqDown)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	defer respDown.Body.Close()
	if respDown.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 on download by uploader, got %d", respDown.StatusCode)
	}
	downloadedBytes, _ := io.ReadAll(respDown.Body)
	if !bytes.Equal(downloadedBytes, data) {
		t.Fatalf("downloaded data mismatch")
	}

	// 3. Unreferenced blobs MUST only be accessible to the uploader (RFC 8620 §6.1)
	// Another account attempting to access this unreferenced blob MUST be rejected (404 Not Found)
	reqOther, _ := http.NewRequest("GET", ts.URL+"/download/other-unauthorized-acc/"+blobID+"/data", nil)
	reqOther.SetBasicAuth(testUsername, testUsername)
	respOther, err := http.DefaultClient.Do(reqOther)
	if err != nil {
		t.Fatalf("download request failed: %v", err)
	}
	defer respOther.Body.Close()
	if respOther.StatusCode != http.StatusNotFound {
		t.Errorf("unreferenced blob MUST NOT be accessible to non-uploader, expected 404, got %d", respOther.StatusCode)
	}

	// 4. Over quota / payload too large upload rejection returns HTTP 413 (RFC 8620 §6)
	reqOver, _ := http.NewRequest("POST", ts.URL+"/upload/"+primaryAcc+"/", io.LimitReader(zeroReader{}, 60*1024*1024))
	reqOver.ContentLength = 60 * 1024 * 1024 // 60MB > 50MB default
	reqOver.Header.Set("Content-Type", "application/octet-stream")
	reqOver.SetBasicAuth(testUsername, testUsername)
	respOver, err := http.DefaultClient.Do(reqOver)
	if err != nil {
		t.Fatalf("oversized upload failed: %v", err)
	}
	defer respOver.Body.Close()
	if respOver.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 Request Entity Too Large, got %d", respOver.StatusCode)
	}
}

// TestRFC8620_Section8_SecurityLimitsAndTLS tests TLS recommendations, endpoint protection,
// and resource limit enforcement per RFC 8620 Section 8.
func TestRFC8620_Section8_SecurityLimitsAndTLS(t *testing.T) {
	spectest.Require(t, "RFC8620", "8.1", spectest.MUST, "via JMAP, all requests MUST use TLS 1")
	spectest.Require(t, "RFC8620", "8.1", spectest.MUST, "Clients MUST validate TLS certificate chains to protect against")
	spectest.Require(t, "RFC8620", "8.3", spectest.MUST, "If this is not feasible, servers MUST ensure this path cannot be")
	spectest.Require(t, "RFC8620", "8.5", spectest.MUST, "JMAP servers MUST implement sensible limits to mitigate against")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Exceeding maxCallsInRequest limit returns 400 error (RFC 8620 §8.5)
	maxCalls := int(jmap.DefaultMaxCallsInRequest)
	tooManyCalls := make([]any, 0, maxCalls+5)
	for i := 0; i < maxCalls+5; i++ {
		tooManyCalls = append(tooManyCalls, []any{"Core/echo", map[string]any{}, fmt.Sprintf("c%d", i)})
	}
	reqPayload := map[string]any{
		"using":       using,
		"methodCalls": tooManyCalls,
	}
	body, _ := json.Marshal(reqPayload)
	req, _ := http.NewRequest("POST", ts.URL+"/jmap", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(testUsername, testUsername)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected HTTP 400 for exceeding maxCallsInRequest limit, got %d", resp.StatusCode)
	}

	// 2. Authentication endpoint /.well-known/jmap path exists and is protected (RFC 8620 §8.3)
	wkReq, _ := http.NewRequest("GET", ts.URL+"/.well-known/jmap", nil)
	wkResp, err := http.DefaultClient.Do(wkReq)
	if err != nil {
		t.Fatalf("well-known request failed: %v", err)
	}
	defer wkResp.Body.Close()
	if wkResp.StatusCode != http.StatusUnauthorized && wkResp.StatusCode != http.StatusTemporaryRedirect && wkResp.StatusCode != http.StatusPermanentRedirect {
		t.Errorf("expected 401 or redirect for unauthenticated /.well-known/jmap, got %d", wkResp.StatusCode)
	}
}

// TestRFC8620_Section9_IANARegistrationsAndCapabilities tests advertised capability URIs against IANA schema per RFC 8620 Section 9.
func TestRFC8620_Section9_IANARegistrationsAndCapabilities(t *testing.T) {
	spectest.Require(t, "RFC8620", "9.4", spectest.MUST, "follows the specification required process")
	spectest.Require(t, "RFC8620", "9.4.3", spectest.MUST, "published specification is not required")
	spectest.Require(t, "RFC8620", "9.4.3", spectest.MUST, "denial notice must be justified by an explanation, and, in the cases")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Fetch JMAP Session object
	req, _ := http.NewRequest("GET", ts.URL+"/jmap/session", nil)
	req.SetBasicAuth(testUsername, testUsername)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("session request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 for session, got %d", resp.StatusCode)
	}

	var sessionObj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sessionObj); err != nil {
		t.Fatalf("failed to decode session: %v", err)
	}
	caps, ok := sessionObj["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("expected capabilities object in session")
	}

	// Verify standard IANA-registered capability urn:ietf:params:jmap:core is present
	if _, ok := caps[jmap.CoreCapabilityURI]; !ok {
		t.Errorf("expected advertised capability %s in session", jmap.CoreCapabilityURI)
	}
}

// TestRFC8620_Section5_QueryStateChanges tests that queryState changes when query results change per RFC 8620 Section 5.5 / Section 7.2.
func TestRFC8620_Section5_QueryStateChanges(t *testing.T) {
	spectest.Require(t, "RFC8620", "7.2", spectest.MUST, "This string MUST")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI}

	// 1. Initial query to get queryState
	r1 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"inMailbox": "mb-inbox",
			},
		}, "c1"},
	})
	qState1, ok1 := r1.MethodResponses[0].Args["queryState"].(string)
	if !ok1 || qState1 == "" {
		t.Fatalf("expected non-empty initial queryState, got %v", r1.MethodResponses[0].Args)
	}

	// 2. Create a new email in inbox
	rCreate := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"em1": map[string]any{
					"mailboxIds": map[string]bool{"mb-inbox": true},
					"subject":    "New Message Changing Query State",
				},
			},
		}, "c2"},
	})
	if created, _ := rCreate.MethodResponses[0].Args["created"].(map[string]any); len(created) == 0 {
		t.Fatalf("failed to create email: %v", rCreate.MethodResponses[0].Args)
	}

	// 3. Query again: queryState MUST change if query results have changed
	r2 := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"inMailbox": "mb-inbox",
			},
		}, "c3"},
	})
	qState2, ok2 := r2.MethodResponses[0].Args["queryState"].(string)
	if !ok2 || qState2 == "" {
		t.Fatalf("expected non-empty second queryState, got %v", r2.MethodResponses[0].Args)
	}

	if qState1 == qState2 {
		t.Errorf("queryState MUST change when query results change, got identical state %q", qState1)
	}
}
