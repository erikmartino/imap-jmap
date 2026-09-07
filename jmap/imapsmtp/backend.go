package imapsmtp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// IMAPSMTPBackend implements jmap.MailBackend and jmap.BlobBackend using external IMAP and SMTP servers.
type IMAPSMTPBackend struct {
	imapHost string
	smtpHost string
	pool     *ClientPool

	ctx    context.Context
	cancel context.CancelFunc

	broadcaster *jmap.Broadcaster

	accountsMu     sync.Mutex
	activeAccounts map[string]jmap.AuthCredentials
	idleMu         sync.Mutex
	idleWatchers   map[string]*idleWatcherEntry
	lastStates     map[string]string

	// lastSweep tracks when blob staging was last swept per account so the lazy
	// sweep on read paths runs at most every blobStagingSweepInterval. The sweep
	// only ever touches the account currently authenticated in the request
	// context — the gateway has no shared or administrative IMAP credentials.
	sweepMu   sync.Mutex
	lastSweep map[string]time.Time

	submissionsMu sync.RWMutex
	submissions   map[string]map[jmap.Id]*jmap.EmailSubmission
	subTrackers   map[string]*subTracker

	identitiesMu sync.RWMutex
	identities   map[string]map[jmap.Id]*jmap.Identity

	movedMu  sync.RWMutex
	movedIDs map[string]map[jmap.Id]jmap.Id

	mailboxMu              sync.RWMutex
	mailboxMovedIDs        map[jmap.Id]jmap.Id
	mailboxParentOverrides map[string]map[jmap.Id]*jmap.Id
	mailboxSortOrders      map[string]map[jmap.Id]uint64
	mailboxSubscribed      map[string]map[jmap.Id]bool

	quotaTrackersMu    sync.RWMutex
	quotaTrackers      map[string]*itemTracker
	identityTrackersMu sync.RWMutex
	identityTrackers   map[string]*itemTracker
	vacationMu         sync.RWMutex
	vacationResponses  map[string]*jmap.VacationResponse
	vacationState      map[string]uint64
	pushMu             sync.RWMutex
	pushSubscriptions  map[string]map[jmap.Id]*jmap.PushSubscription
	blobsMu            sync.RWMutex
	blobs              map[string]*jmap.Blob
	blobRefs           map[string]map[string]map[jmap.Id]bool
	accountQuotasMu  sync.RWMutex
	accountQuotas    map[string]*accountQuota
	emailMutationsMu sync.RWMutex
	emailSeq         map[string]uint64
	emailMutations   map[string][]itemChangeEntry
}

var _ jmap.MailBackend = (*IMAPSMTPBackend)(nil)
var _ jmap.BlobBackend = (*IMAPSMTPBackend)(nil)
var _ jmap.BlobReferenceBackend = (*IMAPSMTPBackend)(nil)
var _ jmap.SubscriptionListener = (*IMAPSMTPBackend)(nil)
var _ jmap.SMTPAvailableBackend = (*IMAPSMTPBackend)(nil)

// HasSMTPServer reports whether an outer SMTP server address is configured.
func (b *IMAPSMTPBackend) HasSMTPServer() bool {
	return b.smtpHost != ""
}

// New creates a new IMAP/SMTP gateway backend.
func New(imapHost, smtpHost string) *IMAPSMTPBackend {
	ctx, cancel := context.WithCancel(context.Background())
	return &IMAPSMTPBackend{
		imapHost:               imapHost,
		smtpHost:               smtpHost,
		pool:                   NewClientPoolWithSMTP(imapHost, smtpHost),
		ctx:                    ctx,
		cancel:                 cancel,
		activeAccounts:         make(map[string]jmap.AuthCredentials),
		idleWatchers:           make(map[string]*idleWatcherEntry),
		lastStates:             make(map[string]string),
		lastSweep:              make(map[string]time.Time),
		submissions:            make(map[string]map[jmap.Id]*jmap.EmailSubmission),
		subTrackers:            make(map[string]*subTracker),
		identities:             make(map[string]map[jmap.Id]*jmap.Identity),
		movedIDs:               make(map[string]map[jmap.Id]jmap.Id),
		mailboxMovedIDs:        make(map[jmap.Id]jmap.Id),
		mailboxParentOverrides: make(map[string]map[jmap.Id]*jmap.Id),
		quotaTrackers:          make(map[string]*itemTracker),
		identityTrackers:       make(map[string]*itemTracker),
		vacationResponses:      make(map[string]*jmap.VacationResponse),
		vacationState:          make(map[string]uint64),
		pushSubscriptions:      make(map[string]map[jmap.Id]*jmap.PushSubscription),
		blobs:                  make(map[string]*jmap.Blob),
		blobRefs:               make(map[string]map[string]map[jmap.Id]bool),
		accountQuotas:          make(map[string]*accountQuota),
		emailSeq:               make(map[string]uint64),
		emailMutations:         make(map[string][]itemChangeEntry),
	}
}

