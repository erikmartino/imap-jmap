package imapsmtp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

type subChangeEntry struct {
	action string // "create", "update", "destroy"
	id     jmapcore.Id
	state  uint64
}

type subTracker struct {
	mu      sync.RWMutex
	counter uint64
	history []subChangeEntry
}

func newSubTracker() *subTracker {
	return &subTracker{
		history: make([]subChangeEntry, 0, 16),
	}
}

func (t *subTracker) State() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return fmt.Sprintf("sub-state-%d", t.counter)
}

func (t *subTracker) Record(id jmapcore.Id, action string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counter++
	t.history = append(t.history, subChangeEntry{
		action: action,
		id:     id,
		state:  t.counter,
	})
	return fmt.Sprintf("sub-state-%d", t.counter)
}

func (t *subTracker) Changes(sinceState string, maxChanges *uint64) (created, updated, destroyed []jmapcore.Id, newState string, hasMore bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var since uint64
	s := strings.TrimPrefix(sinceState, "sub-state-")
	s = strings.TrimPrefix(s, "state-")
	_, _ = fmt.Sscanf(s, "%d", &since)

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

	return created, updated, destroyed, fmt.Sprintf("sub-state-%d", t.counter), false
}

func (b *IMAPSMTPBackend) getSubTrackerLocked(accountID string) *subTracker {
	if b.subTrackers[accountID] == nil {
		b.subTrackers[accountID] = newSubTracker()
	}
	return b.subTrackers[accountID]
}

func (b *IMAPSMTPBackend) getSubMapLocked(accountID string) map[jmapcore.Id]*jmapmail.EmailSubmission {
	if b.submissions[accountID] == nil {
		b.submissions[accountID] = make(map[jmapcore.Id]*jmapmail.EmailSubmission)
	}
	return b.submissions[accountID]
}

// CreateSubmission sends an outbound email via SMTP and stores a sent copy in IMAP Sent folder.
func (b *IMAPSMTPBackend) CreateSubmission(ctx context.Context, sub *jmapmail.EmailSubmission) (*jmapmail.EmailSubmission, error) {
	emails, _, err := b.GetEmails(ctx, []jmapcore.Id{sub.EmailID})
	if err != nil || len(emails) == 0 {
		return nil, fmt.Errorf("referenced email not found: %s", sub.EmailID)
	}
	em := emails[0]

	rawBytes, _ := b.getRawEmailBytes(ctx, sub.EmailID)
	if len(rawBytes) == 0 {
		rawBytes = jmapmail.FormatEmailRFC822(em)
	}

	var from string
	if sub.Envelope != nil && sub.Envelope.MailFrom.Email != "" {
		from = sub.Envelope.MailFrom.Email
	} else if len(em.From) > 0 {
		from = em.From[0].Email
	}
	if from == "" {
		if subj, ok := jmapauth.SubjectFromContext(ctx); ok && subj != "" {
			from = subj
		} else if accID, ok := jmapauth.AccountIDFromContext(ctx); ok && accID != "" {
			if s, ok := jmapauth.SubjectForAccountID(accID); ok {
				from = s
			}
		}
	}
	from = strings.Trim(from, "<>")

	var recipients []string
	if sub.Envelope != nil && len(sub.Envelope.RcptTo) > 0 {
		for _, rcpt := range sub.Envelope.RcptTo {
			recipients = append(recipients, rcpt.Email)
		}
	} else {
		for _, addr := range em.To {
			recipients = append(recipients, addr.Email)
		}
		for _, addr := range em.CC {
			recipients = append(recipients, addr.Email)
		}
		for _, addr := range em.BCC {
			recipients = append(recipients, addr.Email)
		}
	}

	var toSend []string
	for _, rcpt := range recipients {
		clean := strings.Trim(strings.TrimSpace(rcpt), "<>")
		if clean == "" {
			continue
		}
		if st, ok := sub.DeliveryStatus[clean]; ok && st.Delivered == "failed" {
			continue
		}
		if st, ok := sub.DeliveryStatus[rcpt]; ok && st.Delivered == "failed" {
			continue
		}
		toSend = append(toSend, clean)
	}

	// Dispatch over SMTP if configured and recipients exist
	if b.smtpHost != "" && len(toSend) > 0 {
		if err := b.pool.SendMail(ctx, from, toSend, rawBytes); err != nil {
			return nil, fmt.Errorf("failed to send outbound email via SMTP: %w", err)
		}
	}

	if sub.ID == "" {
		sub.ID = jmapcore.Id(fmt.Sprintf("sub-%d", time.Now().UnixNano()))
	}
	if sub.SendAt == "" {
		sub.SendAt = time.Now().UTC().Format(time.RFC3339)
	}
	if sub.UndoStatus == "" {
		sub.UndoStatus = "final"
	}
	if sub.ThreadID == "" {
		sub.ThreadID = em.ThreadID
	}

	if sub.DeliveryStatus == nil {
		sub.DeliveryStatus = make(map[string]jmapmail.DeliveryStatus)
	}
	for _, rcpt := range recipients {
		if _, ok := sub.DeliveryStatus[rcpt]; !ok {
			sub.DeliveryStatus[rcpt] = jmapmail.DeliveryStatus{
				Delivered: "yes",
				SmtpReply: "250 2.0.0 OK message queued",
			}
		}
	}

	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.submissionsMu.Lock()
	m := b.getSubMapLocked(accountID)
	m[sub.ID] = sub
	tr := b.getSubTrackerLocked(accountID)
	tr.Record(sub.ID, "create")
	b.submissionsMu.Unlock()

	b.publishSubmissionStateChange(ctx)

	return sub, nil
}

