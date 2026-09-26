package imapsmtp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/emersion/go-message/mail"

	"imap-jmap/imap"
	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
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
	if _, ok := jmapauth.CredentialsFromContext(ctx); !ok {
		if sub, ok := jmapauth.SubjectForAccountID(accountID); ok && sub != "" {
			ctx = jmapauth.ContextWithCredentials(ctx, sub, sub)
		}
	}
	return ctx
}

// PutBlob stores binary data in backend instance memory cache and stages it into IMAP Trash.
// Fresh uploads are wrapped in an RFC 822 message and marked as \Seen (read) so clients
// never see unread notification badges in their trash folder.
func (b *IMAPSMTPBackend) PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*jmapblob.Blob, error) {
	ctx = b.ensureContextCredentials(ctx, accountID)
	hash := sha256.Sum256(data)
	blobID := hex.EncodeToString(hash[:])

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	blob := &jmapblob.Blob{
		ID:        blobID,
		AccountID: accountID,
		Data:      data,
		Size:      int64(len(data)),
		Type:      contentType,
	}

	b.blobsMu.Lock()
	if b.blobs == nil {
		b.blobs = make(map[string]*jmapblob.Blob)
	}
	b.blobs[blobID] = blob
	b.blobsMu.Unlock()

	// Append fresh upload wrapped in RFC 822 message to IMAP Trash folder marked as \Seen.
	client, err := b.pool.GetClientForContext(ctx)
	if err == nil {
		defer b.pool.ReleaseClient(ctx, client)

		// Wrap the upload in an RFC 822 message with go-message. The body is
		// written raw and the writer applies the base64 transfer encoding (with
		// RFC 2045 line folding). The wrapper advertises application/octet-stream
		// and carries the real media type in X-JMAP-Content-Type.
		var stagingHeader mail.Header
		stagingHeader.SetSubject(fmt.Sprintf("%s %s]", blobStagingMarker, blobID))
		stagingHeader.SetContentType("application/octet-stream", nil)
		stagingHeader.Set("Content-Transfer-Encoding", "base64")
		stagingHeader.Set("X-JMAP-Blob", blobID)
		stagingHeader.Set("X-JMAP-Content-Type", contentType)

		var rawMsg bytes.Buffer
		if w, wErr := mail.CreateSingleInlineWriter(&rawMsg, stagingHeader); wErr == nil {
			_, _ = w.Write(data)
			_ = w.Close()
		}

		if errApp := client.Append("Trash", rawMsg.Bytes(), []string{"\\Seen"}, time.Now()); errApp != nil {
			if errCreate := client.Create("Trash"); errCreate == nil {
				_ = client.Append("Trash", rawMsg.Bytes(), []string{"\\Seen"}, time.Now())
			}
		}
	}

	return blob, nil
}

func (b *IMAPSMTPBackend) maybeSweepBlobStaging(ctx context.Context) {
	accountID, ok := jmapauth.AccountIDFromContext(ctx)
	if !ok || accountID == "" {
		return
	}

	now := time.Now()
	b.sweepMu.Lock()
	if b.lastSweep == nil {
		b.lastSweep = make(map[string]time.Time)
	}
	last := b.lastSweep[accountID]
	if !last.IsZero() && now.Sub(last) < blobStagingSweepInterval {
		b.sweepMu.Unlock()
		return
	}
	b.lastSweep[accountID] = now
	b.sweepMu.Unlock()

	b.sweepBlobStaging(ctx, false)
}

// sweepBlobStaging removes [JMAP-BLOB:] staging messages from Trash and Drafts.
// When removeAll is false, only staging messages older than blobStagingTTL are
// removed. Non-staging messages (regular email or draft content) are never touched.
func (b *IMAPSMTPBackend) sweepBlobStaging(ctx context.Context, removeAll bool) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return
	}
	defer b.pool.ReleaseClient(ctx, client)

	b.sweepFolder(client, "Trash", removeAll)
	b.sweepFolder(client, "Drafts", removeAll)
}

