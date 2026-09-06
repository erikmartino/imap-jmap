package managesieve_test

import (
	"context"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/managesieve"
)

func testCtx() context.Context {
	ctx := context.Background()
	ctx = jmap.ContextWithSubject(ctx, "user@example.com")
	ctx = jmap.ContextWithAccountID(ctx, "user@example.com")
	ctx = jmap.ContextWithCredentials(ctx, "user@example.com", "user@example.com")
	return ctx
}

func TestManageSieveClientAndServer(t *testing.T) {
	srv, err := managesieve.NewEmbeddedServer()
	if err != nil {
		t.Fatalf("Failed to start embedded server: %v", err)
	}
	defer srv.Close()

	client, err := managesieve.Dial(srv.Addr())
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer client.Close()

	if err := client.Authenticate("alice@example.com", "secret"); err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	// CheckScript
	script1 := "require [\"fileinto\"];\nif header :contains \"Subject\" \"meeting\" {\n  fileinto \"Meetings\";\n}\n"
	if err := client.CheckScript(script1); err != nil {
		t.Fatalf("CheckScript failed: %v", err)
	}

	// PutScript
	if err := client.PutScript("filter1", script1); err != nil {
		t.Fatalf("PutScript failed: %v", err)
	}

	// SetActive
	if err := client.SetActive("filter1"); err != nil {
		t.Fatalf("SetActive failed: %v", err)
	}

	// ListScripts
	scripts, err := client.ListScripts()
	if err != nil {
		t.Fatalf("ListScripts failed: %v", err)
	}
	if len(scripts) != 1 || scripts[0].Name != "filter1" || !scripts[0].Active {
		t.Fatalf("Unexpected scripts: %+v", scripts)
	}

	// GetScript
	content, err := client.GetScript("filter1")
	if err != nil {
		t.Fatalf("GetScript failed: %v", err)
	}
	if content != script1 {
		t.Fatalf("Expected script %q, got %q", script1, content)
	}

	// Deactivate and delete
	if err := client.SetActive(""); err != nil {
		t.Fatalf("SetActive(\"\") failed: %v", err)
	}
	if err := client.DeleteScript("filter1"); err != nil {
		t.Fatalf("DeleteScript failed: %v", err)
	}

	scriptsAfter, err := client.ListScripts()
	if err != nil {
		t.Fatalf("ListScripts after delete failed: %v", err)
	}
	if len(scriptsAfter) != 0 {
		t.Fatalf("Expected 0 scripts after delete, got %d", len(scriptsAfter))
	}
}

func TestManageSieveBackend(t *testing.T) {
	_, backend, cleanup := managesieve.NewEmbeddedBackend("user@example.com")
	defer cleanup()

	ctx := testCtx()

	// Initial state
	st0 := backend.SieveScriptState(ctx)
	if st0 == "" {
		t.Fatalf("Expected non-empty state")
	}

	// Create
	s := &jmap.SieveScript{
		Name:     "filter1",
		Content:  "require [\"fileinto\"];\nif header :contains \"Subject\" \"meeting\" {\n  fileinto \"Meetings\";\n}\n",
		IsActive: true,
	}
	created, err := backend.CreateSieveScript(ctx, s)
	if err != nil {
		t.Fatalf("CreateSieveScript failed: %v", err)
	}
	if created.ID == "" || created.Name != "filter1" || !created.IsActive {
		t.Fatalf("Unexpected created script: %+v", created)
	}

	// Get
	fetched, notFound, err := backend.GetSieveScripts(ctx, []jmap.Id{created.ID})
	if err != nil || len(notFound) > 0 || len(fetched) == 0 {
		t.Fatalf("GetSieveScripts failed (notFound=%v): %v", notFound, err)
	}
	if fetched[0].Name != "filter1" {
		t.Fatalf("Expected name filter1, got %s", fetched[0].Name)
	}

	// Query
	ids, total, err := backend.QuerySieveScripts(ctx, map[string]any{"isActive": true}, 0, nil)
	if err != nil || total != 1 || len(ids) != 1 {
		t.Fatalf("QuerySieveScripts failed: total=%d, ids=%v, err=%v", total, ids, err)
	}

	// Update
	newContent := "require [\"fileinto\"];\nif header :contains \"Subject\" \"urgent\" {\n  fileinto \"Urgent\";\n}\n"
	updated, err := backend.UpdateSieveScript(ctx, created.ID, map[string]any{
		"content": newContent,
	})
	if err != nil {
		t.Fatalf("UpdateSieveScript failed: %v", err)
	}
	if updated.Content != newContent {
		t.Fatalf("Expected updated content %q, got %q", newContent, updated.Content)
	}

	// Changes
	cList, uList, dList, st1, _ := backend.SieveScriptChanges(ctx, st0)
	if len(cList) != 1 || len(uList) != 0 || len(dList) != 0 {
		t.Fatalf("Unexpected changes: created=%v, updated=%v, destroyed=%v, st1=%s", cList, uList, dList, st1)
	}

	// Delete
	delOk, err := backend.DeleteSieveScript(ctx, created.ID)
	if err != nil || !delOk {
		t.Fatalf("DeleteSieveScript failed: %v", err)
	}

	all, err := backend.GetAllSieveScripts(ctx)
	if err != nil {
		t.Fatalf("GetAllSieveScripts failed: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("Expected 0 scripts after delete, got %d", len(all))
	}
}
