package imapsmtp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"imap-jmap/jmap"
)

// In-memory cache for fast blob staging alongside IMAP Drafts
type blobCache struct {
	mu    sync.RWMutex
	blobs map[string]*jmap.Blob
}

var globalBlobCache = &blobCache{
	blobs: make(map[string]*jmap.Blob),
}

// blobStagingMarker is the Subject prefix that identifies a blob staging
// message appended to the Drafts folder by PutBlob.
const blobStagingMarker = "[JMAP-BLOB:"

// blobStagingTTL is how old a [JMAP-BLOB:] staging message must be before the
// lazy sweep removes it. The staging copy is never read back (blobs live in the
// in-memory cache and message blobs resolve to the stored email), so it is only
// a short-lived marker; the TTL simply avoids racing any in-flight compose.
const blobStagingTTL = time.Hour

// blobStagingSweepInterval rate-limits the lazy per-account sweep on read
// paths. A sweep is one SELECT + SEARCH, usually empty, so a modest interval
// keeps it cheap.
const blobStagingSweepInterval = 5 * time.Minute

// PutBlob stores binary data in backend instance memory cache.
func (b *IMAPSMTPBackend) PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*jmap.Blob, error) {
	hash := sha256.Sum256(data)
	blobID := hex.EncodeToString(hash[:])

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

	return blob, nil
}

// maybeSweepBlobStaging runs sweepBlobStaging at most every
// blobStagingSweepInterval per account. It is invoked from read paths (e.g.
// GetAllMailboxes) so legacy [JMAP-BLOB:] staging messages left in Drafts by
// earlier versions are removed automatically once the account is next active.
// It runs inside the request context, so it can only ever clean the
// authenticated user's own folder — the gateway never connects as any account
// other than the one currently authenticated.
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
	b.sweepBlobStaging(ctx, false)
}

// sweepBlobStaging deletes [JMAP-BLOB:] staging messages from the account's
// Drafts folder. When removeAll is true every staging message is deleted (used
// before appending a fresh staging copy); otherwise only those older than
// blobStagingTTL are removed. Real user drafts are never touched: only messages
// whose Subject carries the staging marker are selected.
func (b *IMAPSMTPBackend) sweepBlobStaging(ctx context.Context, removeAll bool) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return
	}
	defer b.pool.ReleaseClient(ctx, client)

	if _, err := client.Select("Drafts", nil).Wait(); err != nil {
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

// GetBlob retrieves a binary blob by ID.
func (b *IMAPSMTPBackend) GetBlob(ctx context.Context, accountID, blobID string) (*jmap.Blob, bool, error) {
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

	return nil, false, nil
}

// GetAllBlobs retrieves all blobs stored for an account.
func (b *IMAPSMTPBackend) GetAllBlobs(ctx context.Context, accountID string) ([]*jmap.Blob, error) {
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

func emailReferencesBlob(em *jmap.Email, blobID jmap.Id) bool {
	if em.BlobID == blobID {
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
