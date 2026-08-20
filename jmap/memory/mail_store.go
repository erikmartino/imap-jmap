package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// MemoryBackend implements jmap.MailBackend for in-memory stub storage per RFC 8621 & RFC 9219.
type userMailStore struct {
	mailboxes         map[jmap.Id]*jmap.Mailbox
	threads           map[jmap.Id]*jmap.Thread
	emails            map[jmap.Id]*jmap.Email
	quotas            map[jmap.Id]*jmap.Quota
	identities        map[jmap.Id]*jmap.Identity
	submissions       map[jmap.Id]*jmap.EmailSubmission
	pushSubscriptions map[jmap.Id]*jmap.PushSubscription
	vacationResponse  *jmap.VacationResponse
	state             string

	mailboxState    *changeTracker
	threadState     *changeTracker
	emailState      *changeTracker
	identityState   *changeTracker
	submissionState *changeTracker
	quotaState      *changeTracker
	vacationState   *changeTracker
}

type MemoryBackend struct {
	mu          sync.RWMutex
	users       map[string]*userMailStore
	broadcaster *jmap.Broadcaster
	idCounter   uint64
}

func (mb *MemoryBackend) getStoreLocked(ctx context.Context) *userMailStore {
	accountID, _ := jmap.AccountIDFromContext(ctx)

	us, ok := mb.users[accountID]
	if !ok {
		us = newMemoryUserStore(accountID)
		mb.users[accountID] = us
	}
	return us
}

// Ensure MemoryBackend implements jmap.MailBackend interface.
var _ jmap.MailBackend = (*MemoryBackend)(nil)

// Ensure MemoryBackend implements jmap.BlobReferenceBackend interface.
var _ jmap.BlobReferenceBackend = (*MemoryBackend)(nil)

// SetBroadcaster connects a Broadcaster for SSE state notifications.
func (mb *MemoryBackend) SetBroadcaster(b *jmap.Broadcaster) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.broadcaster = b
}

