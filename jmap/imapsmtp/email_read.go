package imapsmtp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
			// Custom keyword (normalized to lowercase per RFC 8621 Section 4.2.2)
			s := strings.ToLower(string(flag))
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
			if strings.HasPrefix(em.Subject, blobStagingMarker) {
				continue
			}

			em.ID = emailID
			em.BlobID = jmap.Id(emailID)
			em.MailboxIDs = map[jmap.Id]bool{mbID: true}
			em.Keywords = MapIMAPFlagsToKeywords(msg.Flags)
			// The IMAP INTERNALDATE is the true received time; without it every
			// message reports the fetch time and lists lose their chronological order.
			if !msg.InternalDate.IsZero() {
				em.ReceivedAt = msg.InternalDate.UTC().Format(time.RFC3339Nano)
			}
			if em.SentAt == nil && em.ReceivedAt != "" {
				s := em.ReceivedAt
				em.SentAt = &s
			}

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
			if strings.HasPrefix(em.Subject, blobStagingMarker) {
				continue
			}

			em.ID = emailID
			em.BlobID = jmap.Id(emailID)
			em.MailboxIDs = map[jmap.Id]bool{mb.ID: true}
			em.Keywords = MapIMAPFlagsToKeywords(msg.Flags)
			if !msg.InternalDate.IsZero() {
				em.ReceivedAt = msg.InternalDate.UTC().Format(time.RFC3339Nano)
			}
			if em.SentAt == nil && em.ReceivedAt != "" {
				s := em.ReceivedAt
				em.SentAt = &s
			}

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
// mapJMAPKeywordToIMAPFlag converts a JMAP keyword to its canonical IMAP flag.
func mapJMAPKeywordToIMAPFlag(kw string) imap.Flag {
	lower := strings.ToLower(strings.TrimSpace(kw))
	switch lower {
	case "$seen":
		return imap.FlagSeen
	case "$flagged":
		return imap.FlagFlagged
	case "$draft":
		return imap.FlagDraft
	case "$answered":
		return imap.FlagAnswered
	default:
		return imap.Flag(lower)
	}
}

// extractTargetFolders determines which IMAP folders to search given a JMAP filter.
func extractTargetFolders(filter map[string]any, allFolders []string) []string {
	if len(filter) == 0 {
		return allFolders
	}

	// 1. Direct inMailbox constraint
	if inMb, ok := filter["inMailbox"].(string); ok && inMb != "" {
		if folderName, err := NameForMailboxID(jmap.Id(inMb)); err == nil {
			return []string{folderName}
		}
	}

	// 2. Direct inMailboxOtherThan exclusion
	if otherRaw, ok := filter["inMailboxOtherThan"].([]any); ok && len(otherRaw) > 0 {
		exclude := make(map[string]bool)
		for _, v := range otherRaw {
			if s, ok := v.(string); ok {
				if folderName, err := NameForMailboxID(jmap.Id(s)); err == nil {
					exclude[folderName] = true
				}
			}
		}
		var filtered []string
		for _, f := range allFolders {
			if !exclude[f] {
				filtered = append(filtered, f)
			}
		}
		return filtered
	}

	// 3. Recursive AND conditions
	if opRaw, ok := filter["operator"].(string); ok && strings.EqualFold(opRaw, "AND") {
		if condsRaw, ok := filter["conditions"].([]any); ok {
			curr := allFolders
			for _, cond := range condsRaw {
				if condMap, ok := cond.(map[string]any); ok {
					curr = extractTargetFolders(condMap, curr)
				}
			}
			return curr
		}
	}

	return allFolders
}

// buildNestedOr chains a slice of SearchCriteria into a nested binary OR tree per RFC 3501 Section 6.4.4.
func buildNestedOr(crits []imap.SearchCriteria) *imap.SearchCriteria {
	if len(crits) == 0 {
		return &imap.SearchCriteria{}
	}
	if len(crits) == 1 {
		return &crits[0]
	}
	if len(crits) == 2 {
		return &imap.SearchCriteria{
			Or: [][2]imap.SearchCriteria{{crits[0], crits[1]}},
		}
	}
	sub := buildNestedOr(crits[1:])
	return &imap.SearchCriteria{
		Or: [][2]imap.SearchCriteria{{crits[0], *sub}},
	}
}

