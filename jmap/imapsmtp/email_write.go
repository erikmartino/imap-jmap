package imapsmtp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"imap-jmap/jmap"
)

// MapKeywordsToIMAPFlags converts JMAP keywords map to a slice of IMAP flags.
func MapKeywordsToIMAPFlags(keywords map[string]bool) []imap.Flag {
	var flags []imap.Flag
	for kw, val := range keywords {
		if !val {
			continue
		}
		switch kw {
		case "$seen":
			flags = append(flags, imap.FlagSeen)
		case "$flagged":
			flags = append(flags, imap.FlagFlagged)
		case "$draft":
			flags = append(flags, imap.FlagDraft)
		case "$answered":
			flags = append(flags, imap.FlagAnswered)
		default:
			flags = append(flags, imap.Flag(kw))
		}
	}
	return flags
}

// CreateEmail creates or imports an email into an IMAP mailbox via APPEND.
func (b *IMAPSMTPBackend) CreateEmail(ctx context.Context, em *jmap.Email) (*jmap.Email, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	destMbID := jmap.Id("")
	for mbID := range em.MailboxIDs {
		destMbID = mbID
		break
	}

	folderName := "INBOX"
	if destMbID != "" {
		if name, err := NameForMailboxID(destMbID); err == nil {
			folderName = name
		}
	} else {
		destMbID = MailboxIDForName("INBOX")
		em.MailboxIDs = map[jmap.Id]bool{destMbID: true}
	}

	rawBytes := jmap.FormatEmailRFC822(em)
	flags := MapKeywordsToIMAPFlags(em.Keywords)

	appendCmd := client.Append(folderName, int64(len(rawBytes)), &imap.AppendOptions{
		Flags: flags,
		Time:  time.Now(),
	})
	if _, err := appendCmd.Write(rawBytes); err != nil {
		_ = appendCmd.Close()
		return nil, fmt.Errorf("failed to write append bytes: %w", err)
	}
	if err := appendCmd.Close(); err != nil {
		return nil, fmt.Errorf("failed to close append command: %w", err)
	}

	appendData, err := appendCmd.Wait()
	if err != nil {
		return nil, fmt.Errorf("failed to append message to IMAP %s: %w", folderName, err)
	}

	var assignedUID uint32 = 1
	if appendData != nil && appendData.UID != 0 {
		assignedUID = uint32(appendData.UID)
	} else {
		// Fetch UIDNext from folder
		statusCmd := client.Status(folderName, &imap.StatusOptions{UIDNext: true})
		if status, err := statusCmd.Wait(); err == nil && status.UIDNext > 1 {
			assignedUID = uint32(status.UIDNext - 1)
		}
	}

	emailID := EmailIDFor(destMbID, assignedUID)
	em.ID = emailID
	em.BlobID = jmap.Id(emailID)
	em.Size = uint64(len(rawBytes))
	em.ReceivedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return em, nil
}

