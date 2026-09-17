package jmaphandler_test

import (
	"context"
	"testing"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
	"imap-jmap/jmap/jmapsession"
	"imap-jmap/jmap/spectest"
)

func TestMethodRegistry(t *testing.T) {
	spectest.RequireID(t, "RFC8620#3.4-p1-MUST", "Method Registry dispatch")

	reg := jmaphandler.NewMethodRegistry()
	h, ok := reg.Get("Core/echo")
	if !ok || h == nil {
		t.Fatal("Core/echo must be registered by default")
	}

	name, respArgs := h(context.Background(), map[string]any{"hello": "world"}, "call1")
	if name != "Core/echo" {
		t.Fatalf("expected response name Core/echo, got %q", name)
	}
	if respArgs["hello"] != "world" {
		t.Fatalf("expected echoed arg hello=world, got %v", respArgs)
	}

	// Register custom method
	reg.Register("Test/method", func(_ context.Context, args map[string]any, _ string) (string, map[string]any) {
		return "Test/method", map[string]any{"status": "ok"}
	})

	th, ok := reg.Get("Test/method")
	if !ok {
		t.Fatal("Test/method should be registered")
	}
	resName, resArgs := th(context.Background(), nil, "c1")
	if resName != "Test/method" || resArgs["status"] != "ok" {
		t.Fatalf("unexpected handler result: %s %v", resName, resArgs)
	}

	// AliasMethod
	aliased := jmaphandler.AliasMethod("Test/alias", th)
	aName, aArgs := aliased(context.Background(), nil, "c2")
	if aName != "Test/alias" || aArgs["status"] != "ok" {
		t.Fatalf("unexpected alias result: %s %v", aName, aArgs)
	}
}

func TestProperties(t *testing.T) {
	spectest.RequireID(t, "RFC8620#5.1-p1-MUST", "properties argument parsing and filtering")

	// ParseProperties
	if props := jmaphandler.ParseProperties(nil); props != nil {
		t.Fatalf("expected nil properties for nil args, got %v", props)
	}
	if props := jmaphandler.ParseProperties(map[string]any{}); props != nil {
		t.Fatalf("expected nil properties when omitted, got %v", props)
	}
	if props := jmaphandler.ParseProperties(map[string]any{"properties": []any{"name", "size"}}); len(props) != 2 || props[0] != "name" || props[1] != "size" {
		t.Fatalf("unexpected parsed properties: %v", props)
	}

	// FilterProperties
	type sample struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	}
	s := &sample{ID: "id1", Name: "Inbox", Role: "inbox"}

	// When properties is nil, returned unchanged
	unfiltered := jmaphandler.FilterProperties(s, nil)
	if unfiltered != s {
		t.Fatalf("expected unchanged object when properties is nil")
	}

	// Filter with name requested: id MUST always be preserved
	filtered := jmaphandler.FilterProperties(s, []string{"name"}).(map[string]any)
	if filtered["id"] != "id1" {
		t.Fatalf("expected id to be preserved, got %v", filtered["id"])
	}
	if filtered["name"] != "Inbox" {
		t.Fatalf("expected name to be preserved, got %v", filtered["name"])
	}
	if _, ok := filtered["role"]; ok {
		t.Fatalf("role should not be present in filtered object: %v", filtered)
	}

	// FilterList
	list := []*sample{
		{ID: "1", Name: "A", Role: "a"},
		{ID: "2", Name: "B", Role: "b"},
	}
	filteredList := jmaphandler.FilterList(list, []string{"name"})
	if len(filteredList) != 2 {
		t.Fatalf("expected 2 items, got %d", len(filteredList))
	}
	item0 := filteredList[0].(map[string]any)
	if item0["id"] != "1" || item0["name"] != "A" {
		t.Fatalf("unexpected item0: %v", item0)
	}
}

func TestLimits(t *testing.T) {
	spectest.RequireID(t, "RFC8620#6.1-p1-MUST", "Core limits validation")

	limits := jmapsession.CoreCapability{
		MaxObjectsInGet: 10,
		MaxObjectsInSet: 5,
	}
	ctx := jmaphandler.WithCoreLimits(context.Background(), limits)

	// ValidateGetLimits
	_, _, ok := jmaphandler.ValidateGetLimits(ctx, 10)
	if !ok {
		t.Fatal("10 objects should be allowed when limit is 10")
	}
	errName, errArgs, ok := jmaphandler.ValidateGetLimits(ctx, 11)
	if ok || errName != "error" || errArgs["type"] != "requestTooLarge" {
		t.Fatalf("expected requestTooLarge error for 11 objects: %s %v %v", errName, errArgs, ok)
	}

	// ValidateSetLimits
	setArgs := map[string]any{
		"create": map[string]any{"c1": map[string]any{}},
		"update": map[string]any{"u1": map[string]any{}},
		"destroy": []any{"d1", "d2"},
	}
	_, _, ok = jmaphandler.ValidateSetLimits(ctx, setArgs)
	if !ok {
		t.Fatal("4 operations should be allowed when limit is 5")
	}

	setArgs["destroy"] = []any{"d1", "d2", "d3", "d4"} // 1+1+4 = 6 > 5
	errName, errArgs, ok = jmaphandler.ValidateSetLimits(ctx, setArgs)
	if ok || errName != "error" || errArgs["type"] != "requestTooLarge" {
		t.Fatalf("expected requestTooLarge error for 6 operations: %s %v %v", errName, errArgs, ok)
	}
}

func TestCreationRefs(t *testing.T) {
	spectest.RequireID(t, "RFC8620#5.3-p1-MUST", "Creation references handling")

	if jmaphandler.NilIfEmpty(map[string]any{}) != nil {
		t.Fatal("expected nil for empty map")
	}
	if jmaphandler.NilIfEmpty([]any{}) != nil {
		t.Fatal("expected nil for empty slice")
	}
	if jmaphandler.NilIfEmpty(map[string]any{"a": 1}) == nil {
		t.Fatal("expected non-nil for non-empty map")
	}

	refs := map[string]jmapcore.Id{"c1": "real-1"}
	resolvedID := jmaphandler.ResolveCreationID("#c1", refs)
	if resolvedID != "real-1" {
		t.Fatalf("expected real-1, got %q", resolvedID)
	}
	resolvedPlain := jmaphandler.ResolveCreationID("plain-id", refs)
	if resolvedPlain != "plain-id" {
		t.Fatalf("expected plain-id, got %q", resolvedPlain)
	}

	patch := map[string]any{
		"parentId": "#c1",
		"title":    "New Title",
	}
	resolvedPatch := jmaphandler.ResolvePatchCreationRefs(patch, refs)
	if resolvedPatch["parentId"] != "real-1" {
		t.Fatalf("expected resolved parentId real-1, got %v", resolvedPatch["parentId"])
	}
}
