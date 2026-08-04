package jmap

import (
	"context"
	"strings"
)

// PermissionGuard determines whether a principal's accountID may access a target accountID.
type PermissionGuard interface {
	CanAccessAccount(ctx context.Context, principalAccountID, targetAccountID string) bool
}

// SelfAccessGuard is the default PermissionGuard that allows access iff principalAccountID equals targetAccountID.
type SelfAccessGuard struct{}

func (SelfAccessGuard) CanAccessAccount(ctx context.Context, principalAccountID, targetAccountID string) bool {
	if principalAccountID == "" {
		return false
	}
	return principalAccountID == targetAccountID
}

// AccountResolver resolves an email address to a local accountID and returns whether the address is local.
type AccountResolver interface {
	ResolveAccountID(ctx context.Context, emailAddress string) (accountID string, local bool)
}

// PrimaryDomainResolver is the default AccountResolver that treats all addresses matching PrimaryDomain as local.
type PrimaryDomainResolver struct {
	PrimaryDomain string
}

func (r PrimaryDomainResolver) ResolveAccountID(ctx context.Context, emailAddress string) (string, bool) {
	emailAddress = strings.TrimSpace(emailAddress)
	idx := strings.LastIndex(emailAddress, "@")
	if idx < 0 {
		return "", false
	}
	domain := strings.ToLower(emailAddress[idx+1:])
	primary := strings.ToLower(r.PrimaryDomain)
	if primary == "" {
		primary = "example.com"
	}
	if domain == primary {
		return AccountIDForSubject(emailAddress), true
	}
	return "", false
}
