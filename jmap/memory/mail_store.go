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
type MemoryBackend struct {
	mu                sync.RWMutex
	mailboxes         map[jmap.Id]*jmap.Mailbox
	threads           map[jmap.Id]*jmap.Thread
	emails            map[jmap.Id]*jmap.Email
	quotas            map[jmap.Id]*jmap.Quota
	identities        map[jmap.Id]*jmap.Identity
	submissions       map[jmap.Id]*jmap.EmailSubmission
	pushSubscriptions map[jmap.Id]*jmap.PushSubscription
	broadcaster       *jmap.Broadcaster
	idCounter         uint64
	state             string

	mailboxState    *changeTracker
	threadState     *changeTracker
	emailState      *changeTracker
	identityState   *changeTracker
	submissionState *changeTracker
	quotaState      *changeTracker
}

// Ensure MemoryBackend implements jmap.MailBackend interface.
var _ jmap.MailBackend = (*MemoryBackend)(nil)

// SetBroadcaster connects a Broadcaster for SSE state notifications.
func (mb *MemoryBackend) SetBroadcaster(b *jmap.Broadcaster) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.broadcaster = b
}

// NewMemoryBackend initializes a new MemoryBackend pre-populated with standard default mailboxes and stub messages.
func NewMemoryBackend() *MemoryBackend {
	mb := &MemoryBackend{
		mailboxes:         make(map[jmap.Id]*jmap.Mailbox),
		threads:           make(map[jmap.Id]*jmap.Thread),
		emails:            make(map[jmap.Id]*jmap.Email),
		quotas:            make(map[jmap.Id]*jmap.Quota),
		identities:        make(map[jmap.Id]*jmap.Identity),
		submissions:       make(map[jmap.Id]*jmap.EmailSubmission),
		pushSubscriptions: make(map[jmap.Id]*jmap.PushSubscription),
		state:             "m1",

		mailboxState:    newChangeTracker(1000),
		threadState:     newChangeTracker(1000),
		emailState:      newChangeTracker(1000),
		identityState:   newChangeTracker(1000),
		submissionState: newChangeTracker(1000),
		quotaState:      newChangeTracker(1000),
	}

	// Create default Quotas per RFC 9425
	quotaOctetsDesc := "Storage quota in bytes for account"
	mb.quotas["quota-octets"] = &jmap.Quota{
		ID:           "quota-octets",
		Name:         "Storage Quota",
		ResourceType: "octets",
		Used:         3072,
		HardLimit:    10737418240, // 10 GB
		Scope:        "account",
		Description:  &quotaOctetsDesc,
	}

	quotaMessagesDesc := "Message count quota for account"
	mb.quotas["quota-messages"] = &jmap.Quota{
		ID:           "quota-messages",
		Name:         "Message Count Quota",
		ResourceType: "messages",
		Used:         2,
		HardLimit:    100000,
		Scope:        "account",
		Description:  &quotaMessagesDesc,
	}

	// Create standard default mailboxes per RFC 8621 Section 2.1.
	// NOTE: RFC 8621 Section 2.1 mandates that the server MUST create an Inbox mailbox (role: "inbox").
	// Other system mailboxes (sent, trash, drafts, junk, archive) are optional MAY provisions in RFC 8621
	// that we implement for full client interoperability and completeness.
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
		},
		IsSubscribed: true,
	}

	sentRole := "sent" // MAY provision per RFC 8621 Section 2.1
	sent := &jmap.Mailbox{
		ID:            "mb-sent",
		Name:          "Sent",
		Role:          &sentRole,
		SortOrder:     20,
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
			MaySubmit:      false,
		},
		IsSubscribed: true,
	}

	trashRole := "trash" // MAY provision per RFC 8621 Section 2.1
	trash := &jmap.Mailbox{
		ID:            "mb-trash",
		Name:          "Trash",
		Role:          &trashRole,
		SortOrder:     30,
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
			MaySubmit:      false,
		},
		IsSubscribed: true,
	}

	draftsRole := "drafts" // MAY provision per RFC 8621 Section 2.1
	drafts := &jmap.Mailbox{
		ID:            "mb-drafts",
		Name:          "Drafts",
		Role:          &draftsRole,
		SortOrder:     15,
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
			MaySubmit:      false,
		},
		IsSubscribed: true,
	}

	junkRole := "junk" // MAY provision per RFC 8621 Section 2.1
	junk := &jmap.Mailbox{
		ID:            "mb-junk",
		Name:          "Junk",
		Role:          &junkRole,
		SortOrder:     25,
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
			MaySubmit:      false,
		},
		IsSubscribed: true,
	}

	archiveRole := "archive" // MAY provision per RFC 8621 Section 2.1
	archive := &jmap.Mailbox{
		ID:            "mb-archive",
		Name:          "Archive",
		Role:          &archiveRole,
		SortOrder:     35,
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
			MaySubmit:      false,
		},
		IsSubscribed: true,
	}

	mb.mailboxes[inbox.ID] = inbox
	mb.mailboxes[sent.ID] = sent
	mb.mailboxes[trash.ID] = trash
	mb.mailboxes[drafts.ID] = drafts
	mb.mailboxes[junk.ID] = junk
	mb.mailboxes[archive.ID] = archive

	// Default identity
	defaultIdentity := &jmap.Identity{
		ID:    "id-primary",
		Name:  "Primary User",
		Email: "user@example.com",
	}
	mb.identities[defaultIdentity.ID] = defaultIdentity

	// Create sample emails in Inbox, Sent, Drafts, and Archive
	stubStatus := "signed"
	stubVerifiedWith := "admin@example.com"
	stub1 := &jmap.Email{
		Subject:           "Welcome to JMAP Server",
		From:              []jmap.EmailAddress{{Name: "JMAP Admin", Email: "admin@example.com"}},
		To:                []jmap.EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
		MailboxIDs:        map[jmap.Id]bool{"mb-inbox": true},
		Keywords:          map[string]bool{"$seen": true},
		Size:              1024,
		ReceivedAt:        "2026-08-01T12:00:00Z",
		SentAt:            "2026-08-01T11:59:00Z",
		Preview:           "Welcome to your new JMAP mail server. This server supports RFC 8620 and RFC 8621.",
		BlobID:            "blob-stub-1",
		SMIMEStatus:       &stubStatus,
		SMIMEVerifiedWith: &stubVerifiedWith,
		BodyStructure: jmap.EmailBodyPart{
			PartID: "1",
			Type:   "text/plain",
			Size:   75,
		},
		BodyValues: map[string]jmap.EmailBodyValue{
			"1": {Value: "Welcome to your new JMAP mail server. This server supports RFC 8620 and RFC 8621."},
		},
	}
	_, _ = mb.CreateEmail(context.Background(), stub1)

	stub2 := &jmap.Email{
		Subject:    "JMAP Core and Mail Specifications",
		From:       []jmap.EmailAddress{{Name: "IETF JMAP WG", Email: "noreply@ietf.org"}},
		To:         []jmap.EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Keywords:   map[string]bool{"$flagged": true},
		Size:       2048,
		ReceivedAt: "2026-08-01T14:30:00Z",
		SentAt:     "2026-08-01T14:29:00Z",
		Preview:    "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail).",
		BlobID:     "blob-stub-2",
		BodyStructure: jmap.EmailBodyPart{
			PartID: "1",
			Type:   "text/plain",
			Size:   92,
		},
		BodyValues: map[string]jmap.EmailBodyValue{
			"1": {Value: "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail)."},
		},
	}
	_, _ = mb.CreateEmail(context.Background(), stub2)

	return mb
}

