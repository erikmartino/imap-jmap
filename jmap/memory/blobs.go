package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"imap-jmap/jmap"
)

// MemoryBlobBackend manages in-memory blob storage per RFC 8620 Section 6 & RFC 9404.
type MemoryBlobBackend struct {
	mu    sync.RWMutex
	blobs map[string]*jmap.Blob
}

// Ensure MemoryBlobBackend implements jmap.BlobBackend interface.
var _ jmap.BlobBackend = (*MemoryBlobBackend)(nil)

// NewMemoryBlobBackend creates a new MemoryBlobBackend instance.
func NewMemoryBlobBackend() *MemoryBlobBackend {
	return &MemoryBlobBackend{
		blobs: make(map[string]*jmap.Blob),
	}
}

// PutBlob stores data and returns the created Blob metadata.
func (b *MemoryBlobBackend) PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*jmap.Blob, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	hash := sha256.Sum256(data)
	fullHex := hex.EncodeToString(hash[:])
	blobID := fullHex

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	blob := &jmap.Blob{
		ID:           blobID,
		BlobID:       blobID,
		AccountID:    accountID,
		Type:         contentType,
		Size:         int64(len(data)),
		DigestSHA256: fullHex,
		Data:         data,
	}

	key := accountID + ":" + blobID
	b.blobs[key] = blob
	return blob, nil
}

// GetBlob retrieves blob metadata and data by account ID and blob ID.
func (b *MemoryBlobBackend) GetBlob(ctx context.Context, accountID, blobID string) (*jmap.Blob, bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	key := accountID + ":" + blobID
	blob, ok := b.blobs[key]
	return blob, ok, nil
}

// CopyBlob copies a blob from fromAccountID to toAccountID per RFC 9404 Section 4.
func (b *MemoryBlobBackend) CopyBlob(ctx context.Context, fromAccountID, toAccountID string, blobID string) (*jmap.Blob, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	srcKey := fromAccountID + ":" + blobID
	srcBlob, ok := b.blobs[srcKey]
	if !ok {
		return nil, jmap.ErrBlobNotFound
	}

	copiedBlob := &jmap.Blob{
		ID:           srcBlob.ID,
		BlobID:       srcBlob.BlobID,
		AccountID:    toAccountID,
		Type:         srcBlob.Type,
		Size:         srcBlob.Size,
		DigestSHA256: srcBlob.DigestSHA256,
		Data:         srcBlob.Data,
	}

	destKey := toAccountID + ":" + blobID
	b.blobs[destKey] = copiedBlob
	return copiedBlob, nil
}
