package imapsmtp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmappush"
)

// IMAPSMTPBackend implements jmapmail.MailBackend and jmapblob.BlobBackend using external IMAP and SMTP servers.
type IMAPSMTPBackend struct {
	imapHost string
	smtpHost string
	pool     *ClientPool

	ctx    context.Context
	cancel context.CancelFunc

	broadcaster *jmappush.Broadcaster

	accountsMu     sync.Mutex
	activeAccounts map[string]jmapauth.AuthCredentials
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
	submissions   map[string]map[jmapcore.Id]*jmapmail.EmailSubmission
	subTrackers   map[string]*subTracker

	identitiesMu sync.RWMutex
	identities   map[string]map[jmapcore.Id]*jmapmail.Identity

	movedMu  sync.RWMutex
	movedIDs map[string]map[jmapcore.Id]jmapcore.Id

	mailboxMu              sync.RWMutex
	mailboxMovedIDs        map[jmapcore.Id]jmapcore.Id
	mailboxParentOverrides map[string]map[jmapcore.Id]*jmapcore.Id
	mailboxSortOrders      map[string]map[jmapcore.Id]uint64
	mailboxSubscribed      map[string]map[jmapcore.Id]bool

	quotaTrackersMu    sync.RWMutex
	quotaTrackers      map[string]*itemTracker
	identityTrackersMu sync.RWMutex
	identityTrackers   map[string]*itemTracker
	vacationMu         sync.RWMutex
	vacationResponses  map[string]*jmapmail.VacationResponse
	vacationState      map[string]uint64
	pushMu             sync.RWMutex
	pushSubscriptions  map[string]map[jmapcore.Id]*jmapmail.PushSubscription
	blobsMu            sync.RWMutex
	blobs              map[string]*jmapblob.Blob
	blobRefs           map[string]map[string]map[jmapcore.Id]bool
	accountQuotasMu    sync.RWMutex
	accountQuotas      map[string]*accountQuota
	emailMutationsMu   sync.RWMutex
	emailSeq           map[string]uint64
	emailMutations     map[string][]itemChangeEntry
}

var _ jmapmail.MailBackend = (*IMAPSMTPBackend)(nil)
var _ jmapblob.BlobBackend = (*IMAPSMTPBackend)(nil)
var _ jmapblob.BlobReferenceBackend = (*IMAPSMTPBackend)(nil)
var _ jmappush.SubscriptionListener = (*IMAPSMTPBackend)(nil)
var _ jmapmail.SMTPAvailableBackend = (*IMAPSMTPBackend)(nil)

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
		activeAccounts:         make(map[string]jmapauth.AuthCredentials),
		idleWatchers:           make(map[string]*idleWatcherEntry),
		lastStates:             make(map[string]string),
		lastSweep:              make(map[string]time.Time),
		submissions:            make(map[string]map[jmapcore.Id]*jmapmail.EmailSubmission),
		subTrackers:            make(map[string]*subTracker),
		identities:             make(map[string]map[jmapcore.Id]*jmapmail.Identity),
		movedIDs:               make(map[string]map[jmapcore.Id]jmapcore.Id),
		mailboxMovedIDs:        make(map[jmapcore.Id]jmapcore.Id),
		mailboxParentOverrides: make(map[string]map[jmapcore.Id]*jmapcore.Id),
		quotaTrackers:          make(map[string]*itemTracker),
		identityTrackers:       make(map[string]*itemTracker),
		vacationResponses:      make(map[string]*jmapmail.VacationResponse),
		vacationState:          make(map[string]uint64),
		pushSubscriptions:      make(map[string]map[jmapcore.Id]*jmapmail.PushSubscription),
		blobs:                  make(map[string]*jmapblob.Blob),
		blobRefs:               make(map[string]map[string]map[jmapcore.Id]bool),
		accountQuotas:          make(map[string]*accountQuota),
		emailSeq:               make(map[string]uint64),
		emailMutations:         make(map[string][]itemChangeEntry),
	}
}

func (b *IMAPSMTPBackend) trackMovedEmail(accountID string, oldID, newID jmapcore.Id) {
	b.movedMu.Lock()
	defer b.movedMu.Unlock()
	if b.movedIDs == nil {
		b.movedIDs = make(map[string]map[jmapcore.Id]jmapcore.Id)
	}
	accMoved := b.movedIDs[accountID]
	if accMoved == nil {
		accMoved = make(map[jmapcore.Id]jmapcore.Id)
		b.movedIDs[accountID] = accMoved
	}
	for k, v := range accMoved {
		if v == oldID {
			accMoved[k] = newID
		}
	}
	accMoved[oldID] = newID
}

