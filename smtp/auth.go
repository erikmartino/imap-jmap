package smtp

import (
	"context"

	"imap-jmap/jmap"
)

// AuthBackendAuthenticator adapts a jmap.AuthBackend to the SMTP Authenticator
// interface (RFC 4954). It validates credentials with ValidateCredentials (which
// issues no bearer token) and reports the subject's email address as the
// authenticated identity used by the submission transport (RFC 6409 Section 6.1).
type AuthBackendAuthenticator struct {
	backend jmap.AuthBackend
}

// NewAuthBackendAuthenticator returns an Authenticator backed by the given
// jmap.AuthBackend. Credentials are valid exactly when the backend accepts them;
// the authenticated email is the username used to authenticate.
func NewAuthBackendAuthenticator(backend jmap.AuthBackend) *AuthBackendAuthenticator {
	return &AuthBackendAuthenticator{backend: backend}
}

// Authenticate validates username/password against the underlying AuthBackend and
// returns the authenticated email address on success.
func (a *AuthBackendAuthenticator) Authenticate(ctx context.Context, username, password string) (string, bool, error) {
	if a.backend == nil {
		return "", false, nil
	}
	if _, err := a.backend.ValidateCredentials(ctx, username, password); err != nil {
		return "", false, nil
	}
	return username, true, nil
}
