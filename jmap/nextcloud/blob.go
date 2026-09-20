package nextcloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sync"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcore"
)

// BlobBackend implements jmapblob.BlobBackend and jmapblob.BlobReferenceBackend
// backed by Nextcloud WebDAV.
type BlobBackend struct {
	client     *Client
	mu         sync.RWMutex
	fnBackend  *FileNodeBackend
	fallback   jmapblob.BlobBackend
	blobsCache map[string]map[string]*jmapblob.Blob // user -> blobID -> Blob
}

var _ jmapblob.BlobBackend = (*BlobBackend)(nil)
var _ jmapblob.BlobReferenceBackend = (*BlobBackend)(nil)

// NewBlobBackend initializes a new Nextcloud-backed BlobBackend.
func NewBlobBackend(client *Client, fnBackend *FileNodeBackend) *BlobBackend {
	bb := &BlobBackend{
		client:     client,
		fnBackend:  fnBackend,
		blobsCache: make(map[string]map[string]*jmapblob.Blob),
	}
	if fnBackend != nil {
		fnBackend.SetBlobBackend(bb)
	}
	return bb
}

// SetFileNodeBackend sets or updates the associated FileNodeBackend.
func (b *BlobBackend) SetFileNodeBackend(fb *FileNodeBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fnBackend = fb
	if fb != nil && fb.BlobBackend() != b {
		fb.SetBlobBackend(b)
	}
}

// SetFallback sets an optional fallback BlobBackend (such as IMAPSMTPBackend) for resolving mail-staged blobs.
func (b *BlobBackend) SetFallback(fallback jmapblob.BlobBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fallback = fallback
}

func (b *BlobBackend) user(ctx context.Context, accountID string) string {
	if accountID != "" {
		if subj, ok := jmapauth.SubjectForAccountID(accountID); ok && subj != "" {
			return subj
		}
		return accountID
	}
	u, _ := b.client.getUserAndPass(ctx)
	if u != "" {
		return u
	}
	if sub, ok := jmapauth.SubjectFromContext(ctx); ok && sub != "" {
		return sub
	}
	return ""
}