// UpdateEmail modifies keywords or moves an email to another IMAP mailbox.
func (b *IMAPSMTPBackend) UpdateEmail(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Email, error) {
	mbID, uid, err := ParseEmailID(id)
	if err != nil {
		return nil, err
	}

	folderName, err := NameForMailboxID(mbID)
	if err != nil {
		return nil, err
	}

	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	if _, err := client.Select(folderName, nil).Wait(); err != nil {
		return nil, fmt.Errorf("failed to select folder %s: %w", folderName, err)
	}

	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))

	// Update Keywords / Flags (supporting both full keywords object and JSON-pointer patches like keywords/$label:red)
	var flagsToAdd []imap.Flag
	var flagsToDel []imap.Flag
	var flagsToSet []imap.Flag
	hasFlagsSet := false

	for path, val := range patch {
		if path == "keywords" {
			if kwMap, ok := val.(map[string]any); ok {
				keywords := make(map[string]bool)
				for k, v := range kwMap {
					if bVal, ok := v.(bool); ok && bVal {
						keywords[strings.ToLower(k)] = true
					}
				}
				flagsToSet = MapKeywordsToIMAPFlags(keywords)
				hasFlagsSet = true
			}
		} else if strings.HasPrefix(path, "keywords/") {
			kw := strings.ToLower(strings.TrimPrefix(path, "keywords/"))
			flag := mapJMAPKeywordToIMAPFlag(kw)
			if val == nil {
				flagsToDel = append(flagsToDel, flag)
			} else if bVal, ok := val.(bool); ok {
				if bVal {
					flagsToAdd = append(flagsToAdd, flag)
				} else {
					flagsToDel = append(flagsToDel, flag)
				}
			}
		}
	}

	if hasFlagsSet {
		storeCmd := client.Store(uidSet, &imap.StoreFlags{
			Op:     imap.StoreFlagsSet,
			Flags:  flagsToSet,
			Silent: true,
		}, nil)
		_, _ = storeCmd.Collect()
	} else {
		if len(flagsToAdd) > 0 {
			storeCmd := client.Store(uidSet, &imap.StoreFlags{
				Op:     imap.StoreFlagsAdd,
				Flags:  flagsToAdd,
				Silent: true,
			}, nil)
			_, _ = storeCmd.Collect()
		}
		if len(flagsToDel) > 0 {
			storeCmd := client.Store(uidSet, &imap.StoreFlags{
				Op:     imap.StoreFlagsDel,
				Flags:  flagsToDel,
				Silent: true,
			}, nil)
			_, _ = storeCmd.Collect()
		}
	}

	// Move / Mailbox update (supporting both mailboxIds object and mailboxIds/... patches)
	var targetMoveMbID jmap.Id
	if mbVal, ok := patch["mailboxIds"]; ok {
		if mbMap, ok := mbVal.(map[string]any); ok {
			for k, v := range mbMap {
				if bVal, ok := v.(bool); ok && bVal {
					targetMoveMbID = jmap.Id(k)
					break
				}
			}
		}
	}
	for path, val := range patch {
		if strings.HasPrefix(path, "mailboxIds/") {
			mbKey := jmap.Id(strings.TrimPrefix(path, "mailboxIds/"))
			if bVal, ok := val.(bool); ok && bVal {
				targetMoveMbID = mbKey
				break
			}
		}
	}

	if targetMoveMbID != "" && targetMoveMbID != mbID {
		newFolderName, err := NameForMailboxID(targetMoveMbID)
		if err == nil {
			// Copy to new mailbox, flag \Deleted in old mailbox, and expunge
			if _, err := client.Copy(uidSet, newFolderName).Wait(); err == nil {
				storeCmd := client.Store(uidSet, &imap.StoreFlags{
					Op:     imap.StoreFlagsAdd,
					Flags:  []imap.Flag{imap.FlagDeleted},
					Silent: true,
				}, nil)
				_, _ = storeCmd.Collect()
				_, _ = client.Expunge().Collect()
			}
		}
	}

	// Fetch updated message
	emails, _, err := b.GetEmails(ctx, []jmap.Id{id})
	if err == nil && len(emails) > 0 {
		return emails[0], nil
	}

	return &jmap.Email{ID: id}, nil
}

// DeleteEmail removes an email from IMAP via \Deleted flag and EXPUNGE.
func (b *IMAPSMTPBackend) DeleteEmail(ctx context.Context, id jmap.Id) (bool, error) {
	mbID, uid, err := ParseEmailID(id)
	if err != nil {
		return false, err
	}

	folderName, err := NameForMailboxID(mbID)
	if err != nil {
		return false, err
	}

	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return false, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	if _, err := client.Select(folderName, nil).Wait(); err != nil {
		return false, fmt.Errorf("failed to select folder %s: %w", folderName, err)
	}

	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))

	storeCmd := client.Store(uidSet, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagDeleted},
		Silent: true,
	}, nil)
	if _, err := storeCmd.Collect(); err != nil {
		return false, fmt.Errorf("failed to flag email as deleted: %w", err)
	}

	if _, err := client.Expunge().Collect(); err != nil {
		return false, fmt.Errorf("failed to expunge deleted email: %w", err)
	}

	return true, nil
}
