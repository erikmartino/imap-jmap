package jmapcore_test

import (
	"context"
	"testing"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/spectest"
)

func TestRFC8620CorePrimitives(t *testing.T) {
	spectest.RequireID(t, "RFC8620#1.6-p1-MUST", "Id validation rules")
	spectest.RequireID(t, "RFC8620#3.7-p1-MUST", "Result references and JSON Pointer evaluation")
	spectest.RequireID(t, "RFC8620#5.3-p1-MUST", "Creation references resolution")

	// Id Validation
	validId := jmapcore.Id("abc_123-xyz")
	if !validId.Validate() {
		t.Fatalf("expected id %q to be valid", validId)
	}

	invalidId := jmapcore.Id("invalid/id!")
	if invalidId.Validate() {
		t.Fatalf("expected id %q to be invalid", invalidId)
	}

	// Result Reference & JSON Pointer
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

	// Creation References
	resolved := map[string]jmapcore.Id{"c1": "real-1"}
	out, deferred := jmapcore.ResolveCreationRef("#c1", resolved, nil)
	if deferred || out != "real-1" {
		t.Fatalf("expected creation ref resolution to 'real-1', got out=%v, deferred=%v", out, deferred)
	}

	// Context CreationRefs
	refs := jmapcore.NewCreationRefs(map[string]string{"c1": "real-1"})
	ctx := jmapcore.WithCreationRefs(context.Background(), refs)
	fromCtx := jmapcore.CreationRefsFrom(ctx)
	if fromCtx == nil || fromCtx.Snapshot()["c1"] != "real-1" {
		t.Fatalf("expected context to hold creation refs snapshot with c1=real-1")
	}

	// Patch Application
	target := map[string]any{
		"name": "Inbox",
		"keywords": map[string]any{
			"$seen": true,
		},
	}
	patch := jmapcore.PatchObject{
		"name":            "Archive",
		"/keywords/$flag": true,
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
}
