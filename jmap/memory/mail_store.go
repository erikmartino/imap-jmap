package memory

import (
	"context"
	"fmt"
	"sort"
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
		ID:    "id-primary",
		Name:  emailAddr,
		Email: emailAddr,
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
	if mb.broadcaster != nil {
		accountID, _ := jmap.AccountIDFromContext(ctx)
		mb.broadcaster.PublishStateChange(accountID, typeName, newState)
	}
	return newState
}

// State returns current mail change state token.
func (mb *MemoryBackend) State(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.state
}

// MailboxState returns current change state token for Mailbox resources.
func (mb *MemoryBackend) MailboxState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.mailboxState.State()
}

// MailboxChanges returns created, updated, and destroyed Mailboxes since sinceState.
func (mb *MemoryBackend) MailboxChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, []string, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.mailboxState.MailboxChanges(sinceState, maxChanges)
}

// ThreadState returns current change state token for Thread resources.
func (mb *MemoryBackend) ThreadState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.threadState.State()
}

// ThreadChanges returns created, updated, and destroyed Threads since sinceState.
func (mb *MemoryBackend) ThreadChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.threadState.Changes(sinceState, maxChanges)
}

// EmailState returns current change state token for Email resources.
func (mb *MemoryBackend) EmailState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.emailState.State()
}

// EmailChanges returns created, updated, and destroyed Emails since sinceState.
func (mb *MemoryBackend) EmailChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.emailState.Changes(sinceState, maxChanges)
}

// IdentityState returns current change state token for Identity resources.
func (mb *MemoryBackend) IdentityState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.identityState.State()
}

// IdentityChanges returns created, updated, and destroyed Identities since sinceState.
func (mb *MemoryBackend) IdentityChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.identityState.Changes(sinceState, maxChanges)
}

// SubmissionState returns current change state token for EmailSubmission resources.
func (mb *MemoryBackend) SubmissionState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.submissionState.State()
}

// SubmissionChanges returns created, updated, and destroyed EmailSubmissions since sinceState.
func (mb *MemoryBackend) SubmissionChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.submissionState.Changes(sinceState, maxChanges)
}

// QuotaState returns current change state token for Quota resources.
func (mb *MemoryBackend) QuotaState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.quotaState.State()
}

// QuotaChanges returns created, updated, and destroyed Quotas since sinceState.
func (mb *MemoryBackend) QuotaChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.quotaState.Changes(sinceState, maxChanges)
}

