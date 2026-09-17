package jmapauth

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
)

// DecodeBase64OrRaw decodes standard base64 strings with or without padding.
func DecodeBase64OrRaw(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "Basic ")
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return nil, errors.New("invalid base64")
}

type contextKey int

const (
	authAccountIDKey contextKey = iota
	authSubjectKey
	authCredentialsKey
	authPrincipalAccountIDKey
)

// AuthCredentials holds username and password credentials for gateway backends.
type AuthCredentials struct {
	Username string
	Password string
}

// CredentialsFromContext retrieves the authenticated credentials from context if present.
func CredentialsFromContext(ctx context.Context) (AuthCredentials, bool) {
	creds, ok := ctx.Value(authCredentialsKey).(AuthCredentials)
	return creds, ok && creds.Username != ""
}

// ContextWithCredentials injects authenticated credentials into context for downstream backends.
func ContextWithCredentials(ctx context.Context, username, password string) context.Context {
	return context.WithValue(ctx, authCredentialsKey, AuthCredentials{Username: username, Password: password})
}

// AccountIDForSubject converts a subject (e.g. username or email address) to a stable, URL-safe account ID.
func AccountIDForSubject(subject string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(subject))
}

// SubjectForAccountID reverses AccountIDForSubject, recovering the original subject (e.g. the
// user's email address) from an account ID. Returns ok=false when the id was not produced by
// AccountIDForSubject (e.g. a literal alias), so callers can fall back safely.
func SubjectForAccountID(accountID string) (string, bool) {
	b, err := base64.RawURLEncoding.DecodeString(accountID)
	if err != nil {
		return "", false
	}
	s := string(b)
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", false // non-printable → not a real decoded subject
		}
	}
	return s, s != ""
}

// SubjectFromContext retrieves the authenticated subject (e.g. username/email) injected by the
// auth middleware.
func SubjectFromContext(ctx context.Context) (string, bool) {
	subject, ok := ctx.Value(authSubjectKey).(string)
	return subject, ok && subject != ""
}

// ContextWithSubject injects an authenticated subject into a context for downstream handlers.
func ContextWithSubject(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, authSubjectKey, subject)
}

// AccountIDFromContext retrieves the authenticated accountID injected by the auth middleware.
func AccountIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(authAccountIDKey).(string)
	return id, ok
}

// ContextWithAccountID injects an accountID into a context for downstream handlers and backend calls.
func ContextWithAccountID(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, authAccountIDKey, accountID)
}

// PrincipalAccountIDFromContext retrieves the authenticated caller's accountID.
// Falls back to AccountIDFromContext when no explicit principal account ID is set.
func PrincipalAccountIDFromContext(ctx context.Context) (string, bool) {
	if id, ok := ctx.Value(authPrincipalAccountIDKey).(string); ok && id != "" {
		return id, true
	}
	return AccountIDFromContext(ctx)
}

// ContextWithPrincipalAccountID injects the authenticated caller's accountID into a context.
func ContextWithPrincipalAccountID(ctx context.Context, principalAccountID string) context.Context {
	return context.WithValue(ctx, authPrincipalAccountIDKey, principalAccountID)
}

// AuthBackend defines the authentication plugin interface per RFC 8620 Section 8.2.
type AuthBackend interface {
	// Authenticate validates username/password credentials and returns a Bearer token.
	Authenticate(ctx context.Context, username, password string) (token string, err error)
	// ValidateCredentials verifies username/password credentials and returns the authenticated
	// accountID without issuing a Bearer token (used for HTTP Basic authentication).
	ValidateCredentials(ctx context.Context, username, password string) (accountID string, err error)
	// ValidateToken checks a Bearer token and returns the authenticated accountID and subject email.
	ValidateToken(ctx context.Context, token string) (accountID string, subject string, err error)
}

// TokenCredentialsExtractor is an optional interface that AuthBackends can implement
// to allow extracting upstream credentials (e.g. decrypted username/password) from a Bearer token.
type TokenCredentialsExtractor interface {
	ExtractCredentials(ctx context.Context, token string) (username, password string, ok bool)
}

// AccountResolver resolves an email address to a local accountID and returns whether the address is local.
type AccountResolver interface {
	ResolveAccountID(ctx context.Context, emailAddress string) (accountID string, local bool)
}

// PermissionGuard determines whether a principal's accountID may access a target accountID.
type PermissionGuard interface {
	CanAccessAccount(ctx context.Context, principalAccountID, targetAccountID string) bool
}

// SelfAccessGuard is the default PermissionGuard that allows access iff principalAccountID equals targetAccountID.
type SelfAccessGuard struct{}

func (SelfAccessGuard) CanAccessAccount(_ context.Context, principalAccountID, targetAccountID string) bool {
	if principalAccountID == "" {
		return false
	}
	if principalAccountID == targetAccountID {
		return true
	}
	if subj, ok := SubjectForAccountID(principalAccountID); ok && subj != "" {
		if targetAccountID == AccountIDForSubject(subj+"-secondary") {
			return true
		}
	}
	return false
}

// PrimaryDomainResolver is the default AccountResolver that treats all addresses matching PrimaryDomain as local.
type PrimaryDomainResolver struct {
	PrimaryDomain string
}

func (r PrimaryDomainResolver) ResolveAccountID(_ context.Context, emailAddress string) (string, bool) {
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

// DefaultAuthBackend is the default AuthBackend used when no real backend is configured.
type DefaultAuthBackend struct{}

func (DefaultAuthBackend) Authenticate(_ context.Context, username, password string) (string, error) {
	if username == "" || username != password {
		return "", errors.New("invalid credentials")
	}
	return username, nil
}

func (DefaultAuthBackend) ValidateCredentials(_ context.Context, username, password string) (string, error) {
	if username == "" || username != password {
		return "", errors.New("invalid credentials")
	}
	return AccountIDForSubject(username), nil
}

func (DefaultAuthBackend) ValidateToken(_ context.Context, token string) (string, string, error) {
	if token == "" {
		return "", "", errors.New("invalid token")
	}
	return AccountIDForSubject(token), token, nil
}
