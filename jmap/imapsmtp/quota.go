package imapsmtp

import (
	"context"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

// Quotas (RFC 9425 Section 4)

func (b *IMAPSMTPBackend) QuotaState(ctx context.Context) string {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	return b.getQuotaTracker(accountID).State()
}

func (b *IMAPSMTPBackend) QuotaChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	return b.getQuotaTracker(accountID).Changes(sinceState, maxChanges)
}

func (b *IMAPSMTPBackend) GetQuotas(ctx context.Context, ids []jmapcore.Id) ([]*jmapmail.Quota, []jmapcore.Id, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	all := b.getAccountQuotas(accountID)
	allMap := make(map[jmapcore.Id]*jmapmail.Quota, len(all))
	for _, q := range all {
		allMap[q.ID] = q
	}
	var found []*jmapmail.Quota
	var notFound []jmapcore.Id
	for _, id := range ids {
		if q, ok := allMap[id]; ok {
			found = append(found, q)
		} else {
			notFound = append(notFound, id)
		}
	}
	return found, notFound, nil
}

func (b *IMAPSMTPBackend) GetAllQuotas(ctx context.Context) ([]*jmapmail.Quota, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	return b.getAccountQuotas(accountID), nil
}
