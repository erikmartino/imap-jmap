package jmap

import (
	"context"
	"encoding/base64"
)

type contextKey int

const authAccountIDKey contextKey = iota

// AccountIDForSubject converts a subject (e.g. username or email address) to a stable, URL-safe account ID.
func AccountIDForSubject(subject string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(subject))
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
