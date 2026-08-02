package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// defaultTokenTTL is how long access tokens remain valid before expiring.
const defaultTokenTTL = 24 * time.Hour

// tokenRecord stores the account a token belongs to plus its expiry time.
type tokenRecord struct {
	accountID string
	expiresAt time.Time
}

// MemoryAuthBackend is a test AuthBackend that accepts any username where username == password.
// This is intentionally insecure and is only suitable for development and testing.
type MemoryAuthBackend struct {
	mu       sync.RWMutex
	tokens   map[string]tokenRecord // token → accountID (username)
	revoked  map[string]bool
	tokenTTL time.Duration
}

// NewMemoryAuthBackend creates a new MemoryAuthBackend instance.
func NewMemoryAuthBackend() *MemoryAuthBackend {
	return &MemoryAuthBackend{
		tokens:   make(map[string]tokenRecord),
		revoked:  make(map[string]bool),
		tokenTTL: defaultTokenTTL,
	}
}

// SetTokenTTL overrides the token lifetime. A non-positive value disables expiry.
func (a *MemoryAuthBackend) SetTokenTTL(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tokenTTL = d
}

// RevokeToken invalidates a previously issued token immediately.
func (a *MemoryAuthBackend) RevokeToken(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.tokens, token)
	a.revoked[token] = true
}

// Authenticate accepts any username where username == password and returns a random Bearer token.
func (a *MemoryAuthBackend) Authenticate(ctx context.Context, username, password string) (string, error) {
	if _, err := a.ValidateCredentials(ctx, username, password); err != nil {
		return "", err
	}

	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("token generation failed: %w", err)
	}

	a.mu.Lock()
	expiresAt := time.Now().Add(a.tokenTTL)
	delete(a.revoked, token)
	a.tokens[token] = tokenRecord{accountID: username, expiresAt: expiresAt}
	a.mu.Unlock()

	return token, nil
}

// ValidateCredentials accepts any username where username == password and returns the username
// as the accountID without issuing a token.
func (a *MemoryAuthBackend) ValidateCredentials(ctx context.Context, username, password string) (string, error) {
	if username == "" || username != password {
		return "", fmt.Errorf("invalid credentials")
	}
	return username, nil
}

// ValidateToken looks up the token and returns the associated accountID (username).
// Expired or revoked tokens are rejected and removed from the store.
func (a *MemoryAuthBackend) ValidateToken(ctx context.Context, token string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.revoked[token] {
		delete(a.tokens, token)
		return "", fmt.Errorf("invalid or expired token")
	}

	rec, ok := a.tokens[token]
	if !ok {
		return "", fmt.Errorf("invalid or expired token")
	}
	if !rec.expiresAt.IsZero() && time.Now().After(rec.expiresAt) {
		delete(a.tokens, token)
		return "", fmt.Errorf("invalid or expired token")
	}
	return rec.accountID, nil
}

// generateToken creates a cryptographically random 32-byte hex token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