// buildIMAPSearchCriteria converts a JMAP FilterCondition or FilterOperator into IMAP SearchCriteria.
func buildIMAPSearchCriteria(filter map[string]any) *imap.SearchCriteria {
	if len(filter) == 0 {
		return &imap.SearchCriteria{}
	}

	crit := &imap.SearchCriteria{}

	if opRaw, ok := filter["operator"].(string); ok {
		condsRaw, ok := filter["conditions"].([]any)
		if !ok || len(condsRaw) == 0 {
			return crit
		}

		op := strings.ToUpper(opRaw)
		switch op {
		case "AND":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					subCrit := buildIMAPSearchCriteria(condMap)
					mergeSearchCriteria(crit, subCrit)
				}
			}
			return crit
		case "OR":
			var subCrits []imap.SearchCriteria
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					subCrits = append(subCrits, *buildIMAPSearchCriteria(condMap))
				}
			}
			return buildNestedOr(subCrits)
		case "NOT":
			for _, condRaw := range condsRaw {
				if condMap, ok := condRaw.(map[string]any); ok {
					subCrit := buildIMAPSearchCriteria(condMap)
					crit.Not = append(crit.Not, *subCrit)
				}
			}
			return crit
		}
	}

	if kw, ok := filter["hasKeyword"].(string); ok && kw != "" {
		crit.Flag = append(crit.Flag, mapJMAPKeywordToIMAPFlag(kw))
	}
	if notKw, ok := filter["notKeyword"].(string); ok && notKw != "" {
		crit.NotFlag = append(crit.NotFlag, mapJMAPKeywordToIMAPFlag(notKw))
	}
	if text, ok := filter["text"].(string); ok && text != "" {
		for _, f := range strings.Fields(text) {
			clean := jmap.CleanQueryTerm(f)
			if clean != "" {
				crit.Text = append(crit.Text, clean)
			}
		}
	}
	if from, ok := filter["from"].(string); ok && from != "" {
		clean := jmap.CleanQueryTerm(from)
		if clean != "" {
			crit.Header = append(crit.Header, imap.SearchCriteriaHeaderField{Key: "From", Value: clean})
		}
	}
	if to, ok := filter["to"].(string); ok && to != "" {
		clean := jmap.CleanQueryTerm(to)
		if clean != "" {
			crit.Header = append(crit.Header, imap.SearchCriteriaHeaderField{Key: "To", Value: clean})
		}
	}
	if cc, ok := filter["cc"].(string); ok && cc != "" {
		clean := jmap.CleanQueryTerm(cc)
		if clean != "" {
			crit.Header = append(crit.Header, imap.SearchCriteriaHeaderField{Key: "Cc", Value: clean})
		}
	}
	if bcc, ok := filter["bcc"].(string); ok && bcc != "" {
		clean := jmap.CleanQueryTerm(bcc)
		if clean != "" {
			crit.Header = append(crit.Header, imap.SearchCriteriaHeaderField{Key: "Bcc", Value: clean})
		}
	}
	if subj, ok := filter["subject"].(string); ok && subj != "" {
		clean := jmap.CleanQueryTerm(subj)
		if clean != "" {
			crit.Header = append(crit.Header, imap.SearchCriteriaHeaderField{Key: "Subject", Value: clean})
		}
	}
	if body, ok := filter["body"].(string); ok && body != "" {
		clean := jmap.CleanQueryTerm(body)
		if clean != "" {
			crit.Body = append(crit.Body, clean)
		}
	}
	if hdrRaw, ok := filter["header"].([]any); ok && len(hdrRaw) > 0 {
		hdrName, _ := hdrRaw[0].(string)
		hdrVal := ""
		if len(hdrRaw) > 1 {
			hdrVal, _ = hdrRaw[1].(string)
		}
		if hdrName != "" {
			crit.Header = append(crit.Header, imap.SearchCriteriaHeaderField{
				Key:   hdrName,
				Value: jmap.CleanQueryTerm(hdrVal),
			})
		}
	}
	if minSize, ok := filter["minSize"].(float64); ok {
		crit.Larger = int64(minSize)
	}
	if maxSize, ok := filter["maxSize"].(float64); ok {
		crit.Smaller = int64(maxSize)
	}
	if before, ok := filter["before"].(string); ok && before != "" {
		if t, err := time.Parse(time.RFC3339, before); err == nil {
			crit.Before = t
		}
	}
	if after, ok := filter["after"].(string); ok && after != "" {
		if t, err := time.Parse(time.RFC3339, after); err == nil {
			crit.Since = t
		}
	}
	return crit
}

func mergeSearchCriteria(dest, src *imap.SearchCriteria) {
	if src == nil {
		return
	}
	dest.Flag = append(dest.Flag, src.Flag...)
	dest.NotFlag = append(dest.NotFlag, src.NotFlag...)
	dest.Header = append(dest.Header, src.Header...)
	dest.Body = append(dest.Body, src.Body...)
	dest.Text = append(dest.Text, src.Text...)
	dest.Not = append(dest.Not, src.Not...)
	dest.Or = append(dest.Or, src.Or...)
	if !src.Since.IsZero() {
		dest.Since = src.Since
	}
	if !src.Before.IsZero() {
		dest.Before = src.Before
	}
	if src.Larger > 0 {
		dest.Larger = src.Larger
	}
	if src.Smaller > 0 {
		dest.Smaller = src.Smaller
	}
}