func (mb *MemoryBackend) nextID(prefix string) jmap.Id {
	mb.idCounter++
	return jmap.Id(fmt.Sprintf("%s-%d", prefix, mb.idCounter))
}

func (mb *MemoryBackend) recordChange(tracker *changeTracker, id jmap.Id, action string, typeName string) string {
	newState := tracker.record(id, action)
	mb.state = newState
	if mb.broadcaster != nil {
		mb.broadcaster.PublishStateChange("primary", typeName, newState)
	}
	return newState
}

func (mb *MemoryBackend) bumpState(typeName string) {
	mb.state = fmt.Sprintf("m%d", time.Now().UnixNano())
	if mb.broadcaster != nil {
		mb.broadcaster.PublishStateChange("primary", typeName, mb.state)
	}
}

// State returns current mail change state token.
func (mb *MemoryBackend) State(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return mb.state
}

// MailboxState returns current change state token for Mailbox resources.
func (mb *MemoryBackend) MailboxState(ctx context.Context) string {
	return mb.mailboxState.State()
}

// MailboxChanges returns created, updated, and destroyed Mailboxes since sinceState.
func (mb *MemoryBackend) MailboxChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return mb.mailboxState.Changes(sinceState)
}

// ThreadState returns current change state token for Thread resources.
func (mb *MemoryBackend) ThreadState(ctx context.Context) string {
	return mb.threadState.State()
}

