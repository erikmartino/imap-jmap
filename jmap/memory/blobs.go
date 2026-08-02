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
	blobID := fullHex[:16]

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