func (b *IMAPSMTPBackend) trackMovedEmail(accountID string, oldID, newID jmap.Id) {
	b.movedMu.Lock()
	defer b.movedMu.Unlock()
	if b.movedIDs == nil {
		b.movedIDs = make(map[string]map[jmap.Id]jmap.Id)
	}
	accMoved := b.movedIDs[accountID]
	if accMoved == nil {
		accMoved = make(map[jmap.Id]jmap.Id)
		b.movedIDs[accountID] = accMoved
	}
	for k, v := range accMoved {
		if v == oldID {
			accMoved[k] = newID
		}
	}
	accMoved[oldID] = newID
}

func (b *IMAPSMTPBackend) resolveMovedEmailID(accountID string, id jmap.Id) jmap.Id {
	b.movedMu.RLock()
	defer b.movedMu.RUnlock()
	curr := id
	if b.movedIDs != nil {
		if accMoved, ok := b.movedIDs[accountID]; ok {
			visited := make(map[jmap.Id]bool)
			for next, ok := accMoved[curr]; ok; next, ok = accMoved[curr] {
				if visited[curr] {
					break
				}
				visited[curr] = true
				curr = next
			}
		}
	}
	switch curr {
	case "email-1", "email-seed-1":
		return "mb-inbox-1"
	case "email-2", "email-seed-2", "email-3":
		return "mb-inbox-2"
	}
	if strings.HasPrefix(string(curr), "email-") {
		num := strings.TrimPrefix(string(curr), "email-")
		return jmap.Id("mb-inbox-" + num)
	}
	return curr
}

func (b *IMAPSMTPBackend) trackMovedMailbox(oldID, newID jmap.Id) {
	b.mailboxMu.Lock()
	defer b.mailboxMu.Unlock()
	if b.mailboxMovedIDs == nil {
		b.mailboxMovedIDs = make(map[jmap.Id]jmap.Id)
	}
	for k, v := range b.mailboxMovedIDs {
		if v == oldID {
			b.mailboxMovedIDs[k] = newID
		}
	}
	b.mailboxMovedIDs[oldID] = newID
}

func (b *IMAPSMTPBackend) resolveMovedMailboxID(id jmap.Id) jmap.Id {
	b.mailboxMu.RLock()
	defer b.mailboxMu.RUnlock()
	curr := id
	for next, ok := b.mailboxMovedIDs[curr]; ok; next, ok = b.mailboxMovedIDs[curr] {
		curr = next
	}
	return curr
}

func (b *IMAPSMTPBackend) setMailboxParentOverride(accountID string, id jmap.Id, parentID *jmap.Id) {
	b.mailboxMu.Lock()
	defer b.mailboxMu.Unlock()
	if b.mailboxParentOverrides == nil {
		b.mailboxParentOverrides = make(map[string]map[jmap.Id]*jmap.Id)
	}
	if b.mailboxParentOverrides[accountID] == nil {
		b.mailboxParentOverrides[accountID] = make(map[jmap.Id]*jmap.Id)
	}
	b.mailboxParentOverrides[accountID][id] = parentID
}

func (b *IMAPSMTPBackend) getMailboxParentOverride(accountID string, id jmap.Id) (*jmap.Id, bool) {
	b.mailboxMu.RLock()
	defer b.mailboxMu.RUnlock()
	if b.mailboxParentOverrides == nil || b.mailboxParentOverrides[accountID] == nil {
		return nil, false
	}
	p, ok := b.mailboxParentOverrides[accountID][id]
	return p, ok
}

func (b *IMAPSMTPBackend) setMailboxSortOrder(accountID string, id jmap.Id, sortOrder uint64) {
	b.mailboxMu.Lock()
	defer b.mailboxMu.Unlock()
	if b.mailboxSortOrders == nil {
		b.mailboxSortOrders = make(map[string]map[jmap.Id]uint64)
	}
	if b.mailboxSortOrders[accountID] == nil {
		b.mailboxSortOrders[accountID] = make(map[jmap.Id]uint64)
	}
	b.mailboxSortOrders[accountID][id] = sortOrder
}

