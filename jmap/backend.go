package jmap

import (
	"context"

	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcontacts"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmapprincipals"
	"imap-jmap/jmap/jmapsieve"
)

// ErrBlobNotFound indicates the blob referenced by an MDN/parse or Email/import
// request does not exist for the given account, per RFC 9007 Section 2.2.
var ErrBlobNotFound = jmapblob.ErrBlobNotFound

// MailBackend defines the storage interface for JMAP Mail & Quota resources per RFC 8621, RFC 9219, & RFC 9425.
type MailBackend = jmapmail.MailBackend

// SMTPAvailableBackend is an optional interface that MailBackend implementations can fulfill
// to indicate whether an outer SMTP server is available for outbound message dispatch.
type SMTPAvailableBackend = jmapmail.SMTPAvailableBackend

// BlobBackend defines the storage interface for binary blobs per RFC 8620 Section 6 and RFC 9404.
type BlobBackend = jmapblob.BlobBackend

// BlobReferenceBackend performs the reverse lookup of which typed objects reference a blob,
// per RFC 9404 Section 4.3. Implemented by the data store that holds the referencing types.
type BlobReferenceBackend = jmapblob.BlobReferenceBackend

// OutboundDeliveryResult is the outcome of delivering a raw message to one external recipient.
type OutboundDeliveryResult = jmapmail.OutboundDeliveryResult

// OutboundMailSender delivers a raw RFC 5322 message to external recipients.
type OutboundMailSender = jmapmail.OutboundMailSender

// ContactsBackend defines the storage interface for JMAP Contacts resources per RFC 9610.
// The canonical definition lives in jmapcontacts; this is a type alias for backward compatibility.
type ContactsBackend = jmapcontacts.ContactsBackend

// CalendarsBackend defines the storage interface for JMAP Calendars & JSCalendar (RFC 8984) resources.
// The canonical definition lives in jmapcalendar; this is a type alias for backward compatibility.
type CalendarsBackend = jmapcalendar.CalendarsBackend

// SieveBackend defines the storage interface for JMAP for Sieve Scripts (RFC 9661) resources.
// The canonical definition lives in jmapsieve; this is a type alias for backward compatibility.
type SieveBackend = jmapsieve.SieveBackend

// FileNodeBackend defines the storage interface for the JMAP FileNode file storage extension.
type FileNodeBackend interface {
	FileNodeState(ctx context.Context) string
	FileNodeChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetFileNodes(ctx context.Context, ids []Id) (list []*FileNode, notFound []Id, err error)
	GetAllFileNodes(ctx context.Context) ([]*FileNode, error)
	CreateFileNode(ctx context.Context, node *FileNode) (*FileNode, error)
	UpdateFileNode(ctx context.Context, id Id, patch map[string]any) (*FileNode, error)
	DeleteFileNode(ctx context.Context, id Id) (bool, error)
	QueryFileNodes(ctx context.Context, filter map[string]any, position int, limit *uint64) (ids []Id, total int, err error)
}

// IMAPAccessBackend defines the storage interface for JMAPACCESS Extension for IMAP (RFC 9698) resources.
type IMAPAccessBackend interface {
	GetIMAPAccounts(ctx context.Context, ids []Id) (list []*IMAPAccount, notFound []Id, err error)
	GetAllIMAPAccounts(ctx context.Context) ([]*IMAPAccount, error)
	CreateIMAPAccount(ctx context.Context, account *IMAPAccount) (*IMAPAccount, error)
	UpdateIMAPAccount(ctx context.Context, id Id, patch map[string]any) (*IMAPAccount, error)
	DeleteIMAPAccount(ctx context.Context, id Id) (bool, error)
	State(ctx context.Context) string
}

// PrincipalsBackend defines the storage interface for JMAP Principals & Availability (draft-ietf-jmap-principals).
type PrincipalsBackend = jmapprincipals.PrincipalsBackend
