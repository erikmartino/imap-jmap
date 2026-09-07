package imapsmtp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"imap-jmap/jmap"
)

// blobStagingMarker is the Subject prefix that identifies a blob staging
// message appended to the Trash folder by PutBlob.
const blobStagingMarker = "[JMAP-BLOB:"

// blobStagingTTL is how old a [JMAP-BLOB:] staging message must be before the
// lazy sweep removes it.
const blobStagingTTL = time.Hour

// blobStagingSweepInterval rate-limits the lazy per-account sweep on read
// paths. A sweep is one SELECT + SEARCH, usually empty, so a modest interval
// keeps it cheap.
const blobStagingSweepInterval = 5 * time.Minute

func (b *IMAPSMTPBackend) ensureContextCredentials(ctx context.Context, accountID string) context.Context {
	if _, ok := jmap.AccountIDFromContext(ctx); !ok && accountID != "" {
		ctx = jmap.ContextWithAccountID(ctx, accountID)
	}
	if _, ok := jmap.CredentialsFromContext(ctx); !ok {
		b.accountsMu.Lock()
		if creds, ok := b.activeAccounts[accountID]; ok {
			ctx = jmap.ContextWithCredentials(ctx, creds.Username, creds.Password)
			ctx = jmap.ContextWithSubject(ctx, creds.Username)
		} else if sub, ok := jmap.SubjectForAccountID(accountID); ok {
			ctx = jmap.ContextWithCredentials(ctx, sub, sub)
			ctx = jmap.ContextWithSubject(ctx, sub)
		} else if len(b.activeAccounts) == 1 {
			for _, creds := range b.activeAccounts {
				ctx = jmap.ContextWithCredentials(ctx, creds.Username, creds.Password)
				ctx = jmap.ContextWithSubject(ctx, creds.Username)
			}
		}
		b.accountsMu.Unlock()
	}
	return ctx
}

// PutBlob stores binary data in backend instance memory cache and stages it into IMAP Trash.
// Fresh uploads are wrapped in an RFC 822 message and marked as \Seen (read) so clients
// never see unread notification badges in their trash folder.
func (b *IMAPSMTPBackend) PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*jmap.Blob, error) {
	ctx = b.ensureContextCredentials(ctx, accountID)
	hash := sha256.Sum256(data)
	blobID := hex.EncodeToString(hash[:])

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	blob := &jmap.Blob{
		ID:        blobID,
		AccountID: accountID,
		Data:      data,
		Size:      int64(len(data)),
		Type:      contentType,
	}

	b.blobsMu.Lock()
	if b.blobs == nil {
		b.blobs = make(map[string]*jmap.Blob)
	}
	b.blobs[blobID] = blob
	b.blobsMu.Unlock()

	// Append fresh upload wrapped in RFC 822 message to IMAP Trash folder marked as \Seen.
	client, err := b.pool.GetClientForContext(ctx)
	if err == nil {
		defer b.pool.ReleaseClient(ctx, client)

		var bodyBuf bytes.Buffer
		b64w := base64.NewEncoder(base64.StdEncoding, &bodyBuf)
		_, _ = b64w.Write(data)
		_ = b64w.Close()

		msgHeader := fmt.Sprintf("Subject: %s %s]\r\nContent-Type: %s\r\nContent-Transfer-Encoding: base64\r\nX-JMAP-Blob: %s\r\nX-JMAP-Content-Type: %s\r\n\r\n",
			blobStagingMarker, blobID, contentType, blobID, contentType)
		rawMsg := append([]byte(msgHeader), bodyBuf.Bytes()...)

		appendCmd := client.Append("Trash", int64(len(rawMsg)), &imap.AppendOptions{
			Flags: []imap.Flag{imap.FlagSeen},
		})
		if _, err := appendCmd.Write(rawMsg); err == nil {
			_ = appendCmd.Close()
			if _, err := appendCmd.Wait(); err != nil {
				// If Trash folder does not exist yet, attempt creating it and retry
				if errCreate := client.Create("Trash", nil).Wait(); errCreate == nil {
					retryCmd := client.Append("Trash", int64(len(rawMsg)), &imap.AppendOptions{
						Flags: []imap.Flag{imap.FlagSeen},
					})
					if _, errWrite := retryCmd.Write(rawMsg); errWrite == nil {
						_ = retryCmd.Close()
						_, _ = retryCmd.Wait()
					} else {
						_ = retryCmd.Close()
					}
				}
			}
		} else {
			_ = appendCmd.Close()
		}
	}

	return blob, nil
}

