package jmap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// defaultTokenTTL is how long access tokens remain valid before expiring.
const defaultTokenTTL = 24 * time.Hour

// tokenRecord stores the account a token belongs to plus its expiry time.
type tokenRecord struct {
	subject   string
	accountID string
	expiresAt time.Time
}

// MemoryAuthBackend is an in-memory AuthBackend that accepts any username where username == password.
// This is suitable for development and testing.
type MemoryAuthBackend struct {
	mu               sync.RWMutex
	tokens           map[string]tokenRecord // token → tokenRecord
	revoked          map[string]bool
	tokenTTL         time.Duration
	mailBackend      MailBackend
	blobBackend      BlobBackend
	calendarsBackend CalendarsBackend
	contactsBackend  ContactsBackend
	fileNodeBackend  FileNodeBackend
	seededAccounts   map[string]bool
	disableSeeding   bool
}

var _ AuthBackend = (*MemoryAuthBackend)(nil)
var _ TokenCredentialsExtractor = (*MemoryAuthBackend)(nil)

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

// SetDisableSeeding disables automatic sample data seeding for new accounts.
func (a *MemoryAuthBackend) SetDisableSeeding(disable bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.disableSeeding = disable
}

// SetBackends links backends for lazy per-account sample data seeding on first authentication.
func (a *MemoryAuthBackend) SetBackends(mb MailBackend, bb BlobBackend, cb CalendarsBackend, contactsB ContactsBackend, fnB FileNodeBackend) {
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

	if !a.disableSeeding && !alreadySeeded && !strings.HasPrefix(username, "user-") && (mb != nil || bb != nil || cb != nil || contactsB != nil || fnB != nil) {
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
	accountID := AccountIDForSubject(username)

	a.mu.Lock()
	if a.seededAccounts == nil {
		a.seededAccounts = make(map[string]bool)
	}
	alreadySeeded := a.seededAccounts[accountID]
	a.seededAccounts[accountID] = true
	mb, bb, cb, contactsB, fnB := a.mailBackend, a.blobBackend, a.calendarsBackend, a.contactsBackend, a.fileNodeBackend
	a.mu.Unlock()

	if !a.disableSeeding && !alreadySeeded && !strings.HasPrefix(username, "user-") && (mb != nil || bb != nil || cb != nil || contactsB != nil || fnB != nil) {
		SeedAccountSampleData(ctx, accountID, mb, bb, cb, contactsB, fnB)
	}

	return accountID, nil
}

// ValidateToken looks up the token and returns the associated accountID and subject.
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

// ExtractCredentials returns the username and password associated with the given Bearer token.
func (a *MemoryAuthBackend) ExtractCredentials(ctx context.Context, token string) (string, string, bool) {
	_, subj, err := a.ValidateToken(ctx, token)
	if err != nil {
		return "", "", false
	}
	return subj, subj, true
}

// generateToken creates a cryptographically random 32-byte hex token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SeedAccountSampleData populates sample emails, calendars, contacts, and filenodes for an account on first use.
func SeedAccountSampleData(ctx context.Context, accountID string, mb MailBackend, blobB BlobBackend, cb CalendarsBackend, contactsB ContactsBackend, fnB FileNodeBackend) {
	if ctx == nil {
		ctx = context.Background()
	}
	accountCtx := ContextWithAccountID(ctx, accountID)
	userEmail, _ := SubjectForAccountID(accountID)
	if userEmail == "" {
		userEmail = accountID
	}

	if mb != nil {
		SeedStandardMailboxes(accountCtx, mb)

		// Resolve the real mailbox IDs by role.
		roleIDs := make(map[string]Id)
		if mbs, err := mb.GetAllMailboxes(accountCtx); err == nil {
			for _, m := range mbs {
				if m != nil && m.Role != nil && *m.Role != "" {
					roleIDs[*m.Role] = m.ID
				}
			}
		}
		inboxID := roleIDs[RoleInbox]

		emails, _ := mb.GetAllEmails(accountCtx)
		if len(emails) == 0 && inboxID != "" {
			p1 := "1"
			s1 := "2026-08-01T11:59:00Z"
			s2 := "2026-08-02T10:29:00Z"
			if blobB != nil {
				_, _ = blobB.PutBlob(accountCtx, accountID, "text/plain", []byte("Welcome to your new JMAP mail server."))
			}
			stubStatus := "signed"
			stubVerifiedWith := "admin@example.com"
			stub1 := &Email{
				Subject:           "Welcome to JMAP Server",
				From:              []EmailAddress{{Name: "JMAP Admin", Email: "admin@example.com"}},
				To:                []EmailAddress{{Name: userEmail, Email: userEmail}},
				MailboxIDs:        map[Id]bool{inboxID: true},
				Keywords:          map[string]bool{"$seen": true},
				Size:              1024,
				ReceivedAt:        "2026-08-01T12:00:00Z",
				SentAt:            &s1,
				Preview:           "Welcome to your new JMAP mail server.",
				BlobID:            "blob-stub-1",
				SMIMEStatus:       &stubStatus,
				SMIMEVerifiedWith: &stubVerifiedWith,
				BodyStructure:     EmailBodyPart{PartID: &p1, Type: "text/plain", Size: 40},
				BodyValues:        map[string]EmailBodyValue{"1": {Value: "Welcome to your new JMAP mail server."}},
			}
			_, _ = mb.CreateEmail(accountCtx, stub1)

			stub2 := &Email{
				Subject:       "JMAP Core and Mail Specifications",
				From:          []EmailAddress{{Name: "IETF JMAP Working Group", Email: "noreply@ietf.org"}},
				To:            []EmailAddress{{Name: userEmail, Email: userEmail}},
				MailboxIDs:    map[Id]bool{inboxID: true},
				Keywords:      map[string]bool{"$flagged": true},
				Size:          4096,
				ReceivedAt:    "2026-08-02T10:30:00Z",
				SentAt:        &s2,
				Preview:       "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail).",
				BlobID:        "blob-stub-2",
				BodyStructure: EmailBodyPart{PartID: &p1, Type: "text/plain", Size: 88},
				BodyValues:    map[string]EmailBodyValue{"1": {Value: "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail)."}},
			}
			_, _ = mb.CreateEmail(accountCtx, stub2)
		}
	}

	if cb != nil {
		events, _, _ := cb.GetCalendarEvents(accountCtx, nil)
		if len(events) == 0 {
			ev1 := &CalendarEvent{
				ID:          "ev-seed-1",
				CalendarIDs: map[Id]bool{"cal-default": true},
				Type:        "Event",
				Title:       "Welcome & Onboarding",
				Description: "Initial team onboarding and platform overview.",
				Start:       "2026-08-05T10:00:00Z",
				Duration:    "PT1H",
				TimeZone:    "UTC",
				Status:      "confirmed",
			}
			_, _ = cb.CreateCalendarEvent(accountCtx, ev1)
		}
	}

	if contactsB != nil {
		cards, _, _ := contactsB.GetCards(accountCtx, nil)
		if len(cards) == 0 {
			card1 := &Card{
				ID:             "card-seed-1",
				AddressBookIDs: map[Id]bool{"ab-default": true},
				Type:           "Card",
				Version:        "1.0",
				Kind:           "individual",
				Name: &JSContactName{
					Full: "Alice Smith",
					Components: []*JSContactNameComponent{
						{Value: "Alice", Kind: "given"},
						{Value: "Smith", Kind: "surname"},
					},
				},
				Emails: map[string]*JSContactEmailAddress{"e1": {Address: "alice@example.com"}},
			}
			_, _ = contactsB.CreateCard(accountCtx, card1)
		}
	}

	if fnB != nil {
		nodes, _ := fnB.GetAllFileNodes(accountCtx)
		if len(nodes) == 0 {
			folderID := Id("fn-folder-documents")
			file1BlobID := Id("blob-seed-welcome")
			file2BlobID := Id("blob-seed-notes")

			folder := &FileNode{
				ID:       folderID,
				Name:     "Documents",
				IsFolder: true,
			}
			file1 := &FileNode{
				ID:       "fn-file-welcome",
				Name:     "welcome.txt",
				ParentID: &folderID,
				BlobID:   &file1BlobID,
				Size:     28,
				IsFolder: false,
			}
			file2 := &FileNode{
				ID:       "fn-file-notes",
				Name:     "notes.pdf",
				ParentID: &folderID,
				BlobID:   &file2BlobID,
				Size:     1024,
				IsFolder: false,
			}
			_, _ = fnB.CreateFileNode(accountCtx, folder)
			_, _ = fnB.CreateFileNode(accountCtx, file1)
			_, _ = fnB.CreateFileNode(accountCtx, file2)
		}
	}
}

// SeedStandardMailboxes ensures all standard role mailboxes expected by a mail
// client exist on a newly seeded account (idempotent helper).
func SeedStandardMailboxes(ctx context.Context, mb MailBackend) {
	existing, err := mb.GetAllMailboxes(ctx)
	if err != nil {
		return
	}
	hasRole := make(map[string]bool, len(existing))
	for _, mailbox := range existing {
		if mailbox != nil && mailbox.Role != nil {
			hasRole[*mailbox.Role] = true
		}
	}

	for _, spec := range []struct {
		id, name, role string
		sortOrder      uint64
	}{
		{id: "mb-sent", name: "Sent", role: RoleSent, sortOrder: 20},
		{id: "mb-drafts", name: "Drafts", role: RoleDrafts, sortOrder: 30},
		{id: "mb-junk", name: "Junk", role: RoleJunk, sortOrder: 40},
		{id: "mb-trash", name: "Trash", role: RoleTrash, sortOrder: 50},
		{id: "mb-archive", name: "Archive", role: RoleArchive, sortOrder: 60},
	} {
		if hasRole[spec.role] {
			continue
		}
		role := spec.role
		_, _ = mb.CreateMailbox(ctx, &Mailbox{
			ID:           Id(spec.id),
			Name:         spec.name,
			Role:         &role,
			SortOrder:    spec.sortOrder,
			IsSubscribed: true,
		})
	}
}
