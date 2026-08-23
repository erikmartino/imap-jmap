package imapsmtp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
}

// Encode converts CompositeState to an opaque JMAP state token.
func (cs *CompositeState) Encode() string {
	b, _ := json.Marshal(cs)
	return "v1." + base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCompositeState parses a JMAP state token into a CompositeState.
func DecodeCompositeState(token string) (*CompositeState, error) {
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

	cs := &CompositeState{
		Version: 1,
		Folders: make(map[string]FolderState),
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

		statusCmd := client.Status(m.Mailbox, &imap.StatusOptions{
			NumMessages:   true,
			NumUnseen:     true,
			UIDNext:       true,
			UIDValidity:   true,
			HighestModSeq: true,
		})
		status, err := statusCmd.Wait()
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
		return nil, nil, nil, nil, newState, false
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
		return nil, nil, nil, newState, false
	}

	var created []jmap.Id
	var updated []jmap.Id
	var destroyed []jmap.Id

	for folder, newFS := range current.Folders {
		mbID := MailboxIDForName(folder)
		oldFS, existed := old.Folders[folder]
		if !existed {
			// Newly created folder with initial messages
			if newFS.UIDNext > 1 {
				for uid := uint32(1); uid < newFS.UIDNext; uid++ {
					created = append(created, EmailIDFor(mbID, uid))
				}
			}
			continue
		}

		if oldFS.UIDValidity != newFS.UIDValidity {
			// Mailbox recreated: old UIDs destroyed, new UIDs created
			for uid := uint32(1); uid < oldFS.UIDNext; uid++ {
				destroyed = append(destroyed, EmailIDFor(mbID, uid))
			}
			for uid := uint32(1); uid < newFS.UIDNext; uid++ {
				created = append(created, EmailIDFor(mbID, uid))
			}
			continue
		}

		// New messages appended (UIDNext increased)
		if newFS.UIDNext > oldFS.UIDNext {
			for uid := oldFS.UIDNext; uid < newFS.UIDNext; uid++ {
				created = append(created, EmailIDFor(mbID, uid))
			}
		}

		// Flags/metadata updated
		if newFS.HighestModSeq > oldFS.HighestModSeq || (newFS.HighestModSeq == 0 && newFS.Unseen != oldFS.Unseen) {
			// Flag changes detected
			for uid := uint32(1); uid < oldFS.UIDNext; uid++ {
				updated = append(updated, EmailIDFor(mbID, uid))
			}
		}
	}

	// Deleted folders
	for folder, oldFS := range old.Folders {
		if _, exists := current.Folders[folder]; !exists {
			mbID := MailboxIDForName(folder)
			for uid := uint32(1); uid < oldFS.UIDNext; uid++ {
				destroyed = append(destroyed, EmailIDFor(mbID, uid))
			}
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
