package jmap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Blob represents a stored binary blob per RFC 8620 Section 6 and RFC 9404 Section 4.
type Blob struct {
	ID           string `json:"id"`
	BlobID       string `json:"blobId,omitempty"`
	AccountID    string `json:"accountId,omitempty"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`
	DigestSHA256 string `json:"digest:sha-256,omitempty"`
	Data         []byte `json:"-"`
}

// MemoryBlobBackend manages in-memory blob storage per RFC 8620 Section 6 & RFC 9404.
type MemoryBlobBackend struct {
	mu    sync.RWMutex
	blobs map[string]*Blob
}

// Ensure MemoryBlobBackend implements BlobBackend interface.
var _ BlobBackend = (*MemoryBlobBackend)(nil)

// NewMemoryBlobBackend creates a new MemoryBlobBackend instance.
func NewMemoryBlobBackend() *MemoryBlobBackend {
	return &MemoryBlobBackend{
		blobs: make(map[string]*Blob),
	}
}

// PutBlob stores data and returns the created Blob metadata.
func (b *MemoryBlobBackend) PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*Blob, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	hash := sha256.Sum256(data)
	fullHex := hex.EncodeToString(hash[:])
	blobID := fullHex[:16]

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	blob := &Blob{
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
func (b *MemoryBlobBackend) GetBlob(ctx context.Context, accountID, blobID string) (*Blob, bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	key := accountID + ":" + blobID
	blob, ok := b.blobs[key]
	return blob, ok, nil
}

// HandleUpload handles POST requests to /upload/{accountId}/ per RFC 8620 Section 6.1.
func (s *Server) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/upload/")
	accountID := strings.TrimSuffix(path, "/")
	if accountID == "" {
		http.Error(w, "Account ID required", http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	contentType := r.Header.Get("Content-Type")
	blob, err := s.BlobBackend.PutBlob(r.Context(), accountID, contentType, data)
	if err != nil {
		http.Error(w, "Failed to store blob: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(blob)
}

// HandleDownload handles GET requests to /download/{accountId}/{blobId}/{name} per RFC 8620 Section 6.2.
func (s *Server) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/download/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	accountID := parts[0]
	blobID := parts[1]
	name := ""
	if len(parts) == 3 {
		name = parts[2]
	}

	blob, ok, err := s.BlobBackend.GetBlob(r.Context(), accountID, blobID)
	if err != nil || !ok {
		http.NotFound(w, r)
		return
	}

	mediaType := r.URL.Query().Get("type")
	if mediaType == "" {
		mediaType = blob.Type
	}

	w.Header().Set("Content-Type", mediaType)
	if name != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}

	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(blob.Data)
	}
}