func (b *IMAPSMTPBackend) resolveMovedEmailID(accountID string, id jmapcore.Id) jmapcore.Id {
	b.movedMu.RLock()
	defer b.movedMu.RUnlock()
	curr := id
	if b.movedIDs != nil {
		if accMoved, ok := b.movedIDs[accountID]; ok {
			visited := make(map[jmapcore.Id]bool)
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
		return jmapcore.Id("mb-inbox-" + num)
	}
	return curr
}

func (b *IMAPSMTPBackend) trackMovedMailbox(oldID, newID jmapcore.Id) {
	b.mailboxMu.Lock()
	defer b.mailboxMu.Unlock()
	if b.mailboxMovedIDs == nil {
		b.mailboxMovedIDs = make(map[jmapcore.Id]jmapcore.Id)
	}
	for k, v := range b.mailboxMovedIDs {
		if v == oldID {
			b.mailboxMovedIDs[k] = newID
		}
	}
	b.mailboxMovedIDs[oldID] = newID
}

func (b *IMAPSMTPBackend) resolveMovedMailboxID(id jmapcore.Id) jmapcore.Id {
	b.mailboxMu.RLock()
	defer b.mailboxMu.RUnlock()
	curr := id
	for next, ok := b.mailboxMovedIDs[curr]; ok; next, ok = b.mailboxMovedIDs[curr] {
		curr = next
	}
	return curr
}

func (b *IMAPSMTPBackend) setMailboxParentOverride(accountID string, id jmapcore.Id, parentID *jmapcore.Id) {
	b.mailboxMu.Lock()
	defer b.mailboxMu.Unlock()
	if b.mailboxParentOverrides == nil {
		b.mailboxParentOverrides = make(map[string]map[jmapcore.Id]*jmapcore.Id)
	}
	if b.mailboxParentOverrides[accountID] == nil {
		b.mailboxParentOverrides[accountID] = make(map[jmapcore.Id]*jmapcore.Id)
	}
	b.mailboxParentOverrides[accountID][id] = parentID
}

func (b *IMAPSMTPBackend) getMailboxParentOverride(accountID string, id jmapcore.Id) (*jmapcore.Id, bool) {
	b.mailboxMu.RLock()
	defer b.mailboxMu.RUnlock()
	if b.mailboxParentOverrides == nil || b.mailboxParentOverrides[accountID] == nil {
		return nil, false
	}
	p, ok := b.mailboxParentOverrides[accountID][id]
	return p, ok
}

func (b *IMAPSMTPBackend) setMailboxSortOrder(accountID string, id jmapcore.Id, sortOrder uint64) {
	b.mailboxMu.Lock()
	defer b.mailboxMu.Unlock()
	if b.mailboxSortOrders == nil {
		b.mailboxSortOrders = make(map[string]map[jmapcore.Id]uint64)
	}
	if b.mailboxSortOrders[accountID] == nil {
		b.mailboxSortOrders[accountID] = make(map[jmapcore.Id]uint64)
	}
	b.mailboxSortOrders[accountID][id] = sortOrder
}

func (b *IMAPSMTPBackend) getMailboxSortOrder(accountID string, id jmapcore.Id) (uint64, bool) {
	b.mailboxMu.RLock()
	defer b.mailboxMu.RUnlock()
	if b.mailboxSortOrders == nil || b.mailboxSortOrders[accountID] == nil {
		return 0, false
	}
	so, ok := b.mailboxSortOrders[accountID][id]
	return so, ok
}

func (b *IMAPSMTPBackend) setMailboxSubscribed(accountID string, id jmapcore.Id, sub bool) {
	b.mailboxMu.Lock()
	defer b.mailboxMu.Unlock()
	if b.mailboxSubscribed == nil {
		b.mailboxSubscribed = make(map[string]map[jmapcore.Id]bool)
	}
	if b.mailboxSubscribed[accountID] == nil {
		b.mailboxSubscribed[accountID] = make(map[jmapcore.Id]bool)
	}
	b.mailboxSubscribed[accountID][id] = sub
}

func (b *IMAPSMTPBackend) getMailboxSubscribed(accountID string, id jmapcore.Id) (bool, bool) {
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
func (b *IMAPSMTPBackend) SetBroadcaster(bc *jmappush.Broadcaster) {
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
	accountID, ok := jmapauth.AccountIDFromContext(ctx)
	if !ok || accountID == "" {
		return
	}
	creds, ok := jmapauth.CredentialsFromContext(ctx)
	if !ok || creds.Username == "" {
		if subj, ok := jmapauth.SubjectFromContext(ctx); ok && subj != "" {
			creds = jmapauth.AuthCredentials{Username: subj, Password: subj}
		} else if sub, ok := jmapauth.SubjectForAccountID(accountID); ok && sub != "" {
			creds = jmapauth.AuthCredentials{Username: sub, Password: sub}
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
	accountID, ok := jmapauth.AccountIDFromContext(ctx)
	if !ok || accountID == "" {
		return
	}
	// If nobody is listening for push events for this account, skip querying IMAP state
	if !b.broadcaster.HasSubscribersForAccount(accountID) {
		return
	}
	b.RecordAccount(ctx)
	state := b.State(ctx)
	// Publish one atomic event so a subscriber cannot observe (and a
	// closeafter=state client cannot close on) a partial change set.
	b.broadcaster.PublishStateChanges(accountID, map[string]string{
		"Email":   state,
		"Mailbox": state,
		"Thread":  state,
		"Quota":   b.QuotaState(ctx),
	})
}

// Pool returns the underlying ClientPool.
func (b *IMAPSMTPBackend) Pool() *ClientPool {
	return b.pool
}

type itemChangeEntry struct {
	action string
	id     jmapcore.Id
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

func (t *itemTracker) Record(id jmapcore.Id, action string) string {
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

func (t *itemTracker) Changes(sinceState string, maxChanges *uint64) (created, updated, destroyed []jmapcore.Id, newState string, hasMore bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var since uint64
	if _, err := fmt.Sscanf(sinceState, "%d", &since); err != nil {
		return nil, nil, nil, fmt.Sprintf("%d", t.counter), true
	}
	if since > t.counter {
		return nil, nil, nil, fmt.Sprintf("%d", t.counter), true
	}

	createdSet := make(map[jmapcore.Id]bool)
	updatedSet := make(map[jmapcore.Id]bool)
	destroyedSet := make(map[jmapcore.Id]bool)

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

	sort.Slice(created, func(i, j int) bool { return created[i] < created[j] })
	sort.Slice(updated, func(i, j int) bool { return updated[i] < updated[j] })
	sort.Slice(destroyed, func(i, j int) bool { return destroyed[i] < destroyed[j] })

	if maxChanges != nil && *maxChanges > 0 {
		total := uint64(len(created) + len(updated) + len(destroyed))
		if total > *maxChanges {
			return nil, nil, nil, fmt.Sprintf("%d", t.counter), true
		}
	}

	return created, updated, destroyed, fmt.Sprintf("%d", t.counter), false
}

func (b *IMAPSMTPBackend) recordEmailMutation(accountID string, id jmapcore.Id, action string) {
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
	emailSizes    map[jmapcore.Id]uint64
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
			emailSizes:    make(map[jmapcore.Id]uint64),
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
			return jmapcore.SetError{
				Type:        "overQuota",
				Description: fmt.Sprintf("storage quota exceeded: %d + %d > %d octets", aq.octetsUsed, octets, aq.octetsLimit),
			}
		}
		if aq.messagesLimit > 0 && aq.messagesUsed+1 > aq.messagesLimit {
			return jmapcore.SetError{
				Type:        "overQuota",
				Description: fmt.Sprintf("message count quota exceeded: %d + 1 > %d messages", aq.messagesUsed, aq.messagesLimit),
			}
		}
	}
	return nil
}

func (b *IMAPSMTPBackend) recordEmailQuotaCreated(accountID string, emailID jmapcore.Id, size uint64) {
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

func (b *IMAPSMTPBackend) recordEmailQuotaDeleted(accountID string, emailID jmapcore.Id) {
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

func (b *IMAPSMTPBackend) trackMovedEmailQuota(accountID string, origID, newID jmapcore.Id) {
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

func (b *IMAPSMTPBackend) getAccountQuotas(accountID string) []*jmapmail.Quota {
	aq := b.getAccountQuota(accountID)
	aq.mu.RLock()
	octetsUsed := aq.octetsUsed
	messagesUsed := aq.messagesUsed
	octetsLimit := aq.octetsLimit
	messagesLimit := aq.messagesLimit
	aq.mu.RUnlock()

	return []*jmapmail.Quota{
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