// GetMailboxes retrieves requested mailboxes by ID.
func (mb *MemoryBackend) GetMailboxes(ctx context.Context, ids []jmap.Id) ([]*jmap.Mailbox, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.Mailbox
	var notFound []jmap.Id

	for _, id := range ids {
		if item, ok := us.mailboxes[id]; ok {
			list = append(list, item)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllMailboxes retrieves all mailboxes.
func (mb *MemoryBackend) GetAllMailboxes(ctx context.Context) ([]*jmap.Mailbox, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.Mailbox, 0, len(us.mailboxes))
	for _, item := range us.mailboxes {
		list = append(list, item)
	}
	return list, nil
}

// CreateMailbox creates a new mailbox.
func (mb *MemoryBackend) CreateMailbox(ctx context.Context, item *jmap.Mailbox) (*jmap.Mailbox, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if item.ID == "" {
		item.ID = mb.nextID("mb")
	}
	item.MyRights = jmap.MailboxRights{
		MayReadItems:   true,
		MayAddItems:    true,
		MayRemoveItems: true,
		MaySetSeen:     true,
		MaySetKeywords: true,
		MayCreateChild: true,
		MayRename:      true,
		MayDelete:      true,
		MaySubmit:      true,
		MayAdmin:       true,
	}
	us.mailboxes[item.ID] = item
	mb.recordChange(ctx, us.mailboxState, item.ID, "create", "Mailbox")
	return item, nil
}

// UpdateMailbox applies a partial patch to a mailbox (RFC 8621 Section 2.5), preserving
// unaddressed fields. Counts and rights are server-set and cannot be patched by the client.
func (mb *MemoryBackend) UpdateMailbox(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Mailbox, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	item, ok := us.mailboxes[id]
	if !ok {
		mb.mu.Unlock()
		return nil, fmt.Errorf("mailbox %s: %w", id, jmap.ErrNotFound)
	}

	var invalidProps []string

	for prop, val := range patch {
		switch prop {
		case "name":
			name, ok := val.(string)
			if !ok || name == "" {
				invalidProps = append(invalidProps, "name")
				continue
			}
			item.Name = name
		case "parentId":
			if val == nil {
				item.ParentID = nil
				continue
			}
			pid, ok := val.(string)
			if !ok {
				invalidProps = append(invalidProps, "parentId")
				continue
			}
			if jmap.Id(pid) == id {
				invalidProps = append(invalidProps, "parentId")
				continue
			}
			if _, exists := us.mailboxes[jmap.Id(pid)]; !exists {
				invalidProps = append(invalidProps, "parentId")
				continue
			}
			p := jmap.Id(pid)
			item.ParentID = &p
		case "role":
			if val == nil {
				item.Role = nil
				continue
			}
			role, ok := val.(string)
			if !ok {
				invalidProps = append(invalidProps, "role")
				continue
			}
			item.Role = &role
		case "sortOrder":
			if f, ok := val.(float64); ok {
				item.SortOrder = uint64(f)
			} else {
				invalidProps = append(invalidProps, "sortOrder")
			}
		case "isSubscribed":
			if b, ok := val.(bool); ok {
				item.IsSubscribed = b
			} else {
				invalidProps = append(invalidProps, "isSubscribed")
			}
		case "id":
			if s, ok := val.(string); !ok || jmap.Id(s) != item.ID {
				invalidProps = append(invalidProps, "id")
			}
		case "totalEmails":
			if f, ok := val.(float64); !ok || uint64(f) != item.TotalEmails {
				invalidProps = append(invalidProps, "totalEmails")
			}
		case "unreadEmails":
			if f, ok := val.(float64); !ok || uint64(f) != item.UnreadEmails {
				invalidProps = append(invalidProps, "unreadEmails")
			}
		case "totalThreads":
			if f, ok := val.(float64); !ok || uint64(f) != item.TotalThreads {
				invalidProps = append(invalidProps, "totalThreads")
			}
		case "unreadThreads":
			if f, ok := val.(float64); !ok || uint64(f) != item.UnreadThreads {
				invalidProps = append(invalidProps, "unreadThreads")
			}
		case "myRights":
			if m, ok := val.(map[string]any); ok {
				for rk, rv := range m {
					b, isBool := rv.(bool)
					var curr bool
					switch rk {
					case "mayReadItems":
						curr = item.MyRights.MayReadItems
					case "mayAddItems":
						curr = item.MyRights.MayAddItems
					case "mayRemoveItems":
						curr = item.MyRights.MayRemoveItems
					case "maySetSeen":
						curr = item.MyRights.MaySetSeen
					case "maySetKeywords":
						curr = item.MyRights.MaySetKeywords
					case "mayCreateChild":
						curr = item.MyRights.MayCreateChild
					case "mayRename":
						curr = item.MyRights.MayRename
					case "mayDelete":
						curr = item.MyRights.MayDelete
					case "maySubmit":
						curr = item.MyRights.MaySubmit
					case "mayAdmin":
						curr = item.MyRights.MayAdmin
					default:
						invalidProps = append(invalidProps, "myRights/"+rk)
						continue
					}
					if !isBool || b != curr {
						invalidProps = append(invalidProps, "myRights/"+rk)
					}
				}
			} else {
				invalidProps = append(invalidProps, "myRights")
			}
		default:
			if strings.HasPrefix(prop, "myRights/") {
				rk := strings.TrimPrefix(prop, "myRights/")
				b, isBool := val.(bool)
				var curr bool
				switch rk {
				case "mayReadItems":
					curr = item.MyRights.MayReadItems
				case "mayAddItems":
					curr = item.MyRights.MayAddItems
				case "mayRemoveItems":
					curr = item.MyRights.MayRemoveItems
				case "maySetSeen":
					curr = item.MyRights.MaySetSeen
				case "maySetKeywords":
					curr = item.MyRights.MaySetKeywords
				case "mayCreateChild":
					curr = item.MyRights.MayCreateChild
				case "mayRename":
					curr = item.MyRights.MayRename
				case "mayDelete":
					curr = item.MyRights.MayDelete
				case "maySubmit":
					curr = item.MyRights.MaySubmit
				case "mayAdmin":
					curr = item.MyRights.MayAdmin
				default:
					invalidProps = append(invalidProps, prop)
					continue
				}
				if !isBool || b != curr {
					invalidProps = append(invalidProps, prop)
				}
			} else {
				invalidProps = append(invalidProps, prop)
			}
		}
	}
	if len(invalidProps) > 0 {
		mb.mu.Unlock()
		return nil, jmap.SetError{
			Type:       "invalidProperties",
			Properties: invalidProps,
		}
	}
	mb.mu.Unlock()

	mb.recordChange(ctx, us.mailboxState, id, "update", "Mailbox")
	return item, nil
}

// DeleteMailbox deletes a mailbox by ID.
func (mb *MemoryBackend) DeleteMailbox(ctx context.Context, id jmap.Id, onDestroyRemoveMessages bool) (bool, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if _, ok := us.mailboxes[id]; !ok {
		return false, nil
	}

	// Check if mailbox has children
	for _, other := range us.mailboxes {
		if other.ParentID != nil && *other.ParentID == id {
			return false, jmap.SetError{
				Type: "mailboxHasChild",
			}
		}
	}

	// Check if mailbox has emails
	var emailsInMailbox []*jmap.Email
	for _, em := range us.emails {
		if em.MailboxIDs[id] {
			emailsInMailbox = append(emailsInMailbox, em)
		}
	}

	if len(emailsInMailbox) > 0 {
		if !onDestroyRemoveMessages {
			return false, jmap.SetError{
				Type: "mailboxHasEmail",
			}
		}
		// Destroy or update messages
		for _, em := range emailsInMailbox {
			if len(em.MailboxIDs) <= 1 {
				// Destroy email
				delete(us.emails, em.ID)
				if th, ok := us.threads[em.ThreadID]; ok {
					var newIDs []jmap.Id
					for _, eid := range th.EmailIDs {
						if eid != em.ID {
							newIDs = append(newIDs, eid)
						}
					}
					if len(newIDs) == 0 {
						delete(us.threads, em.ThreadID)
						mb.recordChange(ctx, us.threadState, em.ThreadID, "destroy", "Thread")
					} else {
						th.EmailIDs = newIDs
						mb.recordChange(ctx, us.threadState, em.ThreadID, "update", "Thread")
					}
				}
				mb.recordChange(ctx, us.emailState, em.ID, "destroy", "Email")
			} else {
				// Remove from this mailbox
				delete(em.MailboxIDs, id)
				mb.recordChange(ctx, us.emailState, em.ID, "update", "Email")
			}
		}
	}

	delete(us.mailboxes, id)
	mb.recalculateMailboxCounts(us)
	mb.recordChange(ctx, us.mailboxState, id, "destroy", "Mailbox")
	return true, nil
}

// GetThreads retrieves threads by ID.
func (mb *MemoryBackend) GetThreads(ctx context.Context, ids []jmap.Id) ([]*jmap.Thread, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.Thread
	var notFound []jmap.Id

	for _, id := range ids {
		if item, ok := us.threads[id]; ok {
			list = append(list, item)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllThreads retrieves all threads.
func (mb *MemoryBackend) GetAllThreads(ctx context.Context) ([]*jmap.Thread, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.Thread, 0, len(us.threads))
	for _, item := range us.threads {
		list = append(list, item)
	}
	return list, nil
}

// GetEmails retrieves emails by ID.
func (mb *MemoryBackend) GetEmails(ctx context.Context, ids []jmap.Id) ([]*jmap.Email, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.Email
	var notFound []jmap.Id

	for _, id := range ids {
		if item, ok := us.emails[id]; ok {
			list = append(list, item)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllEmails retrieves all emails.
func (mb *MemoryBackend) GetAllEmails(ctx context.Context) ([]*jmap.Email, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.Email, 0, len(us.emails))
	for _, item := range us.emails {
		list = append(list, item)
	}
	return list, nil
}

// CreateEmail creates a new email.
func (mb *MemoryBackend) CreateEmail(ctx context.Context, em *jmap.Email) (*jmap.Email, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if em.ID == "" {
		em.ID = mb.nextID("email")
	}

	if em.ThreadID == "" {
		// Look for existing thread via In-Reply-To / References / Message-ID
		var referencedMIDs []string
		for _, ref := range em.InReplyTo {
			s := strings.Trim(ref, "<> \t")
			if s != "" {
				referencedMIDs = append(referencedMIDs, s)
			}
		}
		for _, ref := range em.References {
			s := strings.Trim(ref, "<> \t")
			if s != "" {
				referencedMIDs = append(referencedMIDs, s)
			}
		}
		for _, h := range em.Headers {
			if strings.EqualFold(h.Name, "In-Reply-To") || strings.EqualFold(h.Name, "References") {
				for _, part := range strings.Fields(h.Value) {
					s := strings.Trim(part, "<> \t")
					if s != "" {
						referencedMIDs = append(referencedMIDs, s)
					}
				}
			}
		}

		for _, ref := range referencedMIDs {
			for _, other := range us.emails {
				for _, mid := range other.MessageID {
					if strings.Trim(mid, "<> \t") == ref {
						em.ThreadID = other.ThreadID
						break
					}
				}
				if em.ThreadID != "" {
					break
				}
				for _, h := range other.Headers {
					if strings.EqualFold(h.Name, "Message-ID") {
						if strings.Trim(h.Value, "<> \t") == ref {
							em.ThreadID = other.ThreadID
							break
						}
					}
				}
				if em.ThreadID != "" {
					break
				}
			}
			if em.ThreadID != "" {
				break
			}
		}
	}

	if em.ThreadID == "" {
		em.ThreadID = mb.nextID("thread")
	}
	if em.MailboxIDs == nil {
		em.MailboxIDs = make(map[jmap.Id]bool)
	}
	if em.Keywords == nil {
		em.Keywords = make(map[string]bool)
	} else {
		lowered := make(map[string]bool, len(em.Keywords))
		for k, v := range em.Keywords {
			lowered[strings.ToLower(k)] = v
		}
		em.Keywords = lowered
	}

	us.emails[em.ID] = em

	// Update Thread
	th, ok := us.threads[em.ThreadID]
	if !ok {
		th = &jmap.Thread{
			ID:       em.ThreadID,
			EmailIDs: []jmap.Id{em.ID},
		}
		us.threads[em.ThreadID] = th
		mb.recordChange(ctx, us.threadState, em.ThreadID, "create", "Thread")
	} else {
		th.EmailIDs = append(th.EmailIDs, em.ID)
		sort.SliceStable(th.EmailIDs, func(i, j int) bool {
			emA := us.emails[th.EmailIDs[i]]
			emB := us.emails[th.EmailIDs[j]]
			if emA == nil || emB == nil {
				return false
			}
			tA := emA.ReceivedAt
			if tA == "" {
				if emA.SentAt != nil {
					tA = *emA.SentAt
				}
			}
			tB := emB.ReceivedAt
			if tB == "" {
				if emB.SentAt != nil {
					tB = *emB.SentAt
				}
			}
			return tA < tB
		})
		mb.recordChange(ctx, us.threadState, em.ThreadID, "update", "Thread")
	}

	mb.recalculateMailboxCounts(us)
	mb.recordChange(ctx, us.emailState, em.ID, "create", "Email")
	for mID := range em.MailboxIDs {
		mb.recordChange(ctx, us.mailboxState, mID, "counts", "Mailbox")
	}
	return em, nil
}

func (mb *MemoryBackend) recalculateMailboxCounts(us *userMailStore) {
	counts := make(map[jmap.Id]*struct{ unread, total uint64 })
	for mID := range us.mailboxes {
		counts[mID] = &struct{ unread, total uint64 }{}
	}

	for _, em := range us.emails {
		isUnread := true
		if em.Keywords != nil {
			if val, hasUnread := em.Keywords["$unread"]; hasUnread {
				isUnread = val
			} else if val, hasSeen := em.Keywords["$seen"]; hasSeen {
				isUnread = !val
			}
		}
		for mID := range em.MailboxIDs {
			if c, ok := counts[mID]; ok {
				c.total++
				if isUnread {
					c.unread++
				}
			}
		}
	}

	for mID, c := range counts {
		if box, ok := us.mailboxes[mID]; ok {
			box.TotalEmails = c.total
			box.UnreadEmails = c.unread
			box.TotalThreads = c.total
			box.UnreadThreads = c.unread
		}
	}
}

// UpdateEmail applies RFC 8621 Section 4.3 patch objects to an existing Email.
func (mb *MemoryBackend) UpdateEmail(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Email, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	em, ok := us.emails[id]
	if !ok {
		return nil, fmt.Errorf("notFound")
	}

	if em.Keywords == nil {
		em.Keywords = make(map[string]bool)
	}
	if em.MailboxIDs == nil {
		em.MailboxIDs = make(map[jmap.Id]bool)
	}

	// Record the mailboxes that may have their counts changed, including any that the
	// patch removes from the email, before applying the patch.
	affected := make(map[jmap.Id]bool)
	for mID := range em.MailboxIDs {
		affected[mID] = true
	}

	for path, val := range patch {
		if path == "keywords" {
			if kwMap, ok := val.(map[string]any); ok {
				em.Keywords = make(map[string]bool)
				for k, v := range kwMap {
					if boolVal, ok := v.(bool); ok {
						em.Keywords[strings.ToLower(k)] = boolVal
					}
				}
			}
		} else if strings.HasPrefix(path, "keywords/") {
			kw := strings.ToLower(strings.TrimPrefix(path, "keywords/"))
			if val == nil {
				delete(em.Keywords, kw)
			} else if boolVal, ok := val.(bool); ok {
				em.Keywords[kw] = boolVal
			}
		} else if path == "mailboxIds" {
			if mbMap, ok := val.(map[string]any); ok {
				em.MailboxIDs = make(map[jmap.Id]bool)
				for k, v := range mbMap {
					if boolVal, ok := v.(bool); ok {
						if boolVal {
							em.MailboxIDs[jmap.Id(k)] = true
						}
					} else if v != nil {
						em.MailboxIDs[jmap.Id(k)] = true
					}
				}
			}
		} else if strings.HasPrefix(path, "mailboxIds/") {
			mID := jmap.Id(strings.TrimPrefix(path, "mailboxIds/"))
			if val == nil {
				delete(em.MailboxIDs, mID)
			} else if boolVal, ok := val.(bool); ok {
				if boolVal {
					em.MailboxIDs[mID] = true
				} else {
					delete(em.MailboxIDs, mID)
				}
			} else {
				em.MailboxIDs[mID] = true
			}
		}
	}

	mb.recalculateMailboxCounts(us)
	mb.recordChange(ctx, us.emailState, id, "update", "Email")
	for mID := range em.MailboxIDs {
		affected[mID] = true
	}
	for mID := range affected {
		mb.recordChange(ctx, us.mailboxState, mID, "counts", "Mailbox")
	}
	return em, nil
}

// DeleteEmail deletes an email by ID.
func (mb *MemoryBackend) DeleteEmail(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	em, ok := us.emails[id]
	if !ok {
		return false, nil
	}

	// The mailboxes the email lived in have their counts changed by the deletion.
	affected := make(map[jmap.Id]bool)
	for mID := range em.MailboxIDs {
		affected[mID] = true
	}
	delete(us.emails, id)

	// Remove from thread
	if th, ok := us.threads[em.ThreadID]; ok {
		newIDs := make([]jmap.Id, 0, len(th.EmailIDs))
		for _, eid := range th.EmailIDs {
			if eid != id {
				newIDs = append(newIDs, eid)
			}
		}
		if len(newIDs) == 0 {
			delete(us.threads, em.ThreadID)
			mb.recordChange(ctx, us.threadState, em.ThreadID, "destroy", "Thread")
		} else {
			th.EmailIDs = newIDs
			mb.recordChange(ctx, us.threadState, em.ThreadID, "update", "Thread")
		}
	}

	mb.recalculateMailboxCounts(us)
	mb.recordChange(ctx, us.emailState, id, "destroy", "Email")
	for mID := range affected {
		mb.recordChange(ctx, us.mailboxState, mID, "counts", "Mailbox")
	}
	return true, nil
}

// QueryEmails evaluates filters, sorting, and pagination per RFC 8621 Section 4.5.
func (mb *MemoryBackend) QueryEmails(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	threadEmailsCount := make(map[jmap.Id]int)
	threadEmailsWithKw := make(map[jmap.Id]map[string]int)
	for _, em := range us.emails {
		threadEmailsCount[em.ThreadID]++
		if _, ok := threadEmailsWithKw[em.ThreadID]; !ok {
			threadEmailsWithKw[em.ThreadID] = make(map[string]int)
		}
		for kw, val := range em.Keywords {
			if val {
				threadEmailsWithKw[em.ThreadID][kw]++
			}
		}
	}
	tc := &jmap.ThreadFilterContext{
		ThreadEmailsCount:  threadEmailsCount,
		ThreadEmailsWithKw: threadEmailsWithKw,
	}

	var matched []*jmap.Email
	for _, em := range us.emails {
		if jmap.MatchesFilterWithThreadContext(em, filter, tc) {
			matched = append(matched, em)
		}
	}

	// Thread keyword sorts evaluate the keyword over every Email in the thread, not just the
	// filtered results (RFC 8621 Section 4.4.2).
	threadHas := make(map[string]bool)
	threadLacks := make(map[string]bool)
	for _, em := range us.emails {
		for _, c := range comparators {
			if c.Property != "allInThreadHaveKeyword" && c.Property != "someInThreadHaveKeyword" {
				continue
			}
			key := string(em.ThreadID) + "\x00" + c.Keyword
			if em.Keywords != nil && em.Keywords[c.Keyword] {
				threadHas[key] = true
			} else {
				threadLacks[key] = true
			}
		}
	}
	all := make(map[string]bool, len(threadHas))
	for key := range threadHas {
		all[key] = !threadLacks[key]
	}

	total := len(matched)
	jmap.SortEmailsWithContext(matched, comparators, all, threadHas)

	position = jmap.NormalizePosition(position, total)
	if position > len(matched) {
		return []jmap.Id{}, total, nil
	}

	end := len(matched)
	if limit != nil {
		l := int(*limit)
		if position+l < end {
			end = position + l
		}
	}

	slice := matched[position:end]
	ids := make([]jmap.Id, len(slice))
	for i, em := range slice {
		ids[i] = em.ID
	}

	return ids, total, nil
}

// VerifySmime implements RFC 9219 Section 4 Email/verifySmime.
func (mb *MemoryBackend) VerifySmime(ctx context.Context, ids []jmap.Id) (map[jmap.Id]*jmap.SmimeVerificationResult, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	verified := make(map[jmap.Id]*jmap.SmimeVerificationResult)
	var notFound []jmap.Id
	now := time.Now().UTC().Format(time.RFC3339)

	for _, id := range ids {
		em, ok := us.emails[id]
		if !ok {
			notFound = append(notFound, id)
			continue
		}

		status := "unsigned"
		if em.SMIMEStatus != nil {
			status = *em.SMIMEStatus
		}

		res := &jmap.SmimeVerificationResult{
			SmimeStatus:       status,
			SmimeStatusAt:     now,
			SmimeVerifiedWith: em.SMIMEVerifiedWith,
			SmimeErrors:       em.SMIMEErrors,
		}
		verified[id] = res
	}

	return verified, notFound, nil
}

// GetIdentities retrieves all identities.
func (mb *MemoryBackend) GetIdentities(ctx context.Context) ([]*jmap.Identity, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.Identity, 0, len(us.identities))
	for _, item := range us.identities {
		list = append(list, item)
	}
	return list, nil
}

// CreateIdentity creates a new Identity (RFC 8621 Section 6.3).
func (mb *MemoryBackend) CreateIdentity(ctx context.Context, identity *jmap.Identity) (*jmap.Identity, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	if identity.Email == "" {
		mb.mu.Unlock()
		return nil, fmt.Errorf("email is required")
	}
	if identity.ID == "" {
		identity.ID = mb.nextID("identity")
	}
	us.identities[identity.ID] = identity
	mb.mu.Unlock()

	mb.recordChange(ctx, us.identityState, identity.ID, "create", "Identity")
	return identity, nil
}

// UpdateIdentity applies a partial patch to an existing Identity, preserving unaddressed fields.
func (mb *MemoryBackend) UpdateIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Identity, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	identity, ok := us.identities[id]
	if !ok {
		mb.mu.Unlock()
		return nil, fmt.Errorf("identity %s: %w", id, jmap.ErrNotFound)
	}

	// The "email" property is server-set and immutable per RFC 8621 Section 6.
	if _, present := patch["email"]; present {
		mb.mu.Unlock()
		return nil, fmt.Errorf("email is immutable")
	}
	if v, ok := patch["name"].(string); ok {
		identity.Name = v
	}
	if v, ok := patch["textSignature"].(string); ok {
		identity.TextSignature = v
	}
	if v, ok := patch["htmlSignature"].(string); ok {
		identity.HTMLSignature = v
	}
	mb.mu.Unlock()

	mb.recordChange(ctx, us.identityState, id, "update", "Identity")
	return identity, nil
}

// DeleteIdentity removes an Identity.
func (mb *MemoryBackend) DeleteIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	if _, ok := us.identities[id]; !ok {
		mb.mu.Unlock()
		return false, nil
	}
	delete(us.identities, id)
	mb.mu.Unlock()

	mb.recordChange(ctx, us.identityState, id, "destroy", "Identity")
	return true, nil
}

// VacationResponseState returns the change token for the VacationResponse singleton (RFC 8621 Section 8).
func (mb *MemoryBackend) VacationResponseState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return mb.getStoreLocked(ctx).vacationState.State()
}

// GetVacationResponse returns the per-account VacationResponse singleton (id "singleton").
func (mb *MemoryBackend) GetVacationResponse(ctx context.Context) (*jmap.VacationResponse, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	cp := *us.vacationResponse
	return &cp, nil
}

// UpdateVacationResponse applies a partial patch to the VacationResponse singleton, leaving
// unaddressed properties untouched (RFC 8621 Section 8.2 / RFC 8620 Section 5.3).
func (mb *MemoryBackend) UpdateVacationResponse(ctx context.Context, patch map[string]any) (*jmap.VacationResponse, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	vr := us.vacationResponse

	setStrPtr := func(v any, dst **string) {
		if v == nil {
			*dst = nil
			return
		}
		if s, ok := v.(string); ok {
			*dst = &s
		}
	}
	for k, v := range patch {
		switch k {
		case "id":
			// Server-set; ignore.
		case "isEnabled":
			if b, ok := v.(bool); ok {
				vr.IsEnabled = b
			}
		case "fromDate":
			setStrPtr(v, &vr.FromDate)
		case "toDate":
			setStrPtr(v, &vr.ToDate)
		case "subject":
			setStrPtr(v, &vr.Subject)
		case "textBody":
			setStrPtr(v, &vr.TextBody)
		case "htmlBody":
			setStrPtr(v, &vr.HTMLBody)
		default:
			mb.mu.Unlock()
			return nil, fmt.Errorf("unknown VacationResponse property: %s", k)
		}
	}
	cp := *vr
	mb.mu.Unlock()

	mb.recordChange(ctx, us.vacationState, "singleton", "update", "VacationResponse")
	return &cp, nil
}

// CreateSubmission creates an EmailSubmission.
func (mb *MemoryBackend) CreateSubmission(ctx context.Context, sub *jmap.EmailSubmission) (*jmap.EmailSubmission, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if sub.ID == "" {
		sub.ID = mb.nextID("sub")
	}
	if sub.SendAt == "" {
		sub.SendAt = time.Now().UTC().Format(time.RFC3339)
	}
	// RFC 8621 Section 7.1: threadId is server-set and must match the referenced email.
	if sub.ThreadID == "" {
		if em, ok := us.emails[sub.EmailID]; ok {
			sub.ThreadID = em.ThreadID
		}
	}
	if sub.UndoStatus == "" {
		if t, err := time.Parse(time.RFC3339, sub.SendAt); err == nil && t.After(time.Now().UTC().Add(1*time.Second)) {
			sub.UndoStatus = "pending"
		} else {
			sub.UndoStatus = "final"
		}
	}
	if sub.DeliveryStatus == nil {
		sub.DeliveryStatus = make(map[string]jmap.DeliveryStatus)
	}

	us.submissions[sub.ID] = sub
	mb.recordChange(ctx, us.submissionState, sub.ID, "create", "EmailSubmission")
	return sub, nil
}

// UpdateSubmission applies a patch to an EmailSubmission per RFC 8621 Section 7.5.
// EmailSubmission objects are immutable except for updating undoStatus to "canceled" when pending.
func (mb *MemoryBackend) UpdateSubmission(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.EmailSubmission, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	sub, ok := us.submissions[id]
	if !ok {
		return nil, fmt.Errorf("notFound: submission not found")
	}

	for k, v := range patch {
		cleanK := strings.TrimPrefix(k, "/")
		switch cleanK {
		case "undoStatus":
			newStatus, _ := v.(string)
			if newStatus != "canceled" {
				return nil, fmt.Errorf("invalidProperties: undoStatus can only be updated to canceled")
			}
			if sub.UndoStatus == "final" {
				return nil, fmt.Errorf("cannotCancel: submission is already final")
			}
			if sub.UndoStatus == "canceled" {
				return nil, fmt.Errorf("alreadyCanceled: submission is already canceled")
			}
			sub.UndoStatus = "canceled"
			for rcpt, ds := range sub.DeliveryStatus {
				if ds.Delivered == "queued" || ds.Delivered == "pending" {
					ds.Delivered = "no"
					ds.SmtpReply = "canceled by user"
					sub.DeliveryStatus[rcpt] = ds
				}
			}
		default:
			return nil, fmt.Errorf("invalidProperties: EmailSubmission property %q cannot be updated", k)
		}
	}

	mb.recordChange(ctx, us.submissionState, sub.ID, "update", "EmailSubmission")
	return sub, nil
}

// DeleteSubmission deletes an EmailSubmission.
func (mb *MemoryBackend) DeleteSubmission(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if _, ok := us.submissions[id]; !ok {
		return false, nil
	}
	delete(us.submissions, id)
	mb.recordChange(ctx, us.submissionState, id, "destroy", "EmailSubmission")
	return true, nil
}

// GetSubmissions retrieves EmailSubmissions by ID.
func (mb *MemoryBackend) GetSubmissions(ctx context.Context, ids []jmap.Id) ([]*jmap.EmailSubmission, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.EmailSubmission
	var notFound []jmap.Id

	for _, id := range ids {
		if sub, ok := us.submissions[id]; ok {
			list = append(list, sub)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllSubmissions retrieves all EmailSubmissions.
func (mb *MemoryBackend) GetAllSubmissions(ctx context.Context) ([]*jmap.EmailSubmission, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.EmailSubmission, 0, len(us.submissions))
	for _, sub := range us.submissions {
		list = append(list, sub)
	}
	return list, nil
}

// LookupBlobReferences implements jmap.BlobReferenceBackend per RFC 9404 Section 4.3: for a
// blob id, which objects of the requested JMAP data types reference it.
func (mb *MemoryBackend) LookupBlobReferences(ctx context.Context, typeNames []string, blobID jmap.Id) (map[string][]jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	matched := make(map[string][]jmap.Id)
	for _, tn := range typeNames {
		switch tn {
		case "Email":
			for _, em := range us.emails {
				if em.BlobID == blobID || emailReferencesBlob(em, blobID) {
					matched["Email"] = append(matched["Email"], em.ID)
				}
			}
		case "Thread":
			for _, th := range us.threads {
				for _, emailID := range th.EmailIDs {
					if em, ok := us.emails[emailID]; ok && (em.BlobID == blobID || emailReferencesBlob(em, blobID)) {
						matched["Thread"] = append(matched["Thread"], th.ID)
						break
					}
				}
			}
		case "Mailbox":
			// No Mailbox property currently references blobs, so nothing can match.
		}
	}
	return matched, nil
}

// emailReferencesBlob reports whether an email references a blob via its attachments.
func emailReferencesBlob(em *jmap.Email, blobID jmap.Id) bool {
	for _, att := range em.Attachments {
		if att.BlobID != nil && *att.BlobID == blobID {
			return true
		}
	}
	return false
}

// MatchSubmissionFilter checks if an EmailSubmission matches a FilterCondition or FilterOperator.
func MatchSubmissionFilter(sub *jmap.EmailSubmission, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}

	// FilterOperator: operator (AND/OR/NOT) + conditions
	if opRaw, ok := filter["operator"].(string); ok {
		condsRaw, ok := filter["conditions"].([]any)
		if !ok {
			return true
		}
		op := strings.ToUpper(opRaw)
		switch op {
		case "AND":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					if !MatchSubmissionFilter(sub, condMap) {
						return false
					}
				}
			}
			return true
		case "OR":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					if MatchSubmissionFilter(sub, condMap) {
						return true
					}
				}
			}
			return len(condsRaw) == 0
		case "NOT":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					if MatchSubmissionFilter(sub, condMap) {
						return false
					}
				}
			}
			return true
		}
	}

	identityFilter := submissionIDFilter(filter, "identityIds")
	emailFilter := submissionIDFilter(filter, "emailIds")
	threadFilter := submissionIDFilter(filter, "threadIds")
	undoStatus, _ := filter["undoStatus"].(string)
	before, _ := filter["before"].(string)
	after, _ := filter["after"].(string)

	if len(identityFilter) > 0 && !identityFilter[sub.IdentityID] {
		return false
	}
	if len(emailFilter) > 0 && !emailFilter[sub.EmailID] {
		return false
	}
	if len(threadFilter) > 0 && !threadFilter[sub.ThreadID] {
		return false
	}
	if undoStatus != "" && sub.UndoStatus != undoStatus {
		return false
	}
	if before != "" && sub.SendAt >= before {
		return false
	}
	if after != "" && sub.SendAt < after {
		return false
	}
	return true
}

