package imapsmtp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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

	// Append as draft staging message to IMAP Drafts
	client, err := b.pool.GetClientForContext(ctx)
	if err == nil {
		defer b.pool.ReleaseClient(ctx, client)

		msg := fmt.Sprintf("Subject: [JMAP-BLOB: %s]\r\nContent-Type: %s\r\nX-JMAP-Blob: %s\r\n\r\n", blobID, contentType, blobID)
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

// GetBlob retrieves a binary blob by ID.
func (b *IMAPSMTPBackend) GetBlob(ctx context.Context, accountID, blobID string) (*jmap.Blob, bool, error) {
	globalBlobCache.mu.RLock()
	blob, ok := globalBlobCache.blobs[blobID]
	globalBlobCache.mu.RUnlock()
	if ok {
		return blob, true, nil
	}

	// Check if blobID is an Email ID format (<mbID>:<uid>)
	if strings.Contains(blobID, ":") {
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
