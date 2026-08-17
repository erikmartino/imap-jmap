package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// defaultTokenTTL is how long access tokens remain valid before expiring.
const defaultTokenTTL = 24 * time.Hour

// tokenRecord stores the account a token belongs to plus its expiry time.
type tokenRecord struct {
	subject   string
	accountID string
	expiresAt time.Time
}

// MemoryAuthBackend is a test AuthBackend that accepts any username where username == password.
// This is intentionally insecure and is only suitable for development and testing.
type MemoryAuthBackend struct {
	mu               sync.RWMutex
	tokens           map[string]tokenRecord // token → tokenRecord
	revoked          map[string]bool
	tokenTTL         time.Duration
	mailBackend      jmap.MailBackend
	blobBackend      jmap.BlobBackend
	calendarsBackend jmap.CalendarsBackend
	contactsBackend  jmap.ContactsBackend
	fileNodeBackend  jmap.FileNodeBackend
	seededAccounts   map[string]bool
}

// NewMemoryAuthBackend creates a new MemoryAuthBackend instance pre-registered with default test users.
func NewMemoryAuthBackend() *MemoryAuthBackend {
	b := &MemoryAuthBackend{
		tokens:         make(map[string]tokenRecord),
		revoked:        make(map[string]bool),
		tokenTTL:       defaultTokenTTL,
		seededAccounts: make(map[string]bool),
	}
	return b
}

// SetBackends links memory backends for lazy per-account sample data seeding on first authentication.
func (a *MemoryAuthBackend) SetBackends(mb jmap.MailBackend, bb jmap.BlobBackend, cb jmap.CalendarsBackend, contactsB jmap.ContactsBackend, fnB jmap.FileNodeBackend) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mailBackend = mb
	a.blobBackend = bb
	a.calendarsBackend = cb
	a.contactsBackend = contactsB
	a.fileNodeBackend = fnB
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
	accountID, err := a.ValidateCredentials(ctx, username, password)
	if err != nil {
		return "", err
	}

	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("token generation failed: %w", err)
	}

	a.mu.Lock()
	expiresAt := time.Now().Add(a.tokenTTL)
	delete(a.revoked, token)
	a.tokens[token] = tokenRecord{subject: username, accountID: accountID, expiresAt: expiresAt}
	if a.seededAccounts == nil {
		a.seededAccounts = make(map[string]bool)
	}
	alreadySeeded := a.seededAccounts[accountID]
	a.seededAccounts[accountID] = true
	mb, bb, cb, contactsB, fnB := a.mailBackend, a.blobBackend, a.calendarsBackend, a.contactsBackend, a.fileNodeBackend
	a.mu.Unlock()

	if !alreadySeeded && (mb != nil || bb != nil || cb != nil || contactsB != nil || fnB != nil) {
		SeedAccountSampleData(ctx, accountID, mb, bb, cb, contactsB, fnB)
	}

	return token, nil
}

// ValidateCredentials accepts any username where username == password and returns the derived accountID
// without issuing a token.
func (a *MemoryAuthBackend) ValidateCredentials(ctx context.Context, username, password string) (string, error) {
	if username == "" || username != password {
		return "", fmt.Errorf("invalid credentials")
	}
	accountID := jmap.AccountIDForSubject(username)

	a.mu.Lock()
	if a.seededAccounts == nil {
		a.seededAccounts = make(map[string]bool)
	}
	alreadySeeded := a.seededAccounts[accountID]
	a.seededAccounts[accountID] = true
	mb, bb, cb, contactsB, fnB := a.mailBackend, a.blobBackend, a.calendarsBackend, a.contactsBackend, a.fileNodeBackend
	a.mu.Unlock()

	if !alreadySeeded && (mb != nil || bb != nil || cb != nil || contactsB != nil || fnB != nil) {
		SeedAccountSampleData(ctx, accountID, mb, bb, cb, contactsB, fnB)
	}

	return accountID, nil
}

// ValidateToken looks up the token and returns the associated accountID.
// Expired or revoked tokens are rejected. Bearer tokens containing a valid subject email
// or issued by Authenticate are accepted.
func (a *MemoryAuthBackend) ValidateToken(ctx context.Context, token string) (string, string, error) {
	if token == "" {
		return "", "", fmt.Errorf("invalid or expired token")
	}
	a.mu.Lock()

	if a.revoked[token] {
		delete(a.tokens, token)
		a.mu.Unlock()
		return "", "", fmt.Errorf("invalid or expired token")
	}

	rec, ok := a.tokens[token]
	if ok {
		if !rec.expiresAt.IsZero() && time.Now().After(rec.expiresAt) {
			delete(a.tokens, token)
			a.mu.Unlock()
			return "", "", fmt.Errorf("invalid or expired token")
		}
		a.mu.Unlock()
		return rec.accountID, rec.subject, nil
	}
	a.mu.Unlock()

	return "", "", fmt.Errorf("invalid or expired token")
}

// generateToken creates a cryptographically random 32-byte hex token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
