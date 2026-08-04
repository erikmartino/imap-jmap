package jmap

import (
	"context"
	"errors"
)

// ErrBlobNotFound indicates the blob referenced by an MDN/parse or Email/import
// request does not exist for the given account, per RFC 9007 Section 2.2.
var ErrBlobNotFound = errors.New("blob not found")

// MailBackend defines the storage interface for JMAP Mail & Quota resources per RFC 8621, RFC 9219, & RFC 9425.
type MailBackend interface {
	// State returns the current change state token for mail data.
	State(ctx context.Context) string

	// Mailboxes (RFC 8621 Section 2)
	MailboxState(ctx context.Context) string
	MailboxChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetMailboxes(ctx context.Context, ids []Id) (list []*Mailbox, notFound []Id, err error)
	GetAllMailboxes(ctx context.Context) ([]*Mailbox, error)
	CreateMailbox(ctx context.Context, mb *Mailbox) (*Mailbox, error)
	UpdateMailbox(ctx context.Context, id Id, patch map[string]any) (*Mailbox, error)
	DeleteMailbox(ctx context.Context, id Id) (bool, error)

	// Threads (RFC 8621 Section 3)
	ThreadState(ctx context.Context) string
	ThreadChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetThreads(ctx context.Context, ids []Id) (list []*Thread, notFound []Id, err error)
	GetAllThreads(ctx context.Context) ([]*Thread, error)

	// Emails (RFC 8621 Section 4)
	EmailState(ctx context.Context) string
	EmailChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetEmails(ctx context.Context, ids []Id) (list []*Email, notFound []Id, err error)
	GetAllEmails(ctx context.Context) ([]*Email, error)
	CreateEmail(ctx context.Context, em *Email) (*Email, error)
	UpdateEmail(ctx context.Context, id Id, patch map[string]any) (*Email, error)
	DeleteEmail(ctx context.Context, id Id) (bool, error)
	QueryEmails(ctx context.Context, filter map[string]any, comparators []Comparator, position int, limit *uint64) (ids []Id, total int, err error)

	// S/MIME Verification (RFC 9219 Section 4)
	VerifySmime(ctx context.Context, ids []Id) (verified map[Id]*SmimeVerificationResult, notFound []Id, err error)

	// Quotas (RFC 9425 Section 4)
	QuotaState(ctx context.Context) string
	QuotaChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetQuotas(ctx context.Context, ids []Id) (list []*Quota, notFound []Id, err error)
	GetAllQuotas(ctx context.Context) ([]*Quota, error)

	// Identities (RFC 8621 Section 6)
	IdentityState(ctx context.Context) string
	IdentityChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetIdentities(ctx context.Context) ([]*Identity, error)
	CreateIdentity(ctx context.Context, identity *Identity) (*Identity, error)
	UpdateIdentity(ctx context.Context, id Id, patch map[string]any) (*Identity, error)
	DeleteIdentity(ctx context.Context, id Id) (bool, error)

	// Submissions (RFC 8621 Section 7)
	SubmissionState(ctx context.Context) string
	SubmissionChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	CreateSubmission(ctx context.Context, sub *EmailSubmission) (*EmailSubmission, error)
	DeleteSubmission(ctx context.Context, id Id) (bool, error)
	GetSubmissions(ctx context.Context, ids []Id) (list []*EmailSubmission, notFound []Id, err error)
	GetAllSubmissions(ctx context.Context) ([]*EmailSubmission, error)
	QuerySubmissions(ctx context.Context, filter map[string]any, comparators []Comparator, position int, limit *uint64) ([]Id, int, error)

	// MDN (RFC 9007 Section 3)
	SendMDN(ctx context.Context, mdn *MDN) (*MDN, error)
	ParseMDN(ctx context.Context, blobID Id) (*MDN, error)

	// PushSubscription (RFC 8620 Section 7.2)
	GetPushSubscriptions(ctx context.Context, ids []Id) (list []*PushSubscription, notFound []Id, err error)
	GetAllPushSubscriptions(ctx context.Context) ([]*PushSubscription, error)
	CreatePushSubscription(ctx context.Context, sub *PushSubscription) (*PushSubscription, error)
	UpdatePushSubscription(ctx context.Context, id Id, patch map[string]any) (*PushSubscription, error)
	DeletePushSubscription(ctx context.Context, id Id) (bool, error)
}

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
	LookupBlobReferences(ctx context.Context, typeNames []string, blobID Id) (map[string][]Id, error)
}

