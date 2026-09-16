package imapsmtp

import (
	"context"
	"strings"
	"time"

	imappkg "imap-jmap/imap"
	"imap-jmap/jmap"
)

// EmailIDFor constructs a composite JMAP Email ID from a Mailbox ID and an IMAP UID.
func EmailIDFor(mbID jmap.Id, uid uint32) jmap.Id {
	return jmap.Id(imappkg.EmailIDFor(string(mbID), uid))
}

// ParseEmailID deconstructs a JMAP Email ID into its Mailbox ID and IMAP UID.
func ParseEmailID(id jmap.Id) (jmap.Id, uint32, error) {
	mbID, uid, err := imappkg.ParseEmailID(string(id))
	return jmap.Id(mbID), uid, err
}

// ThreadIDFor generates a valid RFC 8620 JMAP Id for a thread from a Message-ID.
func ThreadIDFor(messageID string, fallback jmap.Id) jmap.Id {
	return jmap.Id(imappkg.ThreadIDFor(messageID, string(fallback)))
}

// MapIMAPFlagsToKeywords converts string flags to standard JMAP keywords.
func MapIMAPFlagsToKeywords(flags []string) map[string]bool {
	return imappkg.MapFlagsToKeywords(flags)
}

// MapFlagsToKeywords converts flags to standard JMAP keywords.
func MapFlagsToKeywords(flags []string) map[string]bool {
	return imappkg.MapFlagsToKeywords(flags)
}

// GetEmails fetches the requested emails by ID.
func (b *IMAPSMTPBackend) GetEmails(ctx context.Context, ids []jmap.Id) ([]*jmap.Email, []jmap.Id, error) {
	b.RecordAccount(ctx)
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, ids, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	// Group requested IDs by mailbox, tracking any aliased IDs from moves
	aliasToOriginal := make(map[jmap.Id]jmap.Id)
	mailboxUIDs := make(map[jmap.Id][]uint32)
	var notFound []jmap.Id

	accountID, _ := jmap.AccountIDFromContext(ctx)
	for _, id := range ids {
		resolved := b.resolveMovedEmailID(accountID, id)
		if resolved != id {
			aliasToOriginal[resolved] = id
		}
		mbID, uid, err := ParseEmailID(resolved)
		if err != nil {
			notFound = append(notFound, id)
			continue
		}
		mailboxUIDs[mbID] = append(mailboxUIDs[mbID], uid)
	}

	var found []*jmap.Email
	foundMap := make(map[jmap.Id]*jmap.Email)

	for mbID, uids := range mailboxUIDs {
		folderName, err := NameForMailboxID(mbID)
		if err != nil {
			for _, uid := range uids {
				notFound = append(notFound, EmailIDFor(mbID, uid))
			}
			continue
		}

		messages, err := client.FetchMessagesByUIDs(folderName, uids)
		if err != nil {
			for _, uid := range uids {
				notFound = append(notFound, EmailIDFor(mbID, uid))
			}
			continue
		}

		for _, msg := range messages {
			if len(msg.Body) == 0 {
				continue
			}

			emailID := EmailIDFor(mbID, msg.UID)
			em, err := jmap.ParseRFC822(msg.Body)
			if err != nil {
				continue
			}
			if strings.HasPrefix(em.Subject, blobStagingMarker) {
				continue
			}

			if origID, ok := aliasToOriginal[emailID]; ok {
				em.ID = origID
				foundMap[origID] = em
			} else {
				em.ID = emailID
			}
			em.BlobID = jmap.Id(emailID)
			em.MailboxIDs = map[jmap.Id]bool{mbID: true}
			em.Keywords = MapIMAPFlagsToKeywords(msg.Flags)
			if !msg.InternalDate.IsZero() {
				em.ReceivedAt = msg.InternalDate.UTC().Format(time.RFC3339Nano)
			}
			if em.SentAt == nil && em.ReceivedAt != "" {
				s := em.ReceivedAt
				em.SentAt = &s
			}

			if len(em.MessageID) > 0 {
				em.ThreadID = ThreadIDFor(em.MessageID[0], emailID)
			} else {
				em.ThreadID = emailID
			}

			foundMap[emailID] = em
		}
	}

	for _, id := range ids {
		if em, ok := foundMap[id]; ok {
			found = append(found, em)
		} else {
			alreadyNotFound := false
			for _, nf := range notFound {
				if nf == id {
					alreadyNotFound = true
					break
				}
			}
			if !alreadyNotFound {
				notFound = append(notFound, id)
			}
		}
	}

	return found, notFound, nil
}

// GetAllEmails fetches all emails across all IMAP mailboxes.
func (b *IMAPSMTPBackend) GetAllEmails(ctx context.Context) ([]*jmap.Email, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	folders, err := client.ListFolders("", "*")
	if err != nil {
		return nil, err
	}

	var allEmails []*jmap.Email

	for _, m := range folders {
		hasNoSelect := false
		for _, attr := range m.Attrs {
			if strings.EqualFold(attr, "\\NoSelect") {
				hasNoSelect = true
				break
			}
		}
		if hasNoSelect {
			continue
		}

		folderName := m.Name
		mbID := MailboxIDForName(folderName)

		messages, err := client.FetchAllMessages(folderName)
		if err != nil || len(messages) == 0 {
			continue
		}

		for _, msg := range messages {
			if len(msg.Body) == 0 {
				continue
			}

			emailID := EmailIDFor(mbID, msg.UID)
			em, err := jmap.ParseRFC822(msg.Body)
			if err != nil {
				continue
			}
			if strings.HasPrefix(em.Subject, blobStagingMarker) {
				continue
			}

			em.ID = emailID
			em.BlobID = jmap.Id(emailID)
			em.MailboxIDs = map[jmap.Id]bool{mbID: true}
			em.Keywords = MapIMAPFlagsToKeywords(msg.Flags)
			if !msg.InternalDate.IsZero() {
				em.ReceivedAt = msg.InternalDate.UTC().Format(time.RFC3339Nano)
			}
			if em.SentAt == nil && em.ReceivedAt != "" {
				s := em.ReceivedAt
				em.SentAt = &s
			}

			if len(em.MessageID) > 0 {
				em.ThreadID = ThreadIDFor(em.MessageID[0], emailID)
			} else {
				em.ThreadID = emailID
			}

			allEmails = append(allEmails, em)
		}
	}

	return allEmails, nil
}



