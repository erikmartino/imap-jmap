package imapsmtp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/emersion/go-imap/v2"
	"imap-jmap/jmap"
)

// FolderState represents the synchronization markers for a single IMAP folder.
type FolderState struct {
	UIDValidity   uint32 `json:"uv"`
	UIDNext       uint32 `json:"un"`
	HighestModSeq uint64 `json:"ms,omitempty"`
	Messages      uint32 `json:"cnt"`
	Unseen        uint32 `json:"uns"`
}

// CompositeState aggregates the state markers across all mailboxes for an account.
type CompositeState struct {
	Version int                    `json:"v"`
	Folders map[string]FolderState `json:"f"`
	Seq     uint64                 `json:"s,omitempty"`
}

// Encode converts CompositeState to an opaque JMAP state token.
func (cs *CompositeState) Encode() string {
	b, _ := json.Marshal(cs)
	return "v1." + base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCompositeState parses a JMAP state token into a CompositeState.
func DecodeCompositeState(token string) (*CompositeState, error) {
	token = strings.TrimSpace(token)
	if token == "0" || token == "state-0" {
		return &CompositeState{
			Version: 1,
			Folders: make(map[string]FolderState),
			Seq:     0,
		}, nil
	}
	if !strings.HasPrefix(token, "v1.") {
		return nil, fmt.Errorf("invalid state token prefix: %s", token)
	}
	raw := strings.TrimPrefix(token, "v1.")
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode state token: %w", err)
	}

	var cs CompositeState
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state json: %w", err)
	}
	if cs.Folders == nil {
		cs.Folders = make(map[string]FolderState)
	}
	return &cs, nil
}

// GetCurrentCompositeState queries the live IMAP server for the current state markers of all folders.
func (b *IMAPSMTPBackend) GetCurrentCompositeState(ctx context.Context) (*CompositeState, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	listCmd := client.List("", "*", nil)
	mailboxesData, err := listCmd.Collect()
	if err != nil {
		return nil, err
	}

	accountID, _ := jmap.AccountIDFromContext(ctx)
	cs := &CompositeState{
		Version: 1,
		Folders: make(map[string]FolderState),
		Seq:     b.getEmailSeq(accountID),
	}

	for _, m := range mailboxesData {
		hasNoSelect := false
		for _, attr := range m.Attrs {
			if attr == imap.MailboxAttrNoSelect {
				hasNoSelect = true
				break
			}
		}
		if hasNoSelect {
			continue
		}

		statusOpts := &imap.StatusOptions{
			NumMessages:   true,
			NumUnseen:     true,
			UIDNext:       true,
			UIDValidity:   true,
			HighestModSeq: true,
		}
		statusCmd := client.Status(m.Mailbox, statusOpts)
		status, err := statusCmd.Wait()
		if err != nil {
			statusOpts.HighestModSeq = false
			statusCmd = client.Status(m.Mailbox, statusOpts)
			status, err = statusCmd.Wait()
		}
		if err != nil || status == nil {
			continue
		}

		fs := FolderState{
			UIDValidity: status.UIDValidity,
			UIDNext:     uint32(status.UIDNext),
		}
		if status.NumMessages != nil {
			fs.Messages = *status.NumMessages
		}
		if status.NumUnseen != nil {
			fs.Unseen = *status.NumUnseen
		}
		if status.HighestModSeq != 0 {
			fs.HighestModSeq = status.HighestModSeq
		}

		cs.Folders[m.Mailbox] = fs
	}

	return cs, nil
}

// State returns the composite state for the account.
func (b *IMAPSMTPBackend) State(ctx context.Context) string {
	b.RecordAccount(ctx)
	cs, err := b.GetCurrentCompositeState(ctx)
	if err != nil {
		return "1"
	}
	return cs.Encode()
}

// MailboxState returns the current state of mailboxes.
func (b *IMAPSMTPBackend) MailboxState(ctx context.Context) string {
	return b.State(ctx)
}

// MailboxChanges calculates changes in mailboxes since the given state.
func (b *IMAPSMTPBackend) MailboxChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, []string, string, bool) {
	current, err := b.GetCurrentCompositeState(ctx)
	if err != nil {
		return nil, nil, nil, nil, sinceState, false
	}
	newState := current.Encode()

	old, err := DecodeCompositeState(sinceState)
	if err != nil {
		// Cannot calculate changes from unknown state token
		return nil, nil, nil, nil, newState, true
	}

	var created []jmap.Id
	var updated []jmap.Id
	var destroyed []jmap.Id

	// Check new and updated folders
	for folder, newFS := range current.Folders {
		mbID := MailboxIDForName(folder)
		oldFS, existed := old.Folders[folder]
		if !existed {
			created = append(created, mbID)
		} else if oldFS.UIDValidity != newFS.UIDValidity ||
			oldFS.Messages != newFS.Messages ||
			oldFS.Unseen != newFS.Unseen ||
			oldFS.HighestModSeq != newFS.HighestModSeq {
			updated = append(updated, mbID)
		}
	}

	// Check deleted folders
	for folder := range old.Folders {
		if _, exists := current.Folders[folder]; !exists {
			destroyed = append(destroyed, MailboxIDForName(folder))
		}
	}

	sort.Slice(created, func(i, j int) bool { return created[i] < created[j] })
	sort.Slice(updated, func(i, j int) bool { return updated[i] < updated[j] })
	sort.Slice(destroyed, func(i, j int) bool { return destroyed[i] < destroyed[j] })

	if maxChanges != nil && *maxChanges > 0 {
		total := uint64(len(created) + len(updated) + len(destroyed))
		if total > *maxChanges {
			return nil, nil, nil, nil, newState, true
		}
	}

	return created, updated, destroyed, []string{"totalEmails", "unreadEmails"}, newState, false
}

