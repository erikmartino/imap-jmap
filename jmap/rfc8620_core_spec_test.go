package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"imap-jmap/jmap"
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
