package managesieve_test

import (
	"context"
	"testing"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapsieve"
	"imap-jmap/jmap/managesieve"
)

func testCtx() context.Context {
	ctx := context.Background()
	ctx = jmapauth.ContextWithSubject(ctx, "user@example.com")
	ctx = jmapauth.ContextWithAccountID(ctx, "user@example.com")
	ctx = jmapauth.ContextWithCredentials(ctx, "user@example.com", "user@example.com")
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
	s := &jmapsieve.SieveScript{
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
	fetched, notFound, err := backend.GetSieveScripts(ctx, []jmapcore.Id{created.ID})
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

// TestSieveScriptDeterministicIDsAcrossInstances verifies that SieveScript IDs are
// derived deterministically from upstream script names, allowing completely fresh
// backend instances with zero shared memory or caches to interact with identical IDs.
func TestSieveScriptDeterministicIDsAcrossInstances(t *testing.T) {
	srv, err := managesieve.NewEmbeddedServer()
	if err != nil {
		t.Fatalf("Failed to start embedded server: %v", err)
	}
	defer srv.Close()

	ctx := testCtx()

	// Backend 1 creates multiple scripts with various characters (spaces, punctuation, underscores).
	b1 := managesieve.NewBackend(srv.Addr())

	scripts := []struct {
		name    string
		content string
		active  bool
	}{
		{
			name:    "Spam Filter",
			content: "require [\"fileinto\"];\nif header :contains \"X-Spam\" \"Yes\" { fileinto \"Junk\"; }\n",
			active:  true,
		},
		{
			name:    "Vacation (away)",
			content: "require [\"fileinto\"];\nif header :contains \"Subject\" \"vacation\" { fileinto \"Vacation\"; }\n",
			active:  false,
		},
		{
			name:    "Rules_2026",
			content: "require [\"fileinto\"];\nif header :contains \"Subject\" \"meeting\" { fileinto \"Meetings\"; }\n",
			active:  false,
		},
	}

	createdIDs := make(map[string]jmapcore.Id)
	for _, sc := range scripts {
		created, cErr := b1.CreateSieveScript(ctx, &jmapsieve.SieveScript{
			Name:     sc.name,
			Content:  sc.content,
			IsActive: sc.active,
		})
		if cErr != nil {
			t.Fatalf("CreateSieveScript(%q) failed: %v", sc.name, cErr)
		}
		expectedID := managesieve.SieveScriptIDForName(sc.name)
		if created.ID != expectedID {
			t.Fatalf("Expected ID %q for script %q, got %q", expectedID, sc.name, created.ID)
		}
		decodedName := managesieve.NameForSieveScriptID(created.ID)
		if decodedName != sc.name {
			t.Fatalf("Expected decoded name %q, got %q", sc.name, decodedName)
		}
		createdIDs[sc.name] = created.ID
	}

	// Backend 2 is a completely fresh instance pointing at the same ManageSieve server.
	b2 := managesieve.NewBackend(srv.Addr())

	// b2 lists all scripts: should see exact same IDs, names, contents, and active statuses.
	allB2, err := b2.GetAllSieveScripts(ctx)
	if err != nil {
		t.Fatalf("b2.GetAllSieveScripts failed: %v", err)
	}
	if len(allB2) != len(scripts) {
		t.Fatalf("b2 expected %d scripts, got %d", len(scripts), len(allB2))
	}

	for _, s := range allB2 {
		expID := createdIDs[s.Name]
		if s.ID != expID {
			t.Errorf("b2 script %q has ID %q, expected %q", s.Name, s.ID, expID)
		}
	}

	// b2 fetches by specific ID:
	spamID := createdIDs["Spam Filter"]
	fetched, notFound, err := b2.GetSieveScripts(ctx, []jmapcore.Id{spamID})
	if err != nil || len(notFound) > 0 || len(fetched) != 1 {
		t.Fatalf("b2.GetSieveScripts failed: fetched=%v, notFound=%v, err=%v", fetched, notFound, err)
	}
	if fetched[0].Name != "Spam Filter" || !fetched[0].IsActive {
		t.Errorf("b2 fetched unexpected script: %+v", fetched[0])
	}

	// b2 queries scripts:
	queryIDs, total, err := b2.QuerySieveScripts(ctx, map[string]any{"isActive": true}, 0, nil)
	if err != nil || total != 1 || len(queryIDs) != 1 || queryIDs[0] != spamID {
		t.Fatalf("b2.QuerySieveScripts failed: queryIDs=%v, total=%d, err=%v", queryIDs, total, err)
	}

	// b2 deletes a script by ID:
	vacID := createdIDs["Vacation (away)"]
	okDel, err := b2.DeleteSieveScript(ctx, vacID)
	if err != nil || !okDel {
		t.Fatalf("b2.DeleteSieveScript failed: %v", err)
	}

	// b3 is yet another fresh instance: confirms deletion.
	b3 := managesieve.NewBackend(srv.Addr())
	allB3, err := b3.GetAllSieveScripts(ctx)
	if err != nil {
		t.Fatalf("b3.GetAllSieveScripts failed: %v", err)
	}
	if len(allB3) != 2 {
		t.Fatalf("b3 expected 2 scripts after deletion, got %d", len(allB3))
	}
	for _, s := range allB3 {
		if s.Name == "Vacation (away)" {
			t.Errorf("b3 should not find deleted script Vacation (away)")
		}
	}

	// Rename: b1 updates "Rules_2026" to "Rules_2027".
	rulesID := createdIDs["Rules_2026"]
	updated, err := b1.UpdateSieveScript(ctx, rulesID, map[string]any{
		"name": "Rules_2027",
	})
	if err != nil {
		t.Fatalf("b1.UpdateSieveScript rename failed: %v", err)
	}
	expectedNewID := managesieve.SieveScriptIDForName("Rules_2027")
	if updated.ID != expectedNewID || updated.Name != "Rules_2027" {
		t.Fatalf("Unexpected updated script after rename: %+v", updated)
	}

	// b1 can resolve via old ID in the same process due to movedIDs redirection:
	fetchedOld, notFoundOld, err := b1.GetSieveScripts(ctx, []jmapcore.Id{rulesID})
	if err != nil || len(notFoundOld) > 0 || len(fetchedOld) != 1 {
		t.Fatalf("b1 get with redirected old ID failed: fetched=%v, notFound=%v, err=%v", fetchedOld, notFoundOld, err)
	}
	if fetchedOld[0].Name != "Rules_2027" {
		t.Errorf("Expected fetched name Rules_2027, got %q", fetchedOld[0].Name)
	}

	// b4 is a fresh instance with zero local memory:
	b4 := managesieve.NewBackend(srv.Addr())
	allB4, err := b4.GetAllSieveScripts(ctx)
	if err != nil {
		t.Fatalf("b4.GetAllSieveScripts failed: %v", err)
	}
	var foundNew bool
	for _, s := range allB4 {
		if s.Name == "Rules_2027" && s.ID == expectedNewID {
			foundNew = true
		}
		if s.Name == "Rules_2026" {
			t.Errorf("b4 should not see old script name Rules_2026")
		}
	}
	if !foundNew {
		t.Errorf("b4 did not find renamed script Rules_2027 with deterministic ID %s", expectedNewID)
	}

	// b4 requesting old ID gets notFound:
	_, notFoundB4, err := b4.GetSieveScripts(ctx, []jmapcore.Id{rulesID})
	if err != nil || len(notFoundB4) != 1 || notFoundB4[0] != rulesID {
		t.Errorf("b4 expected old ID %s in notFound, got notFound=%v, err=%v", rulesID, notFoundB4, err)
	}
}

