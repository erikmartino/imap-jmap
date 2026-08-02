package jmap

import "context"

// MailBackend defines the storage interface for JMAP Mail & Quota resources per RFC 8621, RFC 9219, & RFC 9425.
type MailBackend interface {
	// State returns the current change state token for mail data.
	State(ctx context.Context) string

	// Mailboxes (RFC 8621 Section 2)
	GetMailboxes(ctx context.Context, ids []Id) (list []*Mailbox, notFound []Id, err error)
	GetAllMailboxes(ctx context.Context) ([]*Mailbox, error)
	CreateMailbox(ctx context.Context, mb *Mailbox) (*Mailbox, error)
	DeleteMailbox(ctx context.Context, id Id) (bool, error)

	// Threads (RFC 8621 Section 3)
	GetThreads(ctx context.Context, ids []Id) (list []*Thread, notFound []Id, err error)

	// Emails (RFC 8621 Section 4)
	GetEmails(ctx context.Context, ids []Id) (list []*Email, notFound []Id, err error)
	GetAllEmails(ctx context.Context) ([]*Email, error)
	CreateEmail(ctx context.Context, em *Email) (*Email, error)
	UpdateEmail(ctx context.Context, id Id, patch map[string]any) (*Email, error)
	DeleteEmail(ctx context.Context, id Id) (bool, error)
	QueryEmails(ctx context.Context, filter map[string]any, comparators []Comparator, position int, limit *uint64) (ids []Id, total int, err error)

	// S/MIME Verification (RFC 9219 Section 4)
	VerifySmime(ctx context.Context, ids []Id) (verified map[Id]*SmimeVerificationResult, notFound []Id, err error)

	// Quotas (RFC 9425 Section 4)
	GetQuotas(ctx context.Context, ids []Id) (list []*Quota, notFound []Id, err error)
	GetAllQuotas(ctx context.Context) ([]*Quota, error)

	// Identities (RFC 8621 Section 6)
	GetIdentities(ctx context.Context) ([]*Identity, error)

	// Submissions (RFC 8621 Section 7)
	CreateSubmission(ctx context.Context, sub *EmailSubmission) (*EmailSubmission, error)
	GetSubmissions(ctx context.Context, ids []Id) (list []*EmailSubmission, notFound []Id, err error)

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

// BlobBackend defines the storage interface for binary blobs per RFC 8620 Section 6.
type BlobBackend interface {
	PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*Blob, error)
	GetBlob(ctx context.Context, accountID, blobID string) (*Blob, bool, error)
}
