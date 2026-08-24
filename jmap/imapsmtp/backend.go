package imapsmtp

import (
	"context"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// IMAPSMTPBackend implements jmap.MailBackend and jmap.BlobBackend using external IMAP and SMTP servers.
type IMAPSMTPBackend struct {
	imapHost string
	smtpHost string
	pool     *ClientPool

	// lastSweep tracks when blob staging was last swept per account so the lazy
	// sweep on read paths runs at most every blobStagingSweepInterval. The sweep
	// only ever touches the account currently authenticated in the request
	// context — the gateway has no shared or administrative IMAP credentials.
	sweepMu   sync.Mutex
	lastSweep map[string]time.Time
}

var _ jmap.MailBackend = (*IMAPSMTPBackend)(nil)
var _ jmap.BlobBackend = (*IMAPSMTPBackend)(nil)

// New creates a new IMAP/SMTP gateway backend.
func New(imapHost, smtpHost string) *IMAPSMTPBackend {
	return &IMAPSMTPBackend{
		imapHost:  imapHost,
		smtpHost:  smtpHost,
		pool:      NewClientPoolWithSMTP(imapHost, smtpHost),
		lastSweep: make(map[string]time.Time),
	}
}

// Pool returns the underlying ClientPool.
func (b *IMAPSMTPBackend) Pool() *ClientPool {
	return b.pool
}

// Quotas (RFC 9425 Section 4)
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

// Identities (RFC 8621 Section 6)
func (b *IMAPSMTPBackend) IdentityState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) IdentityChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return nil, nil, nil, "1", false
}

func (b *IMAPSMTPBackend) GetIdentities(ctx context.Context) ([]*jmap.Identity, error) {
	email := "user@example.com"
	if subject, ok := jmap.SubjectFromContext(ctx); ok && subject != "" {
		email = subject
	} else if accountID, ok := jmap.AccountIDFromContext(ctx); ok {
		if sub, ok := jmap.SubjectForAccountID(accountID); ok {
			email = sub
		}
	}

	return []*jmap.Identity{
		{
			ID:       "id-1",
			Name:     email,
			Email:    email,
			MayDelete: false,
		},
	}, nil
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

// VacationResponse is a per-account singleton per RFC 8621 Section 8.
func (b *IMAPSMTPBackend) VacationResponseState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) GetVacationResponse(ctx context.Context) (*jmap.VacationResponse, error) {
	return &jmap.VacationResponse{ID: "singleton"}, nil
}

func (b *IMAPSMTPBackend) UpdateVacationResponse(ctx context.Context, patch map[string]any) (*jmap.VacationResponse, error) {
	return &jmap.VacationResponse{ID: "singleton"}, nil
}

// MDN (RFC 9007 Section 3)
func (b *IMAPSMTPBackend) SendMDN(ctx context.Context, mdn *jmap.MDN) (*jmap.MDN, error) {
	return mdn, nil
}

func (b *IMAPSMTPBackend) ParseMDN(ctx context.Context, blobID jmap.Id) (*jmap.MDN, error) {
	return &jmap.MDN{}, nil
}

// PushSubscription (RFC 8620 Section 7.2)
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
