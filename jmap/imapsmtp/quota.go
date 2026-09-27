package imapsmtp

import (
	"context"
	"strings"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmappush"
)

// Quotas (RFC 9425 Section 4)

func quotaStateMap(items []*jmapmail.Quota) map[string]string {
	fps := make(map[string]string, len(items))
	for _, it := range items {
		if it != nil {
			fps[string(it.ID)] = jmappush.ObjectFingerprint(it)
		}
	}
	return fps
}

func (b *IMAPSMTPBackend) QuotaState(ctx context.Context) string {
	all, err := b.GetAllQuotas(ctx)
	if err != nil {
		accountID, _ := jmapauth.AccountIDFromContext(ctx)
		return b.getQuotaTracker(accountID).State()
	}
	return jmappush.EncodeStateVector("quota-v1:", quotaStateMap(all))
}

func (b *IMAPSMTPBackend) QuotaChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	if !strings.HasPrefix(sinceState, "quota-v1:") {
		return b.getQuotaTracker(accountID).Changes(sinceState, maxChanges)
	}
	old, err := jmappush.DecodeStateVector("quota-v1:", sinceState)
	if err != nil {
		return nil, nil, nil, "", false
	}
	all, err := b.GetAllQuotas(ctx)
	if err != nil {
		return nil, nil, nil, "", false
	}
	cur := quotaStateMap(all)
	newState := jmappush.EncodeStateVector("quota-v1:", cur)
	created, updated, destroyed := jmappush.DiffStateVectors(old, cur)
	return created, updated, destroyed, newState, false
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