// EmailState returns the current state of emails.
func (b *IMAPSMTPBackend) EmailState(ctx context.Context) string {
	return b.State(ctx)
}

// EmailChanges calculates created, updated, and destroyed emails since the given state.
func (b *IMAPSMTPBackend) EmailChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	current, err := b.GetCurrentCompositeState(ctx)
	if err != nil {
		return nil, nil, nil, sinceState, false
	}
	newState := current.Encode()

	old, err := DecodeCompositeState(sinceState)
	if err != nil {
		// Malformed or foreign state token
		return nil, nil, nil, newState, true
	}

	accountID, _ := jmap.AccountIDFromContext(ctx)

	b.emailMutationsMu.RLock()
	mutations := b.emailMutations[accountID]
	var hasMutationHistory bool
	if len(mutations) > 0 {
		if mutations[0].state <= old.Seq+1 {
			hasMutationHistory = true
		}
	} else if old.Seq == current.Seq {
		hasMutationHistory = true
	}

	createdSet := make(map[jmap.Id]bool)
	updatedSet := make(map[jmap.Id]bool)
	destroyedSet := make(map[jmap.Id]bool)

	if hasMutationHistory {
		for _, entry := range mutations {
			if entry.state > old.Seq {
				switch entry.action {
				case "create":
					createdSet[entry.id] = true
					delete(updatedSet, entry.id)
					delete(destroyedSet, entry.id)
				case "update":
					if !createdSet[entry.id] {
						updatedSet[entry.id] = true
					}
				case "destroy":
					delete(createdSet, entry.id)
					delete(updatedSet, entry.id)
					destroyedSet[entry.id] = true
				}
			}
		}
	}
	b.emailMutationsMu.RUnlock()

	if !hasMutationHistory && old.Seq < current.Seq {
		// History was pruned or discarded
		return nil, nil, nil, newState, true
	}

	for folder, newFS := range current.Folders {
		mbID := MailboxIDForName(folder)
		oldFS, existed := old.Folders[folder]
		if !existed {
			// Newly created folder with initial messages
			if newFS.UIDNext > 1 {
				for uid := uint32(1); uid < newFS.UIDNext; uid++ {
					createdSet[EmailIDFor(mbID, uid)] = true
				}
			}
			continue
		}

		if oldFS.UIDValidity != newFS.UIDValidity {
			// Mailbox recreated: old UIDs destroyed, new UIDs created
			for uid := uint32(1); uid < oldFS.UIDNext; uid++ {
				destroyedSet[EmailIDFor(mbID, uid)] = true
			}
			for uid := uint32(1); uid < newFS.UIDNext; uid++ {
				createdSet[EmailIDFor(mbID, uid)] = true
			}
			continue
		}

		// New messages appended (UIDNext increased)
		if newFS.UIDNext > oldFS.UIDNext {
			for uid := oldFS.UIDNext; uid < newFS.UIDNext; uid++ {
				createdSet[EmailIDFor(mbID, uid)] = true
			}
		}

		// If no mutation history at all (e.g. seq == 0), fallback to folder modseq
		if old.Seq == 0 && current.Seq == 0 {
			if newFS.HighestModSeq > oldFS.HighestModSeq {
				for uid := uint32(1); uid < oldFS.UIDNext; uid++ {
					updatedSet[EmailIDFor(mbID, uid)] = true
				}
			} else if newFS.HighestModSeq == 0 {
				newMsgs := int64(newFS.Messages) - int64(oldFS.Messages)
				unseenDiff := int64(newFS.Unseen) - int64(oldFS.Unseen)
				if (newMsgs == 0 && unseenDiff != 0) || unseenDiff < 0 || unseenDiff > newMsgs {
					for uid := uint32(1); uid < oldFS.UIDNext; uid++ {
						updatedSet[EmailIDFor(mbID, uid)] = true
					}
				}
			}
		}
	}

	// Deleted folders
	for folder, oldFS := range old.Folders {
		if _, exists := current.Folders[folder]; !exists {
			mbID := MailboxIDForName(folder)
			for uid := uint32(1); uid < oldFS.UIDNext; uid++ {
				destroyedSet[EmailIDFor(mbID, uid)] = true
			}
		}
	}

	var created []jmap.Id
	var updated []jmap.Id
	var destroyed []jmap.Id

	for id := range createdSet {
		created = append(created, id)
	}
	for id := range updatedSet {
		updated = append(updated, id)
	}
	for id := range destroyedSet {
		destroyed = append(destroyed, id)
	}

	sort.Slice(created, func(i, j int) bool { return created[i] < created[j] })
	sort.Slice(updated, func(i, j int) bool { return updated[i] < updated[j] })
	sort.Slice(destroyed, func(i, j int) bool { return destroyed[i] < destroyed[j] })

	if maxChanges != nil && *maxChanges > 0 {
		total := uint64(len(created) + len(updated) + len(destroyed))
		if total > *maxChanges {
			return nil, nil, nil, newState, true
		}
	}

	return created, updated, destroyed, newState, false
}

// ThreadState returns the current thread state.
func (b *IMAPSMTPBackend) ThreadState(ctx context.Context) string {
	return b.State(ctx)
}

// ThreadChanges returns changes in threads since the given state.
func (b *IMAPSMTPBackend) ThreadChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	created, updated, destroyed, newState, hasMore := b.EmailChanges(ctx, sinceState, maxChanges)
	return created, updated, destroyed, newState, hasMore
}