func newMemoryUserStore(accountID string) *userMailStore {
	us := &userMailStore{
		mailboxes:         make(map[jmap.Id]*jmap.Mailbox),
		threads:           make(map[jmap.Id]*jmap.Thread),
		emails:            make(map[jmap.Id]*jmap.Email),
		quotas:            make(map[jmap.Id]*jmap.Quota),
		identities:        make(map[jmap.Id]*jmap.Identity),
		submissions:       make(map[jmap.Id]*jmap.EmailSubmission),
		pushSubscriptions: make(map[jmap.Id]*jmap.PushSubscription),
		vacationResponse:  &jmap.VacationResponse{ID: "singleton", IsEnabled: false},
		state:             "m1",

		mailboxState:    newChangeTracker(1000),
		threadState:     newChangeTracker(1000),
		emailState:      newChangeTracker(1000),
		identityState:   newChangeTracker(1000),
		submissionState: newChangeTracker(1000),
		quotaState:      newChangeTracker(1000),
		vacationState:   newChangeTracker(1000),
	}

	// Create default Quotas per RFC 9425
	quotaOctetsDesc := "Storage quota in bytes for account"
	us.quotas["quota-octets"] = &jmap.Quota{
		ID:           "quota-octets",
		Name:         "Storage Quota",
		ResourceType: "octets",
		Used:         3072,
		HardLimit:    10737418240, // 10 GB
		Scope:        "account",
		Description:  &quotaOctetsDesc,
	}

	quotaMessagesDesc := "Message count quota for account"
	us.quotas["quota-messages"] = &jmap.Quota{
		ID:           "quota-messages",
		Name:         "Message Count Quota",
		ResourceType: "messages",
		Used:         2,
		HardLimit:    100000,
		Scope:        "account",
		Description:  &quotaMessagesDesc,
	}

	// Create mandatory default Inbox mailbox per RFC 8621 Section 2.1.
	inboxRole := "inbox" // MUST provision per RFC 8621 Section 2.1
	inbox := &jmap.Mailbox{
		ID:            "mb-inbox",
		Name:          "Inbox",
		Role:          &inboxRole,
		SortOrder:     10,
		TotalEmails:   0,
		UnreadEmails:  0,
		TotalThreads:  0,
		UnreadThreads: 0,
		MyRights: jmap.MailboxRights{
			MayReadItems:   true,
			MayAddItems:    true,
			MayRemoveItems: true,
			MaySetSeen:     true,
			MaySetKeywords: true,
			MayCreateChild: true,
			MayRename:      false,
			MayDelete:      false,
			MaySubmit:      true,
			MayAdmin:       true,
		},
		IsSubscribed: true,
	}
	us.mailboxes[inbox.ID] = inbox

	// Default identity. The account ID is the base64url encoding of the user's
	// subject (email address), so recover the real address for the From identity
	// rather than surfacing the opaque account ID. Fall back only if it cannot be
	// decoded to a real address.
	emailAddr := accountID
	if subject, ok := jmap.SubjectForAccountID(accountID); ok && strings.Contains(subject, "@") {
		emailAddr = subject
	} else if !strings.Contains(emailAddr, "@") {
		emailAddr = accountID + "@example.com"
	}
	defaultIdentity := &jmap.Identity{
		ID:        "id-primary",
		Name:      emailAddr,
		Email:     emailAddr,
		MayDelete: true,
	}
	us.identities[defaultIdentity.ID] = defaultIdentity

	isDefaultUser := accountID == "" || accountID == jmap.AccountIDForSubject("user@example.com")
	if isDefaultUser {
		sentRole := "sent"
		trashRole := "trash"
		draftsRole := "drafts"
		junkRole := "junk"
		archiveRole := "archive"
		defaultMailboxes := []*jmap.Mailbox{
			{
				ID: "mb-sent", Name: "Sent", Role: &sentRole, SortOrder: 20, IsSubscribed: true,
				MyRights: jmap.MailboxRights{MayReadItems: true, MayAddItems: true, MayRemoveItems: true, MaySetSeen: true, MaySetKeywords: true, MayCreateChild: true, MayAdmin: true},
			},
			{
				ID: "mb-trash", Name: "Trash", Role: &trashRole, SortOrder: 50, IsSubscribed: true,
				MyRights: jmap.MailboxRights{MayReadItems: true, MayAddItems: true, MayRemoveItems: true, MaySetSeen: true, MaySetKeywords: true, MayCreateChild: true, MayAdmin: true},
			},
			{
				ID: "mb-drafts", Name: "Drafts", Role: &draftsRole, SortOrder: 30, IsSubscribed: true,
				MyRights: jmap.MailboxRights{MayReadItems: true, MayAddItems: true, MayRemoveItems: true, MaySetSeen: true, MaySetKeywords: true, MayCreateChild: true, MayAdmin: true},
			},
			{
				ID: "mb-junk", Name: "Junk", Role: &junkRole, SortOrder: 40, IsSubscribed: true,
				MyRights: jmap.MailboxRights{MayReadItems: true, MayAddItems: true, MayRemoveItems: true, MaySetSeen: true, MaySetKeywords: true, MayCreateChild: true, MayAdmin: true},
			},
			{
				ID: "mb-archive", Name: "Archive", Role: &archiveRole, SortOrder: 60, IsSubscribed: true,
				MyRights: jmap.MailboxRights{MayReadItems: true, MayAddItems: true, MayRemoveItems: true, MaySetSeen: true, MaySetKeywords: true, MayCreateChild: true, MayAdmin: true},
			},
		}
		for _, mb := range defaultMailboxes {
			us.mailboxes[mb.ID] = mb
		}

		p1 := "1"
		s1 := "2026-08-01T11:59:00Z"
		s2 := "2026-08-01T14:29:00Z"
		stubStatus := "signed"
		stubVerifiedWith := "admin@example.com"
		stub1 := &jmap.Email{
			ID:                "email-1",
			ThreadID:          "thread-2",
			Subject:           "Welcome to JMAP Server",
			From:              []jmap.EmailAddress{{Name: "JMAP Admin", Email: "admin@example.com"}},
			To:                []jmap.EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
			MailboxIDs:        map[jmap.Id]bool{"mb-inbox": true},
			Keywords:          map[string]bool{"$seen": true},
			Size:              1024,
			ReceivedAt:        "2026-08-01T12:00:00Z",
			SentAt:            &s1,
			Preview:           "Welcome to your new JMAP mail server. This server supports RFC 8620 and RFC 8621.",
			BlobID:            "blob-stub-1",
			SMIMEStatus:       &stubStatus,
			SMIMEVerifiedWith: &stubVerifiedWith,
			BodyStructure: jmap.EmailBodyPart{
				PartID: &p1,
				Type:   "text/plain",
				Size:   75,
			},
			BodyValues: map[string]jmap.EmailBodyValue{
				"1": {Value: "Welcome to your new JMAP mail server. This server supports RFC 8620 and RFC 8621."},
			},
		}
		us.emails[stub1.ID] = stub1
		us.threads[stub1.ThreadID] = &jmap.Thread{ID: stub1.ThreadID, EmailIDs: []jmap.Id{stub1.ID}}
		us.emailState.record(stub1.ID, "create")
		us.threadState.record(stub1.ThreadID, "create")

		stub2 := &jmap.Email{
			ID:         "email-3",
			ThreadID:   "thread-4",
			Subject:    "JMAP Core and Mail Specifications",
			From:       []jmap.EmailAddress{{Name: "IETF JMAP WG", Email: "noreply@ietf.org"}},
			To:         []jmap.EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
			MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
			Keywords:   map[string]bool{"$flagged": true},
			Size:       2048,
			ReceivedAt: "2026-08-01T14:30:00Z",
			SentAt:     &s2,
			Preview:    "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail).",
			BlobID:     "blob-stub-2",
			BodyStructure: jmap.EmailBodyPart{
				PartID: &p1,
				Type:   "text/plain",
				Size:   92,
			},
			BodyValues: map[string]jmap.EmailBodyValue{
				"1": {Value: "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail)."},
			},
		}
		us.emails[stub2.ID] = stub2
		us.threads[stub2.ThreadID] = &jmap.Thread{ID: stub2.ThreadID, EmailIDs: []jmap.Id{stub2.ID}}
		us.emailState.record(stub2.ID, "create")
		us.threadState.record(stub2.ThreadID, "create")

		us.mailboxes["mb-inbox"].TotalEmails = 2
		us.mailboxes["mb-inbox"].TotalThreads = 2
		us.mailboxes["mb-inbox"].UnreadEmails = 1
		us.mailboxes["mb-inbox"].UnreadThreads = 1
	}

	return us
}