// ThreadChanges returns created, updated, and destroyed Threads since sinceState.
func (mb *MemoryBackend) ThreadChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return mb.threadState.Changes(sinceState)
}

// EmailState returns current change state token for Email resources.
func (mb *MemoryBackend) EmailState(ctx context.Context) string {
	return mb.emailState.State()
}

// EmailChanges returns created, updated, and destroyed Emails since sinceState.
func (mb *MemoryBackend) EmailChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return mb.emailState.Changes(sinceState)
}

// IdentityState returns current change state token for Identity resources.
func (mb *MemoryBackend) IdentityState(ctx context.Context) string {
	return mb.identityState.State()
}

// IdentityChanges returns created, updated, and destroyed Identities since sinceState.
func (mb *MemoryBackend) IdentityChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return mb.identityState.Changes(sinceState)
}

// SubmissionState returns current change state token for EmailSubmission resources.
func (mb *MemoryBackend) SubmissionState(ctx context.Context) string {
	return mb.submissionState.State()
}

// SubmissionChanges returns created, updated, and destroyed EmailSubmissions since sinceState.
func (mb *MemoryBackend) SubmissionChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return mb.submissionState.Changes(sinceState)
}

// QuotaState returns current change state token for Quota resources.
func (mb *MemoryBackend) QuotaState(ctx context.Context) string {
	return mb.quotaState.State()
}

// QuotaChanges returns created, updated, and destroyed Quotas since sinceState.
func (mb *MemoryBackend) QuotaChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return mb.quotaState.Changes(sinceState)
}