type blobMeta struct {
	ID          string `json:"id"`
	AccountID   string `json:"accountId"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
}

// PutBlob stores binary data into Nextcloud WebDAV under .blobs/{blobID}.
func (b *BlobBackend) PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*jmapblob.Blob, error) {
	u := b.user(ctx, accountID)
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

	// Persist to WebDAV under .blobs/
	fs, _, err := b.client.WebDAV(ctx)
	if err == nil {
		_ = fs.Mkdir(ctx, ".blobs")
		blobPath := path.Join(".blobs", blobID)
		if wc, err := fs.Create(ctx, blobPath); err == nil {
			_, _ = wc.Write(data)
			_ = wc.Close()
		}

		// Store metadata
		meta := blobMeta{
			ID:        blobID,
			AccountID: accountID,
			Type:      contentType,
			Size:      int64(len(data)),
		}
		if metaBytes, err := json.Marshal(meta); err == nil {
			if mc, err := fs.Create(ctx, blobPath+".meta"); err == nil {
				_, _ = mc.Write(metaBytes)
				_ = mc.Close()
			}
		}
	}

	b.mu.Lock()
	if b.blobsCache[u] == nil {
		b.blobsCache[u] = make(map[string]*jmapblob.Blob)
	}
	b.blobsCache[u][blobID] = blob
	b.mu.Unlock()

	return blob, nil
}

// RegisterCachedBlob registers an in-memory blob discovered from existing WebDAV files.
func (b *BlobBackend) RegisterCachedBlob(u string, blob *jmapblob.Blob) {
	if blob == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.blobsCache[u] == nil {
		b.blobsCache[u] = make(map[string]*jmapblob.Blob)
	}
	b.blobsCache[u][blob.ID] = blob
}

// GetBlob retrieves a binary blob by ID from cache, WebDAV .blobs/, or an existing FileNode.
func (b *BlobBackend) GetBlob(ctx context.Context, accountID, blobID string) (*jmapblob.Blob, bool, error) {
	u := b.user(ctx, accountID)

	b.mu.RLock()
	if userBlobs, ok := b.blobsCache[u]; ok {
		if blob, ok := userBlobs[blobID]; ok {
			b.mu.RUnlock()
			return blob, true, nil
		}
	}
	b.mu.RUnlock()

	fs, _, err := b.client.WebDAV(ctx)
	if err != nil {
		return nil, false, err
	}

	// 1. Try reading from .blobs/{blobID}
	blobPath := path.Join(".blobs", blobID)
	if rc, err := fs.Open(ctx, blobPath); err == nil {
		data, errRead := io.ReadAll(rc)
		_ = rc.Close()
		if errRead == nil {
			cType := "application/octet-stream"
			// Check .meta
			if mc, err := fs.Open(ctx, blobPath+".meta"); err == nil {
				var meta blobMeta
				if json.NewDecoder(mc).Decode(&meta) == nil && meta.Type != "" {
					cType = meta.Type
				}
				_ = mc.Close()
			}
			blob := &jmapblob.Blob{
				ID:        blobID,
				AccountID: accountID,
				Data:      data,
				Size:      int64(len(data)),
				Type:      cType,
			}
			b.RegisterCachedBlob(u, blob)
			return blob, true, nil
		}
	}

	// 2. Try looking up in FileNodeBackend
	if b.fnBackend != nil {
		filePath, cType, data, err := b.fnBackend.GetFileByBlobID(ctx, blobID)
		if err == nil && data != nil {
			blob := &jmapblob.Blob{
				ID:        blobID,
				AccountID: accountID,
				Data:      data,
				Size:      int64(len(data)),
				Type:      cType,
			}
			b.RegisterCachedBlob(u, blob)
			return blob, true, nil
		} else if filePath != "" {
			if stat, err := fs.Stat(ctx, filePath); err == nil && stat.IsDir {
				return nil, false, fmt.Errorf("cannot read blob from %q: path is a folder, not a file", filePath)
			}
			if rc, err := fs.Open(ctx, filePath); err == nil {
				fileData, errRead := io.ReadAll(rc)
				_ = rc.Close()
				if errRead == nil {
					blob := &jmapblob.Blob{
						ID:        blobID,
						AccountID: accountID,
						Data:      fileData,
						Size:      int64(len(fileData)),
						Type:      cType,
					}
					b.RegisterCachedBlob(u, blob)
					return blob, true, nil
				}
			}
		}
	}

	if b.fallback != nil {
		return b.fallback.GetBlob(ctx, accountID, blobID)
	}

	return nil, false, nil
}

// GetAllBlobs returns all stored blobs for an account.
func (b *BlobBackend) GetAllBlobs(ctx context.Context, accountID string) ([]*jmapblob.Blob, error) {
	u := b.user(ctx, accountID)

	// Ensure files are synced from WebDAV so their blobs are registered
	if b.fnBackend != nil {
		_, _ = b.fnBackend.GetAllFileNodes(ctx)
	}

	// Check if there are blobs in .blobs/ on WebDAV
	if fs, _, err := b.client.WebDAV(ctx); err == nil {
		if fis, err := fs.ReadDir(ctx, ".blobs", false); err == nil {
			for _, fi := range fis {
				if fi.IsDir {
					// Ensure directories inside .blobs are not treated as blobs
					continue
				}
				base := path.Base(fi.Path)
				if base == "" || base == "." || base == ".." || base == ".blobs" || path.Ext(base) == ".meta" {
					continue
				}
				_, _, _ = b.GetBlob(ctx, accountID, base)
			}
		}
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	var list []*jmapblob.Blob
	seen := make(map[string]bool)
	if userBlobs, ok := b.blobsCache[u]; ok {
		for _, bl := range userBlobs {
			list = append(list, bl)
			seen[bl.ID] = true
		}
	}
	if b.fallback != nil {
		if fbList, err := b.fallback.GetAllBlobs(ctx, accountID); err == nil {
			for _, bl := range fbList {
				if bl != nil && !seen[bl.ID] {
					list = append(list, bl)
					seen[bl.ID] = true
				}
			}
		}
	}
	return list, nil
}

// CopyBlob copies a blob from one account to another in Nextcloud WebDAV.
func (b *BlobBackend) CopyBlob(ctx context.Context, fromAccountID, toAccountID string, blobID string) (*jmapblob.Blob, error) {
	if toAccountID == "" {
		return nil, fmt.Errorf("toAccountID is required")
	}
	blob, found, err := b.GetBlob(ctx, fromAccountID, blobID)
	if err != nil {
		return nil, err
	}
	if !found || blob == nil {
		return nil, jmapblob.ErrBlobNotFound
	}
	toUser := b.user(ctx, toAccountID)
	if toUser == "" {
		return nil, fmt.Errorf("invalid toAccountID")
	}
	toCtx := jmapauth.ContextWithAccountID(ctx, toAccountID)
	toCtx = jmapauth.ContextWithSubject(toCtx, toUser)
	toCtx = jmapauth.ContextWithCredentials(toCtx, toUser, toUser)
	return b.PutBlob(toCtx, toAccountID, blob.Type, bytes.Clone(blob.Data))
}

// LookupBlobReferences checks which FileNode objects reference the given blobID.
func (b *BlobBackend) LookupBlobReferences(ctx context.Context, typeNames []string, blobID jmapcore.Id) (map[string][]jmapcore.Id, error) {
	matched := make(map[string][]jmapcore.Id)
	for _, tn := range typeNames {
		switch tn {
		case "FileNode":
			if b.fnBackend != nil {
				nodes, err := b.fnBackend.GetAllFileNodes(ctx)
				if err == nil {
					for _, node := range nodes {
						if node != nil && !node.IsFolder && node.Type != "folder" && node.Type != "directory" && node.BlobID != nil && *node.BlobID == blobID {
							matched["FileNode"] = append(matched["FileNode"], node.ID)
						}
					}
				}
			}
		}
	}
	if b.fallback != nil {
		if refBackend, ok := b.fallback.(jmapblob.BlobReferenceBackend); ok {
			if fbRefs, err := refBackend.LookupBlobReferences(ctx, typeNames, blobID); err == nil {
				for tn, ids := range fbRefs {
					matched[tn] = append(matched[tn], ids...)
				}
			}
		}
	}
	return matched, nil
}
