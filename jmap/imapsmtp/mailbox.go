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
	switch strings.ToLower(name) {
	case "inbox":
		return "mb-inbox"
	case "drafts":
		return "mb-drafts"
	case "sent":
		return "mb-sent"
	case "trash":
		return "mb-trash"
	case "junk":
		return "mb-junk"
	case "archive":
		return "mb-archive"
	}
	return jmap.Id(base64.RawURLEncoding.EncodeToString([]byte(name)))
}

// NameForMailboxID converts a JMAP Mailbox ID back to an IMAP folder name.
func NameForMailboxID(id jmap.Id) (string, error) {
	switch id {
	case "mb-inbox":
		return "INBOX", nil
	case "mb-drafts":
		return "Drafts", nil
	case "mb-sent":
		return "Sent", nil
	case "mb-trash":
		return "Trash", nil
	case "mb-junk":
		return "Junk", nil
	case "mb-archive":
		return "Archive", nil
	}
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
	b.RecordAccount(ctx)
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
		if strings.EqualFold(dispName, "INBOX") {
			dispName = "Inbox"
		}

		accountID, _ := jmap.AccountIDFromContext(ctx)
		if pOverride, ok := b.getMailboxParentOverride(accountID, mbID); ok {
			parentID = pOverride
		} else if role != "" {
			if pOverride, ok := b.getMailboxParentOverride(accountID, jmap.Id("mb-"+role)); ok {
				parentID = pOverride
			}
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
		if so, ok := b.getMailboxSortOrder(accountID, mbID); ok {
			sortOrder = so
		} else if role != "" {
			if so, ok := b.getMailboxSortOrder(accountID, jmap.Id("mb-"+role)); ok {
				sortOrder = so
			}
		}

		isSubscribed := true
		if sub, ok := b.getMailboxSubscribed(accountID, mbID); ok {
			isSubscribed = sub
		} else if role != "" {
			if sub, ok := b.getMailboxSubscribed(accountID, jmap.Id("mb-"+role)); ok {
				isSubscribed = sub
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
			IsSubscribed:  isSubscribed,
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
			continue
		}
		resolved := b.resolveMovedMailboxID(id)
		if mb, ok := allMap[resolved]; ok {
			found = append(found, mb)
			continue
		}
		if realName, err := NameForMailboxID(id); err == nil {
			realID := MailboxIDForName(realName)
			if mb, ok := allMap[realID]; ok {
				found = append(found, mb)
				continue
			}
			resolvedReal := b.resolveMovedMailboxID(realID)
			if mb, ok := allMap[resolvedReal]; ok {
				found = append(found, mb)
				continue
			}
			var matched *jmap.Mailbox
			for _, m := range all {
				if strings.EqualFold(m.Name, realName) || (m.Role != nil && strings.EqualFold(*m.Role, strings.TrimPrefix(string(id), "mb-"))) {
					matched = m
					break
				}
			}
			if matched != nil {
				found = append(found, matched)
				continue
			}
		}
		notFound = append(notFound, id)
	}

	return found, notFound, nil
}

// CreateMailbox creates a new IMAP mailbox.
func (b *IMAPSMTPBackend) CreateMailbox(ctx context.Context, mb *jmap.Mailbox) (*jmap.Mailbox, error) {
	folderName := mb.Name
	if mb.ParentID != nil {
		parentName, err := NameForMailboxID(*mb.ParentID)
		if err == nil {
			folderName = path.Join(parentName, mb.Name)
		}
	}

	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}

	if err := client.Create(folderName, nil).Wait(); err != nil {
		b.pool.ReleaseClient(ctx, client)
		return nil, fmt.Errorf("failed to create IMAP mailbox %s: %w", folderName, err)
	}

	if mb.IsSubscribed {
		_ = client.Subscribe(folderName).Wait()
	}
	b.pool.ReleaseClient(ctx, client)

	mb.ID = MailboxIDForName(folderName)
	accountID, _ := jmap.AccountIDFromContext(ctx)
	if mb.SortOrder != 0 {
		b.setMailboxSortOrder(accountID, mb.ID, mb.SortOrder)
	}
	b.setMailboxSubscribed(accountID, mb.ID, mb.IsSubscribed)

	b.publishStateChange(ctx)
	return mb, nil
}

