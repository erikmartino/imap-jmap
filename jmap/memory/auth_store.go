package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

// MemoryAuthBackend is a test AuthBackend that accepts any username where username == password.
// This is intentionally insecure and is only suitable for development and testing.
type MemoryAuthBackend struct {
	mu     sync.RWMutex
	tokens map[string]string // token → accountID (username)
}

// NewMemoryAuthBackend creates a new MemoryAuthBackend instance.
func NewMemoryAuthBackend() *MemoryAuthBackend {
	return &MemoryAuthBackend{
		tokens: make(map[string]string),
	}
}

// Authenticate accepts any username where username == password and returns a random Bearer token.
func (a *MemoryAuthBackend) Authenticate(ctx context.Context, username, password string) (string, error) {
	if username == "" || username != password {
		return "", fmt.Errorf("invalid credentials")
	}

	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("token generation failed: %w", err)
	}

	a.mu.Lock()
	a.tokens[token] = username
	a.mu.Unlock()

	return token, nil
}

// ValidateToken looks up the token and returns the associated accountID (username).
func (a *MemoryAuthBackend) ValidateToken(ctx context.Context, token string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	accountID, ok := a.tokens[token]
	if !ok {
		return "", fmt.Errorf("invalid or expired token")
	}
	return accountID, nil
}

// generateToken creates a cryptographically random 32-byte hex token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
