package imapsmtp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

// PushSubscription (RFC 8620 Section 7.2)

func (b *IMAPSMTPBackend) GetPushSubscriptions(ctx context.Context, ids []jmapcore.Id) ([]*jmapmail.PushSubscription, []jmapcore.Id, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.pushMu.RLock()
	defer b.pushMu.RUnlock()
	m := b.pushSubscriptions[accountID]
	var found []*jmapmail.PushSubscription
	var notFound []jmapcore.Id
	for _, id := range ids {
		if sub, ok := m[id]; ok {
			found = append(found, sub)
		} else {
			notFound = append(notFound, id)
		}
	}
	return found, notFound, nil
}

func (b *IMAPSMTPBackend) GetAllPushSubscriptions(ctx context.Context) ([]*jmapmail.PushSubscription, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.pushMu.RLock()
	defer b.pushMu.RUnlock()
	m := b.pushSubscriptions[accountID]
	var list []*jmapmail.PushSubscription
	for _, sub := range m {
		list = append(list, sub)
	}
	return list, nil
}

func (b *IMAPSMTPBackend) CreatePushSubscription(ctx context.Context, sub *jmapmail.PushSubscription) (*jmapmail.PushSubscription, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	if sub.ID == "" {
		sub.ID = jmapcore.Id(fmt.Sprintf("push-%d", time.Now().UnixNano()))
	}
	// High-entropy verification code per RFC 8620 §8.6
	randBuf := make([]byte, 32)
	_, _ = rand.Read(randBuf)
	vCode := hex.EncodeToString(randBuf)
	sub.VerificationCode = &vCode

	b.pushMu.Lock()
	defer b.pushMu.Unlock()
	if b.pushSubscriptions == nil {
		b.pushSubscriptions = make(map[string]map[jmapcore.Id]*jmapmail.PushSubscription)
	}
	if b.pushSubscriptions[accountID] == nil {
		b.pushSubscriptions[accountID] = make(map[jmapcore.Id]*jmapmail.PushSubscription)
	}
	// Limit maximum push subscriptions per user (RFC 8620 §8.6)
	const maxPushSubscriptionsPerAccount = 50
	if len(b.pushSubscriptions[accountID]) >= maxPushSubscriptionsPerAccount {
		return nil, fmt.Errorf("maximum limit of push subscriptions reached for account")
	}
	b.pushSubscriptions[accountID][sub.ID] = sub
	return sub, nil
}

func (b *IMAPSMTPBackend) UpdatePushSubscription(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapmail.PushSubscription, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.pushMu.Lock()
	defer b.pushMu.Unlock()
	if m, ok := b.pushSubscriptions[accountID]; ok {
		if sub, ok := m[id]; ok {
			if vCode, ok := patch["verificationCode"].(string); ok {
				if sub.VerificationCode == nil || *sub.VerificationCode != vCode {
					return nil, fmt.Errorf("invalid verification code")
				}
				sub.VerificationCode = nil
			}
			if types, ok := patch["types"].([]any); ok {
				var ts []string
				for _, item := range types {
					if s, ok := item.(string); ok {
						ts = append(ts, s)
					}
				}
				sub.Types = ts
			}
			if exp, ok := patch["expires"].(string); ok {
				sub.Expires = &exp
			}
			return sub, nil
		}
	}
	return nil, jmapcore.ErrNotFound
}

func (b *IMAPSMTPBackend) DeletePushSubscription(ctx context.Context, id jmapcore.Id) (bool, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.pushMu.Lock()
	defer b.pushMu.Unlock()
	if m, ok := b.pushSubscriptions[accountID]; ok {
		if sub, ok := m[id]; ok {
			// Securely erase URL and encryption keys per RFC 8620 §7.2 / §2.0
			if sub != nil {
				sub.URL = ""
				sub.Keys = nil
				sub.VerificationCode = nil
			}
			delete(m, id)
			return true, nil
		}
	}
	return false, nil
}