// QuerySubmissions filters, sorts, and pages EmailSubmissions per RFC 8621 Section 7.2.
// Supported filter conditions: identityIds, emailIds, threadIds, undoStatus, before, after.
// Sorting supports the RFC 8621 Section 7.2 properties emailId, threadId, sendAt, undoStatus
// ("sentAt" is accepted as an alias for sendAt); the default sort is sendAt descending.
func (mb *MemoryBackend) QuerySubmissions(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var matched []*jmap.EmailSubmission
	for _, sub := range us.submissions {
		if MatchSubmissionFilter(sub, filter) {
			matched = append(matched, sub)
		}
	}

	SortSubmissions(matched, comparators)

	total := len(matched)
	position = jmap.NormalizePosition(position, total)
	if position > total {
		return []jmap.Id{}, total, nil
	}

	end := total
	if limit != nil {
		l := int(*limit)
		if position+l < end {
			end = position + l
		}
	}

	ids := make([]jmap.Id, 0, end-position)
	for _, sub := range matched[position:end] {
		ids = append(ids, sub.ID)
	}
	return ids, total, nil
}

// SortSubmissions sorts EmailSubmissions in-place per RFC 8621 Section 7.2 comparators.
// Supported properties: emailId, threadId, sendAt, undoStatus (and "sentAt" as an alias per the RFC
// 8621 sort list). The default sort is sendAt descending.
func SortSubmissions(subs []*jmap.EmailSubmission, comparators []jmap.Comparator) {
	if len(comparators) == 0 {
		comparators = []jmap.Comparator{
			{Property: "sendAt", IsAscending: false},
		}
	}

	sort.SliceStable(subs, func(i, j int) bool {
		a, b := subs[i], subs[j]
		for _, comp := range comparators {
			var cmp int
			switch comp.Property {
			case "emailId":
				cmp = strings.Compare(string(a.EmailID), string(b.EmailID))
			case "threadId":
				cmp = strings.Compare(string(a.ThreadID), string(b.ThreadID))
			case "sendAt", "sentAt":
				cmp = strings.Compare(a.SendAt, b.SendAt)
			case "undoStatus":
				cmp = strings.Compare(a.UndoStatus, b.UndoStatus)
			}
			if cmp != 0 {
				if !comp.IsAscending {
					return cmp > 0
				}
				return cmp < 0
			}
		}
		return i < j
	})
}