// maybeSweepBlobStaging runs sweepBlobStaging at most every
// blobStagingSweepInterval per account. It is invoked from read paths (e.g.
// GetAllMailboxes) so [JMAP-BLOB:] staging messages left in Trash or Drafts
// older than blobStagingTTL are cleaned automatically.
func (b *IMAPSMTPBackend) maybeSweepBlobStaging(ctx context.Context) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	if accountID == "" {
		return
	}
	b.sweepMu.Lock()
	if time.Since(b.lastSweep[accountID]) < blobStagingSweepInterval {
		b.sweepMu.Unlock()
		return
	}
	b.lastSweep[accountID] = time.Now()
	b.sweepMu.Unlock()

	creds, _ := jmap.CredentialsFromContext(ctx)
	go func() {
		bgCtx := jmap.ContextWithAccountID(context.Background(), accountID)
		bgCtx = jmap.ContextWithCredentials(bgCtx, creds.Username, creds.Password)
		bgCtx = jmap.ContextWithSubject(bgCtx, creds.Username)
		b.sweepBlobStaging(bgCtx, false)
	}()
}

// sweepBlobStaging deletes [JMAP-BLOB:] staging messages from the account's
// Trash and Drafts folders. When removeAll is true every staging message is deleted;
// otherwise only those older than blobStagingTTL are removed. Real user messages
// are never touched: only messages whose Subject carries the staging marker are selected.
func (b *IMAPSMTPBackend) sweepBlobStaging(ctx context.Context, removeAll bool) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return
	}
	defer b.pool.ReleaseClient(ctx, client)

	b.sweepFolder(client, "Trash", removeAll)
	b.sweepFolder(client, "Drafts", removeAll)
}

func (b *IMAPSMTPBackend) sweepFolder(client *imapclient.Client, folderName string, removeAll bool) {
	if _, err := client.Select(folderName, nil).Wait(); err != nil {
		return
	}

	searchCmd := client.UIDSearch(&imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: blobStagingMarker}},
	}, nil)
	data, err := searchCmd.Wait()
	if err != nil {
		return
	}
	uids := data.AllUIDs()
	if len(uids) == 0 {
		return
	}

	var uidSet imap.UIDSet
	for _, u := range uids {
		uidSet.AddNum(u)
	}

	var toDelete imap.UIDSet
	if removeAll {
		toDelete = uidSet
	} else {
		cutoff := time.Now().Add(-blobStagingTTL)
		fetchCmd := client.Fetch(uidSet, &imap.FetchOptions{InternalDate: true})
		msgs, err := fetchCmd.Collect()
		if err != nil {
			return
		}
		for _, msg := range msgs {
			if !msg.InternalDate.IsZero() && msg.InternalDate.Before(cutoff) {
				toDelete.AddNum(msg.UID)
			}
		}
	}

	if len(toDelete) == 0 {
		return
	}

	storeCmd := client.Store(toDelete, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagDeleted},
		Silent: true,
	}, nil)
	if _, err := storeCmd.Collect(); err != nil {
		return
	}
	_, _ = client.Expunge().Collect()
}

func (b *IMAPSMTPBackend) getRawEmailBytes(ctx context.Context, id jmap.Id) ([]byte, error) {
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
		return nil, err
	}
	bodySection := &imap.FetchItemBodySection{Peek: true}
	var uidSet imap.UIDSet
	uidSet.AddNum(imap.UID(uid))
	fetchCmd := client.Fetch(uidSet, &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{bodySection},
	})
	msgs, err := fetchCmd.Collect()
	if err != nil || len(msgs) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	raw := msgs[0].FindBodySection(bodySection)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	return raw, nil
}

