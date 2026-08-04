package jmap_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestFileNode_FilterPropertiesPosNeg tests FileNode/query name, type, and isFolder filter conditions.
// FileNode is a custom (non-RFC) file-storage extension; see the rfcless_ test prefix convention.
func TestFileNode_FilterPropertiesPosNeg(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := context.Background()

	f1, _ := srv.FileNodeBackend.CreateFileNode(ctx, &jmap.FileNode{
		Name:     "document.pdf",
		Type:     "application/pdf",
		IsFolder: false,
	})
	f2, _ := srv.FileNodeBackend.CreateFileNode(ctx, &jmap.FileNode{
		Name:     "ProjectsFolder",
		Type:     "directory",
		IsFolder: true,
	})

	using := []string{jmap.CoreCapabilityURI, jmap.FileNodeCapabilityURI}

	// 1. Positive filter by isFolder: true -> returns f2
	res1 := postJMAP(t, ts.URL, using, []any{
		[]any{"FileNode/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"isFolder": true},
		}, "c1"},
	})
	ids1, _ := res1.MethodResponses[0].Args["ids"].([]any)
	if len(ids1) != 1 || ids1[0] != string(f2.ID) {
		t.Errorf("FileNode isFolder:true expected [%s], got %v", f2.ID, ids1)
	}

	// 2. Positive filter by type: "application/pdf" -> returns f1
	res2 := postJMAP(t, ts.URL, using, []any{
		[]any{"FileNode/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"type": "application/pdf"},
		}, "c2"},
	})
	ids2, _ := res2.MethodResponses[0].Args["ids"].([]any)
	if len(ids2) != 1 || ids2[0] != string(f1.ID) {
		t.Errorf("FileNode type positive expected [%s], got %v", f1.ID, ids2)
	}

	// 3. Negative filter by type: "image/png" -> returns empty []
	res3 := postJMAP(t, ts.URL, using, []any{
		[]any{"FileNode/query", map[string]any{
			"accountId": "primary",
			"filter":    map[string]any{"type": "image/png"},
		}, "c3"},
	})
	ids3, _ := res3.MethodResponses[0].Args["ids"].([]any)
	if len(ids3) != 0 {
		t.Errorf("FileNode type negative expected empty [], got %v", ids3)
	}
}
