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
)

// AccountIDForSubject converts a subject (e.g. username or email address) to a stable, URL-safe account ID.
func AccountIDForSubject(subject string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(subject))
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
	// ValidateToken checks a Bearer token and returns the authenticated accountID.
	ValidateToken(ctx context.Context, token string) (accountID string, err error)
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

func (defaultAuthBackend) ValidateToken(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", errors.New("invalid token")
	}
	return AccountIDForSubject(token), nil
}