// submissionIDFilter builds a set of Ids from an array-valued filter condition.
func submissionIDFilter(filter map[string]any, key string) map[jmap.Id]bool {
	set := make(map[jmap.Id]bool)
	if raw, ok := filter[key].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				set[jmap.Id(s)] = true
			}
		}
	}
	return set
}

// GetQuotas retrieves requested Quota objects by ID per RFC 9425 Section 4.
func (mb *MemoryBackend) GetQuotas(ctx context.Context, ids []jmap.Id) ([]*jmap.Quota, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.Quota
	var notFound []jmap.Id

	for _, id := range ids {
		if q, ok := us.quotas[id]; ok {
			list = append(list, q)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllQuotas retrieves all Quota objects per RFC 9425 Section 4.
func (mb *MemoryBackend) GetAllQuotas(ctx context.Context) ([]*jmap.Quota, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.Quota, 0, len(us.quotas))
	for _, q := range us.quotas {
		list = append(list, q)
	}
	return list, nil
}

// SendMDN sends a Message Disposition Notification per RFC 9007 Section 3.1.
func (mb *MemoryBackend) SendMDN(ctx context.Context, mdn *jmap.MDN) (*jmap.MDN, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	targetEmail, ok := us.emails[mdn.ForEmailID]
	if !ok {
		return nil, fmt.Errorf("email %s not found", mdn.ForEmailID)
	}

	if mdn.ID == "" {
		mb.idCounter++
		mdn.ID = jmap.Id(fmt.Sprintf("mdn-%d", mb.idCounter))
	}

	if mdn.Subject == "" {
		mdn.Subject = fmt.Sprintf("Disposition Notification: %s", targetEmail.Subject)
	}

	if mdn.ReportingUA == "" {
		mdn.ReportingUA = "imap-jmap-server/1.0"
	}

		partID1 := "1"
		mdnEmail := &jmap.Email{
			ID:         mb.nextID("email"),
			BlobID:     jmap.Id(fmt.Sprintf("blob-mdn-%d", mb.idCounter)),
			ThreadID:   targetEmail.ThreadID,
			MailboxIDs: map[jmap.Id]bool{"mb-sent": true},
			Keywords:   map[string]bool{"$seen": true},
			Subject:    mdn.Subject,
			ReceivedAt: time.Now().UTC().Format(time.RFC3339),
			BodyValues: map[string]jmap.EmailBodyValue{
				"1": {Value: fmt.Sprintf("MDN for email %s: %s (%s/%s)", mdn.ForEmailID, mdn.Disposition.Type, mdn.Disposition.ActionMode, mdn.Disposition.SendingMode)},
			},
			TextBody: []jmap.EmailBodyPart{{PartID: &partID1, Type: "text/plain"}},
		}
	us.emails[mdnEmail.ID] = mdnEmail

	// RFC 9007 Section 3.1: sending an MDN creates an email in the Sent mailbox; the
	// Email, Thread, and Mailbox types all change state. MDN itself has no state.
	mb.recalculateMailboxCounts(us)
	mb.recordChange(ctx, us.emailState, mdnEmail.ID, "create", "Email")
	mb.recordChange(ctx, us.threadState, mdnEmail.ThreadID, "update", "Thread")
	mb.recordChange(ctx, us.mailboxState, "mb-sent", "update", "Mailbox")
	return mdn, nil
}

// ParseMDN parses a raw blob containing an MDN message per RFC 9007 Section 3.2.
// The blobID must reference an existing message blob; otherwise the error
// jmap.ErrBlobNotFound is returned so the caller may report it as notFound.
func (mb *MemoryBackend) ParseMDN(ctx context.Context, blobID jmap.Id) (*jmap.MDN, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	for _, em := range us.emails {
		if em.BlobID == blobID {
			return &jmap.MDN{
				ID:          jmap.Id("mdn-parsed-" + string(blobID)),
				ForEmailID:  em.ID,
				Subject:     em.Subject,
				ReportingUA: "imap-jmap-server/1.0",
				Disposition: jmap.MDNDisposition{
					ActionMode:  "automatic-action",
					SendingMode: "MDN-sent-automatically",
					Type:        "displayed",
				},
				TextBody: em.Preview,
			}, nil
		}
	}

	return nil, jmap.ErrBlobNotFound
}

// GetPushSubscriptions retrieves PushSubscription objects by ID per RFC 8620 Section 7.2.1.
func (mb *MemoryBackend) GetPushSubscriptions(ctx context.Context, ids []jmap.Id) ([]*jmap.PushSubscription, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.PushSubscription
	var notFound []jmap.Id

	for _, id := range ids {
		if sub, ok := us.pushSubscriptions[id]; ok {
			list = append(list, sub)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllPushSubscriptions retrieves all PushSubscription objects per RFC 8620 Section 7.2.1.
func (mb *MemoryBackend) GetAllPushSubscriptions(ctx context.Context) ([]*jmap.PushSubscription, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.PushSubscription, 0, len(us.pushSubscriptions))
	for _, sub := range us.pushSubscriptions {
		list = append(list, sub)
	}
	return list, nil
}

// CreatePushSubscription creates a new PushSubscription per RFC 8620 Section 7.2.2.
// Upon creation, the server MUST send a PushVerification to the subscription URL.
func (mb *MemoryBackend) CreatePushSubscription(ctx context.Context, sub *jmap.PushSubscription) (*jmap.PushSubscription, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if sub.ID == "" {
		mb.idCounter++
		sub.ID = jmap.Id(fmt.Sprintf("push-%d", mb.idCounter))
	}

	// Set a verification code per RFC 8620 Section 7.2.2.
	verificationCode := fmt.Sprintf("verify-%s-%d", sub.ID, mb.idCounter)
	sub.VerificationCode = &verificationCode

	us.pushSubscriptions[sub.ID] = sub
	return sub, nil
}

// UpdatePushSubscription updates a PushSubscription per RFC 8620 Section 7.2.2.
// Only "expires", "types", and "verificationCode" are mutable per spec.
func (mb *MemoryBackend) UpdatePushSubscription(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.PushSubscription, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	sub, ok := us.pushSubscriptions[id]
	if !ok {
		return nil, fmt.Errorf("push subscription %s not found", id)
	}

	if v, ok := patch["verificationCode"]; ok {
		if s, ok := v.(string); ok {
			sub.VerificationCode = &s
		}
	}
	if v, ok := patch["expires"]; ok {
		if s, ok := v.(string); ok {
			sub.Expires = &s
		} else if v == nil {
			sub.Expires = nil
		}
	}
	if v, ok := patch["types"]; ok {
		if arr, ok := v.([]any); ok {
			types := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					types = append(types, s)
				}
			}
			sub.Types = types
		} else if v == nil {
			sub.Types = nil
		}
	}

	us.pushSubscriptions[id] = sub
	return sub, nil
}

// DeletePushSubscription deletes a PushSubscription per RFC 8620 Section 7.2.2.
func (mb *MemoryBackend) DeletePushSubscription(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if _, ok := us.pushSubscriptions[id]; !ok {
		return false, nil
	}
	delete(us.pushSubscriptions, id)
	return true, nil
}
