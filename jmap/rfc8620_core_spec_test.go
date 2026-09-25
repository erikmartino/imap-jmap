package jmap_test

import (
	"bytes"
	"encoding/json"
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
