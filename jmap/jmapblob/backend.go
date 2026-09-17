package jmapblob

import (
	"context"
	"errors"

	"imap-jmap/jmap/jmapcore"
)

// ErrBlobNotFound indicates the blob referenced by an MDN/parse or Email/import
// request does not exist for the given account, per RFC 9007 Section 2.2.
var ErrBlobNotFound = errors.New("blob not found")

// BlobBackend defines the storage interface for binary blobs per RFC 8620 Section 6 and RFC 9404.
type BlobBackend interface {
	PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*Blob, error)
	GetBlob(ctx context.Context, accountID, blobID string) (*Blob, bool, error)
	GetAllBlobs(ctx context.Context, accountID string) ([]*Blob, error)
	CopyBlob(ctx context.Context, fromAccountID, toAccountID string, blobID string) (*Blob, error)
}

// BlobReferenceBackend performs the reverse lookup of which typed objects reference a blob,
// per RFC 9404 Section 4.3. Implemented by the data store that holds the referencing types.
type BlobReferenceBackend interface {
	LookupBlobReferences(ctx context.Context, typeNames []string, blobID jmapcore.Id) (map[string][]jmapcore.Id, error)
}
