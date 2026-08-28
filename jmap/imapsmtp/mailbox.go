package imapsmtp

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/emersion/go-imap/v2"
	"imap-jmap/jmap"
)

// MailboxIDForName converts an IMAP folder name to a JMAP Mailbox ID.
func MailboxIDForName(name string) jmap.Id {
	return jmap.Id(base64.RawURLEncoding.EncodeToString([]byte(name)))
}

// NameForMailboxID converts a JMAP Mailbox ID back to an IMAP folder name.
func NameForMailboxID(id jmap.Id) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(string(id))
	if err != nil {
		return "", fmt.Errorf("invalid mailbox id: %w", err)
	}
	return string(b), nil
}

// DetectRole determines the JMAP role for an IMAP mailbox from its name and attributes.
func DetectRole(name string, attrs []imap.MailboxAttr) string {
	for _, attr := range attrs {
		switch attr {
		case imap.MailboxAttrDrafts:
			return "drafts"
		case imap.MailboxAttrSent:
			return "sent"
		case imap.MailboxAttrTrash:
			return "trash"
		case imap.MailboxAttrJunk:
			return "junk"
		case imap.MailboxAttrArchive:
			return "archive"
		}
	}

	lower := strings.ToLower(name)
	switch {
	case strings.EqualFold(name, "INBOX"):
		return "inbox"
	case strings.Contains(lower, "draft"):
		return "drafts"
	case strings.Contains(lower, "sent"):
		return "sent"
	case strings.Contains(lower, "trash") || strings.Contains(lower, "bin") || strings.Contains(lower, "deleted"):
		return "trash"
	case strings.Contains(lower, "junk") || strings.Contains(lower, "spam"):
		return "junk"
	case strings.Contains(lower, "archive"):
		return "archive"
	case strings.Contains(lower, "outbox"):
		return "outbox"
	case strings.Contains(lower, "template"):
		return "templates"
	default:
		return ""
	}
}

// GetAllMailboxes retrieves all mailboxes from upstream IMAP.
func (b *IMAPSMTPBackend) GetAllMailboxes(ctx context.Context) ([]*jmap.Mailbox, error) {
	// Clean up the account's own [JMAP-BLOB:] staging messages in Drafts (lazy,
	// rate-limited). This runs on the authenticated user's request, so it only
	// ever touches that user's folder.
	b.maybeSweepBlobStaging(ctx)

	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	listCmd := client.List("", "*", nil)
	mailboxesData, err := listCmd.Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to list mailboxes: %w", err)
	}

	var result []*jmap.Mailbox
	for _, m := range mailboxesData {
		hasNoSelect := false
		for _, attr := range m.Attrs {
			if attr == imap.MailboxAttrNoSelect {
				hasNoSelect = true
				break
			}
		}

		total := uint64(0)
		unread := uint64(0)
		if !hasNoSelect {
			statusCmd := client.Status(m.Mailbox, &imap.StatusOptions{
				NumMessages: true,
				NumUnseen:   true,
				UIDNext:     true,
				UIDValidity: true,
			})
			statusData, statusErr := statusCmd.Wait()
			if statusErr == nil && statusData != nil {
				if statusData.NumMessages != nil {
					total = uint64(*statusData.NumMessages)
				}
				if statusData.NumUnseen != nil {
					unread = uint64(*statusData.NumUnseen)
				}
			}
		}

		name := m.Mailbox
		mbID := MailboxIDForName(name)
		role := DetectRole(name, m.Attrs)

		var parentID *jmap.Id
		if m.Delim != 0 {
			delimStr := string(m.Delim)
			if idx := strings.LastIndex(name, delimStr); idx > 0 {
				parentName := name[:idx]
				pID := MailboxIDForName(parentName)
				parentID = &pID
			}
		}

		dispName := name
		if parentID != nil && m.Delim != 0 {
			delimStr := string(m.Delim)
			parts := strings.Split(name, delimStr)
			dispName = parts[len(parts)-1]
		}

		var rolePtr *string
		if role != "" {
			rolePtr = &role
		}

		mayRename := !strings.EqualFold(name, "INBOX")
		mayDelete := !strings.EqualFold(name, "INBOX")

		sortOrder := uint64(100)
		if role != "" {
			switch role {
			case "inbox":
				sortOrder = 10
			case "drafts":
				sortOrder = 20
			case "sent":
				sortOrder = 30
			case "archive":
				sortOrder = 40
			case "trash":
				sortOrder = 50
			case "junk":
				sortOrder = 60
			case "outbox":
				sortOrder = 70
			case "templates":
				sortOrder = 80
			}
		}

		mb := &jmap.Mailbox{
			ID:            mbID,
			Name:          dispName,
			ParentID:      parentID,
			Role:          rolePtr,
			SortOrder:     sortOrder,
			TotalEmails:   total,
			UnreadEmails:  unread,
			TotalThreads:  total,
			UnreadThreads: unread,
			MyRights: jmap.MailboxRights{
				MayReadItems:   !hasNoSelect,
				MayAddItems:    !hasNoSelect,
				MayRemoveItems: !hasNoSelect,
				MaySetSeen:     !hasNoSelect,
				MaySetKeywords: !hasNoSelect,
				MayCreateChild: true,
				MayRename:      mayRename,
				MayDelete:      mayDelete,
				MaySubmit:      true,
				MayAdmin:       true,
			},
			IsSubscribed:  true,
		}
		result = append(result, mb)
	}

	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SortOrder != result[j].SortOrder {
			return result[i].SortOrder < result[j].SortOrder
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})

	return result, nil
}