func (b *IMAPSMTPBackend) sweepFolder(client *imap.Client, folderName string, removeAll bool) {
	uids, err := client.SearchSubject(folderName, blobStagingMarker)
	if err != nil || len(uids) == 0 {
		return
	}

	var toDelete []uint32
	if removeAll {
		toDelete = uids
	} else {
		cutoff := time.Now().Add(-blobStagingTTL)
		msgs, err := client.FetchStagingMessages(folderName, uids)
		if err != nil {
			return
		}
		for _, msg := range msgs {
			if !msg.InternalDate.IsZero() && msg.InternalDate.Before(cutoff) {
				toDelete = append(toDelete, msg.UID)
			}
		}
	}

	if len(toDelete) == 0 {
		return
	}

	_ = client.MarkDeletedAndExpunge(folderName, toDelete)
}

func (b *IMAPSMTPBackend) getRawEmailBytes(ctx context.Context, id jmapcore.Id) ([]byte, error) {
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

	return client.FetchRawMessageByUID(folderName, uid)
}

// GetBlob retrieves a binary blob by ID. It searches:
// 1. In-memory cache for fast session lookup
// 2. Full email RFC 822 format (if blobID is an Email ID)
// 3. Email attachments across the account (RFC 822 MIME extraction on demand)
// 4. Staged uploads in Trash (or legacy Drafts)
func (b *IMAPSMTPBackend) GetBlob(ctx context.Context, accountID, blobID string) (*jmapblob.Blob, bool, error) {
	ctx = b.ensureContextCredentials(ctx, accountID)

	b.blobsMu.RLock()
	blob, ok := b.blobs[blobID]
	b.blobsMu.RUnlock()
	if ok {
		if blob.AccountID != "" && accountID != "" && blob.AccountID != accountID {
			// Unreferenced blobs MUST only be accessible to the uploader (RFC 8620 §6.1)
			return nil, false, nil
		}
		return blob, true, nil
	}

	// Check if blobID is an Email ID format (<mbID>-<uid> or <mbID>:<uid>)
	if _, _, err := ParseEmailID(jmapcore.Id(blobID)); err == nil {
		if raw, err := b.getRawEmailBytes(ctx, jmapcore.Id(blobID)); err == nil {
			return &jmapblob.Blob{
				ID:        blobID,
				AccountID: accountID,
				Data:      raw,
				Size:      int64(len(raw)),
				Type:      "message/rfc822",
			}, true, nil
		}
		emails, notFound, err := b.GetEmails(ctx, []jmapcore.Id{jmapcore.Id(blobID)})
		if err == nil && len(emails) > 0 && len(notFound) == 0 {
			raw := jmapmail.FormatEmailRFC822(emails[0])
			return &jmapblob.Blob{
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

func (b *IMAPSMTPBackend) getBlobFromEmailAttachments(ctx context.Context, accountID, blobID string) (*jmapblob.Blob, bool, error) {
	// 1. Check known blob refs first for instant lookup
	b.blobsMu.RLock()
	var candidateEmailIDs []jmapcore.Id
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
			if data, cType, found := jmapmail.ExtractBlobFromRFC822(raw, blobID); found {
				blob := &jmapblob.Blob{
					ID:        blobID,
					AccountID: accountID,
					Data:      data,
					Size:      int64(len(data)),
					Type:      cType,
				}
				b.blobsMu.Lock()
				if b.blobs == nil {
					b.blobs = make(map[string]*jmapblob.Blob)
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
		if emailReferencesBlob(em, jmapcore.Id(blobID)) {
			if raw, err := b.getRawEmailBytes(ctx, em.ID); err == nil {
				if data, cType, found := jmapmail.ExtractBlobFromRFC822(raw, blobID); found {
					blob := &jmapblob.Blob{
						ID:        blobID,
						AccountID: accountID,
						Data:      data,
						Size:      int64(len(data)),
						Type:      cType,
					}
					b.recordBlobRef(accountID, blobID, em.ID)
					b.blobsMu.Lock()
					if b.blobs == nil {
						b.blobs = make(map[string]*jmapblob.Blob)
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

func (b *IMAPSMTPBackend) getBlobFromTrash(ctx context.Context, accountID, blobID string) (*jmapblob.Blob, bool, error) {
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

func (b *IMAPSMTPBackend) getBlobFromStagingFolder(ctx context.Context, client *imap.Client, folderName, accountID, blobID string) (*jmapblob.Blob, bool, error) {
	subjectTarget := fmt.Sprintf("%s %s]", blobStagingMarker, blobID)
	uids, err := client.SearchSubject(folderName, subjectTarget)
	if err != nil {
		return nil, false, err
	}
	if len(uids) == 0 {
		uids, err = client.SearchSubject(folderName, blobID)
		if err != nil {
			return nil, false, err
		}
	}
	if len(uids) == 0 {
		return nil, false, nil
	}

	targetUID := uids[len(uids)-1]
	raw, err := client.FetchRawMessageByUID(folderName, targetUID)
	if err != nil || len(raw) == 0 {
		return nil, false, nil
	}

	dataBytes, cType, found := jmapmail.ExtractBlobFromRFC822(raw, blobID)
	if !found {
		return nil, false, nil
	}

	blob := &jmapblob.Blob{
		ID:        blobID,
		AccountID: accountID,
		Data:      dataBytes,
		Size:      int64(len(dataBytes)),
		Type:      cType,
	}

	b.blobsMu.Lock()
	if b.blobs == nil {
		b.blobs = make(map[string]*jmapblob.Blob)
	}
	b.blobs[blobID] = blob
	b.blobsMu.Unlock()

	return blob, true, nil
}

// GetAllBlobs retrieves all blobs stored for an account.
func (b *IMAPSMTPBackend) GetAllBlobs(ctx context.Context, accountID string) ([]*jmapblob.Blob, error) {
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

	var list []*jmapblob.Blob
	for _, bl := range b.blobs {
		if bl.AccountID == accountID || accountID == "" {
			list = append(list, bl)
		}
	}
	return list, nil
}

// CopyBlob copies a blob to another account.
func (b *IMAPSMTPBackend) CopyBlob(ctx context.Context, fromAccountID, toAccountID string, blobID string) (*jmapblob.Blob, error) {
	blob, ok, err := b.GetBlob(ctx, fromAccountID, blobID)
	if err != nil || !ok {
		return nil, fmt.Errorf("blob not found: %s", blobID)
	}

	return b.PutBlob(ctx, toAccountID, blob.Type, blob.Data)
}

func (b *IMAPSMTPBackend) recordBlobRef(accountID, blobID string, emailID jmapcore.Id) {
	b.blobsMu.Lock()
	defer b.blobsMu.Unlock()
	if b.blobRefs == nil {
		b.blobRefs = make(map[string]map[string]map[jmapcore.Id]bool)
	}
	if b.blobRefs[accountID] == nil {
		b.blobRefs[accountID] = make(map[string]map[jmapcore.Id]bool)
	}
	if b.blobRefs[accountID][blobID] == nil {
		b.blobRefs[accountID][blobID] = make(map[jmapcore.Id]bool)
	}
	b.blobRefs[accountID][blobID][emailID] = true
}

func (b *IMAPSMTPBackend) deleteBlobRefsForEmail(accountID string, emailID jmapcore.Id) {
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
func (b *IMAPSMTPBackend) LookupBlobReferences(ctx context.Context, typeNames []string, blobID jmapcore.Id) (map[string][]jmapcore.Id, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	emails, err := b.GetAllEmails(ctx)
	if err != nil {
		return nil, err
	}

	b.blobsMu.RLock()
	extraEmailIDs := make(map[jmapcore.Id]bool)
	if acctRefs, ok := b.blobRefs[accountID]; ok {
		if emailMap, ok := acctRefs[string(blobID)]; ok {
			for eid := range emailMap {
				extraEmailIDs[eid] = true
			}
		}
	}
	b.blobsMu.RUnlock()

	matched := make(map[string][]jmapcore.Id)
	for _, tn := range typeNames {
		switch tn {
		case "Email":
			for _, em := range emails {
				if em.BlobID == blobID || emailReferencesBlob(em, blobID) || extraEmailIDs[em.ID] {
					matched["Email"] = append(matched["Email"], em.ID)
				}
			}
		case "Thread":
			seenThreads := make(map[jmapcore.Id]bool)
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

func bodyPartReferencesBlob(p *jmapmail.EmailBodyPart, blobID jmapcore.Id) bool {
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

func emailReferencesBlob(em *jmapmail.Email, blobID jmapcore.Id) bool {
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
