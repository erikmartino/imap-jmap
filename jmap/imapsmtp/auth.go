package imapsmtp

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"imap-jmap/jmap"
)

// IMAPAuthBackend authenticates users directly against an upstream IMAP server and issues encrypted session tokens.
type IMAPAuthBackend struct {
	pool      *ClientPool
	secretKey []byte
}

var _ jmap.AuthBackend = (*IMAPAuthBackend)(nil)
var _ jmap.TokenCredentialsExtractor = (*IMAPAuthBackend)(nil)

type tokenPayload struct {
	Username  string `json:"u"`
	Password  string `json:"p"`
	ExpiresAt int64  `json:"exp"`
}

// NewAuthBackend creates an IMAPAuthBackend with a secret key for session token encryption/decryption.
func NewAuthBackend(pool *ClientPool, secret string) *IMAPAuthBackend {
	hash := sha256.Sum256([]byte(secret))
	return &IMAPAuthBackend{
		pool:      pool,
		secretKey: hash[:],
	}
}

// Authenticate verifies credentials against upstream IMAP and returns an encrypted session Bearer token.
func (a *IMAPAuthBackend) Authenticate(ctx context.Context, username, password string) (string, error) {
	if username == "" || password == "" {
		return "", errors.New("empty credentials")
	}

	// Validate against live upstream IMAP using the user's own credentials.
	client, err := a.pool.GetClient(ctx, username, password)
	if err != nil {
		return "", fmt.Errorf("upstream IMAP authentication failed: %w", err)
	}
	a.pool.ReleaseClientForUser(username, password, client)

	payload := tokenPayload{
		Username:  username,
		Password:  password,
		ExpiresAt: time.Now().Add(24 * 7 * time.Hour).Unix(),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(a.secretKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, payloadBytes, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// ValidateCredentials verifies credentials against upstream IMAP without issuing a token (for HTTP Basic auth).
func (a *IMAPAuthBackend) ValidateCredentials(ctx context.Context, username, password string) (string, error) {
	if username == "" || password == "" {
		return "", errors.New("empty credentials")
	}

	client, err := a.pool.GetClient(ctx, username, password)
	if err != nil {
		return "", fmt.Errorf("upstream IMAP authentication failed: %w", err)
	}
	// Keep the verified connection for reuse instead of dropping it; every JMAP
	// request re-authenticates over Basic, so a fresh dial+login per request is
	// wasteful.
	a.pool.ReleaseClientForUser(username, password, client)

	return jmap.AccountIDForSubject(username), nil
}

// ValidateToken decrypts the session token and returns the authenticated accountID and subject username.
func (a *IMAPAuthBackend) ValidateToken(ctx context.Context, token string) (string, string, error) {
	u, _, ok := a.ExtractCredentials(ctx, token)
	if !ok {
		return "", "", errors.New("invalid or expired session token")
	}
	return jmap.AccountIDForSubject(u), u, nil
}

// ExtractCredentials decrypts username and password from an encrypted session token.
func (a *IMAPAuthBackend) ExtractCredentials(ctx context.Context, token string) (string, string, bool) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", false
	}

	block, err := aes.NewCipher(a.secretKey)
	if err != nil {
		return "", "", false
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", false
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", "", false
	}

	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", "", false
	}

	var payload tokenPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return "", "", false
	}

	if time.Now().Unix() > payload.ExpiresAt {
		return "", "", false
	}

	return payload.Username, payload.Password, true
}
