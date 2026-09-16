package jmapblob

import (
	"imap-jmap/jmap/jmapcore"
)

// Blob represents a stored binary blob per RFC 8620 Section 6 and RFC 9404 Section 4.
// @spec RFC8620#6-p1-MUST
type Blob struct {
	ID           string `json:"id"`
	BlobID       string `json:"blobId,omitempty"`
	AccountID    string `json:"accountId,omitempty"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`
	DigestSHA256 string `json:"digest:sha-256,omitempty"`
	Data         []byte `json:"-"`
}

type Id = jmapcore.Id
