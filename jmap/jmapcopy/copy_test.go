package jmapcopy_test

import (
	"context"
	"testing"

	"imap-jmap/jmap/jmapcopy"
	"imap-jmap/jmap/spectest"
)

func TestCopyPrimitives(t *testing.T) {
	spectest.RequireID(t, "RFC8620#5.4-p1-MUST", "Foo/copy account ID resolution")
	spectest.RequireID(t, "RFC8620#5.4-p2-MUST", "Foo/copy state validation")

	// Account IDs resolution
	args := map[string]any{"accountId": "acc-1", "fromAccountId": "acc-2"}
	accID, fromID := jmapcopy.ResolveCopyAccountIDs(args)
	if accID != "acc-1" || fromID != "acc-2" {
		t.Fatalf("unexpected account IDs: accID=%s, fromID=%s", accID, fromID)
	}

	// Default fromAccountId
	argsDef := map[string]any{"accountId": "acc-1"}
	_, fromIDDef := jmapcopy.ResolveCopyAccountIDs(argsDef)
	if fromIDDef != "acc-1" {
		t.Fatalf("expected default fromAccountId to match accountId")
	}

	// State validation
	getState := func(ctx context.Context) string { return "state-1" }
	oldState, errInv := jmapcopy.ValidateCopyStates(context.Background(), context.Background(), map[string]any{"ifInState": "state-1"}, getState, getState)
	if errInv != nil || oldState != "state-1" {
		t.Fatalf("expected state validation success, got errInv=%v", errInv)
	}

	_, errMismatch := jmapcopy.ValidateCopyStates(context.Background(), context.Background(), map[string]any{"ifInState": "state-2"}, getState, getState)
	if errMismatch == nil || errMismatch.Name != "error" {
		t.Fatalf("expected stateMismatch error invocation")
	}

	// Property overrides merge
	src := map[string]any{"id": "old-id", "name": "Folder 1"}
	merged := jmapcopy.MergeCopyOverrides(src, map[string]any{"name": "Folder 2"})
	if _, hasID := merged["id"]; hasID {
		t.Fatalf("expected id to be stripped from merged copy overrides")
	}
	if merged["name"] != "Folder 2" {
		t.Fatalf("expected name to be overridden to Folder 2")
	}
}