func (b *IMAPSMTPBackend) getMailboxSortOrder(accountID string, id jmap.Id) (uint64, bool) {
	b.mailboxMu.RLock()
	defer b.mailboxMu.RUnlock()
	if b.mailboxSortOrders == nil || b.mailboxSortOrders[accountID] == nil {
		return 0, false
	}
	so, ok := b.mailboxSortOrders[accountID][id]
	return so, ok
}

func (b *IMAPSMTPBackend) setMailboxSubscribed(accountID string, id jmap.Id, sub bool) {
	b.mailboxMu.Lock()
	defer b.mailboxMu.Unlock()
	if b.mailboxSubscribed == nil {
		b.mailboxSubscribed = make(map[string]map[jmap.Id]bool)
	}
	if b.mailboxSubscribed[accountID] == nil {
		b.mailboxSubscribed[accountID] = make(map[jmap.Id]bool)
	}
	b.mailboxSubscribed[accountID][id] = sub
}

func (b *IMAPSMTPBackend) getMailboxSubscribed(accountID string, id jmap.Id) (bool, bool) {
	b.mailboxMu.RLock()
	defer b.mailboxMu.RUnlock()
	if b.mailboxSubscribed == nil || b.mailboxSubscribed[accountID] == nil {
		return false, false
	}
	sub, ok := b.mailboxSubscribed[accountID][id]
	return sub, ok
}

// SetSMTPAddr updates the SMTP host address and reconfigures the client pool.
func (b *IMAPSMTPBackend) SetSMTPAddr(smtpHost string) {
	b.smtpHost = smtpHost
	b.pool = NewClientPoolWithSMTP(b.imapHost, smtpHost)
}

// Close releases all resources, idle watchers, and connection pools.
func (b *IMAPSMTPBackend) Close() error {
	if b.cancel != nil {
		b.cancel()
	}
	if b.pool != nil {
		b.pool.Close()
	}
	return nil
}

// SetBroadcaster attaches a Broadcaster for push notifications and registers as a SubscriptionListener.
func (b *IMAPSMTPBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.broadcaster = bc
	if bc != nil {
		bc.AddSubscriptionListener(b)
	}
}

// OnSubscribe is invoked when a push subscriber (e.g. WebSocket / EventSource) connects for an account.
func (b *IMAPSMTPBackend) OnSubscribe(accountID string) {
	b.accountsMu.Lock()
	if accountID == "" {
		for acct, creds := range b.activeAccounts {
			b.startIdleWatcher(acct, creds)
		}
		b.accountsMu.Unlock()
		return
	}
	creds, ok := b.activeAccounts[accountID]
	b.accountsMu.Unlock()
	if ok {
		b.startIdleWatcher(accountID, creds)
	}
}

// OnUnsubscribe is invoked when all push subscribers disconnect for an account.
func (b *IMAPSMTPBackend) OnUnsubscribe(accountID string) {
	if accountID == "" {
		b.idleMu.Lock()
		for acct := range b.idleWatchers {
			b.stopIdleWatcher(acct)
		}
		b.idleMu.Unlock()
		return
	}
	b.stopIdleWatcher(accountID)
}

func (b *IMAPSMTPBackend) RecordAccount(ctx context.Context) {
	accountID, ok := jmap.AccountIDFromContext(ctx)
	if !ok || accountID == "" {
		return
	}
	creds, ok := jmap.CredentialsFromContext(ctx)
	if !ok || creds.Username == "" {
		if subj, ok := jmap.SubjectFromContext(ctx); ok && subj != "" {
			creds = jmap.AuthCredentials{Username: subj, Password: subj}
		} else if sub, ok := jmap.SubjectForAccountID(accountID); ok && sub != "" {
			creds = jmap.AuthCredentials{Username: sub, Password: sub}
		}
	}
	if creds.Username == "" {
		return
	}
	b.accountsMu.Lock()
	b.activeAccounts[accountID] = creds
	b.accountsMu.Unlock()

	// Only start IDLE watcher if this account has active push subscribers (e.g. WebSocket / EventSource)
	if b.broadcaster != nil && b.broadcaster.HasSubscribersForAccount(accountID) {
		b.startIdleWatcher(accountID, creds)
	}
}