// GetBlob retrieves a binary blob by ID. It searches:
// 1. In-memory cache for fast session lookup
// 2. Full email RFC 822 format (if blobID is an Email ID)
// 3. Email attachments across the account (RFC 822 MIME extraction on demand)
// 4. Staged uploads in Trash (or legacy Drafts)
func (b *IMAPSMTPBackend) GetBlob(ctx context.Context, accountID, blobID string) (*jmap.Blob, bool, error) {
	ctx = b.ensureContextCredentials(ctx, accountID)

	b.blobsMu.RLock()
	blob, ok := b.blobs[blobID]
	b.blobsMu.RUnlock()
	if ok {
		return blob, true, nil
	}

	// Check if blobID is an Email ID format (<mbID>-<uid> or <mbID>:<uid>)
	if _, _, err := ParseEmailID(jmap.Id(blobID)); err == nil {
		if raw, err := b.getRawEmailBytes(ctx, jmap.Id(blobID)); err == nil {
			return &jmap.Blob{
				ID:        blobID,
				AccountID: accountID,
				Data:      raw,
				Size:      int64(len(raw)),
				Type:      "message/rfc822",
			}, true, nil
		}
		emails, notFound, err := b.GetEmails(ctx, []jmap.Id{jmap.Id(blobID)})
		if err == nil && len(emails) > 0 && len(notFound) == 0 {
			raw := jmap.FormatEmailRFC822(emails[0])
			return &jmap.Blob{
				ID:        blobID,
				AccountID: accountID,
				Data:      raw,
				Size:      int64(len(raw)),
				Type:      "message/rfc822",
			}, true, nil
		}
	}

	// Extract blob from email attachments on IMAP
	if blob, found, err := b.getBlobFromEmailAttachments(ctx, accountID, blobID); err == nil && found {
		return blob, true, nil
	}

	// Recover fresh upload staged in Trash (or legacy Drafts)
	if blob, found, err := b.getBlobFromTrash(ctx, accountID, blobID); err == nil && found {
		return blob, true, nil
	}

	return nil, false, nil
}

func (b *IMAPSMTPBackend) getBlobFromEmailAttachments(ctx context.Context, accountID, blobID string) (*jmap.Blob, bool, error) {
	// 1. Check known blob refs first for instant lookup
	b.blobsMu.RLock()
	var candidateEmailIDs []jmap.Id
	if acctRefs, ok := b.blobRefs[accountID]; ok {
		if emailMap, ok := acctRefs[blobID]; ok {
			for eid := range emailMap {
				candidateEmailIDs = append(candidateEmailIDs, eid)
			}
		}
	}
	b.blobsMu.RUnlock()

	for _, eid := range candidateEmailIDs {
		if raw, err := b.getRawEmailBytes(ctx, eid); err == nil {
			if data, cType, found := jmap.ExtractBlobFromRFC822(raw, blobID); found {
				blob := &jmap.Blob{
					ID:        blobID,
					AccountID: accountID,
					Data:      data,
					Size:      int64(len(data)),
					Type:      cType,
				}
				b.blobsMu.Lock()
				if b.blobs == nil {
					b.blobs = make(map[string]*jmap.Blob)
				}
				b.blobs[blobID] = blob
				b.blobsMu.Unlock()
				return blob, true, nil
			}
		}
	}

	// 2. Scan account emails
	emails, err := b.GetAllEmails(ctx)
	if err != nil {
		return nil, false, err
	}

	for _, em := range emails {
		if emailReferencesBlob(em, jmap.Id(blobID)) {
			if raw, err := b.getRawEmailBytes(ctx, em.ID); err == nil {
				if data, cType, found := jmap.ExtractBlobFromRFC822(raw, blobID); found {
					blob := &jmap.Blob{
						ID:        blobID,
						AccountID: accountID,
						Data:      data,
						Size:      int64(len(data)),
						Type:      cType,
					}
					b.recordBlobRef(accountID, blobID, em.ID)
					b.blobsMu.Lock()
					if b.blobs == nil {
						b.blobs = make(map[string]*jmap.Blob)
					}
					b.blobs[blobID] = blob
					b.blobsMu.Unlock()
					return blob, true, nil
				}
			}
		}
	}

	return nil, false, nil
}

