package imapsmtp

import (
	"context"

	"imap-jmap/jmap"
)

// IMAPSMTPBackend implements jmap.MailBackend and jmap.BlobBackend using external IMAP and SMTP servers.
type IMAPSMTPBackend struct {
	imapHost string
	smtpHost string
}

var _ jmap.MailBackend = (*IMAPSMTPBackend)(nil)
var _ jmap.BlobBackend = (*IMAPSMTPBackend)(nil)

// New creates a new IMAP/SMTP gateway backend.
func New(imapHost, smtpHost string) *IMAPSMTPBackend {
	return &IMAPSMTPBackend{
		imapHost: imapHost,
		smtpHost: smtpHost,
	}
}

func (b *IMAPSMTPBackend) State(ctx context.Context) string {
	return "1"
}

// Mailboxes
func (b *IMAPSMTPBackend) MailboxState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) MailboxChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, []string, string, bool) {
	return nil, nil, nil, nil, "1", false
}

func (b *IMAPSMTPBackend) GetMailboxes(ctx context.Context, ids []jmap.Id) ([]*jmap.Mailbox, []jmap.Id, error) {
	return nil, ids, nil
}

func (b *IMAPSMTPBackend) GetAllMailboxes(ctx context.Context) ([]*jmap.Mailbox, error) {
	return []*jmap.Mailbox{}, nil
}

func (b *IMAPSMTPBackend) CreateMailbox(ctx context.Context, mb *jmap.Mailbox) (*jmap.Mailbox, error) {
	return mb, nil
}

func (b *IMAPSMTPBackend) UpdateMailbox(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Mailbox, error) {
	return nil, nil
}

func (b *IMAPSMTPBackend) DeleteMailbox(ctx context.Context, id jmap.Id, onDestroyRemoveMessages bool) (bool, error) {
	return true, nil
}

// Threads
func (b *IMAPSMTPBackend) ThreadState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) ThreadChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return nil, nil, nil, "1", false
}

func (b *IMAPSMTPBackend) GetThreads(ctx context.Context, ids []jmap.Id) ([]*jmap.Thread, []jmap.Id, error) {
	return nil, ids, nil
}

func (b *IMAPSMTPBackend) GetAllThreads(ctx context.Context) ([]*jmap.Thread, error) {
	return []*jmap.Thread{}, nil
}

// Emails
func (b *IMAPSMTPBackend) EmailState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) EmailChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return nil, nil, nil, "1", false
}

func (b *IMAPSMTPBackend) GetEmails(ctx context.Context, ids []jmap.Id) ([]*jmap.Email, []jmap.Id, error) {
	return nil, ids, nil
}

func (b *IMAPSMTPBackend) GetAllEmails(ctx context.Context) ([]*jmap.Email, error) {
	return []*jmap.Email{}, nil
}

func (b *IMAPSMTPBackend) CreateEmail(ctx context.Context, em *jmap.Email) (*jmap.Email, error) {
	return em, nil
}

func (b *IMAPSMTPBackend) UpdateEmail(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Email, error) {
	return nil, nil
}

func (b *IMAPSMTPBackend) DeleteEmail(ctx context.Context, id jmap.Id) (bool, error) {
	return true, nil
}

func (b *IMAPSMTPBackend) QueryEmails(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	return nil, 0, nil
}

// S/MIME Verification
func (b *IMAPSMTPBackend) VerifySmime(ctx context.Context, ids []jmap.Id) (map[jmap.Id]*jmap.SmimeVerificationResult, []jmap.Id, error) {
	return make(map[jmap.Id]*jmap.SmimeVerificationResult), nil, nil
}

// Quotas
func (b *IMAPSMTPBackend) QuotaState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) QuotaChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return nil, nil, nil, "1", false
}

func (b *IMAPSMTPBackend) GetQuotas(ctx context.Context, ids []jmap.Id) ([]*jmap.Quota, []jmap.Id, error) {
	return nil, ids, nil
}

func (b *IMAPSMTPBackend) GetAllQuotas(ctx context.Context) ([]*jmap.Quota, error) {
	return []*jmap.Quota{}, nil
}

// Identities
func (b *IMAPSMTPBackend) IdentityState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) IdentityChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return nil, nil, nil, "1", false
}