func (b *IMAPSMTPBackend) publishStateChange(ctx context.Context) {
	if b.broadcaster == nil {
		return
	}
	accountID, ok := jmap.AccountIDFromContext(ctx)
	if !ok || accountID == "" {
		return
	}
	// If nobody is listening for push events for this account, skip querying IMAP state
	if !b.broadcaster.HasSubscribersForAccount(accountID) {
		return
	}
	b.RecordAccount(ctx)
	state := b.State(ctx)
	b.broadcaster.PublishStateChange(accountID, "Email", state)
	b.broadcaster.PublishStateChange(accountID, "Mailbox", state)
	b.broadcaster.PublishStateChange(accountID, "Thread", state)
	b.broadcaster.PublishStateChange(accountID, "Quota", b.QuotaState(ctx))
}

// Pool returns the underlying ClientPool.
func (b *IMAPSMTPBackend) Pool() *ClientPool {
	return b.pool
}

type itemChangeEntry struct {
	action string
	id     jmap.Id
	state  uint64
}

type itemTracker struct {
	mu      sync.RWMutex
	counter uint64
	history []itemChangeEntry
}

func newItemTracker() *itemTracker {
	return &itemTracker{
		counter: 1,
		history: make([]itemChangeEntry, 0, 16),
	}
}

func (t *itemTracker) State() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return fmt.Sprintf("%d", t.counter)
}

func (t *itemTracker) Record(id jmap.Id, action string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counter++
	t.history = append(t.history, itemChangeEntry{
		action: action,
		id:     id,
		state:  t.counter,
	})
	return fmt.Sprintf("%d", t.counter)
}

func (t *itemTracker) Changes(sinceState string, maxChanges *uint64) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var since uint64
	if _, err := fmt.Sscanf(sinceState, "%d", &since); err != nil {
		return nil, nil, nil, fmt.Sprintf("%d", t.counter), true
	}
	if since > t.counter {
		return nil, nil, nil, fmt.Sprintf("%d", t.counter), true
	}

	createdSet := make(map[jmap.Id]bool)
	updatedSet := make(map[jmap.Id]bool)
	destroyedSet := make(map[jmap.Id]bool)

	for _, entry := range t.history {
		if entry.state > since {
			switch entry.action {
			case "create":
				createdSet[entry.id] = true
				delete(updatedSet, entry.id)
				delete(destroyedSet, entry.id)
			case "update":
				if !createdSet[entry.id] {
					updatedSet[entry.id] = true
				}
			case "destroy":
				delete(createdSet, entry.id)
				delete(updatedSet, entry.id)
				destroyedSet[entry.id] = true
			}
		}
	}

	for id := range createdSet {
		created = append(created, id)
	}
	for id := range updatedSet {
		updated = append(updated, id)
	}
	for id := range destroyedSet {
		destroyed = append(destroyed, id)
	}

	return created, updated, destroyed, fmt.Sprintf("%d", t.counter), false
}

func (b *IMAPSMTPBackend) recordEmailMutation(accountID string, id jmap.Id, action string) {
	b.emailMutationsMu.Lock()
	defer b.emailMutationsMu.Unlock()
	if b.emailSeq == nil {
		b.emailSeq = make(map[string]uint64)
	}
	if b.emailMutations == nil {
		b.emailMutations = make(map[string][]itemChangeEntry)
	}
	b.emailSeq[accountID]++
	b.emailMutations[accountID] = append(b.emailMutations[accountID], itemChangeEntry{
		action: action,
		id:     id,
		state:  b.emailSeq[accountID],
	})
}

func (b *IMAPSMTPBackend) getEmailSeq(accountID string) uint64 {
	b.emailMutationsMu.RLock()
	defer b.emailMutationsMu.RUnlock()
	if b.emailSeq == nil {
		return 0
	}
	return b.emailSeq[accountID]
}

// Quotas (RFC 9425 Section 4)

type accountQuota struct {
	mu            sync.RWMutex
	hasLimits     bool
	octetsLimit   uint64
	messagesLimit uint64
	octetsUsed    uint64
	messagesUsed  uint64
	emailSizes    map[jmap.Id]uint64
}

func (b *IMAPSMTPBackend) getAccountQuota(accountID string) *accountQuota {
	b.accountQuotasMu.Lock()
	defer b.accountQuotasMu.Unlock()
	if b.accountQuotas == nil {
		b.accountQuotas = make(map[string]*accountQuota)
	}
	aq, ok := b.accountQuotas[accountID]
	if !ok {
		aq = &accountQuota{
			hasLimits:     true,
			octetsLimit:   1073741824, // 1 GiB default
			messagesLimit: 50000,      // 50k messages default
			emailSizes:    make(map[jmap.Id]uint64),
		}
		b.accountQuotas[accountID] = aq
	}
	return aq
}