// NewMemoryBackend initializes a new MemoryBackend pre-populated with standard default mailboxes and stub messages.
func NewMemoryBackend() *MemoryBackend {
	mb := &MemoryBackend{
		users:     make(map[string]*userMailStore),
		idCounter: 4,
	}
	// Pre-populate primary test user store
	_ = mb.getStoreLocked(context.Background())
	return mb
}

func (mb *MemoryBackend) nextID(prefix string) jmap.Id {
	mb.idCounter++
	return jmap.Id(fmt.Sprintf("%s-%d", prefix, mb.idCounter))
}

func (mb *MemoryBackend) recordChange(ctx context.Context, tracker *changeTracker, id jmap.Id, action string, typeName string) string {
	newState := tracker.record(id, action)
	us := mb.getStoreLocked(ctx)
	us.state = newState
	accountID, _ := jmap.AccountIDFromContext(ctx)
	if mb.broadcaster != nil {
		mb.broadcaster.PublishStateChange(accountID, typeName, newState)
	}
	for _, sub := range us.pushSubscriptions {
		if sub.VerificationCode == nil {
			if len(sub.Types) == 0 {
				go sendWebPushNotification(sub.URL, accountID, typeName, newState)
			} else {
				for _, t := range sub.Types {
					if strings.EqualFold(t, typeName) {
						go sendWebPushNotification(sub.URL, accountID, typeName, newState)
						break
					}
				}
			}
		}
	}
	return newState
}

func sendWebPushNotification(targetURL, accountID, typeName, newState string) {
	if !strings.HasPrefix(strings.ToLower(targetURL), "https://") && !strings.HasPrefix(strings.ToLower(targetURL), "http://") {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"@type": "StateChange",
		"changed": map[string]map[string]string{
			accountID: {
				typeName: newState,
			},
		},
	})
	if err != nil {
		return
	}
	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(payload))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}
}

// State returns current mail change state token.
func (mb *MemoryBackend) State(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.state
}

// MailboxState returns current change state token for Mailbox resources.
