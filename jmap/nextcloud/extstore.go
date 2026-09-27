package nextcloud

import (
	"context"
	"io"
	"path"

	"imap-jmap/jmap/jmapextstore"
)

// WebDAVExtensionStore implements jmapextstore.Store by storing JSON state in .jmap/<key>.json on WebDAV.
type WebDAVExtensionStore struct {
	client *Client
}

// NewWebDAVExtensionStore creates an extension store connected to the Nextcloud WebDAV client.
func NewWebDAVExtensionStore(client *Client) *WebDAVExtensionStore {
	return &WebDAVExtensionStore{client: client}
}

var _ jmapextstore.Store = (*WebDAVExtensionStore)(nil)

func (s *WebDAVExtensionStore) filePath(accountID, key string) string {
	if accountID != "" {
		return path.Join(".jmap", accountID, key+".json")
	}
	return path.Join(".jmap", key+".json")
}

func (s *WebDAVExtensionStore) Get(ctx context.Context, accountID, key string) ([]byte, error) {
	fs, _, err := s.client.WebDAV(ctx)
	if err != nil {
		return nil, err
	}
	rc, err := fs.Open(ctx, s.filePath(accountID, key))
	if err != nil {
		// Not found
		return nil, nil
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func (s *WebDAVExtensionStore) Put(ctx context.Context, accountID, key string, data []byte) error {
	fs, _, err := s.client.WebDAV(ctx)
	if err != nil {
		return err
	}
	_ = fs.Mkdir(ctx, ".jmap")
	if accountID != "" {
		_ = fs.Mkdir(ctx, path.Join(".jmap", accountID))
	}
	wc, err := fs.Create(ctx, s.filePath(accountID, key))
	if err != nil {
		return err
	}
	if _, err := wc.Write(data); err != nil {
		_ = wc.Close()
		return err
	}
	return wc.Close()
}

func (s *WebDAVExtensionStore) Delete(ctx context.Context, accountID, key string) error {
	fs, _, err := s.client.WebDAV(ctx)
	if err != nil {
		return err
	}
	return fs.RemoveAll(ctx, s.filePath(accountID, key))
}