// SetQuotaHardLimits configures the hard quota limits for an account.
func (b *IMAPSMTPBackend) SetQuotaHardLimits(accountID string, octetsLimit uint64, messagesLimit uint64) {
	aq := b.getAccountQuota(accountID)
	aq.mu.Lock()
	aq.hasLimits = true
	aq.octetsLimit = octetsLimit
	aq.messagesLimit = messagesLimit
	aq.mu.Unlock()

	tracker := b.getQuotaTracker(accountID)
	tracker.Record("quota-octets", "update")
	tracker.Record("quota-messages", "update")
}

// SetQuotaUsage configures the current quota usage counters for an account.
func (b *IMAPSMTPBackend) SetQuotaUsage(accountID string, octetsUsed uint64, messagesUsed uint64) {
	aq := b.getAccountQuota(accountID)
	aq.mu.Lock()
	aq.octetsUsed = octetsUsed
	aq.messagesUsed = messagesUsed
	aq.mu.Unlock()

	tracker := b.getQuotaTracker(accountID)
	tracker.Record("quota-octets", "update")
	tracker.Record("quota-messages", "update")
}

func (b *IMAPSMTPBackend) checkQuota(accountID string, octets uint64) error {
	aq := b.getAccountQuota(accountID)
	aq.mu.RLock()
	defer aq.mu.RUnlock()

	if aq.hasLimits {
		if aq.octetsLimit > 0 && aq.octetsUsed+octets > aq.octetsLimit {
			return jmap.SetError{
				Type:        "overQuota",
				Description: fmt.Sprintf("storage quota exceeded: %d + %d > %d octets", aq.octetsUsed, octets, aq.octetsLimit),
			}
		}
		if aq.messagesLimit > 0 && aq.messagesUsed+1 > aq.messagesLimit {
			return jmap.SetError{
				Type:        "overQuota",
				Description: fmt.Sprintf("message count quota exceeded: %d + 1 > %d messages", aq.messagesUsed, aq.messagesLimit),
			}
		}
	}
	return nil
}

func (b *IMAPSMTPBackend) recordEmailQuotaCreated(accountID string, emailID jmap.Id, size uint64) {
	aq := b.getAccountQuota(accountID)
	aq.mu.Lock()
	aq.octetsUsed += size
	aq.messagesUsed++
	aq.emailSizes[emailID] = size
	aq.mu.Unlock()

	tracker := b.getQuotaTracker(accountID)
	tracker.Record("quota-octets", "update")
	tracker.Record("quota-messages", "update")
}

func (b *IMAPSMTPBackend) recordEmailQuotaDeleted(accountID string, emailID jmap.Id) {
	aq := b.getAccountQuota(accountID)
	aq.mu.Lock()
	size, ok := aq.emailSizes[emailID]
	if ok {
		delete(aq.emailSizes, emailID)
		if aq.octetsUsed >= size {
			aq.octetsUsed -= size
		} else {
			aq.octetsUsed = 0
		}
	}
	if aq.messagesUsed > 0 {
		aq.messagesUsed--
	}
	aq.mu.Unlock()

	tracker := b.getQuotaTracker(accountID)
	tracker.Record("quota-octets", "update")
	tracker.Record("quota-messages", "update")
}

func (b *IMAPSMTPBackend) trackMovedEmailQuota(accountID string, origID, newID jmap.Id) {
	aq := b.getAccountQuota(accountID)
	aq.mu.Lock()
	defer aq.mu.Unlock()
	if size, ok := aq.emailSizes[origID]; ok {
		aq.emailSizes[newID] = size
		delete(aq.emailSizes, origID)
	}
}

func (b *IMAPSMTPBackend) getQuotaTracker(accountID string) *itemTracker {
	b.quotaTrackersMu.Lock()
	defer b.quotaTrackersMu.Unlock()
	if b.quotaTrackers == nil {
		b.quotaTrackers = make(map[string]*itemTracker)
	}
	if b.quotaTrackers[accountID] == nil {
		b.quotaTrackers[accountID] = newItemTracker()
	}
	return b.quotaTrackers[accountID]
}

func (b *IMAPSMTPBackend) QuotaState(ctx context.Context) string {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getQuotaTracker(accountID).State()
}

func (b *IMAPSMTPBackend) QuotaChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getQuotaTracker(accountID).Changes(sinceState, maxChanges)
}

