package smtp_test

import (
	"context"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC6409_MessageSubmission tests RFC 6409 Message Submission protocol processing via EmailSubmission.
func TestRFC6409_MessageSubmission(t *testing.T) {
	memBackend := memory.NewMemoryBackend()

	sub, err := memBackend.CreateSubmission(context.Background(), &jmap.EmailSubmission{
		EmailID:  "email-1",
		ThreadID: "thread-1",
	})
	if err != nil {
		t.Fatalf("CreateSubmission failed per RFC 6409: %v", err)
	}

	if sub.ID == "" {
		t.Errorf("Expected submission ID per RFC 6409")
	}
}
