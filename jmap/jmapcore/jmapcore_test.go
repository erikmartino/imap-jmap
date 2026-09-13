package jmapcore_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/spectest"
)

func TestRFC8620CorePrimitives(t *testing.T) {
	spectest.RequireID(t, "RFC8620#1.6-p1-MUST", "Id validation rules")
	spectest.RequireID(t, "RFC8620#3.1-p1-MUST", "Request object representation")
	spectest.RequireID(t, "RFC8620#3.2-p1-MUST", "Invocation marshaling and unmarshaling")
	spectest.RequireID(t, "RFC8620#3.5-p1-MUST", "Response object representation")
	spectest.RequireID(t, "RFC8620#3.6.1-p1-MUST", "Request error representation")
	spectest.RequireID(t, "RFC8620#3.6.2-p1-MUST", "Method error arguments construction")
	spectest.RequireID(t, "RFC8620#3.7-p1-MUST", "Result references and JSON Pointer evaluation")
	spectest.RequireID(t, "RFC8620#5.3-p1-MUST", "Creation references resolution and create loop")
	spectest.RequireID(t, "RFC8620#5.5-p1-MUST", "Query position, anchor parsing, and application")

	// 1. Id Validation
	validId := jmapcore.Id("abc_123-xyz")
	if !validId.Validate() {
		t.Fatalf("expected id %q to be valid", validId)
	}

	invalidId := jmapcore.Id("invalid/id!")
	if invalidId.Validate() {
		t.Fatalf("expected id %q to be invalid", invalidId)
	}

	// 2. Invocation & Envelope JSON Serialization/Deserialization
	inv := jmapcore.Invocation{
		Name:         "Core/echo",
		Args:         map[string]any{"hello": "world"},
		ClientCallID: "c1",
	}
	rawInv, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("failed to marshal invocation: %v", err)
	}
	var unmarshaledInv jmapcore.Invocation
	if err := json.Unmarshal(rawInv, &unmarshaledInv); err != nil {
		t.Fatalf("failed to unmarshal invocation: %v", err)
	}
	if unmarshaledInv.Name != inv.Name || unmarshaledInv.ClientCallID != inv.ClientCallID || unmarshaledInv.Args["hello"] != "world" {
		t.Fatalf("unmarshaled invocation mismatch: %+v", unmarshaledInv)
	}

	// Nil args marshaling check
	nilArgsInv := jmapcore.Invocation{Name: "Core/echo", ClientCallID: "c1"}
	rawNilArgs, _ := json.Marshal(nilArgsInv)
	if string(rawNilArgs) != `["Core/echo",{},"c1"]` {
		t.Fatalf("expected nil args to marshal as empty object, got %s", rawNilArgs)
	}

	// Invalid invocation tuple length and types test
	var badInv jmapcore.Invocation
	if err := json.Unmarshal([]byte(`["Core/echo", {}]`), &badInv); err == nil {
		t.Fatalf("expected error unmarshaling 2-element invocation array")
	}
	if err := json.Unmarshal([]byte(`[123, {}, "c1"]`), &badInv); err == nil {
		t.Fatalf("expected error unmarshaling non-string method name")
	}
	if err := json.Unmarshal([]byte(`["Core/echo", "badArgs", "c1"]`), &badInv); err == nil {
		t.Fatalf("expected error unmarshaling non-object method args")
	}
	if err := json.Unmarshal([]byte(`["Core/echo", {}, 456]`), &badInv); err == nil {
		t.Fatalf("expected error unmarshaling non-string clientCallId")
	}

	req := jmapcore.Request{
		Using:       []string{"urn:ietf:params:jmap:core"},
		MethodCalls: []jmapcore.Invocation{inv},
	}
	rawReq, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	var unmarshaledReq jmapcore.Request
	if err := json.Unmarshal(rawReq, &unmarshaledReq); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	resp := jmapcore.Response{
		MethodResponses: []jmapcore.Invocation{inv},
		SessionState:    "s1",
	}
	if resp.SessionState != "s1" || len(resp.MethodResponses) != 1 {
		t.Fatalf("unexpected response structure: %+v", resp)
	}

	reqErr := jmapcore.RequestError{
		Type:   jmapcore.ErrorNotJSON,
		Status: 400,
		Detail: "Malformed JSON",
	}
	if reqErr.Type != jmapcore.ErrorNotJSON {
		t.Fatalf("unexpected request error type: %v", reqErr.Type)
	}

	errArgs := jmapcore.MethodErrorArgs("invalidArguments", "missing field")
	if errArgs["type"] != "invalidArguments" || errArgs["description"] != "missing field" {
		t.Fatalf("unexpected MethodErrorArgs output: %v", errArgs)
	}
	noDescErrArgs := jmapcore.MethodErrorArgs("unknownMethod", "")
	if _, hasDesc := noDescErrArgs["description"]; hasDesc {
		t.Fatalf("expected no description key in MethodErrorArgs when empty")
	}

	// 3. Result Reference & JSON Pointer
	if jmapcore.IsResultReference(nil) {
		t.Fatalf("IsResultReference(nil) should return false")
	}
	m := map[string]any{
		"resultOf": "c1",
		"name":     "Email/query",
		"path":     "/ids",
	}
	if !jmapcore.IsResultReference(m) {
		t.Fatalf("expected IsResultReference to return true for %v", m)
	}

	data := map[string]any{
		"emails": []any{
			map[string]any{"id": "msg-1", "subject": "Hello"},
			map[string]any{"id": "msg-2", "subject": "World"},
		},
	}
	res, err := jmapcore.EvaluateJSONPointer(data, "/emails/*/id")
	if err != nil {
		t.Fatalf("EvaluateJSONPointer failed: %v", err)
	}
	ids, ok := res.([]any)
	if !ok || len(ids) != 2 || ids[0] != "msg-1" || ids[1] != "msg-2" {
		t.Fatalf("unexpected pointer result: %v", res)
	}

	// JSON Pointer edge cases and errors
	if emptyPtr, err := jmapcore.EvaluateJSONPointer(data, ""); err != nil || emptyPtr == nil {
		t.Fatalf("empty pointer should return root data")
	}
	if _, err := jmapcore.EvaluateJSONPointer(data, "invalid/no-slash"); err == nil {
		t.Fatalf("expected error for pointer without leading slash")
	}
	if _, err := jmapcore.EvaluateJSONPointer(data, "/emails/99"); err == nil {
		t.Fatalf("expected out of bounds error for array index 99")
	}
	if _, err := jmapcore.EvaluateJSONPointer(data, "/emails/not-an-int"); err == nil {
		t.Fatalf("expected error for invalid array index token")
	}
	if _, err := jmapcore.EvaluateJSONPointer(data, "/nonExistentKey"); err == nil {
		t.Fatalf("expected error for non-existent object key")
	}

	// Unescape pointer token test (~1 -> /, ~0 -> ~)
	unescData := map[string]any{"a/b~c": "found"}
	unescRes, err := jmapcore.EvaluateJSONPointer(unescData, "/a~1b~0c")
	if err != nil || unescRes != "found" {
		t.Fatalf("EvaluateJSONPointer failed unescaping ~1/~0: %v, err=%v", unescRes, err)
	}

	// 4. Creation References & Resolution Helpers
	resolved := map[string]jmapcore.Id{"c1": "real-1"}
	out, deferred := jmapcore.ResolveCreationRef("#c1", resolved, nil)
	if deferred || out != "real-1" {
		t.Fatalf("expected creation ref resolution to 'real-1', got out=%v, deferred=%v", out, deferred)
	}
	if nonRef, def := jmapcore.ResolveCreationRef("real-1", resolved, nil); def || nonRef != "real-1" {
		t.Fatalf("expected non-placeholder to pass through")
	}

	if resolvedCid := jmapcore.ResolveCreationID("#c1", resolved); resolvedCid != "real-1" {
		t.Fatalf("expected ResolveCreationID to resolve #c1 to real-1, got %s", resolvedCid)
	}
	if unresCid := jmapcore.ResolveCreationID("#unknown", resolved); unresCid != "#unknown" {
		t.Fatalf("expected ResolveCreationID to return placeholder for unresolved cid, got %s", unresCid)
	}

	// Id[Boolean] map ref resolution
	boolMap := map[string]any{
		"#c1":      true,
		"normalId": false,
	}
	resBoolMap, defBool := jmapcore.ResolveIdBooleanMapRefs(boolMap, resolved, nil)
	if defBool || resBoolMap["real-1"] != true || resBoolMap["normalId"] != false {
		t.Fatalf("ResolveIdBooleanMapRefs failed: %v", resBoolMap)
	}

	// ResolvePatchCreationRefs test
	patchMap := map[string]any{
		"#c1":             "value",
		"parentId":        "#c1",
		"mailboxIds/#c1": true,
	}
	resPatch := jmapcore.ResolvePatchCreationRefs(patchMap, resolved)
	if resPatch["real-1"] != "value" || resPatch["parentId"] != "real-1" {
		t.Fatalf("ResolvePatchCreationRefs failed: %v", resPatch)
	}

	// SetError tests
	setErr := jmapcore.SetError{Type: "forbidden", Description: "access denied"}
	if setErr.Error() != "forbidden: access denied" {
		t.Fatalf("unexpected SetError string: %s", setErr.Error())
	}
	setErrNoDesc := jmapcore.SetError{Type: "notFound"}
	if setErrNoDesc.Error() != "notFound" {
		t.Fatalf("unexpected SetError string: %s", setErrNoDesc.Error())
	}

	// RunCreateLoop forward dependency resolution & error types
	createRaw := map[string]any{
		"child": map[string]any{
			"parentId": "#parent",
		},
		"parent": map[string]any{
			"name": "Folder",
		},
		"forbiddenItem": map[string]any{
			"name": "Forbidden",
		},
		"errItem": map[string]any{
			"name": "Err",
		},
	}
	refs := jmapcore.NewCreationRefs(nil)
	refsMap := jmapcore.NewSetCreationRefs(context.Background())
	notCreated := jmapcore.RunCreateLoop(createRaw, refsMap, func(cid string, res map[string]any) (string, error) {
		if cid == "forbiddenItem" {
			return "", errors.New("forbidden: action not allowed")
		}
		if cid == "errItem" {
			return "", errors.New("generic failure")
		}
		if cid == "child" {
			if pId, ok := res["parentId"].(string); !ok || pId != "real-parent" {
				return "", errors.New("parent reference unresolved")
			}
			return "real-child", nil
		}
		return "real-parent", nil
	})
	if len(notCreated) != 2 || notCreated["forbiddenItem"] == nil || notCreated["errItem"] == nil {
		t.Fatalf("expected 2 notCreated errors, got %v", notCreated)
	}

	// Unresolved cyclic dependency in RunCreateLoop
	cyclicRaw := map[string]any{
		"a": map[string]any{"parentId": "#b"},
		"b": map[string]any{"parentId": "#a"},
	}
	cyclicNotCreated := jmapcore.RunCreateLoop(cyclicRaw, make(map[string]jmapcore.Id), func(cid string, res map[string]any) (string, error) {
		return "real-" + cid, nil
	})
	if len(cyclicNotCreated) != 2 {
		t.Fatalf("expected cyclic dependency to fail both items, got %v", cyclicNotCreated)
	}

	// Context CreationRefs
	ctx := jmapcore.WithCreationRefs(context.Background(), refs)
	jmapcore.RecordCreationRefs(ctx, refsMap, "c2", "real-2")
	fromCtx := jmapcore.CreationRefsFrom(ctx)
	if fromCtx == nil || fromCtx.Snapshot()["c2"] != "real-2" {
		t.Fatalf("expected context to hold creation refs snapshot with c2=real-2")
	}

	// 5. Query Positioning, Anchor Parsing, & Application
	pos, errMsg := jmapcore.ParseQueryPosition(map[string]any{"position": float64(10)})
	if pos != 10 || errMsg != "" {
		t.Fatalf("unexpected ParseQueryPosition: pos=%d, err=%s", pos, errMsg)
	}
	if noPos, _ := jmapcore.ParseQueryPosition(map[string]any{}); noPos != 0 {
		t.Fatalf("expected 0 for absent position")
	}

	if norm := jmapcore.NormalizePosition(-2, 10); norm != 8 {
		t.Fatalf("expected negative position -2 of 10 to normalize to 8, got %d", norm)
	}
	if norm := jmapcore.NormalizePosition(-20, 10); norm != 0 {
		t.Fatalf("expected negative overflow position -20 of 10 to normalize to 0, got %d", norm)
	}

	anchor, offset, anchorErr := jmapcore.ParseQueryAnchor(map[string]any{"anchor": "item-5", "anchorOffset": float64(-1)})
	if anchor != "item-5" || offset != -1 || anchorErr != "" {
		t.Fatalf("unexpected ParseQueryAnchor: anchor=%s, offset=%d, err=%s", anchor, offset, anchorErr)
	}
	if _, _, errStr := jmapcore.ParseQueryAnchor(map[string]any{"anchor": 123}); errStr == "" {
		t.Fatalf("expected error for non-string anchor")
	}
	if _, _, errStr := jmapcore.ParseQueryAnchor(map[string]any{"anchor": "item-1", "anchorOffset": 1.5}); errStr == "" {
		t.Fatalf("expected error for non-integer anchorOffset")
	}

	queryIds := []jmapcore.Id{"id-1", "id-2", "id-3", "item-5", "id-6"}
	limit := uint64(2)
	anchoredPos, anchoredOut, found := jmapcore.ApplyQueryAnchor("item-5", -1, queryIds, &limit)
	if !found || anchoredPos != 2 || len(anchoredOut) != 2 || anchoredOut[0] != "id-3" || anchoredOut[1] != "item-5" {
		t.Fatalf("unexpected ApplyQueryAnchor output: found=%v, pos=%d, out=%v", found, anchoredPos, anchoredOut)
	}

	// Anchor not found test
	if _, _, foundNotFound := jmapcore.ApplyQueryAnchor("missing", 0, queryIds, nil); foundNotFound {
		t.Fatalf("expected ApplyQueryAnchor to return found=false for missing anchor")
	}
	// Anchor offset past end test
	if _, emptyOut, _ := jmapcore.ApplyQueryAnchor("item-5", 100, queryIds, nil); len(emptyOut) != 0 {
		t.Fatalf("expected empty slice when position exceeds array length")
	}

	// 6. Patch Application & NilIfEmpty
	target := map[string]any{
		"name": "Inbox",
		"keywords": map[string]any{
			"$seen": true,
		},
	}
	patch := jmapcore.PatchObject{
		"name":            "Archive",
		"/keywords/$flag": true,
		"/description":   nil,
	}
	if err := jmapcore.ApplyPatch(target, patch); err != nil {
		t.Fatalf("ApplyPatch failed: %v", err)
	}
	if target["name"] != "Archive" {
		t.Fatalf("expected name Archive, got %v", target["name"])
	}
	kw, _ := target["keywords"].(map[string]any)
	if kw["$flag"] != true || kw["$seen"] != true {
		t.Fatalf("expected keywords patched with $flag, got %v", kw)
	}

	// Patch non-object step error
	badPatch := jmapcore.PatchObject{"/name/sub": "fail"}
	if err := jmapcore.ApplyPatch(target, badPatch); err == nil {
		t.Fatalf("expected error when stepping into non-object target")
	}

	if res := jmapcore.NilIfEmpty(nil); res != nil {
		t.Fatalf("expected NilIfEmpty on nil to return nil")
	}
	if res := jmapcore.NilIfEmpty(map[string]any{}); res != nil {
		t.Fatalf("expected NilIfEmpty on empty map to return nil, got %v", res)
	}
	if res := jmapcore.NilIfEmpty([]string{}); res != nil {
		t.Fatalf("expected NilIfEmpty on empty slice to return nil, got %v", res)
	}
	if res := jmapcore.NilIfEmpty("non-empty"); res != "non-empty" {
		t.Fatalf("expected NilIfEmpty on non-empty string to return original string")
	}
}