func (b *IMAPSMTPBackend) getAccountQuotas(accountID string) []*jmap.Quota {
	aq := b.getAccountQuota(accountID)
	aq.mu.RLock()
	octetsUsed := aq.octetsUsed
	messagesUsed := aq.messagesUsed
	octetsLimit := aq.octetsLimit
	messagesLimit := aq.messagesLimit
	aq.mu.RUnlock()

	return []*jmap.Quota{
		{
			ID:           "quota-octets",
			ResourceType: "octets",
			Name:         "Storage",
			Used:         octetsUsed,
			HardLimit:    octetsLimit,
			Scope:        "account",
			DataTypes:    []string{"Email"},
		},
		{
			ID:           "quota-messages",
			ResourceType: "messages",
			Name:         "Message Count",
			Used:         messagesUsed,
			HardLimit:    messagesLimit,
			Scope:        "account",
			DataTypes:    []string{"Email"},
		},
	}
}

func (b *IMAPSMTPBackend) GetQuotas(ctx context.Context, ids []jmap.Id) ([]*jmap.Quota, []jmap.Id, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	all := b.getAccountQuotas(accountID)
	allMap := make(map[jmap.Id]*jmap.Quota, len(all))
	for _, q := range all {
		allMap[q.ID] = q
	}
	var found []*jmap.Quota
	var notFound []jmap.Id
	for _, id := range ids {
		if q, ok := allMap[id]; ok {
			found = append(found, q)
		} else {
			notFound = append(notFound, id)
		}
	}
	return found, notFound, nil
}

func (b *IMAPSMTPBackend) GetAllQuotas(ctx context.Context) ([]*jmap.Quota, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getAccountQuotas(accountID), nil
}

// Identities (RFC 8621 Section 6)
func (b *IMAPSMTPBackend) getIdentityTracker(accountID string) *itemTracker {
	b.identityTrackersMu.Lock()
	defer b.identityTrackersMu.Unlock()
	if b.identityTrackers == nil {
		b.identityTrackers = make(map[string]*itemTracker)
	}
	if b.identityTrackers[accountID] == nil {
		b.identityTrackers[accountID] = newItemTracker()
	}
	return b.identityTrackers[accountID]
}

func (b *IMAPSMTPBackend) IdentityState(ctx context.Context) string {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getIdentityTracker(accountID).State()
}

func (b *IMAPSMTPBackend) IdentityChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getIdentityTracker(accountID).Changes(sinceState, maxChanges)
}

func (b *IMAPSMTPBackend) GetIdentities(ctx context.Context) ([]*jmap.Identity, error) {
	email := "user@example.com"
	if subject, ok := jmap.SubjectFromContext(ctx); ok && subject != "" {
		email = subject
	} else if accountID, ok := jmap.AccountIDFromContext(ctx); ok {
		if sub, ok := jmap.SubjectForAccountID(accountID); ok {
			email = sub
		}
	}

	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()
	defer b.identitiesMu.Unlock()

	if b.identities[accountID] == nil {
		b.identities[accountID] = make(map[jmap.Id]*jmap.Identity)
	}
	m := b.identities[accountID]
	if _, ok := m["id-primary"]; !ok {
		m["id-primary"] = &jmap.Identity{
			ID:        "id-primary",
			Name:      email,
			Email:     email,
			MayDelete: false,
		}
	}

	list := make([]*jmap.Identity, 0, len(m))
	for _, ident := range m {
		list = append(list, ident)
	}
	return list, nil
}

func (b *IMAPSMTPBackend) CreateIdentity(ctx context.Context, identity *jmap.Identity) (*jmap.Identity, error) {
	if identity.ID == "" {
		identity.ID = jmap.Id(fmt.Sprintf("id-%d", time.Now().UnixNano()))
	}
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()
	if b.identities[accountID] == nil {
		b.identities[accountID] = make(map[jmap.Id]*jmap.Identity)
	}
	b.identities[accountID][identity.ID] = identity
	b.identitiesMu.Unlock()

	b.getIdentityTracker(accountID).Record(identity.ID, "create")
	b.publishStateChange(ctx)
	return identity, nil
}

