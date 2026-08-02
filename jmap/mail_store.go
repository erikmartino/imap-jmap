package jmap

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryBackend implements MailBackend for in-memory stub storage per RFC 8621 & RFC 9219.
type MemoryBackend struct {
	mu          sync.RWMutex
	mailboxes   map[Id]*Mailbox
	threads     map[Id]*Thread
	emails      map[Id]*Email
	quotas      map[Id]*Quota
	identities  map[Id]*Identity
	submissions map[Id]*EmailSubmission
	broadcaster *Broadcaster
	idCounter   uint64
	state       string
}

// Ensure MemoryBackend implements MailBackend interface.
var _ MailBackend = (*MemoryBackend)(nil)

// SetBroadcaster connects a Broadcaster for SSE state notifications.
func (mb *MemoryBackend) SetBroadcaster(b *Broadcaster) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.broadcaster = b
}

// NewMemoryBackend initializes a new MemoryBackend pre-populated with standard default mailboxes and stub messages.
func NewMemoryBackend() *MemoryBackend {
	mb := &MemoryBackend{
		mailboxes:   make(map[Id]*Mailbox),
		threads:     make(map[Id]*Thread),
		emails:      make(map[Id]*Email),
		quotas:      make(map[Id]*Quota),
		identities:  make(map[Id]*Identity),
		submissions: make(map[Id]*EmailSubmission),
		state:       "m1",
	}

	// Create default Quotas per RFC 9425
	quotaOctetsDesc := "Storage quota in bytes for account"
	mb.quotas["quota-octets"] = &Quota{
		ID:           "quota-octets",
		Name:         "Storage Quota",
		ResourceType: "octets",
		Used:         3072,
		HardLimit:    10737418240, // 10 GB
		Scope:        "account",
		Description:  &quotaOctetsDesc,
	}

	quotaMessagesDesc := "Message count quota for account"
	mb.quotas["quota-messages"] = &Quota{
		ID:           "quota-messages",
		Name:         "Message Count Quota",
		ResourceType: "messages",
		Used:         2,
		HardLimit:    100000,
		Scope:        "account",
		Description:  &quotaMessagesDesc,
	}

	// Create standard default mailboxes per RFC 8621 Section 2
	inboxRole := "inbox"
	inbox := &Mailbox{
		ID:            "mb-inbox",
		Name:          "Inbox",
		Role:          &inboxRole,
		SortOrder:     10,
		TotalEmails:   0,
		UnreadEmails:  0,
		TotalThreads:  0,
		UnreadThreads: 0,
		MyRights: MailboxRights{
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

	sentRole := "sent"
	sent := &Mailbox{
		ID:            "mb-sent",
		Name:          "Sent",
		Role:          &sentRole,
		SortOrder:     20,
		TotalEmails:   0,
		UnreadEmails:  0,
		TotalThreads:  0,
		UnreadThreads: 0,
		MyRights: MailboxRights{
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

	trashRole := "trash"
	trash := &Mailbox{
		ID:            "mb-trash",
		Name:          "Trash",
		Role:          &trashRole,
		SortOrder:     30,
		TotalEmails:   0,
		UnreadEmails:  0,
		TotalThreads:  0,
		UnreadThreads: 0,
		MyRights: MailboxRights{
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

	// Default identity
	defaultIdentity := &Identity{
		ID:    "id-primary",
		Name:  "Primary User",
		Email: "user@example.com",
	}
	mb.identities[defaultIdentity.ID] = defaultIdentity

	// Create stub messages in Inbox
	stubStatus := "signed"
	stubVerifiedWith := "admin@example.com"
	stub1 := &Email{
		Subject:           "Welcome to JMAP Server",
		From:              []EmailAddress{{Name: "JMAP Admin", Email: "admin@example.com"}},
		To:                []EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
		MailboxIDs:        map[Id]bool{"mb-inbox": true},
		Keywords:          map[string]bool{"$seen": true},
		Size:              1024,
		ReceivedAt:        "2026-08-01T12:00:00Z",
		SentAt:            "2026-08-01T11:59:00Z",
		Preview:           "Welcome to your new JMAP mail server. This server supports RFC 8620 and RFC 8621.",
		BlobID:            "blob-stub-1",
		SMIMEStatus:       &stubStatus,
		SMIMEVerifiedWith: &stubVerifiedWith,
		BodyStructure: EmailBodyPart{
			PartID: "1",
			Type:   "text/plain",
			Size:   75,
		},
		BodyValues: map[string]EmailBodyValue{
			"1": {Value: "Welcome to your new JMAP mail server. This server supports RFC 8620 and RFC 8621."},
		},
	}
	_, _ = mb.CreateEmail(context.Background(), stub1)

	stub2 := &Email{
		Subject:    "JMAP Core and Mail Specifications",
		From:       []EmailAddress{{Name: "IETF JMAP WG", Email: "noreply@ietf.org"}},
		To:         []EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
		MailboxIDs: map[Id]bool{"mb-inbox": true},
		Keywords:   map[string]bool{"$flagged": true},
		Size:       2048,
		ReceivedAt: "2026-08-01T14:30:00Z",
		SentAt:     "2026-08-01T14:29:00Z",
		Preview:    "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail).",
		BlobID:     "blob-stub-2",
		BodyStructure: EmailBodyPart{
			PartID: "1",
			Type:   "text/plain",
			Size:   92,
		},
		BodyValues: map[string]EmailBodyValue{
			"1": {Value: "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail)."},
		},
	}
	_, _ = mb.CreateEmail(context.Background(), stub2)

	return mb
}

func (mb *MemoryBackend) nextID(prefix string) Id {
	mb.idCounter++
	return Id(fmt.Sprintf("%s-%d", prefix, mb.idCounter))
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

// GetMailboxes retrieves requested mailboxes by ID.
func (mb *MemoryBackend) GetMailboxes(ctx context.Context, ids []Id) ([]*Mailbox, []Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var list []*Mailbox
	var notFound []Id

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
func (mb *MemoryBackend) GetAllMailboxes(ctx context.Context) ([]*Mailbox, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	list := make([]*Mailbox, 0, len(mb.mailboxes))
	for _, item := range mb.mailboxes {
		list = append(list, item)
	}
	return list, nil
}

// CreateMailbox creates a new mailbox.
func (mb *MemoryBackend) CreateMailbox(ctx context.Context, item *Mailbox) (*Mailbox, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if item.ID == "" {
		item.ID = mb.nextID("mb")
	}
	item.MyRights = MailboxRights{
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
	mb.bumpState("Mailbox")
	return item, nil
}

// DeleteMailbox deletes a mailbox by ID.
func (mb *MemoryBackend) DeleteMailbox(ctx context.Context, id Id) (bool, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if _, ok := mb.mailboxes[id]; !ok {
		return false, nil
	}
	delete(mb.mailboxes, id)
	mb.bumpState("Mailbox")
	return true, nil
}

// GetThreads retrieves threads by ID.
func (mb *MemoryBackend) GetThreads(ctx context.Context, ids []Id) ([]*Thread, []Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var list []*Thread
	var notFound []Id

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
func (mb *MemoryBackend) GetEmails(ctx context.Context, ids []Id) ([]*Email, []Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var list []*Email
	var notFound []Id

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
func (mb *MemoryBackend) GetAllEmails(ctx context.Context) ([]*Email, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	list := make([]*Email, 0, len(mb.emails))
	for _, item := range mb.emails {
		list = append(list, item)
	}
	return list, nil
}

// CreateEmail creates a new email.
func (mb *MemoryBackend) CreateEmail(ctx context.Context, em *Email) (*Email, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if em.ID == "" {
		em.ID = mb.nextID("email")
	}
	if em.ThreadID == "" {
		em.ThreadID = mb.nextID("thread")
	}
	if em.MailboxIDs == nil {
		em.MailboxIDs = make(map[Id]bool)
	}
	if em.Keywords == nil {
		em.Keywords = make(map[string]bool)
	}

	mb.emails[em.ID] = em

	// Update Thread
	th, ok := mb.threads[em.ThreadID]
	if !ok {
		th = &Thread{
			ID:       em.ThreadID,
			EmailIDs: []Id{em.ID},
		}
		mb.threads[em.ThreadID] = th
	} else {
		th.EmailIDs = append(th.EmailIDs, em.ID)
	}

	// Update Mailbox counters
	for mbID := range em.MailboxIDs {
		if box, exists := mb.mailboxes[mbID]; exists {
			box.TotalEmails++
			box.TotalThreads = uint64(len(mb.threads))
			if !em.Keywords["$seen"] {
				box.UnreadEmails++
			}
		}
	}

	mb.bumpState("Email")
	return em, nil
}

// DeleteEmail deletes an email by ID.
func (mb *MemoryBackend) DeleteEmail(ctx context.Context, id Id) (bool, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	em, ok := mb.emails[id]
	if !ok {
		return false, nil
	}
	delete(mb.emails, id)

	// Remove from thread
	if th, ok := mb.threads[em.ThreadID]; ok {
		newIDs := make([]Id, 0, len(th.EmailIDs))
		for _, eid := range th.EmailIDs {
			if eid != id {
				newIDs = append(newIDs, eid)
			}
		}
		if len(newIDs) == 0 {
			delete(mb.threads, em.ThreadID)
		} else {
			th.EmailIDs = newIDs
		}
	}

	mb.bumpState("Email")
	return true, nil
}

// QueryEmails evaluates filters, sorting, and pagination per RFC 8621 Section 4.5.
func (mb *MemoryBackend) QueryEmails(ctx context.Context, filter map[string]any, comparators []Comparator, position int, limit *uint64) ([]Id, int, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var matched []*Email
	for _, em := range mb.emails {
		if MatchesFilter(em, filter) {
			matched = append(matched, em)
		}
	}

	total := len(matched)
	SortEmails(matched, comparators)

	if position < 0 {
		position = 0
	}
	if position > len(matched) {
		return []Id{}, total, nil
	}

	end := len(matched)
	if limit != nil {
		l := int(*limit)
		if position+l < end {
			end = position + l
		}
	}

	slice := matched[position:end]
	ids := make([]Id, len(slice))
	for i, em := range slice {
		ids[i] = em.ID
	}

	return ids, total, nil
}

// VerifySmime implements RFC 9219 Section 4 Email/verifySmime.
func (mb *MemoryBackend) VerifySmime(ctx context.Context, ids []Id) (map[Id]*SmimeVerificationResult, []Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	verified := make(map[Id]*SmimeVerificationResult)
	var notFound []Id
	now := time.Now().UTC().Format(time.RFC3339)

	for _, id := range ids {
		em, ok := mb.emails[id]
		if !ok {
			notFound = append(notFound, id)
			continue
		}

		status := "signed"
		if em.SMIMEStatus != nil {
			status = *em.SMIMEStatus
		}

		res := &SmimeVerificationResult{
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
func (mb *MemoryBackend) GetIdentities(ctx context.Context) ([]*Identity, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	list := make([]*Identity, 0, len(mb.identities))
	for _, item := range mb.identities {
		list = append(list, item)
	}
	return list, nil
}

// CreateSubmission creates an EmailSubmission.
func (mb *MemoryBackend) CreateSubmission(ctx context.Context, sub *EmailSubmission) (*EmailSubmission, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if sub.ID == "" {
		sub.ID = mb.nextID("sub")
	}
	if sub.SendAt == "" {
		sub.SendAt = time.Now().UTC().Format(time.RFC3339)
	}
	sub.UndoStatus = "final"
	sub.DeliveryStatus = map[string]any{
		"user@example.com": map[string]any{
			"delivered": "granted",
		},
	}

	mb.submissions[sub.ID] = sub
	mb.bumpState("EmailSubmission")
	return sub, nil
}

// GetSubmissions retrieves EmailSubmissions by ID.
func (mb *MemoryBackend) GetSubmissions(ctx context.Context, ids []Id) ([]*EmailSubmission, []Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var list []*EmailSubmission
	var notFound []Id

	for _, id := range ids {
		if sub, ok := mb.submissions[id]; ok {
			list = append(list, sub)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetQuotas retrieves requested Quota objects by ID per RFC 9425 Section 4.
func (mb *MemoryBackend) GetQuotas(ctx context.Context, ids []Id) ([]*Quota, []Id, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	var list []*Quota
	var notFound []Id

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
func (mb *MemoryBackend) GetAllQuotas(ctx context.Context) ([]*Quota, error) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	list := make([]*Quota, 0, len(mb.quotas))
	for _, q := range mb.quotas {
		list = append(list, q)
	}
	return list, nil
}
