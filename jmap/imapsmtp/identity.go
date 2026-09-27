package imapsmtp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapextstore"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmappush"
)

// Identities (RFC 8621 Section 6)

func (b *IMAPSMTPBackend) loadIdentitiesLocked(ctx context.Context, accountID string) {
	if b.identities == nil {
		b.identities = make(map[string]map[jmapcore.Id]*jmapmail.Identity)
	}
	if b.identities[accountID] != nil {
		return
	}
	if loaded, ok, _ := jmapextstore.Load[map[jmapcore.Id]*jmapmail.Identity](ctx, b.extStore, accountID, "identities"); ok && loaded != nil {
		b.identities[accountID] = *loaded
		return
	}
	b.identities[accountID] = make(map[jmapcore.Id]*jmapmail.Identity)
}

func (b *IMAPSMTPBackend) persistIdentitiesLocked(ctx context.Context, accountID string) {
	if b.extStore != nil && b.identities[accountID] != nil {
		_ = jmapextstore.Save(ctx, b.extStore, accountID, "identities", b.identities[accountID])
	}
}

func identityStateMap(items []*jmapmail.Identity) map[string]string {
	fps := make(map[string]string, len(items))
	for _, it := range items {
		if it != nil {
			fps[string(it.ID)] = jmappush.ObjectFingerprint(it)
		}
	}
	return fps
}

func (b *IMAPSMTPBackend) IdentityState(ctx context.Context) string {
	items, err := b.GetIdentities(ctx)
	if err != nil {
		accountID, _ := jmapauth.AccountIDFromContext(ctx)
		return b.getIdentityTracker(accountID).State()
	}
	return jmappush.EncodeStateVector("id-v1:", identityStateMap(items))
}

func (b *IMAPSMTPBackend) IdentityChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	if !strings.HasPrefix(sinceState, "id-v1:") {
		return b.getIdentityTracker(accountID).Changes(sinceState, maxChanges)
	}
	old, err := jmappush.DecodeStateVector("id-v1:", sinceState)
	if err != nil {
		return nil, nil, nil, "", false
	}
	items, err := b.GetIdentities(ctx)
	if err != nil {
		return nil, nil, nil, "", false
	}
	cur := identityStateMap(items)
	newState := jmappush.EncodeStateVector("id-v1:", cur)
	created, updated, destroyed := jmappush.DiffStateVectors(old, cur)
	return created, updated, destroyed, newState, false
}

func (b *IMAPSMTPBackend) GetIdentities(ctx context.Context) ([]*jmapmail.Identity, error) {
	email := "user@example.com"
	if subject, ok := jmapauth.SubjectFromContext(ctx); ok && subject != "" {
		email = subject
	} else if accountID, ok := jmapauth.AccountIDFromContext(ctx); ok {
		if sub, ok := jmapauth.SubjectForAccountID(accountID); ok {
			email = sub
		}
	}

	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()
	defer b.identitiesMu.Unlock()

	b.loadIdentitiesLocked(ctx, accountID)
	m := b.identities[accountID]
	if _, ok := m["id-primary"]; !ok {
		m["id-primary"] = &jmapmail.Identity{
			ID:        "id-primary",
			Name:      email,
			Email:     email,
			MayDelete: false,
		}
		b.persistIdentitiesLocked(ctx, accountID)
	}

	list := make([]*jmapmail.Identity, 0, len(m))
	for _, ident := range m {
		list = append(list, ident)
	}
	return list, nil
}

func (b *IMAPSMTPBackend) CreateIdentity(ctx context.Context, identity *jmapmail.Identity) (*jmapmail.Identity, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()

	b.loadIdentitiesLocked(ctx, accountID)

	// Ensure default primary identity exists for duplicate detection
	if _, ok := b.identities[accountID]["id-primary"]; !ok {
		email := "user@example.com"
		if subject, ok := jmapauth.SubjectFromContext(ctx); ok && subject != "" {
			email = subject
		} else if sub, ok := jmapauth.SubjectForAccountID(accountID); ok {
			email = sub
		}
		b.identities[accountID]["id-primary"] = &jmapmail.Identity{
			ID:        "id-primary",
			Name:      email,
			Email:     email,
			MayDelete: false,
		}
	}

	for existingID, existing := range b.identities[accountID] {
		if strings.EqualFold(existing.Email, identity.Email) {
			b.identitiesMu.Unlock()
			return nil, jmapcore.SetError{
				Type:        "alreadyExists",
				ExistingID:  existingID,
				Description: fmt.Sprintf("identity with email %q already exists", identity.Email),
			}
		}
	}

	if identity.ID == "" {
		identity.ID = jmapcore.Id(fmt.Sprintf("id-%d", time.Now().UnixNano()))
	}
	b.identities[accountID][identity.ID] = identity
	b.persistIdentitiesLocked(ctx, accountID)

	b.getIdentityTracker(accountID).Record(identity.ID, "create")
	b.identitiesMu.Unlock()

	b.publishStateChange(ctx)
	return identity, nil
}

func (b *IMAPSMTPBackend) UpdateIdentity(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapmail.Identity, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()

	b.loadIdentitiesLocked(ctx, accountID)
	if m, ok := b.identities[accountID]; ok {
		if ident, ok := m[id]; ok {
			if name, ok := patch["name"].(string); ok {
				ident.Name = name
			}
			if email, ok := patch["email"].(string); ok {
				ident.Email = email
			}
			if replyTo, ok := patch["replyTo"].([]jmapmail.EmailAddress); ok {
				ident.ReplyTo = replyTo
			}
			if bcc, ok := patch["bcc"].([]jmapmail.EmailAddress); ok {
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
			b.persistIdentitiesLocked(ctx, accountID)
			b.getIdentityTracker(accountID).Record(id, "update")
			b.identitiesMu.Unlock()
			b.publishStateChange(ctx)
			return ident, nil
		}
	}
	b.identitiesMu.Unlock()
	return nil, jmapcore.ErrNotFound
}

func (b *IMAPSMTPBackend) DeleteIdentity(ctx context.Context, id jmapcore.Id) (bool, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	b.identitiesMu.Lock()

	b.loadIdentitiesLocked(ctx, accountID)
	if m, ok := b.identities[accountID]; ok {
		if _, ok := m[id]; ok {
			delete(m, id)
			b.persistIdentitiesLocked(ctx, accountID)
			b.getIdentityTracker(accountID).Record(id, "destroy")
			b.identitiesMu.Unlock()
			b.publishStateChange(ctx)
			return true, nil
		}
	}
	b.identitiesMu.Unlock()
	return false, nil
}