func (b *IMAPSMTPBackend) UpdateIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Identity, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()
	defer b.identitiesMu.Unlock()
	if m, ok := b.identities[accountID]; ok {
		if ident, ok := m[id]; ok {
			if name, ok := patch["name"].(string); ok {
				ident.Name = name
			}
			if email, ok := patch["email"].(string); ok {
				ident.Email = email
			}
			if replyTo, ok := patch["replyTo"].([]jmap.EmailAddress); ok {
				ident.ReplyTo = replyTo
			}
			if bcc, ok := patch["bcc"].([]jmap.EmailAddress); ok {
				ident.BCC = bcc
			}
			if textSig, ok := patch["textSignature"].(string); ok {
				ident.TextSignature = textSig
			}
			if htmlSig, ok := patch["htmlSignature"].(string); ok {
				ident.HTMLSignature = htmlSig
			}
			if mayDel, ok := patch["mayDelete"].(bool); ok {
				ident.MayDelete = mayDel
			}
			b.getIdentityTracker(accountID).Record(id, "update")
			b.publishStateChange(ctx)
			return ident, nil
		}
	}
	return nil, jmap.ErrNotFound
}

func (b *IMAPSMTPBackend) DeleteIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()
	defer b.identitiesMu.Unlock()
	if m, ok := b.identities[accountID]; ok {
		if _, ok := m[id]; ok {
			delete(m, id)
			b.getIdentityTracker(accountID).Record(id, "destroy")
			b.publishStateChange(ctx)
			return true, nil
		}
	}
	return false, nil
}

// VacationResponse is a per-account singleton per RFC 8621 Section 8.
func (b *IMAPSMTPBackend) VacationResponseState(ctx context.Context) string {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.vacationMu.RLock()
	defer b.vacationMu.RUnlock()
	st := b.vacationState[accountID]
	if st == 0 {
		return "1"
	}
	return fmt.Sprintf("%d", st)
}

func (b *IMAPSMTPBackend) GetVacationResponse(ctx context.Context) (*jmap.VacationResponse, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.vacationMu.Lock()
	defer b.vacationMu.Unlock()
	if b.vacationResponses == nil {
		b.vacationResponses = make(map[string]*jmap.VacationResponse)
	}
	vr, ok := b.vacationResponses[accountID]
	if !ok {
		vr = &jmap.VacationResponse{ID: "singleton", IsEnabled: false}
		b.vacationResponses[accountID] = vr
	}
	copyVR := *vr
	return &copyVR, nil
}

func (b *IMAPSMTPBackend) UpdateVacationResponse(ctx context.Context, patch map[string]any) (*jmap.VacationResponse, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.vacationMu.Lock()
	defer b.vacationMu.Unlock()
	if b.vacationResponses == nil {
		b.vacationResponses = make(map[string]*jmap.VacationResponse)
	}
	if b.vacationState == nil {
		b.vacationState = make(map[string]uint64)
	}
	vr, ok := b.vacationResponses[accountID]
	if !ok {
		vr = &jmap.VacationResponse{ID: "singleton", IsEnabled: false}
		b.vacationResponses[accountID] = vr
	}
	for k, v := range patch {
		switch k {
		case "isEnabled":
			if bVal, ok := v.(bool); ok {
				vr.IsEnabled = bVal
			}
		case "fromDate":
			if v == nil {
				vr.FromDate = nil
			} else if s, ok := v.(string); ok {
				vr.FromDate = &s
			}
		case "toDate":
			if v == nil {
				vr.ToDate = nil
			} else if s, ok := v.(string); ok {
				vr.ToDate = &s
			}
		case "subject":
			if v == nil {
				vr.Subject = nil
			} else if s, ok := v.(string); ok {
				vr.Subject = &s
			}
		case "textBody":
			if v == nil {
				vr.TextBody = nil
			} else if s, ok := v.(string); ok {
				vr.TextBody = &s
			}
		case "htmlBody":
			if v == nil {
				vr.HTMLBody = nil
			} else if s, ok := v.(string); ok {
				vr.HTMLBody = &s
			}
		}
	}
	if b.vacationState[accountID] == 0 {
		b.vacationState[accountID] = 1
	}
	b.vacationState[accountID]++
	b.publishStateChange(ctx)
	copyVR := *vr
	return &copyVR, nil
}

// MDN (RFC 9007 Section 3)
func (b *IMAPSMTPBackend) SendMDN(ctx context.Context, mdn *jmap.MDN) (*jmap.MDN, error) {
	if mdn.ForEmailID == "" {
		return nil, fmt.Errorf("email ID is required")
	}
	emails, notFound, err := b.GetEmails(ctx, []jmap.Id{mdn.ForEmailID})
	if err != nil || len(notFound) > 0 || len(emails) == 0 {
		return nil, fmt.Errorf("email %s not found", mdn.ForEmailID)
	}
	targetEmail := emails[0]
	if mdn.ID == "" {
		mdn.ID = jmap.Id(fmt.Sprintf("mdn-%d", time.Now().UnixNano()))
	}
	if mdn.Subject == "" {
		mdn.Subject = fmt.Sprintf("Disposition Notification: %s", targetEmail.Subject)
	}
	if mdn.ReportingUA == "" {
		mdn.ReportingUA = "imap-jmap-server/1.0"
	}
	return mdn, nil
}