// QueryEmails searches emails based on JMAP filter criteria across mailboxes.
func (b *IMAPSMTPBackend) QueryEmails(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	// List all mailboxes to establish available search scope
	var allFolderNames []string
	mailboxes, err := b.GetAllMailboxes(ctx)
	if err != nil {
		return nil, 0, err
	}
	for _, mb := range mailboxes {
		name, err := NameForMailboxID(mb.ID)
		if err == nil {
			allFolderNames = append(allFolderNames, name)
		}
	}

	targetFolders := extractTargetFolders(filter, allFolderNames)
	searchCriteria := buildIMAPSearchCriteria(filter)

	var allMatching []struct {
		mbID jmap.Id
		uid  uint32
	}

	for _, folderName := range targetFolders {
		mbID := MailboxIDForName(folderName)
		selectCmd := client.Select(folderName, nil)
		selectData, err := selectCmd.Wait()
		if err != nil || selectData.NumMessages == 0 {
			continue
		}

		searchCmd := client.UIDSearch(searchCriteria, nil)
		searchData, err := searchCmd.Wait()
		if err != nil {
			continue
		}

		uids := searchData.AllUIDs()
		if len(uids) > 0 {
			var uidSet imap.UIDSet
			for _, u := range uids {
				uidSet.AddNum(u)
			}
			fetchCmd := client.Fetch(uidSet, &imap.FetchOptions{Envelope: true, UID: true})
			msgs, err := fetchCmd.Collect()
			if err == nil {
				for _, msg := range msgs {
					if msg.Envelope != nil && strings.HasPrefix(msg.Envelope.Subject, blobStagingMarker) {
						continue
					}
					allMatching = append(allMatching, struct {
						mbID jmap.Id
						uid  uint32
					}{mbID: mbID, uid: uint32(msg.UID)})
				}
			} else {
				for _, uid := range uids {
					allMatching = append(allMatching, struct {
						mbID jmap.Id
						uid  uint32
					}{mbID: mbID, uid: uint32(uid)})
				}
			}
		}
	}

	allMatchingIDs := make([]jmap.Id, 0, len(allMatching))
	for _, m := range allMatching {
		allMatchingIDs = append(allMatchingIDs, EmailIDFor(m.mbID, m.uid))
	}

	// Honor a sort comparator on receivedAt (RFC 8621 Section 4.4.2). The
	// IMAP SEARCH result is in UID order, which does not reflect the client's
	// expected newest-first listing, so reorder by INTERNALDATE.
	for _, comp := range comparators {
		if comp.Property == "receivedAt" || comp.Property == "sentAt" {
			recv := make(map[jmap.Id]time.Time)
			for _, m := range allMatching {
				var uidSet imap.UIDSet
				uidSet.AddNum(imap.UID(m.uid))
				folderName, err := NameForMailboxID(m.mbID)
				if err != nil {
					continue
				}
				if _, err := client.Select(folderName, nil).Wait(); err != nil {
					continue
				}
				fetchCmd := client.Fetch(uidSet, &imap.FetchOptions{InternalDate: true})
				msgs, err := fetchCmd.Collect()
				if err != nil {
					continue
				}
				for _, msg := range msgs {
					if !msg.InternalDate.IsZero() {
						recv[EmailIDFor(m.mbID, uint32(msg.UID))] = msg.InternalDate
					}
				}
			}
			sort.SliceStable(allMatching, func(i, j int) bool {
				ti, oki := recv[EmailIDFor(allMatching[i].mbID, allMatching[i].uid)]
				tj, okj := recv[EmailIDFor(allMatching[j].mbID, allMatching[j].uid)]
				// IMAP INTERNALDATE has only second precision, so ties are broken
				// by UID (monotonic with append order): newest append first.
				uidCmp := func() bool {
					if comp.IsAscending {
						return allMatching[i].uid < allMatching[j].uid
					}
					return allMatching[i].uid > allMatching[j].uid
				}
				if !oki && !okj {
					return uidCmp()
				}
				if !oki {
					return !comp.IsAscending
				}
				if !okj {
					return comp.IsAscending
				}
				if ti.Equal(tj) {
					return uidCmp()
				}
				if comp.IsAscending {
					return ti.Before(tj)
				}
				return ti.After(tj)
			})
			break
		}
	}

	allMatchingIDs = make([]jmap.Id, 0, len(allMatching))
	for _, m := range allMatching {
		allMatchingIDs = append(allMatchingIDs, EmailIDFor(m.mbID, m.uid))
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
