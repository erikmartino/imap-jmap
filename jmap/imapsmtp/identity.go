package imapsmtp

import (
	"context"
	"fmt"
	"time"

	"imap-jmap/jmap"
)

// Identities (RFC 8621 Section 6)

func (b *IMAPSMTPBackend) IdentityState(ctx context.Context) string {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getIdentityTracker(accountID).State()
}

func (b *IMAPSMTPBackend) IdentityChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	return b.getIdentityTracker(accountID).Changes(sinceState, maxChanges)
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

	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()
	defer b.identitiesMu.Unlock()

	if b.identities[accountID] == nil {
		b.identities[accountID] = make(map[jmap.Id]*jmap.Identity)
	}
	m := b.identities[accountID]
	if _, ok := m["id-primary"]; !ok {
		m["id-primary"] = &jmap.Identity{
			ID:        "id-primary",
			Name:      email,
			Email:     email,
			MayDelete: false,
		}
	}

	list := make([]*jmap.Identity, 0, len(m))
	for _, ident := range m {
		list = append(list, ident)
	}
	return list, nil
}

func (b *IMAPSMTPBackend) CreateIdentity(ctx context.Context, identity *jmap.Identity) (*jmap.Identity, error) {
	if identity.ID == "" {
		identity.ID = jmap.Id(fmt.Sprintf("id-%d", time.Now().UnixNano()))
	}
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()
	if b.identities[accountID] == nil {
		b.identities[accountID] = make(map[jmap.Id]*jmap.Identity)
	}
	b.identities[accountID][identity.ID] = identity
	b.identitiesMu.Unlock()

	b.getIdentityTracker(accountID).Record(identity.ID, "create")
	b.publishStateChange(ctx)
	return identity, nil
}

func (b *IMAPSMTPBackend) UpdateIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Identity, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()
	defer b.identitiesMu.Unlock()
	if m, ok := b.identities[accountID]; ok {
		if ident, ok := m[id]; ok {
			if name, ok := patch["name"].(string); ok {
				ident.Name = name
			}
			if email, ok := patch["email"].(string); ok {
				ident.Email = email
			}
			if replyTo, ok := patch["replyTo"].([]jmap.EmailAddress); ok {
				ident.ReplyTo = replyTo
			}
			if bcc, ok := patch["bcc"].([]jmap.EmailAddress); ok {
				ident.BCC = bcc
			}
			if textSig, ok := patch["textSignature"].(string); ok {
				ident.TextSignature = textSig
			}
			if htmlSig, ok := patch["htmlSignature"].(string); ok {
				ident.HTMLSignature = htmlSig
			}
			if mayDel, ok := patch["mayDelete"].(bool); ok {
				ident.MayDelete = mayDel
			}
			b.getIdentityTracker(accountID).Record(id, "update")
			b.publishStateChange(ctx)
			return ident, nil
		}
	}
	return nil, jmap.ErrNotFound
}

func (b *IMAPSMTPBackend) DeleteIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()
	defer b.identitiesMu.Unlock()
	if m, ok := b.identities[accountID]; ok {
		if _, ok := m[id]; ok {
			delete(m, id)
			b.getIdentityTracker(accountID).Record(id, "destroy")
			b.publishStateChange(ctx)
			return true, nil
		}
	}
	return false, nil
}