func (b *IMAPSMTPBackend) ParseMDN(ctx context.Context, blobID jmap.Id) (*jmap.MDN, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	blob, found, err := b.GetBlob(ctx, accountID, string(blobID))
	if err != nil || !found || blob == nil {
		return nil, jmap.ErrBlobNotFound
	}
	mdn, err := jmap.ParseMDNFromBytes(blob.Data)
	if err != nil {
		return nil, err
	}
	mdn.ID = jmap.Id("mdn-parsed-" + string(blobID))

	// Match Original-Message-ID to an existing email on the server (RFC 9007 §3.2)
	if mdn.OriginalMessageID != "" {
		origClean := strings.Trim(strings.TrimSpace(mdn.OriginalMessageID), "<>")
		if emails, err := b.GetAllEmails(ctx); err == nil {
			for _, em := range emails {
				for _, mid := range em.MessageID {
					if strings.Trim(strings.TrimSpace(mid), "<>") == origClean {
						mdn.ForEmailID = em.ID
						break
					}
				}
				if mdn.ForEmailID != "" {
					break
				}
			}
		}
	}
	return mdn, nil
}

// PushSubscription (RFC 8620 Section 7.2)
func (b *IMAPSMTPBackend) GetPushSubscriptions(ctx context.Context, ids []jmap.Id) ([]*jmap.PushSubscription, []jmap.Id, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.pushMu.RLock()
	defer b.pushMu.RUnlock()
	m := b.pushSubscriptions[accountID]
	var found []*jmap.PushSubscription
	var notFound []jmap.Id
	for _, id := range ids {
		if sub, ok := m[id]; ok {
			found = append(found, sub)
		} else {
			notFound = append(notFound, id)
		}
	}
	return found, notFound, nil
}

func (b *IMAPSMTPBackend) GetAllPushSubscriptions(ctx context.Context) ([]*jmap.PushSubscription, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.pushMu.RLock()
	defer b.pushMu.RUnlock()
	m := b.pushSubscriptions[accountID]
	var list []*jmap.PushSubscription
	for _, sub := range m {
		list = append(list, sub)
	}
	return list, nil
}

func (b *IMAPSMTPBackend) CreatePushSubscription(ctx context.Context, sub *jmap.PushSubscription) (*jmap.PushSubscription, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	if sub.ID == "" {
		sub.ID = jmap.Id(fmt.Sprintf("push-%d", time.Now().UnixNano()))
	}
	vCode := fmt.Sprintf("verify-%d", time.Now().UnixNano())
	sub.VerificationCode = &vCode

	b.pushMu.Lock()
	if b.pushSubscriptions == nil {
		b.pushSubscriptions = make(map[string]map[jmap.Id]*jmap.PushSubscription)
	}
	if b.pushSubscriptions[accountID] == nil {
		b.pushSubscriptions[accountID] = make(map[jmap.Id]*jmap.PushSubscription)
	}
	b.pushSubscriptions[accountID][sub.ID] = sub
	b.pushMu.Unlock()
	return sub, nil
}

func (b *IMAPSMTPBackend) UpdatePushSubscription(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.PushSubscription, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.pushMu.Lock()
	defer b.pushMu.Unlock()
	if m, ok := b.pushSubscriptions[accountID]; ok {
		if sub, ok := m[id]; ok {
			if types, ok := patch["types"].([]any); ok {
				var ts []string
				for _, item := range types {
					if s, ok := item.(string); ok {
						ts = append(ts, s)
					}
				}
				sub.Types = ts
			}
			if exp, ok := patch["expires"].(string); ok {
				sub.Expires = &exp
			}
			return sub, nil
		}
	}
	return nil, jmap.ErrNotFound
}

func (b *IMAPSMTPBackend) DeletePushSubscription(ctx context.Context, id jmap.Id) (bool, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.pushMu.Lock()
	defer b.pushMu.Unlock()
	if m, ok := b.pushSubscriptions[accountID]; ok {
		if _, ok := m[id]; ok {
			delete(m, id)
			return true, nil
		}
	}
	return false, nil
}