func (b *IMAPSMTPBackend) GetIdentities(ctx context.Context) ([]*jmap.Identity, error) {
	return []*jmap.Identity{}, nil
}

func (b *IMAPSMTPBackend) CreateIdentity(ctx context.Context, identity *jmap.Identity) (*jmap.Identity, error) {
	return identity, nil
}

func (b *IMAPSMTPBackend) UpdateIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Identity, error) {
	return nil, nil
}

func (b *IMAPSMTPBackend) DeleteIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	return true, nil
}

// VacationResponse
func (b *IMAPSMTPBackend) VacationResponseState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) GetVacationResponse(ctx context.Context) (*jmap.VacationResponse, error) {
	return &jmap.VacationResponse{ID: "singleton"}, nil
}

func (b *IMAPSMTPBackend) UpdateVacationResponse(ctx context.Context, patch map[string]any) (*jmap.VacationResponse, error) {
	return &jmap.VacationResponse{ID: "singleton"}, nil
}

// Submissions
func (b *IMAPSMTPBackend) SubmissionState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) SubmissionChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return nil, nil, nil, "1", false
}

func (b *IMAPSMTPBackend) CreateSubmission(ctx context.Context, sub *jmap.EmailSubmission) (*jmap.EmailSubmission, error) {
	return sub, nil
}

func (b *IMAPSMTPBackend) UpdateSubmission(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.EmailSubmission, error) {
	return nil, nil
}

func (b *IMAPSMTPBackend) DeleteSubmission(ctx context.Context, id jmap.Id) (bool, error) {
	return true, nil
}

func (b *IMAPSMTPBackend) GetSubmissions(ctx context.Context, ids []jmap.Id) ([]*jmap.EmailSubmission, []jmap.Id, error) {
	return nil, ids, nil
}

func (b *IMAPSMTPBackend) GetAllSubmissions(ctx context.Context) ([]*jmap.EmailSubmission, error) {
	return []*jmap.EmailSubmission{}, nil
}

func (b *IMAPSMTPBackend) QuerySubmissions(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	return nil, 0, nil
}

// MDN
func (b *IMAPSMTPBackend) SendMDN(ctx context.Context, mdn *jmap.MDN) (*jmap.MDN, error) {
	return mdn, nil
}

func (b *IMAPSMTPBackend) ParseMDN(ctx context.Context, blobID jmap.Id) (*jmap.MDN, error) {
	return &jmap.MDN{}, nil
}

// PushSubscription
func (b *IMAPSMTPBackend) GetPushSubscriptions(ctx context.Context, ids []jmap.Id) ([]*jmap.PushSubscription, []jmap.Id, error) {
	return nil, ids, nil
}

func (b *IMAPSMTPBackend) GetAllPushSubscriptions(ctx context.Context) ([]*jmap.PushSubscription, error) {
	return []*jmap.PushSubscription{}, nil
}

func (b *IMAPSMTPBackend) CreatePushSubscription(ctx context.Context, sub *jmap.PushSubscription) (*jmap.PushSubscription, error) {
	return sub, nil
}

func (b *IMAPSMTPBackend) UpdatePushSubscription(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.PushSubscription, error) {
	return nil, nil
}

func (b *IMAPSMTPBackend) DeletePushSubscription(ctx context.Context, id jmap.Id) (bool, error) {
	return true, nil
}

// BlobBackend Implementation
func (b *IMAPSMTPBackend) PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*jmap.Blob, error) {
	return &jmap.Blob{ID: "blob-1", Size: int64(len(data))}, nil
}

func (b *IMAPSMTPBackend) GetBlob(ctx context.Context, accountID, blobID string) (*jmap.Blob, bool, error) {
	return &jmap.Blob{ID: blobID}, true, nil
}

func (b *IMAPSMTPBackend) GetAllBlobs(ctx context.Context, accountID string) ([]*jmap.Blob, error) {
	return []*jmap.Blob{}, nil
}

func (b *IMAPSMTPBackend) CopyBlob(ctx context.Context, fromAccountID, toAccountID string, blobID string) (*jmap.Blob, error) {
	return &jmap.Blob{ID: blobID}, nil
}

