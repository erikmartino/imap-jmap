package memory

import (
	"context"
	"fmt"
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

	// Create standard default mailboxes per RFC 8621 Section 2
	inboxRole := "inbox"
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

	sentRole := "sent"
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

	trashRole := "trash"
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

	mb.mailboxes[inbox.ID] = inbox
	mb.mailboxes[sent.ID] = sent
	mb.mailboxes[trash.ID] = trash

	// Default identity
	defaultIdentity := &jmap.Identity{
		ID:    "id-primary",
		Name:  "Primary User",
		Email: "user@example.com",
	}
	mb.identities[defaultIdentity.ID] = defaultIdentity

	// Create stub messages in Inbox
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
	mb.bumpState("Mailbox")
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
	mb.bumpState("Mailbox")
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
	} else {
		th.EmailIDs = append(th.EmailIDs, em.ID)
	}

	mb.recalculateMailboxCounts()
	mb.bumpState("Email")
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
					if v != nil {
						em.MailboxIDs[jmap.Id(k)] = true
					}
				}
			}
		} else if strings.HasPrefix(path, "mailboxIds/") {
			mID := jmap.Id(strings.TrimPrefix(path, "mailboxIds/"))
			if val == nil {
				delete(em.MailboxIDs, mID)
			} else {
				em.MailboxIDs[mID] = true
			}
		}
	}

	mb.recalculateMailboxCounts()
	mb.bumpState("Email")
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
		} else {
			th.EmailIDs = newIDs
		}
	}

	mb.recalculateMailboxCounts()
	mb.bumpState("Email")
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

		status := "signed"
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

	return &jmap.MDN{
		ID:          jmap.Id("mdn-parsed-" + string(blobID)),
		ForEmailID:  "email-stub",
		Subject:     "Disposition Notification",
		ReportingUA: "imap-jmap-server/1.0",
		Disposition: jmap.MDNDisposition{
			ActionMode:  "automatic-action",
			SendingMode: "MDN-sent-automatically",
			Type:        "displayed",
		},
	}, nil
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