func (b *IMAPSMTPBackend) getBlobFromTrash(ctx context.Context, accountID, blobID string) (*jmap.Blob, bool, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, false, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	if blob, found, err := b.getBlobFromStagingFolder(ctx, client, "Trash", accountID, blobID); err == nil && found {
		return blob, true, nil
	}
	return b.getBlobFromStagingFolder(ctx, client, "Drafts", accountID, blobID)
}

func (b *IMAPSMTPBackend) getBlobFromStagingFolder(ctx context.Context, client *imapclient.Client, folderName, accountID, blobID string) (*jmap.Blob, bool, error) {
	if _, err := client.Select(folderName, nil).Wait(); err != nil {
		return nil, false, nil
	}

	subjectTarget := fmt.Sprintf("%s %s]", blobStagingMarker, blobID)
	searchCmd := client.UIDSearch(&imap.SearchCriteria{
		Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: subjectTarget}},
	}, nil)
	data, err := searchCmd.Wait()
	if err != nil {
		return nil, false, err
	}
	uids := data.AllUIDs()
	if len(uids) == 0 {
		searchCmd = client.UIDSearch(&imap.SearchCriteria{
			Header: []imap.SearchCriteriaHeaderField{{Key: "Subject", Value: blobID}},
		}, nil)
		data, err = searchCmd.Wait()
		if err != nil {
			return nil, false, err
		}
		uids = data.AllUIDs()
	}
	if len(uids) == 0 {
		return nil, false, nil
	}

	var uidSet imap.UIDSet
	targetUID := uids[len(uids)-1]
	uidSet.AddNum(targetUID)

	bodySection := &imap.FetchItemBodySection{Peek: true}
	fetchCmd := client.Fetch(uidSet, &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{bodySection},
	})
	msgs, err := fetchCmd.Collect()
	if err != nil || len(msgs) == 0 {
		return nil, false, nil
	}

	raw := msgs[0].FindBodySection(bodySection)
	if len(raw) == 0 {
		return nil, false, nil
	}

	dataBytes, cType, found := jmap.ExtractBlobFromRFC822(raw, blobID)
	if !found {
		return nil, false, nil
	}

	blob := &jmap.Blob{
		ID:        blobID,
		AccountID: accountID,
		Data:      dataBytes,
		Size:      int64(len(dataBytes)),
		Type:      cType,
	}

	b.blobsMu.Lock()
	if b.blobs == nil {
		b.blobs = make(map[string]*jmap.Blob)
	}
	b.blobs[blobID] = blob
	b.blobsMu.Unlock()

	return blob, true, nil
}

// GetAllBlobs retrieves all blobs stored for an account.
func (b *IMAPSMTPBackend) GetAllBlobs(ctx context.Context, accountID string) ([]*jmap.Blob, error) {
	ctx = b.ensureContextCredentials(ctx, accountID)

	if emails, err := b.GetAllEmails(ctx); err == nil {
		for _, em := range emails {
			for _, att := range em.Attachments {
				if att.BlobID != nil && *att.BlobID != "" {
					_, _, _ = b.GetBlob(ctx, accountID, string(*att.BlobID))
				}
			}
		}
	}

	b.blobsMu.RLock()
	defer b.blobsMu.RUnlock()

	var list []*jmap.Blob
	for _, bl := range b.blobs {
		if bl.AccountID == accountID || accountID == "" {
			list = append(list, bl)
		}
	}
	return list, nil
}