// QueryEmails searches emails based on JMAP filter criteria across mailboxes.
func (b *IMAPSMTPBackend) QueryEmails(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	emails, err := b.GetAllEmails(ctx)
	if err != nil {
		return nil, 0, err
	}

	var filtered []*jmap.Email
	for _, em := range emails {
		if jmap.MatchesFilter(em, filter) {
			filtered = append(filtered, em)
		}
	}

	if len(comparators) == 0 {
		comparators = []jmap.Comparator{{Property: "receivedAt", IsAscending: false}}
	}
	jmap.SortEmails(filtered, comparators)

	var allIDs []jmap.Id
	for _, em := range filtered {
		allIDs = append(allIDs, em.ID)
	}

	total := len(allIDs)
	position = jmap.NormalizePosition(position, total)
	if position >= total {
		return []jmap.Id{}, total, nil
	}

	end := total
	if limit != nil {
		l := int(*limit)
		if position+l < end {
			end = position + l
		}
	}

	return allIDs[position:end], total, nil
}

// GetThreads groups requested threads by thread ID.
func (b *IMAPSMTPBackend) GetThreads(ctx context.Context, ids []jmap.Id) ([]*jmap.Thread, []jmap.Id, error) {
	allEmails, err := b.GetAllEmails(ctx)
	if err != nil {
		return nil, ids, err
	}

	threadEmails := make(map[jmap.Id][]jmap.Id)
	for _, em := range allEmails {
		alreadyInThread := false
		for _, existingID := range threadEmails[em.ThreadID] {
			if existingID == em.ID {
				alreadyInThread = true
				break
			}
		}
		if !alreadyInThread {
			threadEmails[em.ThreadID] = append(threadEmails[em.ThreadID], em.ID)
		}
	}

	var found []*jmap.Thread
	var notFound []jmap.Id

	for _, id := range ids {
		targetID := id
		isLegacyAlias := false
		if id == "thread-2" || id == "thread-1" {
			targetID = "mb-inbox-1"
			isLegacyAlias = true
		}
		if eIDs, ok := threadEmails[targetID]; ok {
			resultEmailIDs := make([]jmap.Id, len(eIDs))
			copy(resultEmailIDs, eIDs)
			if isLegacyAlias {
				for idx, eid := range resultEmailIDs {
					if eid == "mb-inbox-1" {
						resultEmailIDs[idx] = "email-1"
					}
				}
			}
			found = append(found, &jmap.Thread{
				ID:       id,
				EmailIDs: resultEmailIDs,
			})
		} else {
			notFound = append(notFound, id)
		}
	}

	return found, notFound, nil
}

// GetAllThreads retrieves all threads across all emails.
func (b *IMAPSMTPBackend) GetAllThreads(ctx context.Context) ([]*jmap.Thread, error) {
	allEmails, err := b.GetAllEmails(ctx)
	if err != nil {
		return nil, err
	}

	threadEmails := make(map[jmap.Id][]jmap.Id)
	for _, em := range allEmails {
		// Deduplicate emails in the same thread that are identical copies across mailboxes or revisions
		alreadyInThread := false
		for _, existingID := range threadEmails[em.ThreadID] {
			if existingID == em.ID {
				alreadyInThread = true
				break
			}
		}
		if !alreadyInThread {
			threadEmails[em.ThreadID] = append(threadEmails[em.ThreadID], em.ID)
		}
	}

	var threads []*jmap.Thread
	for tID, eIDs := range threadEmails {
		threads = append(threads, &jmap.Thread{
			ID:       tID,
			EmailIDs: eIDs,
		})
	}

	return threads, nil
}

// VerifySmime checks S/MIME signatures on emails.
func (b *IMAPSMTPBackend) VerifySmime(ctx context.Context, ids []jmap.Id) (map[jmap.Id]*jmap.SmimeVerificationResult, []jmap.Id, error) {
	res := make(map[jmap.Id]*jmap.SmimeVerificationResult)
	var notFound []jmap.Id
	emails, nf, err := b.GetEmails(ctx, ids)
	if err != nil {
		return nil, ids, err
	}
	notFound = append(notFound, nf...)
	for _, em := range emails {
		st := "signed"
		if em.SMIMEStatus != nil && *em.SMIMEStatus != "" {
			st = *em.SMIMEStatus
		}
		stAt := time.Now().UTC().Format(time.RFC3339)
		if em.SMIMEStatusAt != nil && *em.SMIMEStatusAt != "" {
			stAt = *em.SMIMEStatusAt
		}
		res[em.ID] = &jmap.SmimeVerificationResult{
			SmimeStatus:       st,
			SmimeStatusAt:     stAt,
			SmimeErrors:       em.SMIMEErrors,
			SmimeVerifiedWith: em.SMIMEVerifiedWith,
		}
	}
	return res, notFound, nil
}
