package jmap_test

import (
	"context"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/managesieve"
)

// TestEnsureDefaultSieveScript verifies that the iTIP-tagging Sieve script is installed
// and activated on first login, and that it is not duplicated on subsequent logins.
func TestEnsureDefaultSieveScript(t *testing.T) {
	_, sieve, cleanup := managesieve.NewEmbeddedBackend("user@example.com")
	defer cleanup()

	ctx := context.Background()
	ctx = jmapauth.ContextWithSubject(ctx, "user@example.com")
	ctx = jmapauth.ContextWithAccountID(ctx, "user@example.com")
	ctx = jmapauth.ContextWithCredentials(ctx, "user@example.com", "user@example.com")

	if err := jmap.EnsureDefaultSieveScript(ctx, sieve); err != nil {
		t.Fatalf("EnsureDefaultSieveScript failed: %v", err)
	}
	scripts, err := sieve.GetAllSieveScripts(ctx)
	if err != nil {
		t.Fatalf("GetAllSieveScripts failed: %v", err)
	}
	if len(scripts) != 1 {
		t.Fatalf("expected 1 installed script, got %d", len(scripts))
	}
	s := scripts[0]
	if !s.IsActive {
		t.Errorf("expected the default script to be active")
	}
	if !strings.Contains(s.Content, "$itip") || !strings.Contains(s.Content, "addflag") {
		t.Errorf("expected the script to add the $itip flag, got %q", s.Content)
	}

	// Idempotent: a second login must not add another script.
	if err := jmap.EnsureDefaultSieveScript(ctx, sieve); err != nil {
		t.Fatalf("second EnsureDefaultSieveScript failed: %v", err)
	}
	scripts, _ = sieve.GetAllSieveScripts(ctx)
	if len(scripts) != 1 {
		t.Fatalf("expected still 1 script, got %d", len(scripts))
	}
}
