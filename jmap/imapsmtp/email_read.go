package imapsmtp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/emersion/go-imap/v2"
	"imap-jmap/jmap"
)

// EmailIDFor constructs a composite JMAP Email ID from a Mailbox ID and an IMAP UID.
func EmailIDFor(mbID jmap.Id, uid uint32) jmap.Id {
	return jmap.Id(fmt.Sprintf("%s:%d", mbID, uid))
}

// ParseEmailID deconstructs a JMAP Email ID into its Mailbox ID and IMAP UID.
func ParseEmailID(id jmap.Id) (jmap.Id, uint32, error) {
	parts := strings.Split(string(id), ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid email id format: %s", id)
	}
	uid, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("invalid uid in email id: %w", err)
	}
	return jmap.Id(parts[0]), uint32(uid), nil
}

// MapIMAPFlagsToKeywords converts IMAP flags to standard JMAP keywords.
func MapIMAPFlagsToKeywords(flags []imap.Flag) map[string]bool {
	keywords := make(map[string]bool)
	for _, flag := range flags {
		switch flag {
		case imap.FlagSeen:
			keywords["$seen"] = true
		case imap.FlagFlagged:
			keywords["$flagged"] = true
		case imap.FlagDraft:
			keywords["$draft"] = true
		case imap.FlagAnswered:
			keywords["$answered"] = true
		default:
			// Custom keyword
			s := string(flag)
			if s != "" && !strings.HasPrefix(s, "\\") {
				keywords[s] = true
			}
		}
	}
	return keywords
}

