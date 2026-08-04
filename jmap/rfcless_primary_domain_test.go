package jmap_test

import (
	"context"
	"testing"

	"imap-jmap/jmap"
)

// TestPrimaryDomainResolver_Defaults tests that default PrimaryDomain resolves to example.com.
func TestPrimaryDomainResolver_Defaults(t *testing.T) {
	resolver := jmap.PrimaryDomainResolver{}
	ctx := context.Background()

	id, local := resolver.ResolveAccountID(ctx, "user@example.com")
	if !local {
		t.Errorf("Expected user@example.com to be local by default")
	}
	if id != jmap.AccountIDForSubject("user@example.com") {
		t.Errorf("Expected derived accountId, got %q", id)
	}

	_, localExt := resolver.ResolveAccountID(ctx, "user@otherdomain.org")
	if localExt {
		t.Errorf("user@otherdomain.org should not be local for default domain example.com")
	}
}

// TestPrimaryDomainResolver_CustomDomain tests custom primary domain resolution.
func TestPrimaryDomainResolver_CustomDomain(t *testing.T) {
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "custom.org"}
	ctx := context.Background()

	id, local := resolver.ResolveAccountID(ctx, "admin@custom.org")
	if !local {
		t.Errorf("Expected admin@custom.org to be local for custom domain")
	}
	if id != jmap.AccountIDForSubject("admin@custom.org") {
		t.Errorf("Expected derived accountId, got %q", id)
	}

	_, localExample := resolver.ResolveAccountID(ctx, "admin@example.com")
	if localExample {
		t.Errorf("admin@example.com should not be local when PrimaryDomain is custom.org")
	}
}
