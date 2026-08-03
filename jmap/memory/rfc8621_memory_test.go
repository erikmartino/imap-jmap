package memory

import (
	"context"
	"strings"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_ChangeTracker_CreateUpdateDestroyResolution tests RFC 8620 Section 5.2 state change resolution.
func TestRFC8620_ChangeTracker_CreateUpdateDestroyResolution(t *testing.T) {
	tr := newChangeTracker(1000)

	// A card created, updated, then destroyed within the window resolves to nothing.
	tr.record("card-1", "create")
	tr.record("card-1", "update")
	tr.record("card-1", "destroy")

	// A card created then updated resolves to created only.
	tr.record("card-2", "create")
	tr.record("card-2", "update")

	// A card that already existed and was updated resolves to updated.
	tr.record("card-3", "update")

	// A card destroyed without being created within the window resolves to destroyed.
	tr.record("card-4", "destroy")

	created, updated, destroyed, newState, hasMore := tr.Changes("~0")
	if hasMore {
		t.Errorf("expected hasMore=false for full history, got true")
	}
	if len(created) != 1 || created[0] != "card-2" {
		t.Errorf("expected created=[card-2], got %v", created)
	}
	if len(updated) != 1 || updated[0] != "card-3" {
		t.Errorf("expected updated=[card-3], got %v", updated)
	}
	if len(destroyed) != 1 || destroyed[0] != "card-4" {
		t.Errorf("expected destroyed=[card-4], got %v", destroyed)
	}

	// Querying with the latest state yields no changes.
	c2, u2, d2, _, _ := tr.Changes(newState)
	if len(c2)+len(u2)+len(d2) != 0 {
		t.Errorf("expected no changes from newState, got c=%v u=%v d=%v", c2, u2, d2)
	}
}

// TestRFC8620_ChangeTrackerHasMoreWhenHistoryDiscarded verifies RFC 8620 Section 5.2 retains window history bounds.
func TestRFC8620_ChangeTrackerHasMoreWhenHistoryDiscarded(t *testing.T) {
	tr := newChangeTracker(5)
	for i := 0; i < 10; i++ {
		tr.record(jmap.Id("card-a"), "update")
	}

	// A sinceState older than the retained window must set hasMoreChanges so the
	// client re-fetches the full state (RFC 8620 Section 5.2).
	_, _, _, _, hasMore := tr.Changes("~2")
	if !hasMore {
		t.Errorf("expected hasMore=true when sinceState predates retained history")
	}
}

// TestRFC9404_BlobIDFullSHA256Digest tests RFC 9404 Blob Management SHA-256 digest IDs.
func TestRFC9404_BlobIDFullSHA256Digest(t *testing.T) {
	b := NewMemoryBlobBackend()
	data := []byte("hello world")
	blob, err := b.PutBlob(context.Background(), "primary", "text/plain", data)
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}

	// The blob ID must be the full 64-char SHA-256 hex digest (RFC 8620 Section 6),
	// not a truncated prefix.
	if len(blob.ID) != 64 {
		t.Fatalf("expected 64-char blobID, got %q (len %d)", blob.ID, len(blob.ID))
	}
	for _, c := range blob.ID {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("blobID %q contains non-hex char %q", blob.ID, c)
		}
	}
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9" // sha256("hello world")
	if blob.ID != expected {
		t.Errorf("blobID = %q, want %q", blob.ID, expected)
	}

	// Identical content must dedupe to the same blob ID.
	blob2, err := b.PutBlob(context.Background(), "folder", "text/plain", data)
	if err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	if blob2.ID != blob.ID {
		t.Errorf("dedupe failed: %q != %q", blob2.ID, blob.ID)
	}
}
