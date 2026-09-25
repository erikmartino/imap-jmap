package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

const (
	MaxEmailRawSize     = jmapmail.MaxEmailRawSize
	MaxMIMENestingDepth = jmapmail.MaxMIMENestingDepth
	MaxMIMEParts        = jmapmail.MaxMIMEParts
)

// ParseRFC822 parses a raw RFC 5322 MIME message into a JMAP Email object (RFC 8621 Section 4.1).
func ParseRFC822(raw []byte, blobBackend ...BlobBackend) (*Email, error) {
	return jmapmail.ParseRFC822(raw, blobBackend...)
}

func parseRFC822(raw []byte, blobBackend ...BlobBackend) (*Email, error) {
	return jmapmail.ParseRFC822(raw, blobBackend...)
}

func parseRFC822WithAccount(accountID string, raw []byte, blobBackend ...BlobBackend) (*Email, error) {
	return jmapmail.ParseRFC822WithAccount(accountID, raw, blobBackend...)
}

// ParseRFC822WithAccount parses an RFC 5322 MIME message into a JMAP Email object with optional blob storage.
func ParseRFC822WithAccount(accountID string, raw []byte, blobBackend ...BlobBackend) (*Email, error) {
	return jmapmail.ParseRFC822WithAccount(accountID, raw, blobBackend...)
}

// FormatEmailRFC822 serializes an Email object to raw RFC 5322 MIME message bytes.
func FormatEmailRFC822(em *Email) []byte {
	return jmapmail.FormatEmailRFC822(em)
}

// ExtractBlobFromRFC822 walks an RFC 822 MIME message and extracts the body bytes
// and content-type of the part whose SHA-256 hash matches targetBlobID.
func ExtractBlobFromRFC822(raw []byte, targetBlobID string) ([]byte, string, bool) {
	return jmapmail.ExtractBlobFromRFC822(raw, targetBlobID)
}

// GenerateMessageID creates an RFC 5322 Section 3.6.4 compliant Message-ID
// value without enclosing angle brackets (in conformance with JMAP RFC 8621 Section 4.1.2).
func GenerateMessageID(domainOrAddress string) string {
	return jmapmail.GenerateMessageID(domainOrAddress)
}

// HasValidMessageID reports whether the message data carries a Message-ID header
// field whose value conforms to the RFC 5322 Section 3.6.4 msg-id syntax.
func HasValidMessageID(data []byte) bool {
	return jmapmail.HasValidMessageID(data)
}

// EnsureValidMessageID inspects raw RFC 822/5322 message bytes for a valid Message-ID header.
// If missing or syntactically invalid, it adds or replaces the Message-ID field
// in conformance with RFC 6409 Section 8.3 and RFC 5322 Section 3.6.4.
func EnsureValidMessageID(data []byte, domainOrAddress string) []byte {
	return jmapmail.EnsureValidMessageID(data, domainOrAddress)
}

var (
	ExtractBodyStructureParts = jmapmail.ExtractBodyStructureParts
	extractBodyStructureParts = jmapmail.ExtractBodyStructureParts
	Preview                   = jmapmail.Preview
	preview                   = jmapmail.Preview
)