// UpdateMailbox renames an IMAP mailbox or updates its metadata.
func (b *IMAPSMTPBackend) UpdateMailbox(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Mailbox, error) {
	accountID, _ := jmap.AccountIDFromContext(ctx)
	origID := id
	id = b.resolveMovedMailboxID(id)

	folderName, err := NameForMailboxID(id)
	if err != nil {
		folderName = string(id)
	}

	all, err := b.GetAllMailboxes(ctx)
	if err != nil {
		return nil, err
	}
	var target *jmap.Mailbox
	for _, mb := range all {
		if mb.ID == id || mb.ID == origID || strings.EqualFold(mb.Name, folderName) {
			target = mb
			break
		}
	}
	if target == nil {
		return nil, jmap.SetError{Type: "notFound", Description: "mailbox not found"}
	}

	currentFolder := folderName
	if realName, err := NameForMailboxID(target.ID); err == nil && realName != "" {
		currentFolder = realName
	}

	if so, ok := patch["sortOrder"].(float64); ok {
		val := uint64(so)
		b.setMailboxSortOrder(accountID, target.ID, val)
		b.setMailboxSortOrder(accountID, origID, val)
		target.SortOrder = val
	} else if prevSO, ok := b.getMailboxSortOrder(accountID, target.ID); ok {
		target.SortOrder = prevSO
	}

	if sub, ok := patch["isSubscribed"].(bool); ok {
		b.setMailboxSubscribed(accountID, target.ID, sub)
		b.setMailboxSubscribed(accountID, origID, sub)
		target.IsSubscribed = sub
	} else if prevSub, ok := b.getMailboxSubscribed(accountID, target.ID); ok {
		target.IsSubscribed = prevSub
	}

	var newParentID *jmap.Id
	hasParentUpdate := false
	if rawPID, ok := patch["parentId"]; ok {
		hasParentUpdate = true
		if rawPID != nil {
			if pidStr, ok := rawPID.(string); ok && pidStr != "" {
				p := jmap.Id(pidStr)
				newParentID = &p
			}
		}
	}

	isInbox := strings.EqualFold(currentFolder, "INBOX") || strings.EqualFold(target.Name, "INBOX") || (target.Role != nil && *target.Role == "inbox")

	if isInbox {
		if hasParentUpdate {
			b.setMailboxParentOverride(accountID, target.ID, newParentID)
			b.setMailboxParentOverride(accountID, origID, newParentID)
			b.setMailboxParentOverride(accountID, "mb-inbox", newParentID)
			target.ParentID = newParentID
		}
		b.publishStateChange(ctx)
		return target, nil
	}

	newLeafName := target.Name
	if n, ok := patch["name"].(string); ok && n != "" {
		newLeafName = n
	}

	var newParentFolder string
	if hasParentUpdate {
		if newParentID != nil {
			if pName, err := NameForMailboxID(*newParentID); err == nil {
				newParentFolder = pName
			}
		}
	} else {
		delim := "/"
		if idx := strings.LastIndex(currentFolder, delim); idx > 0 {
			newParentFolder = currentFolder[:idx]
		}
	}

	newFolderPath := newLeafName
	if newParentFolder != "" {
		newFolderPath = path.Join(newParentFolder, newLeafName)
	}

	if newFolderPath != currentFolder {
		client, err := b.pool.GetClientForContext(ctx)
		if err != nil {
			return nil, err
		}
		if err := client.Rename(currentFolder, newFolderPath, nil).Wait(); err != nil {
			b.pool.ReleaseClient(ctx, client)
			return nil, fmt.Errorf("failed to rename IMAP mailbox: %w", err)
		}
		b.pool.ReleaseClient(ctx, client)

		newID := MailboxIDForName(newFolderPath)
		b.trackMovedMailbox(origID, newID)
		b.trackMovedMailbox(id, newID)
		b.trackMovedMailbox(target.ID, newID)
		if so, ok := b.getMailboxSortOrder(accountID, target.ID); ok {
			b.setMailboxSortOrder(accountID, newID, so)
		}
		if sub, ok := b.getMailboxSubscribed(accountID, target.ID); ok {
			b.setMailboxSubscribed(accountID, newID, sub)
		}
		target.ID = newID
		target.Name = newLeafName
	}

	if hasParentUpdate {
		b.setMailboxParentOverride(accountID, target.ID, newParentID)
		b.setMailboxParentOverride(accountID, origID, newParentID)
		target.ParentID = newParentID
	}

	b.publishStateChange(ctx)
	return target, nil
}

// DeleteMailbox deletes an IMAP mailbox.
func (b *IMAPSMTPBackend) DeleteMailbox(ctx context.Context, id jmap.Id, onDestroyRemoveMessages bool) (bool, error) {
	origID := id
	id = b.resolveMovedMailboxID(id)

	folderName, err := NameForMailboxID(id)
	if err != nil {
		folderName = string(id)
	}

	all, err := b.GetAllMailboxes(ctx)
	if err != nil {
		return false, err
	}
	var target *jmap.Mailbox
	for _, mb := range all {
		if mb.ID == id || mb.ID == origID || strings.EqualFold(mb.Name, folderName) {
			target = mb
			break
		}
	}
	if target == nil {
		return false, nil
	}

	folderToDelete := folderName
	if realName, err := NameForMailboxID(target.ID); err == nil && realName != "" {
		folderToDelete = realName
	}

	for _, other := range all {
		if other.ParentID != nil && (*other.ParentID == target.ID || *other.ParentID == id || *other.ParentID == origID) {
			return false, jmap.SetError{Type: "mailboxHasChild"}
		}
	}

	client, err := b.pool.GetClientForContext(ctx)
	if err != nil {
		return false, err
	}
	if err := client.Delete(folderToDelete).Wait(); err != nil {
		b.pool.ReleaseClient(ctx, client)
		return false, fmt.Errorf("failed to delete IMAP mailbox %s: %w", folderToDelete, err)
	}
	b.pool.ReleaseClient(ctx, client)

	b.publishStateChange(ctx)
	return true, nil
}
