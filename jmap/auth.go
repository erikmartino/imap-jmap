package jmap

import (
	"context"
	"encoding/base64"
	"errors"
)

type contextKey int

const (
	authAccountIDKey contextKey = iota
	authSubjectKey
	authCredentialsKey
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
// auth middleware. It is only present for credential-based authentication where the subject is
// known; token-authenticated requests carry only the accountID instead.
func SubjectFromContext(ctx context.Context) (string, bool) {
	subject, ok := ctx.Value(authSubjectKey).(string)
	return subject, ok && subject != ""
}

// ContextWithSubject injects an authenticated subject into a context for downstream handlers.
func ContextWithSubject(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, authSubjectKey, subject)
}

// AuthBackend defines the authentication plugin interface per RFC 8620 Section 8.2.
// Implementations are responsible for credential validation and token lifecycle.
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

// AccountIDFromContext retrieves the authenticated accountID injected by the auth middleware.
func AccountIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(authAccountIDKey).(string)
	return id, ok
}

// ContextWithAccountID injects an accountID into a context for downstream handlers and backend calls.
func ContextWithAccountID(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, authAccountIDKey, accountID)
}

type defaultAuthBackend struct{}

func (defaultAuthBackend) Authenticate(ctx context.Context, username, password string) (string, error) {
	if username == "" || username != password {
		return "", errors.New("invalid credentials")
	}
	return username, nil
}

func (defaultAuthBackend) ValidateCredentials(ctx context.Context, username, password string) (string, error) {
	if username == "" || username != password {
		return "", errors.New("invalid credentials")
	}
	return AccountIDForSubject(username), nil
}

func (defaultAuthBackend) ValidateToken(ctx context.Context, token string) (string, string, error) {
	if token == "" {
		return "", "", errors.New("invalid token")
	}
	return AccountIDForSubject(token), token, nil
}