// GetMailboxes fetches specified mailboxes by ID.
func (b *IMAPSMTPBackend) GetMailboxes(ctx context.Context, ids []jmap.Id) ([]*jmap.Mailbox, []jmap.Id, error) {
	all, err := b.GetAllMailboxes(ctx)
	if err != nil {
		return nil, ids, err
	}

	allMap := make(map[jmap.Id]*jmap.Mailbox, len(all))
	for _, mb := range all {
		allMap[mb.ID] = mb
	}

	var found []*jmap.Mailbox
	var notFound []jmap.Id
	for _, id := range ids {
		if mb, ok := allMap[id]; ok {
			found = append(found, mb)
		} else {
			notFound = append(notFound, id)
		}
	}

	return found, notFound, nil
}

// CreateMailbox creates a new IMAP mailbox.
func (b *IMAPSMTPBackend) CreateMailbox(ctx context.Context, mb *jmap.Mailbox) (*jmap.Mailbox, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	folderName := mb.Name
	if mb.ParentID != nil {
		parentName, err := NameForMailboxID(*mb.ParentID)
		if err == nil {
			folderName = path.Join(parentName, mb.Name)
		}
	}

	if err := client.Create(folderName, nil).Wait(); err != nil {
		return nil, fmt.Errorf("failed to create IMAP mailbox %s: %w", folderName, err)
	}

	mb.ID = MailboxIDForName(folderName)
	mb.IsSubscribed = true
	return mb, nil
}

// UpdateMailbox renames an IMAP mailbox or updates its metadata.
func (b *IMAPSMTPBackend) UpdateMailbox(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Mailbox, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	oldName, err := NameForMailboxID(id)
	if err != nil {
		return nil, err
	}

	newName := oldName
	if n, ok := patch["name"].(string); ok && n != "" && n != oldName {
		newName = n
		if err := client.Rename(oldName, newName, nil).Wait(); err != nil {
			return nil, fmt.Errorf("failed to rename IMAP mailbox: %w", err)
		}
	}

	return &jmap.Mailbox{
		ID:           MailboxIDForName(newName),
		Name:         newName,
		IsSubscribed: true,
	}, nil
}

// DeleteMailbox deletes an IMAP mailbox.
func (b *IMAPSMTPBackend) DeleteMailbox(ctx context.Context, id jmap.Id, onDestroyRemoveMessages bool) (bool, error) {
	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return false, err
	}
	defer b.pool.ReleaseClient(ctx, client)

	folderName, err := NameForMailboxID(id)
	if err != nil {
		return false, err
	}

	if err := client.Delete(folderName).Wait(); err != nil {
		return false, fmt.Errorf("failed to delete IMAP mailbox %s: %w", folderName, err)
	}

	return true, nil
}
