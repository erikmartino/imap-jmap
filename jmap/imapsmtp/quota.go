package imapsmtp

import (
	"context"

	"imap-jmap/jmap"
)

// Quotas (RFC 9425 Section 4)

func (b *IMAPSMTPBackend) QuotaState(ctx context.Context) string {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getQuotaTracker(accountID).State()
}

func (b *IMAPSMTPBackend) QuotaChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getQuotaTracker(accountID).Changes(sinceState, maxChanges)
}

func (b *IMAPSMTPBackend) GetQuotas(ctx context.Context, ids []jmap.Id) ([]*jmap.Quota, []jmap.Id, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	all := b.getAccountQuotas(accountID)
	allMap := make(map[jmap.Id]*jmap.Quota, len(all))
	for _, q := range all {
		allMap[q.ID] = q
	}
	var found []*jmap.Quota
	var notFound []jmap.Id
	for _, id := range ids {
		if q, ok := allMap[id]; ok {
			found = append(found, q)
		} else {
			notFound = append(notFound, id)
		}
	}
	return found, notFound, nil
}

func (b *IMAPSMTPBackend) GetAllQuotas(ctx context.Context) ([]*jmap.Quota, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getAccountQuotas(accountID), nil
}
