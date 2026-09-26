package imapsmtp

import (
	"context"
	"fmt"
	"strings"
	"time"

	imappkg "imap-jmap/imap"
	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

// MapKeywordsToIMAPFlags converts JMAP keywords map to a slice of IMAP flag strings.
func MapKeywordsToIMAPFlags(keywords map[string]bool) []string {
	return imappkg.MapKeywordsToFlags(keywords)
}


// CreateEmail creates or imports an email into an IMAP mailbox via APPEND.
func (b *IMAPSMTPBackend) CreateEmail(ctx context.Context, em *jmapmail.Email) (*jmapmail.Email, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}

	destMbID := jmapcore.Id("")
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
		em.MailboxIDs = map[jmapcore.Id]bool{destMbID: true}
	}

	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	var rawBytes []byte
	originalBlobID := em.BlobID
	if em.BlobID != "" {
		if blob, ok, err := b.GetBlob(ctx, accountID, string(em.BlobID)); err == nil && ok && blob != nil && len(blob.Data) > 0 {
			rawBytes = blob.Data
		}
	}
	if len(rawBytes) == 0 {
		rawBytes = jmapmail.FormatEmailRFC822(em)
	}
	flags := MapKeywordsToIMAPFlags(em.Keywords)

	msgTime := time.Now()
	if em.ReceivedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, em.ReceivedAt); err == nil {
			msgTime = t
		} else if t, err := time.Parse(time.RFC3339, em.ReceivedAt); err == nil {
			msgTime = t
		}
	}

	emailSize := uint64(len(rawBytes))
	if err := b.checkQuota(accountID, emailSize); err != nil {
		b.pool.ReleaseClient(ctx, client)
		return nil, err
	}

	uid, err := client.AppendAndGetUID(folderName, rawBytes, flags, msgTime)
	if err != nil {
		b.pool.ReleaseClient(ctx, client)
		return nil, fmt.Errorf("failed to append message to IMAP %s: %w", folderName, err)
	}
	b.pool.ReleaseClient(ctx, client)

	emailID := EmailIDFor(destMbID, uid)
	em.ID = emailID
	em.BlobID = jmapcore.Id(emailID)
	em.Size = emailSize
	if em.ReceivedAt == "" {
		em.ReceivedAt = msgTime.UTC().Format(time.RFC3339Nano)
	}
	if em.ThreadID == "" {
		rootMsgID := ""
		if len(em.References) > 0 && em.References[0] != "" {
			rootMsgID = em.References[0]
		} else if len(em.InReplyTo) > 0 && em.InReplyTo[0] != "" {
			rootMsgID = em.InReplyTo[0]
		} else if len(em.MessageID) > 0 && em.MessageID[0] != "" {
			rootMsgID = em.MessageID[0]
		}
		if rootMsgID != "" {
			em.ThreadID = ThreadIDFor(rootMsgID, emailID)
		} else {
			em.ThreadID = emailID
		}
	}

	b.recordEmailMutation(accountID, emailID, "create")
	b.recordEmailQuotaCreated(accountID, emailID, emailSize)
	if originalBlobID != "" {
		b.recordBlobRef(accountID, string(originalBlobID), emailID)
	}
	for _, part := range em.TextBody {
		if part.BlobID != nil && *part.BlobID != "" {
			b.recordBlobRef(accountID, string(*part.BlobID), emailID)
		}
	}
	for _, part := range em.HTMLBody {
		if part.BlobID != nil && *part.BlobID != "" {
			b.recordBlobRef(accountID, string(*part.BlobID), emailID)
		}
	}
	for _, att := range em.Attachments {
		if att.BlobID != nil && *att.BlobID != "" {
			b.recordBlobRef(accountID, string(*att.BlobID), emailID)
		}
	}
	b.publishStateChange(ctx)
	return em, nil
}

// UpdateEmail modifies keywords or moves an email to another IMAP mailbox.
func (b *IMAPSMTPBackend) UpdateEmail(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapmail.Email, error) {
	origID := id
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	id = b.resolveMovedEmailID(accountID, id)
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

	// Update Keywords / Flags (supporting both full keywords object and JSON-pointer patches like keywords/$label:red)
	var flagsToAdd []string
	var flagsToDel []string
	var flagsToSet []string
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
			flag := imappkg.MapJMAPKeywordToIMAPFlag(kw)
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
		_ = client.SetFlagsByUID(folderName, uid, flagsToSet)
	} else {
		if len(flagsToAdd) > 0 {
			_ = client.AddFlagsByUID(folderName, uid, flagsToAdd)
		}
		if len(flagsToDel) > 0 {
			_ = client.RemoveFlagsByUID(folderName, uid, flagsToDel)
		}
	}

	// Move / Mailbox update (supporting both mailboxIds object and mailboxIds/... patches)
	var targetMoveMbID jmapcore.Id
	if mbVal, ok := patch["mailboxIds"]; ok {
		if mbMap, ok := mbVal.(map[string]any); ok {
			for k, v := range mbMap {
				if bVal, ok := v.(bool); ok && bVal {
					targetMoveMbID = jmapcore.Id(k)
					break
				}
			}
		}
	}
	for path, val := range patch {
		if strings.HasPrefix(path, "mailboxIds/") {
			mbKey := jmapcore.Id(strings.TrimPrefix(path, "mailboxIds/"))
			if bVal, ok := val.(bool); ok && bVal {
				targetMoveMbID = mbKey
				break
			}
		}
	}

	if targetMoveMbID != "" && targetMoveMbID != mbID {
		newFolderName, err := NameForMailboxID(targetMoveMbID)
		if err == nil {
			newUID, err := client.MoveByUID(folderName, uid, newFolderName)
			b.pool.ReleaseClient(ctx, client)

			if err == nil && newUID > 0 {
				newID := EmailIDFor(targetMoveMbID, newUID)
				b.trackMovedEmail(accountID, origID, newID)
				b.trackMovedEmail(accountID, id, newID)
				b.trackMovedEmailQuota(accountID, origID, newID)
			}
			emails, _, _ := b.GetEmails(ctx, []jmapcore.Id{origID})
			accountID, _ := jmapauth.AccountIDFromContext(ctx)
			b.recordEmailMutation(accountID, origID, "update")
			b.publishStateChange(ctx)
			if len(emails) > 0 {
				return emails[0], nil
			}
			return &jmapmail.Email{
				ID:         origID,
				MailboxIDs: map[jmapcore.Id]bool{targetMoveMbID: true},
			}, nil
		}
	}

	b.pool.ReleaseClient(ctx, client)

	// Fetch updated message
	emails, _, err := b.GetEmails(ctx, []jmapcore.Id{origID})
	b.recordEmailMutation(accountID, origID, "update")
	b.publishStateChange(ctx)
	if err == nil && len(emails) > 0 {
		return emails[0], nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("email not found: %s", origID)
}

// DeleteEmail removes an email from IMAP via \Deleted flag and EXPUNGE.
func (b *IMAPSMTPBackend) DeleteEmail(ctx context.Context, id jmapcore.Id) (bool, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	id = b.resolveMovedEmailID(accountID, id)
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

	if err := client.MarkDeletedAndExpunge(folderName, []uint32{uid}); err != nil {
		b.pool.ReleaseClient(ctx, client)
		return false, err
	}
	b.pool.ReleaseClient(ctx, client)

	b.recordEmailMutation(accountID, id, "destroy")
	b.recordEmailQuotaDeleted(accountID, id)
	b.deleteBlobRefsForEmail(accountID, id)
	b.publishStateChange(ctx)
	return true, nil
}