// CopyBlob copies a blob to another account.
func (b *IMAPSMTPBackend) CopyBlob(ctx context.Context, fromAccountID, toAccountID string, blobID string) (*jmap.Blob, error) {
	blob, ok, err := b.GetBlob(ctx, fromAccountID, blobID)
	if err != nil || !ok {
		return nil, fmt.Errorf("blob not found: %s", blobID)
	}

	return b.PutBlob(ctx, toAccountID, blob.Type, blob.Data)
}

func (b *IMAPSMTPBackend) recordBlobRef(accountID, blobID string, emailID jmap.Id) {
	b.blobsMu.Lock()
	defer b.blobsMu.Unlock()
	if b.blobRefs == nil {
		b.blobRefs = make(map[string]map[string]map[jmap.Id]bool)
	}
	if b.blobRefs[accountID] == nil {
		b.blobRefs[accountID] = make(map[string]map[jmap.Id]bool)
	}
	if b.blobRefs[accountID][blobID] == nil {
		b.blobRefs[accountID][blobID] = make(map[jmap.Id]bool)
	}
	b.blobRefs[accountID][blobID][emailID] = true
}

func (b *IMAPSMTPBackend) deleteBlobRefsForEmail(accountID string, emailID jmap.Id) {
	b.blobsMu.Lock()
	defer b.blobsMu.Unlock()
	if b.blobRefs == nil || b.blobRefs[accountID] == nil {
		return
	}
	for _, emailMap := range b.blobRefs[accountID] {
		delete(emailMap, emailID)
	}
}

// LookupBlobReferences implements jmap.BlobReferenceBackend per RFC 9404 Section 4.3.
func (b *IMAPSMTPBackend) LookupBlobReferences(ctx context.Context, typeNames []string, blobID jmap.Id) (map[string][]jmap.Id, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	emails, err := b.GetAllEmails(ctx)
	if err != nil {
		return nil, err
	}

	b.blobsMu.RLock()
	extraEmailIDs := make(map[jmap.Id]bool)
	if acctRefs, ok := b.blobRefs[accountID]; ok {
		if emailMap, ok := acctRefs[string(blobID)]; ok {
			for eid := range emailMap {
				extraEmailIDs[eid] = true
			}
		}
	}
	b.blobsMu.RUnlock()

	matched := make(map[string][]jmap.Id)
	for _, tn := range typeNames {
		switch tn {
		case "Email":
			for _, em := range emails {
				if em.BlobID == blobID || emailReferencesBlob(em, blobID) || extraEmailIDs[em.ID] {
					matched["Email"] = append(matched["Email"], em.ID)
				}
			}
		case "Thread":
			seenThreads := make(map[jmap.Id]bool)
			for _, em := range emails {
				if (em.BlobID == blobID || emailReferencesBlob(em, blobID) || extraEmailIDs[em.ID]) && em.ThreadID != "" {
					if !seenThreads[em.ThreadID] {
						seenThreads[em.ThreadID] = true
						matched["Thread"] = append(matched["Thread"], em.ThreadID)
					}
				}
			}
		case "Mailbox":
			// No Mailbox property references blobs
		}
	}
	return matched, nil
}

func bodyPartReferencesBlob(p *jmap.EmailBodyPart, blobID jmap.Id) bool {
	if p == nil {
		return false
	}
	if p.BlobID != nil && *p.BlobID == blobID {
		return true
	}
	for i := range p.SubParts {
		if bodyPartReferencesBlob(&p.SubParts[i], blobID) {
			return true
		}
	}
	return false
}

func emailReferencesBlob(em *jmap.Email, blobID jmap.Id) bool {
	if em.BlobID == blobID {
		return true
	}
	if bodyPartReferencesBlob(&em.BodyStructure, blobID) {
		return true
	}
	for _, att := range em.Attachments {
		if att.BlobID != nil && *att.BlobID == blobID {
			return true
		}
	}
	for _, part := range em.TextBody {
		if part.BlobID != nil && *part.BlobID == blobID {
			return true
		}
	}
	for _, part := range em.HTMLBody {
		if part.BlobID != nil && *part.BlobID == blobID {
			return true
		}
	}
	return false
}