func (b *IMAPSMTPBackend) publishSubmissionStateChange(ctx context.Context) {
	if b.broadcaster == nil {
		return
	}
	accountID, ok := jmapauth.AccountIDFromContext(ctx)
	if !ok || accountID == "" {
		return
	}
	state := b.SubmissionState(ctx)
	b.broadcaster.PublishStateChange(accountID, "EmailSubmission", state)
}

func (b *IMAPSMTPBackend) SubmissionState(ctx context.Context) string {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.submissionsMu.Lock()
	defer b.submissionsMu.Unlock()
	tr := b.getSubTrackerLocked(accountID)
	return tr.State()
}

func (b *IMAPSMTPBackend) SubmissionChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.submissionsMu.Lock()
	defer b.submissionsMu.Unlock()
	tr := b.getSubTrackerLocked(accountID)
	return tr.Changes(sinceState, maxChanges)
}

func (b *IMAPSMTPBackend) UpdateSubmission(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapmail.EmailSubmission, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.submissionsMu.Lock()

	m := b.getSubMapLocked(accountID)
	sub, ok := m[id]
	if !ok {
		b.submissionsMu.Unlock()
		return nil, nil
	}

	for k, v := range patch {
		switch k {
		case "undoStatus":
			s, _ := v.(string)
			if s != "canceled" {
				b.submissionsMu.Unlock()
				return nil, fmt.Errorf("invalidProperties: undoStatus can only be updated to \"canceled\"")
			}
			if sub.UndoStatus == "canceled" {
				b.submissionsMu.Unlock()
				return nil, fmt.Errorf("alreadyCanceled: submission has already been canceled")
			}
			if sub.UndoStatus != "pending" {
				b.submissionsMu.Unlock()
				return nil, fmt.Errorf("cannotCancel: submission is not pending")
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
			b.submissionsMu.Unlock()
			return nil, fmt.Errorf("invalidProperties: EmailSubmission property %q cannot be updated", k)
		}
	}

	tr := b.getSubTrackerLocked(accountID)
	tr.Record(id, "update")
	b.submissionsMu.Unlock()

	b.publishSubmissionStateChange(ctx)
	return sub, nil
}

func (b *IMAPSMTPBackend) DeleteSubmission(ctx context.Context, id jmapcore.Id) (bool, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.submissionsMu.Lock()

	m := b.getSubMapLocked(accountID)
	if _, ok := m[id]; !ok {
		b.submissionsMu.Unlock()
		return false, nil
	}
	delete(m, id)
	tr := b.getSubTrackerLocked(accountID)
	tr.Record(id, "destroy")
	b.submissionsMu.Unlock()

	b.publishSubmissionStateChange(ctx)
	return true, nil
}

func (b *IMAPSMTPBackend) GetSubmissions(ctx context.Context, ids []jmapcore.Id) ([]*jmapmail.EmailSubmission, []jmapcore.Id, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.submissionsMu.RLock()
	defer b.submissionsMu.RUnlock()

	m := b.getSubMapLocked(accountID)
	var list []*jmapmail.EmailSubmission
	var notFound []jmapcore.Id

	for _, id := range ids {
		if sub, ok := m[id]; ok {
			list = append(list, sub)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *IMAPSMTPBackend) GetAllSubmissions(ctx context.Context) ([]*jmapmail.EmailSubmission, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.submissionsMu.RLock()
	defer b.submissionsMu.RUnlock()

	m := b.getSubMapLocked(accountID)
	list := make([]*jmapmail.EmailSubmission, 0, len(m))
	for _, sub := range m {
		list = append(list, sub)
	}
	return list, nil
}

func (b *IMAPSMTPBackend) QuerySubmissions(ctx context.Context, filter map[string]any, comparators []jmapcore.Comparator, position int, limit *uint64) ([]jmapcore.Id, int, error) {
	subs, err := b.GetAllSubmissions(ctx)
	if err != nil {
		return nil, 0, err
	}

	var matched []*jmapmail.EmailSubmission
	for _, sub := range subs {
		if matchSubmissionFilter(sub, filter) {
			matched = append(matched, sub)
		}
	}

	sortSubmissions(matched, comparators)

	total := len(matched)
	position = jmapcore.NormalizePosition(position, total)
	if position > total {
		return []jmapcore.Id{}, total, nil
	}

	end := total
	if limit != nil {
		l := int(*limit)
		if position+l < end {
			end = position + l
		}
	}

	ids := make([]jmapcore.Id, 0, end-position)
	for _, sub := range matched[position:end] {
		ids = append(ids, sub.ID)
	}
	return ids, total, nil
}

func matchSubmissionFilter(sub *jmapmail.EmailSubmission, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}

	if match, isOp := jmapcore.EvalFilterOperator(filter, func(cond map[string]any) bool {
		return matchSubmissionFilter(sub, cond)
	}); isOp {
		return match
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

func sortSubmissions(subs []*jmapmail.EmailSubmission, comparators []jmapcore.Comparator) {
	if len(comparators) == 0 {
		comparators = []jmapcore.Comparator{
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

func submissionIDFilter(filter map[string]any, key string) map[jmapcore.Id]bool {
	set := make(map[jmapcore.Id]bool)
	if raw, ok := filter[key].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				set[jmapcore.Id(s)] = true
			}
		}
	}
	return set
}
