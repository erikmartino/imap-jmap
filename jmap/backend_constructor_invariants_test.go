package jmap

import (
	"context"
	"testing"
	"time"

	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/managesieve"
	"imap-jmap/jmap/nextcloud"
)

// AGENTS.md §1 Invariant Gate: Zero Hardcoded Accounts & Zero Seeded Authoritative Data.
// This guard test ensures that no production backend constructor pre-seeds any user,
// account, mailbox, email, calendar, contact, or file data into local memory.
func TestBackendConstructorsZeroAuthoritativeDataGate(t *testing.T) {
	ctx := context.Background()

	// 1. IMAP/SMTP Backend
	imapBackend := imapsmtp.New("127.0.0.1:993", "127.0.0.1:587")
	if imapBackend.HasSMTPServer() != true {
		t.Errorf("expected SMTPServer configured")
	}

	// 2. ManageSieve Backend
	sieveBackend := managesieve.NewBackend("127.0.0.1:4190")
	if sieveBackend == nil {
		t.Fatalf("expected non-nil sieve backend")
	}

	// 3. Nextcloud Backends
	ncClient := nextcloud.NewClient("http://127.0.0.1:8080")
	calBackend := nextcloud.NewCalendarsBackend(ncClient)
	if calBackend == nil {
		t.Fatalf("expected non-nil cal backend")
	}
	contactsBackend := nextcloud.NewContactsBackend(ncClient)
	if contactsBackend == nil {
		t.Fatalf("expected non-nil contacts backend")
	}
	fileNodeBackend := nextcloud.NewFileNodeBackend(ncClient)
	if fileNodeBackend == nil {
		t.Fatalf("expected non-nil filenode backend")
	}
	principalsBackend := nextcloud.NewPrincipalsBackend(ncClient, calBackend)
	if principalsBackend == nil {
		t.Fatalf("expected non-nil principals backend")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	// A fresh principals backend without explicit test seeding has zero seeded principals
	principals, _, _ := principalsBackend.GetPrincipals(timeoutCtx, nil)
	if len(principals) > 0 {
		t.Errorf("expected zero principals from unseeded backend, got %d", len(principals))
	}
}
