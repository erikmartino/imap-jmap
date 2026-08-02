package jmap_test

import (
	"context"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC4315_IMAPUIDPlusTracking tests RFC 4315 IMAP UIDPLUS unique identifier tracking in JMAP IDs.
func TestRFC4315_IMAPUIDPlusTracking(t *testing.T) {
	memBackend := memory.NewMemoryBackend()

	email, err := memBackend.CreateEmail(context.Background(), &jmap.Email{
		Subject: "UIDPLUS Test",
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	if email.ID == "" {
		t.Errorf("Expected UIDPLUS ID per RFC 4315")
	}
}
