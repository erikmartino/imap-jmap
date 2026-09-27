package imap

import (
	"testing"
)

func TestClientStruct(t *testing.T) {
	// Simple structure verification ensuring package compiles without JMAP dependencies
}

func TestClientSubscription(t *testing.T) {
	ts, cleanup := NewTestServer("testuser@example.com")
	defer cleanup()

	client, err := Dial(ts.Addr, "testuser@example.com", "testuser@example.com")
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer client.Close()

	// Initial default mailboxes should be subscribed
	folders, err := client.ListFolders("", "*")
	if err != nil {
		t.Fatalf("ListFolders failed: %v", err)
	}

	foundDrafts := false
	for _, f := range folders {
		if f.Name == "Drafts" {
			foundDrafts = true
			if !f.IsSubscribed {
				t.Errorf("expected initial Drafts to be subscribed")
			}
		}
	}
	if !foundDrafts {
		t.Fatalf("Drafts folder not found in ListFolders")
	}

	// Unsubscribe Drafts
	if err := client.Unsubscribe("Drafts"); err != nil {
		t.Fatalf("Unsubscribe Drafts failed: %v", err)
	}

	foldersAfter, err := client.ListFolders("", "*")
	if err != nil {
		t.Fatalf("ListFolders after unsubscribe failed: %v", err)
	}
	for _, f := range foldersAfter {
		if f.Name == "Drafts" && f.IsSubscribed {
			t.Errorf("expected Drafts to be unsubscribed, but IsSubscribed is true")
		}
	}

	// Re-subscribe Drafts
	if err := client.Subscribe("Drafts"); err != nil {
		t.Fatalf("Subscribe Drafts failed: %v", err)
	}

	foldersResub, err := client.ListFolders("", "*")
	if err != nil {
		t.Fatalf("ListFolders after resubscribe failed: %v", err)
	}
	for _, f := range foldersResub {
		if f.Name == "Drafts" && !f.IsSubscribed {
			t.Errorf("expected Drafts to be subscribed again, but IsSubscribed is false")
		}
	}
}