// ContactsBackend defines the storage interface for JMAP Contacts resources per RFC 9610.
type ContactsBackend interface {
	// AddressBooks (RFC 9610 Section 2)
	AddressBookState(ctx context.Context) string
	AddressBookChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetAddressBooks(ctx context.Context, ids []Id) (list []*AddressBook, notFound []Id, err error)
	GetAllAddressBooks(ctx context.Context) ([]*AddressBook, error)
	CreateAddressBook(ctx context.Context, ab *AddressBook) (*AddressBook, error)
	UpdateAddressBook(ctx context.Context, id Id, patch map[string]any) (*AddressBook, error)
	DeleteAddressBook(ctx context.Context, id Id) (bool, error)

	// Cards (RFC 9610 Section 3)
	CardState(ctx context.Context) string
	CardChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetCards(ctx context.Context, ids []Id) (list []*Card, notFound []Id, err error)
	GetAllCards(ctx context.Context) ([]*Card, error)
	CreateCard(ctx context.Context, card *Card) (*Card, error)
	UpdateCard(ctx context.Context, id Id, patch map[string]any) (*Card, error)
	DeleteCard(ctx context.Context, id Id) (bool, error)
	QueryCards(ctx context.Context, filter map[string]any, position int, limit *uint64) (ids []Id, total int, err error)
}

// CalendarsBackend defines the storage interface for JMAP Calendars & JSCalendar (RFC 8984) resources.
type CalendarsBackend interface {
	// Calendars
	CalendarState(ctx context.Context) string
	CalendarChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetCalendars(ctx context.Context, ids []Id) (list []*Calendar, notFound []Id, err error)
	GetAllCalendars(ctx context.Context) ([]*Calendar, error)
	CreateCalendar(ctx context.Context, cal *Calendar) (*Calendar, error)
	UpdateCalendar(ctx context.Context, id Id, patch map[string]any) (*Calendar, error)
	DeleteCalendar(ctx context.Context, id Id) (bool, error)

	// CalendarEvents (JSCalendar RFC 8984)
	CalendarEventState(ctx context.Context) string
	CalendarEventChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetCalendarEvents(ctx context.Context, ids []Id) (list []*CalendarEvent, notFound []Id, err error)
	GetAllCalendarEvents(ctx context.Context) ([]*CalendarEvent, error)
	CreateCalendarEvent(ctx context.Context, event *CalendarEvent) (*CalendarEvent, error)
	UpdateCalendarEvent(ctx context.Context, id Id, patch map[string]any) (*CalendarEvent, error)
	DeleteCalendarEvent(ctx context.Context, id Id) (bool, error)
	QueryCalendarEvents(ctx context.Context, filter map[string]any, sort []Comparator, position int, limit *uint64, expandRecurrences bool) (ids []Id, total int, err error)
}

// SieveBackend defines the storage interface for JMAP for Sieve Scripts (RFC 9661) resources.
type SieveBackend interface {
	SieveScriptState(ctx context.Context) string
	SieveScriptChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMoreChanges bool)
	GetSieveScripts(ctx context.Context, ids []Id) (list []*SieveScript, notFound []Id, err error)
	GetAllSieveScripts(ctx context.Context) ([]*SieveScript, error)
	CreateSieveScript(ctx context.Context, script *SieveScript) (*SieveScript, error)
	UpdateSieveScript(ctx context.Context, id Id, patch map[string]any) (*SieveScript, error)
	DeleteSieveScript(ctx context.Context, id Id) (bool, error)
	QuerySieveScripts(ctx context.Context, filter map[string]any, position int, limit *uint64) (ids []Id, total int, err error)
	ValidateSieveScript(ctx context.Context, content string) (isValid bool, errDetail string)
}

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