// GetMailboxes retrieves requested mailboxes by ID.
func (mb *MemoryBackend) GetMailboxes(ctx context.Context, ids []jmap.Id) ([]*jmap.Mailbox, []jmap.Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var list []*jmap.Mailbox
	var notFound []jmap.Id

	for _, id := range ids {
		if item, ok := mb.mailboxes[id]; ok {
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
	defer mb.mu.RUnlock()

	list := make([]*jmap.Mailbox, 0, len(mb.mailboxes))
	for _, item := range mb.mailboxes {
		list = append(list, item)
	}
	return list, nil
}

// CreateMailbox creates a new mailbox.
func (mb *MemoryBackend) CreateMailbox(ctx context.Context, item *jmap.Mailbox) (*jmap.Mailbox, error) {
	mb.mu.Lock()
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
	}
	mb.mailboxes[item.ID] = item
	mb.recordChange(mb.mailboxState, item.ID, "create", "Mailbox")
	return item, nil
}

// UpdateMailbox applies a partial patch to a mailbox (RFC 8621 Section 2.5), preserving
// unaddressed fields. Counts and rights are server-set and cannot be patched by the client.
func (mb *MemoryBackend) UpdateMailbox(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Mailbox, error) {
	mb.mu.Lock()
	item, ok := mb.mailboxes[id]
	if !ok {
		mb.mu.Unlock()
		return nil, fmt.Errorf("mailbox not found: %s", id)
	}

	for prop, val := range patch {
		switch prop {
		case "name":
			name, ok := val.(string)
			if !ok || name == "" {
				mb.mu.Unlock()
				return nil, fmt.Errorf("invalid name")
			}
			item.Name = name
		case "parentId":
			if val == nil {
				item.ParentID = nil
				continue
			}
			pid, ok := val.(string)
			if !ok {
				mb.mu.Unlock()
				return nil, fmt.Errorf("invalid parentId")
			}
			if jmap.Id(pid) == id {
				mb.mu.Unlock()
				return nil, fmt.Errorf("a mailbox cannot be its own parent")
			}
			if _, exists := mb.mailboxes[jmap.Id(pid)]; !exists {
				mb.mu.Unlock()
				return nil, fmt.Errorf("parent mailbox not found: %s", pid)
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
				mb.mu.Unlock()
				return nil, fmt.Errorf("invalid role")
			}
			item.Role = &role
		case "sortOrder":
			if f, ok := val.(float64); ok {
				item.SortOrder = uint64(f)
			}
		case "isSubscribed":
			if b, ok := val.(bool); ok {
				item.IsSubscribed = b
			}
		default:
			mb.mu.Unlock()
			return nil, fmt.Errorf("unknown or immutable property: %s", prop)
		}
	}
	mb.mu.Unlock()

	mb.recordChange(mb.mailboxState, id, "update", "Mailbox")
	return item, nil
}

// DeleteMailbox deletes a mailbox by ID.
func (mb *MemoryBackend) DeleteMailbox(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if _, ok := mb.mailboxes[id]; !ok {
		return false, nil
	}
	delete(mb.mailboxes, id)
	mb.recordChange(mb.mailboxState, id, "destroy", "Mailbox")
	return true, nil
}

// GetThreads retrieves threads by ID.
func (mb *MemoryBackend) GetThreads(ctx context.Context, ids []jmap.Id) ([]*jmap.Thread, []jmap.Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var list []*jmap.Thread
	var notFound []jmap.Id

	for _, id := range ids {
		if item, ok := mb.threads[id]; ok {
			list = append(list, item)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetEmails retrieves emails by ID.
func (mb *MemoryBackend) GetEmails(ctx context.Context, ids []jmap.Id) ([]*jmap.Email, []jmap.Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var list []*jmap.Email
	var notFound []jmap.Id

	for _, id := range ids {
		if item, ok := mb.emails[id]; ok {
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
	defer mb.mu.RUnlock()

	list := make([]*jmap.Email, 0, len(mb.emails))
	for _, item := range mb.emails {
		list = append(list, item)
	}
	return list, nil
}

// CreateEmail creates a new email.
func (mb *MemoryBackend) CreateEmail(ctx context.Context, em *jmap.Email) (*jmap.Email, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if em.ID == "" {
		em.ID = mb.nextID("email")
	}
	if em.ThreadID == "" {
		em.ThreadID = mb.nextID("thread")
	}
	if em.MailboxIDs == nil {
		em.MailboxIDs = make(map[jmap.Id]bool)
	}
	if em.Keywords == nil {
		em.Keywords = make(map[string]bool)
	}

	mb.emails[em.ID] = em

	// Update Thread
	th, ok := mb.threads[em.ThreadID]
	if !ok {
		th = &jmap.Thread{
			ID:       em.ThreadID,
			EmailIDs: []jmap.Id{em.ID},
		}
		mb.threads[em.ThreadID] = th
		mb.recordChange(mb.threadState, em.ThreadID, "create", "Thread")
	} else {
		th.EmailIDs = append(th.EmailIDs, em.ID)
		mb.recordChange(mb.threadState, em.ThreadID, "update", "Thread")
	}

	mb.recalculateMailboxCounts()
	mb.recordChange(mb.emailState, em.ID, "create", "Email")
	mb.bumpState("Mailbox")
	return em, nil
}

func (mb *MemoryBackend) recalculateMailboxCounts() {
	counts := make(map[jmap.Id]*struct{ unread, total uint64 })
	for mID := range mb.mailboxes {
		counts[mID] = &struct{ unread, total uint64 }{}
	}

	for _, em := range mb.emails {
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
		if box, ok := mb.mailboxes[mID]; ok {
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
	defer mb.mu.Unlock()

	em, ok := mb.emails[id]
	if !ok {
		return nil, fmt.Errorf("notFound")
	}

	if em.Keywords == nil {
		em.Keywords = make(map[string]bool)
	}
	if em.MailboxIDs == nil {
		em.MailboxIDs = make(map[jmap.Id]bool)
	}

	for path, val := range patch {
		if path == "keywords" {
			if kwMap, ok := val.(map[string]any); ok {
				em.Keywords = make(map[string]bool)
				for k, v := range kwMap {
					if boolVal, ok := v.(bool); ok {
						em.Keywords[k] = boolVal
					}
				}
			}
		} else if strings.HasPrefix(path, "keywords/") {
			kw := strings.TrimPrefix(path, "keywords/")
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

	mb.recalculateMailboxCounts()
	mb.recordChange(mb.emailState, id, "update", "Email")
	mb.bumpState("Mailbox")
	return em, nil
}

// DeleteEmail deletes an email by ID.
func (mb *MemoryBackend) DeleteEmail(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	em, ok := mb.emails[id]
	if !ok {
		return false, nil
	}
	delete(mb.emails, id)

	// Remove from thread
	if th, ok := mb.threads[em.ThreadID]; ok {
		newIDs := make([]jmap.Id, 0, len(th.EmailIDs))
		for _, eid := range th.EmailIDs {
			if eid != id {
				newIDs = append(newIDs, eid)
			}
		}
		if len(newIDs) == 0 {
			delete(mb.threads, em.ThreadID)
			mb.recordChange(mb.threadState, em.ThreadID, "destroy", "Thread")
		} else {
			th.EmailIDs = newIDs
			mb.recordChange(mb.threadState, em.ThreadID, "update", "Thread")
		}
	}

	mb.recalculateMailboxCounts()
	mb.recordChange(mb.emailState, id, "destroy", "Email")
	mb.bumpState("Mailbox")
	return true, nil
}

// QueryEmails evaluates filters, sorting, and pagination per RFC 8621 Section 4.5.
func (mb *MemoryBackend) QueryEmails(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var matched []*jmap.Email
	for _, em := range mb.emails {
		if jmap.MatchesFilter(em, filter) {
			matched = append(matched, em)
		}
	}

	total := len(matched)
	jmap.SortEmails(matched, comparators)

	if position < 0 {
		position = 0
	}
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
	defer mb.mu.RUnlock()

	verified := make(map[jmap.Id]*jmap.SmimeVerificationResult)
	var notFound []jmap.Id
	now := time.Now().UTC().Format(time.RFC3339)

	for _, id := range ids {
		em, ok := mb.emails[id]
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
	defer mb.mu.RUnlock()

	list := make([]*jmap.Identity, 0, len(mb.identities))
	for _, item := range mb.identities {
		list = append(list, item)
	}
	return list, nil
}

// CreateIdentity creates a new Identity (RFC 8621 Section 6.3).
func (mb *MemoryBackend) CreateIdentity(ctx context.Context, identity *jmap.Identity) (*jmap.Identity, error) {
	mb.mu.Lock()
	if identity.Email == "" {
		mb.mu.Unlock()
		return nil, fmt.Errorf("email is required")
	}
	if identity.ID == "" {
		identity.ID = mb.nextID("identity")
	}
	mb.identities[identity.ID] = identity
	mb.mu.Unlock()

	mb.recordChange(mb.identityState, identity.ID, "create", "Identity")
	return identity, nil
}

// UpdateIdentity applies a partial patch to an existing Identity, preserving unaddressed fields.
func (mb *MemoryBackend) UpdateIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Identity, error) {
	mb.mu.Lock()
	identity, ok := mb.identities[id]
	if !ok {
		mb.mu.Unlock()
		return nil, fmt.Errorf("identity not found: %s", id)
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

	mb.recordChange(mb.identityState, id, "update", "Identity")
	return identity, nil
}

// DeleteIdentity removes an Identity.
func (mb *MemoryBackend) DeleteIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	if _, ok := mb.identities[id]; !ok {
		mb.mu.Unlock()
		return false, nil
	}
	delete(mb.identities, id)
	mb.mu.Unlock()

	mb.recordChange(mb.identityState, id, "destroy", "Identity")
	return true, nil
}

// CreateSubmission creates an EmailSubmission.
func (mb *MemoryBackend) CreateSubmission(ctx context.Context, sub *jmap.EmailSubmission) (*jmap.EmailSubmission, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if sub.ID == "" {
		sub.ID = mb.nextID("sub")
	}
	if sub.SendAt == "" {
		sub.SendAt = time.Now().UTC().Format(time.RFC3339)
	}
	// RFC 8621 Section 7.1: threadId is server-set and must match the referenced email.
	if sub.ThreadID == "" {
		if em, ok := mb.emails[sub.EmailID]; ok {
			sub.ThreadID = em.ThreadID
		}
	}
	sub.UndoStatus = "final"
	sub.DeliveryStatus = map[string]any{
		"user@example.com": map[string]any{
			"delivered": "granted",
		},
	}

	mb.submissions[sub.ID] = sub
	mb.recordChange(mb.submissionState, sub.ID, "create", "EmailSubmission")
	return sub, nil
}

// GetSubmissions retrieves EmailSubmissions by ID.
func (mb *MemoryBackend) GetSubmissions(ctx context.Context, ids []jmap.Id) ([]*jmap.EmailSubmission, []jmap.Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var list []*jmap.EmailSubmission
	var notFound []jmap.Id

	for _, id := range ids {
		if sub, ok := mb.submissions[id]; ok {
			list = append(list, sub)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// QuerySubmissions filters, sorts, and pages EmailSubmissions per RFC 8621 Section 7.2.
// Supported filter conditions: identityIds, emailIds, threadIds, before, after. Results are
// sorted by sendAt descending (the RFC 8621 Section 7.2 default).
func (mb *MemoryBackend) QuerySubmissions(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmap.Id, int, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	identityFilter := submissionIDFilter(filter, "identityIds")
	emailFilter := submissionIDFilter(filter, "emailIds")
	threadFilter := submissionIDFilter(filter, "threadIds")
	before, _ := filter["before"].(string)
	after, _ := filter["after"].(string)

	var matched []*jmap.EmailSubmission
	for _, sub := range mb.submissions {
		if len(identityFilter) > 0 && !identityFilter[sub.IdentityID] {
			continue
		}
		if len(emailFilter) > 0 && !emailFilter[sub.EmailID] {
			continue
		}
		if len(threadFilter) > 0 && !threadFilter[sub.ThreadID] {
			continue
		}
		if before != "" && sub.SendAt >= before {
			continue
		}
		if after != "" && sub.SendAt < after {
			continue
		}
		matched = append(matched, sub)
	}

	// Default sort: sendAt descending per RFC 8621 Section 7.2.
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].SendAt > matched[j].SendAt
	})

	total := len(matched)
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
	defer mb.mu.RUnlock()

	var list []*jmap.Quota
	var notFound []jmap.Id

	for _, id := range ids {
		if q, ok := mb.quotas[id]; ok {
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
	defer mb.mu.RUnlock()

	list := make([]*jmap.Quota, 0, len(mb.quotas))
	for _, q := range mb.quotas {
		list = append(list, q)
	}
	return list, nil
}

// SendMDN sends a Message Disposition Notification per RFC 9007 Section 3.1.
func (mb *MemoryBackend) SendMDN(ctx context.Context, mdn *jmap.MDN) (*jmap.MDN, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	targetEmail, ok := mb.emails[mdn.ForEmailID]
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
		TextBody: []jmap.EmailBodyPart{{PartID: "1", Type: "text/plain"}},
	}
	mb.emails[mdnEmail.ID] = mdnEmail

	mb.bumpState("MDN")
	mb.bumpState("Email")
	return mdn, nil
}

// ParseMDN parses a raw blob containing an MDN message per RFC 9007 Section 3.2.
// The blobID must reference an existing message blob; otherwise the error
// jmap.ErrBlobNotFound is returned so the caller may report it as notFound.
func (mb *MemoryBackend) ParseMDN(ctx context.Context, blobID jmap.Id) (*jmap.MDN, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	for _, em := range mb.emails {
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
	defer mb.mu.RUnlock()

	var list []*jmap.PushSubscription
	var notFound []jmap.Id

	for _, id := range ids {
		if sub, ok := mb.pushSubscriptions[id]; ok {
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
	defer mb.mu.RUnlock()

	list := make([]*jmap.PushSubscription, 0, len(mb.pushSubscriptions))
	for _, sub := range mb.pushSubscriptions {
		list = append(list, sub)
	}
	return list, nil
}

// CreatePushSubscription creates a new PushSubscription per RFC 8620 Section 7.2.2.
// Upon creation, the server MUST send a PushVerification to the subscription URL.
func (mb *MemoryBackend) CreatePushSubscription(ctx context.Context, sub *jmap.PushSubscription) (*jmap.PushSubscription, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if sub.ID == "" {
		mb.idCounter++
		sub.ID = jmap.Id(fmt.Sprintf("push-%d", mb.idCounter))
	}

	// Set a verification code per RFC 8620 Section 7.2.2.
	verificationCode := fmt.Sprintf("verify-%s-%d", sub.ID, mb.idCounter)
	sub.VerificationCode = &verificationCode

	mb.pushSubscriptions[sub.ID] = sub
	mb.bumpState("PushSubscription")
	return sub, nil
}

// UpdatePushSubscription updates a PushSubscription per RFC 8620 Section 7.2.2.
// Only "expires", "types", and "verificationCode" are mutable per spec.
func (mb *MemoryBackend) UpdatePushSubscription(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.PushSubscription, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	sub, ok := mb.pushSubscriptions[id]
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

	mb.pushSubscriptions[id] = sub
	mb.bumpState("PushSubscription")
	return sub, nil
}

// DeletePushSubscription deletes a PushSubscription per RFC 8620 Section 7.2.2.
func (mb *MemoryBackend) DeletePushSubscription(ctx context.Context, id jmap.Id) (bool, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if _, ok := mb.pushSubscriptions[id]; !ok {
		return false, nil
	}
	delete(mb.pushSubscriptions, id)
	mb.bumpState("PushSubscription")
	return true, nil
}
