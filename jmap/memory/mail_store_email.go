package memory

import (
	"context"
	"fmt"
	"imap-jmap/jmap"
	"sort"
	"strings"
	"time"
)

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
		myMIDs := make(map[string]bool)
		for _, mid := range em.MessageID {
			s := strings.Trim(mid, "<> \t")
			if s != "" {
				myMIDs[s] = true
			}
		}
		for _, h := range em.Headers {
			if strings.EqualFold(h.Name, "Message-ID") {
				for _, part := range strings.Fields(h.Value) {
					s := strings.Trim(part, "<> \t")
					if s != "" {
						myMIDs[s] = true
					}
				}
			}
		}

		myRefs := make(map[string]bool)
		for _, ref := range em.InReplyTo {
			s := strings.Trim(ref, "<> \t")
			if s != "" {
				myRefs[s] = true
			}
		}
		for _, ref := range em.References {
			s := strings.Trim(ref, "<> \t")
			if s != "" {
				myRefs[s] = true
			}
		}
		for _, h := range em.Headers {
			if strings.EqualFold(h.Name, "In-Reply-To") || strings.EqualFold(h.Name, "References") {
				for _, part := range strings.Fields(h.Value) {
					s := strings.Trim(part, "<> \t")
					if s != "" {
						myRefs[s] = true
					}
				}
			}
		}

		for _, other := range us.emails {
			otherMIDs := make(map[string]bool)
			for _, mid := range other.MessageID {
				s := strings.Trim(mid, "<> \t")
				if s != "" {
					otherMIDs[s] = true
				}
			}
			for _, h := range other.Headers {
				if strings.EqualFold(h.Name, "Message-ID") {
					for _, part := range strings.Fields(h.Value) {
						s := strings.Trim(part, "<> \t")
						if s != "" {
							otherMIDs[s] = true
						}
					}
				}
			}

			otherRefs := make(map[string]bool)
			for _, ref := range other.InReplyTo {
				s := strings.Trim(ref, "<> \t")
				if s != "" {
					otherRefs[s] = true
				}
			}
			for _, ref := range other.References {
				s := strings.Trim(ref, "<> \t")
				if s != "" {
					otherRefs[s] = true
				}
			}
			for _, h := range other.Headers {
				if strings.EqualFold(h.Name, "In-Reply-To") || strings.EqualFold(h.Name, "References") {
					for _, part := range strings.Fields(h.Value) {
						s := strings.Trim(part, "<> \t")
						if s != "" {
							otherRefs[s] = true
						}
					}
				}
			}

			matched := false
			for omid := range otherMIDs {
				if myRefs[omid] {
					matched = true
					break
				}
			}
			if !matched {
				for mymid := range myMIDs {
					if otherRefs[mymid] {
						matched = true
						break
					}
				}
			}
			if !matched {
				for myref := range myRefs {
					if otherRefs[myref] {
						matched = true
						break
					}
				}
			}

			if matched {
				em.ThreadID = other.ThreadID
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