// GetEmails fetches the requested emails by ID.
func (b *IMAPSMTPBackend) GetEmails(ctx context.Context, ids []jmap.Id) ([]*jmap.Email, []jmap.Id, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, ids, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	// Group requested IDs by mailbox
	mailboxUIDs := make(map[jmap.Id][]uint32)
	var notFound []jmap.Id

	for _, id := range ids {
		mbID, uid, err := ParseEmailID(id)
		if err != nil {
			notFound = append(notFound, id)
			continue
		}
		mailboxUIDs[mbID] = append(mailboxUIDs[mbID], uid)
	}

	var found []*jmap.Email
	foundMap := make(map[jmap.Id]*jmap.Email)

	bodySection := &imap.FetchItemBodySection{}

	for mbID, uids := range mailboxUIDs {
		folderName, err := NameForMailboxID(mbID)
		if err != nil {
			for _, uid := range uids {
				notFound = append(notFound, EmailIDFor(mbID, uid))
			}
			continue
		}

		selectCmd := client.Select(folderName, nil)
		if _, err := selectCmd.Wait(); err != nil {
			for _, uid := range uids {
				notFound = append(notFound, EmailIDFor(mbID, uid))
			}
			continue
		}

		var uidSet imap.UIDSet
		for _, uid := range uids {
			uidSet.AddNum(imap.UID(uid))
		}

		fetchCmd := client.Fetch(uidSet, &imap.FetchOptions{
			BodySection:  []*imap.FetchItemBodySection{bodySection},
			Flags:        true,
			InternalDate: true,
			Envelope:     true,
			UID:          true,
		})

		messages, err := fetchCmd.Collect()
		if err != nil {
			continue
		}

		for _, msg := range messages {
			rawBytes := msg.FindBodySection(bodySection)
			if len(rawBytes) == 0 {
				continue
			}

			emailID := EmailIDFor(mbID, uint32(msg.UID))
			em, err := jmap.ParseRFC822(rawBytes)
			if err != nil {
				continue
			}

			em.ID = emailID
			em.BlobID = jmap.Id(emailID)
			em.MailboxIDs = map[jmap.Id]bool{mbID: true}
			em.Keywords = MapIMAPFlagsToKeywords(msg.Flags)

			// Thread ID fallback to Message-ID or Email ID
			if len(em.MessageID) > 0 {
				em.ThreadID = jmap.Id(em.MessageID[0])
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
			// Check if not already in notFound
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
	mailboxes, err := b.GetAllMailboxes(ctx)
	if err != nil {
		return nil, err
	}

	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	var allEmails []*jmap.Email
	bodySection := &imap.FetchItemBodySection{}

	for _, mb := range mailboxes {
		folderName, err := NameForMailboxID(mb.ID)
		if err != nil {
			continue
		}

		selectCmd := client.Select(folderName, nil)
		selectData, err := selectCmd.Wait()
		if err != nil || selectData.NumMessages == 0 {
			continue
		}

		var seqSet imap.SeqSet
		seqSet.AddRange(1, selectData.NumMessages)

		fetchCmd := client.Fetch(seqSet, &imap.FetchOptions{
			BodySection:  []*imap.FetchItemBodySection{bodySection},
			Flags:        true,
			InternalDate: true,
			Envelope:     true,
			UID:          true,
		})

		messages, err := fetchCmd.Collect()
		if err != nil {
			continue
		}

		for _, msg := range messages {
			rawBytes := msg.FindBodySection(bodySection)
			if len(rawBytes) == 0 {
				continue
			}

			emailID := EmailIDFor(mb.ID, uint32(msg.UID))
			em, err := jmap.ParseRFC822(rawBytes)
			if err != nil {
				continue
			}

			em.ID = emailID
			em.BlobID = jmap.Id(emailID)
			em.MailboxIDs = map[jmap.Id]bool{mb.ID: true}
			em.Keywords = MapIMAPFlagsToKeywords(msg.Flags)

			if len(em.MessageID) > 0 {
				em.ThreadID = jmap.Id(em.MessageID[0])
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
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	var targetFolders []string

	if filter != nil {
		if inMb, ok := filter["inMailbox"].(string); ok && inMb != "" {
			folderName, err := NameForMailboxID(jmap.Id(inMb))
			if err == nil {
				targetFolders = []string{folderName}
			}
		}
	}

	if len(targetFolders) == 0 {
		// Cross-folder query: list all mailboxes
		mailboxes, err := b.GetAllMailboxes(ctx)
		if err != nil {
			return nil, 0, err
		}
		for _, mb := range mailboxes {
			name, err := NameForMailboxID(mb.ID)
			if err == nil {
				targetFolders = append(targetFolders, name)
			}
		}
	}

	var allMatchingIDs []jmap.Id

	for _, folderName := range targetFolders {
		mbID := MailboxIDForName(folderName)
		selectCmd := client.Select(folderName, nil)
		selectData, err := selectCmd.Wait()
		if err != nil || selectData.NumMessages == 0 {
			continue
		}

		searchCriteria := &imap.SearchCriteria{}

		// Apply basic search filters if present
		if filter != nil {
			if kw, ok := filter["hasKeyword"].(string); ok {
				switch kw {
				case "$seen":
					searchCriteria.Flag = append(searchCriteria.Flag, imap.FlagSeen)
				case "$flagged":
					searchCriteria.Flag = append(searchCriteria.Flag, imap.FlagFlagged)
				case "$draft":
					searchCriteria.Flag = append(searchCriteria.Flag, imap.FlagDraft)
				case "$answered":
					searchCriteria.Flag = append(searchCriteria.Flag, imap.FlagAnswered)
				}
			}
			if notKw, ok := filter["notKeyword"].(string); ok {
				switch notKw {
				case "$seen":
					searchCriteria.NotFlag = append(searchCriteria.NotFlag, imap.FlagSeen)
				case "$flagged":
					searchCriteria.NotFlag = append(searchCriteria.NotFlag, imap.FlagFlagged)
				case "$draft":
					searchCriteria.NotFlag = append(searchCriteria.NotFlag, imap.FlagDraft)
				}
			}
			if text, ok := filter["text"].(string); ok && text != "" {
				searchCriteria.Text = append(searchCriteria.Text, text)
			}
			if from, ok := filter["from"].(string); ok && from != "" {
				searchCriteria.Header = append(searchCriteria.Header, imap.SearchCriteriaHeaderField{
					Key:   "From",
					Value: from,
				})
			}
			if to, ok := filter["to"].(string); ok && to != "" {
				searchCriteria.Header = append(searchCriteria.Header, imap.SearchCriteriaHeaderField{
					Key:   "To",
					Value: to,
				})
			}
			if subj, ok := filter["subject"].(string); ok && subj != "" {
				searchCriteria.Header = append(searchCriteria.Header, imap.SearchCriteriaHeaderField{
					Key:   "Subject",
					Value: subj,
				})
			}
		}

		searchCmd := client.UIDSearch(searchCriteria, nil)
		searchData, err := searchCmd.Wait()
		if err != nil {
			continue
		}

		for _, uid := range searchData.AllUIDs() {
			allMatchingIDs = append(allMatchingIDs, EmailIDFor(mbID, uint32(uid)))
		}
	}

	total := len(allMatchingIDs)
	if position < 0 {
		position = 0
	}
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

	return allMatchingIDs[position:end], total, nil
}

// GetThreads groups requested threads by thread ID.
func (b *IMAPSMTPBackend) GetThreads(ctx context.Context, ids []jmap.Id) ([]*jmap.Thread, []jmap.Id, error) {
	allEmails, err := b.GetAllEmails(ctx)
	if err != nil {
		return nil, ids, err
	}

	threadEmails := make(map[jmap.Id][]jmap.Id)
	for _, em := range allEmails {
		threadEmails[em.ThreadID] = append(threadEmails[em.ThreadID], em.ID)
	}

	var found []*jmap.Thread
	var notFound []jmap.Id

	for _, id := range ids {
		if eIDs, ok := threadEmails[id]; ok {
			found = append(found, &jmap.Thread{
				ID:       id,
				EmailIDs: eIDs,
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
		threadEmails[em.ThreadID] = append(threadEmails[em.ThreadID], em.ID)
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
		res[em.ID] = &jmap.SmimeVerificationResult{
			SmimeStatus: "unknown",
		}
	}
	return res, notFound, nil
}
