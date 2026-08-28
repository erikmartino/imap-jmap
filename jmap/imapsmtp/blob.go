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

// PutBlob stores binary data staged in IMAP Drafts and memory cache.
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

	globalBlobCache.mu.Lock()
	globalBlobCache.blobs[blobID] = blob
	globalBlobCache.mu.Unlock()

	// Append as draft staging message to IMAP Drafts. First drop any previous
	// [JMAP-BLOB:] staging message so the folder never accumulates them — the
	// blob itself is held in the in-memory cache. This only touches the
	// authenticated account's own Drafts folder (request-context credentials).
	b.sweepBlobStaging(ctx, true)

	client, err := b.pool.GetClientForContext(ctx)
	if err == nil {
		defer b.pool.ReleaseClient(ctx, client)

		msg := fmt.Sprintf("Subject: %s %s]\r\nContent-Type: %s\r\nX-JMAP-Blob: %s\r\n\r\n", blobStagingMarker, blobID, contentType, blobID)
		msgBytes := append([]byte(msg), data...)

		appendCmd := client.Append("Drafts", int64(len(msgBytes)), &imap.AppendOptions{
			Flags: []imap.Flag{imap.FlagDraft},
			Time:  time.Now(),
		})
		_, _ = appendCmd.Write(msgBytes)
		_ = appendCmd.Close()
		_, _ = appendCmd.Wait()
	}

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

// GetBlob retrieves a binary blob by ID.
func (b *IMAPSMTPBackend) GetBlob(ctx context.Context, accountID, blobID string) (*jmap.Blob, bool, error) {
	globalBlobCache.mu.RLock()
	blob, ok := globalBlobCache.blobs[blobID]
	globalBlobCache.mu.RUnlock()
	if ok {
		return blob, true, nil
	}

	// Check if blobID is an Email ID format (<mbID>-<uid> or <mbID>:<uid>)
	if _, _, err := ParseEmailID(jmap.Id(blobID)); err == nil {
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
	globalBlobCache.mu.RLock()
	defer globalBlobCache.mu.RUnlock()

	var list []*jmap.Blob
	for _, bl := range globalBlobCache.blobs {
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
