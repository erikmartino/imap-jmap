package memory

import (
	"context"
	"fmt"
	"imap-jmap/jmap"
	"strings"
)

func (mb *MemoryBackend) MailboxState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.mailboxState.State()
}

// MailboxChanges returns created, updated, and destroyed Mailboxes since sinceState.
func (mb *MemoryBackend) MailboxChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, []string, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.mailboxState.MailboxChanges(sinceState, maxChanges)
}

// ThreadState returns current change state token for Thread resources.

func (mb *MemoryBackend) GetMailboxes(ctx context.Context, ids []jmap.Id) ([]*jmap.Mailbox, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.Mailbox
	var notFound []jmap.Id

	for _, id := range ids {
		if item, ok := us.mailboxes[id]; ok {
			list = append(list, item)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllMailboxes retrieves all mailboxes.
func (mb *MemoryBackend) GetAllMailboxes(ctx context.Context) ([]*jmap.Mailbox, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.Mailbox, 0, len(us.mailboxes))
	for _, item := range us.mailboxes {
		list = append(list, item)
	}
	return list, nil
}

// CreateMailbox creates a new mailbox.
func (mb *MemoryBackend) CreateMailbox(ctx context.Context, item *jmap.Mailbox) (*jmap.Mailbox, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	for _, existing := range us.mailboxes {
		sameParent := (existing.ParentID == nil && item.ParentID == nil) ||
			(existing.ParentID != nil && item.ParentID != nil && *existing.ParentID == *item.ParentID)
		if sameParent && strings.EqualFold(existing.Name, item.Name) {
			return nil, jmap.SetError{
				Type:        "alreadyExists",
				Description: "duplicate mailbox name under same parent",
				Properties:  []string{"name"},
			}
		}
	}

	if item.ID == "" {
		item.ID = mb.nextID("mb")
	}
	item.MyRights = jmap.MailboxRights{
		MayReadItems:   true,
		MayAddItems:    true,
		MayRemoveItems: true,
		MaySetSeen:     true,
		MaySetKeywords: true,
		MayCreateChild: true,
		MayRename:      true,
		MayDelete:      true,
		MaySubmit:      true,
		MayAdmin:       true,
	}
	us.mailboxes[item.ID] = item
	mb.recordChange(ctx, us.mailboxState, item.ID, "create", "Mailbox")
	return item, nil
}

// UpdateMailbox applies a partial patch to a mailbox (RFC 8621 Section 2.5), preserving
// unaddressed fields. Counts and rights are server-set and cannot be patched by the client.
func (mb *MemoryBackend) UpdateMailbox(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Mailbox, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	item, ok := us.mailboxes[id]
	if !ok {
		mb.mu.Unlock()
		return nil, fmt.Errorf("mailbox %s: %w", id, jmap.ErrNotFound)
	}

	var invalidProps []string

	for prop, val := range patch {
		switch prop {
		case "name":
			name, ok := val.(string)
			if !ok || name == "" {
				invalidProps = append(invalidProps, "name")
				continue
			}
			item.Name = name
		case "parentId":
			if val == nil {
				item.ParentID = nil
				continue
			}
			pid, ok := val.(string)
			if !ok {
				invalidProps = append(invalidProps, "parentId")
				continue
			}
			if jmap.Id(pid) == id {
				invalidProps = append(invalidProps, "parentId")
				continue
			}
			if _, exists := us.mailboxes[jmap.Id(pid)]; !exists {
				invalidProps = append(invalidProps, "parentId")
				continue
			}
			p := jmap.Id(pid)
			item.ParentID = &p
		case "role":
			if val == nil {
				item.Role = nil
				continue
			}
			role, ok := val.(string)
			if !ok {
				invalidProps = append(invalidProps, "role")
				continue
			}
			item.Role = &role
		case "sortOrder":
			if f, ok := val.(float64); ok {
				item.SortOrder = uint64(f)
			} else {
				invalidProps = append(invalidProps, "sortOrder")
			}
		case "isSubscribed":
			if b, ok := val.(bool); ok {
				item.IsSubscribed = b
			} else {
				invalidProps = append(invalidProps, "isSubscribed")
			}
		case "id":
			if s, ok := val.(string); !ok || jmap.Id(s) != item.ID {
				invalidProps = append(invalidProps, "id")
			}
		case "totalEmails":
			if f, ok := val.(float64); !ok || uint64(f) != item.TotalEmails {
				invalidProps = append(invalidProps, "totalEmails")
			}
		case "unreadEmails":
			if f, ok := val.(float64); !ok || uint64(f) != item.UnreadEmails {
				invalidProps = append(invalidProps, "unreadEmails")
			}
		case "totalThreads":
			if f, ok := val.(float64); !ok || uint64(f) != item.TotalThreads {
				invalidProps = append(invalidProps, "totalThreads")
			}
		case "unreadThreads":
			if f, ok := val.(float64); !ok || uint64(f) != item.UnreadThreads {
				invalidProps = append(invalidProps, "unreadThreads")
			}
		case "myRights":
			if m, ok := val.(map[string]any); ok {
				for rk, rv := range m {
					b, isBool := rv.(bool)
					var curr bool
					switch rk {
					case "mayReadItems":
						curr = item.MyRights.MayReadItems
					case "mayAddItems":
						curr = item.MyRights.MayAddItems
					case "mayRemoveItems":
						curr = item.MyRights.MayRemoveItems
					case "maySetSeen":
						curr = item.MyRights.MaySetSeen
					case "maySetKeywords":
						curr = item.MyRights.MaySetKeywords
					case "mayCreateChild":
						curr = item.MyRights.MayCreateChild
					case "mayRename":
						curr = item.MyRights.MayRename
					case "mayDelete":
						curr = item.MyRights.MayDelete
					case "maySubmit":
						curr = item.MyRights.MaySubmit
					case "mayAdmin":
						curr = item.MyRights.MayAdmin
					default:
						invalidProps = append(invalidProps, "myRights/"+rk)
						continue
					}
					if !isBool || b != curr {
						invalidProps = append(invalidProps, "myRights/"+rk)
					}
				}
			} else {
				invalidProps = append(invalidProps, "myRights")
			}
		default:
			if strings.HasPrefix(prop, "myRights/") {
				rk := strings.TrimPrefix(prop, "myRights/")
				b, isBool := val.(bool)
				var curr bool
				switch rk {
				case "mayReadItems":
					curr = item.MyRights.MayReadItems
				case "mayAddItems":
					curr = item.MyRights.MayAddItems
				case "mayRemoveItems":
					curr = item.MyRights.MayRemoveItems
				case "maySetSeen":
					curr = item.MyRights.MaySetSeen
				case "maySetKeywords":
					curr = item.MyRights.MaySetKeywords
				case "mayCreateChild":
					curr = item.MyRights.MayCreateChild
				case "mayRename":
					curr = item.MyRights.MayRename
				case "mayDelete":
					curr = item.MyRights.MayDelete
				case "maySubmit":
					curr = item.MyRights.MaySubmit
				case "mayAdmin":
					curr = item.MyRights.MayAdmin
				default:
					invalidProps = append(invalidProps, prop)
					continue
				}
				if !isBool || b != curr {
					invalidProps = append(invalidProps, prop)
				}
			} else {
				invalidProps = append(invalidProps, prop)
			}
		}
	}
	for _, existing := range us.mailboxes {
		if existing.ID == id {
			continue
		}
		sameParent := (existing.ParentID == nil && item.ParentID == nil) ||
			(existing.ParentID != nil && item.ParentID != nil && *existing.ParentID == *item.ParentID)
		if sameParent && strings.EqualFold(existing.Name, item.Name) {
			mb.mu.Unlock()
			return nil, jmap.SetError{
				Type:        "alreadyExists",
				Description: "duplicate mailbox name under same parent",
				Properties:  []string{"name"},
			}
		}
	}

	if len(invalidProps) > 0 {
		mb.mu.Unlock()
		return nil, jmap.SetError{
			Type:       "invalidProperties",
			Properties: invalidProps,
		}
	}
	mb.mu.Unlock()

	mb.recordChange(ctx, us.mailboxState, id, "update", "Mailbox")
	return item, nil
}

// DeleteMailbox deletes a mailbox by ID.
func (mb *MemoryBackend) DeleteMailbox(ctx context.Context, id jmap.Id, onDestroyRemoveMessages bool) (bool, error) {
	mb.mu.Lock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.Unlock()

	if _, ok := us.mailboxes[id]; !ok {
		return false, nil
	}

	// Check if mailbox has children
	for _, other := range us.mailboxes {
		if other.ParentID != nil && *other.ParentID == id {
			return false, jmap.SetError{
				Type: "mailboxHasChild",
			}
		}
	}

	// Check if mailbox has emails
	var emailsInMailbox []*jmap.Email
	for _, em := range us.emails {
		if em.MailboxIDs[id] {
			emailsInMailbox = append(emailsInMailbox, em)
		}
	}

	if len(emailsInMailbox) > 0 {
		if !onDestroyRemoveMessages {
			return false, jmap.SetError{
				Type: "mailboxHasEmail",
			}
		}
		// Destroy or update messages
		for _, em := range emailsInMailbox {
			if len(em.MailboxIDs) <= 1 {
				// Destroy email
				delete(us.emails, em.ID)
				if th, ok := us.threads[em.ThreadID]; ok {
					var newIDs []jmap.Id
					for _, eid := range th.EmailIDs {
						if eid != em.ID {
							newIDs = append(newIDs, eid)
						}
					}
					if len(newIDs) == 0 {
						delete(us.threads, em.ThreadID)
						mb.recordChange(ctx, us.threadState, em.ThreadID, "destroy", "Thread")
					} else {
						th.EmailIDs = newIDs
						mb.recordChange(ctx, us.threadState, em.ThreadID, "update", "Thread")
					}
				}
				mb.recordChange(ctx, us.emailState, em.ID, "destroy", "Email")
			} else {
				// Remove from this mailbox
				delete(em.MailboxIDs, id)
				mb.recordChange(ctx, us.emailState, em.ID, "update", "Email")
			}
		}
	}

	delete(us.mailboxes, id)
	mb.recalculateMailboxCounts(us)
	mb.recordChange(ctx, us.mailboxState, id, "destroy", "Mailbox")
	return true, nil
}

// GetThreads retrieves threads by ID.

func (mb *MemoryBackend) recalculateMailboxCounts(us *userMailStore) {
	counts := make(map[jmap.Id]*struct{ unread, total uint64 })
	for mID := range us.mailboxes {
		counts[mID] = &struct{ unread, total uint64 }{}
	}

	for _, em := range us.emails {
		isUnread := true
		if em.Keywords != nil {
			if val, hasUnread := em.Keywords["$unread"]; hasUnread {
				isUnread = val
			} else if val, hasSeen := em.Keywords["$seen"]; hasSeen {
				isUnread = !val
			}
		}
		for mID := range em.MailboxIDs {
			if c, ok := counts[mID]; ok {
				c.total++
				if isUnread {
					c.unread++
				}
			}
		}
	}

	for mID, c := range counts {
		if box, ok := us.mailboxes[mID]; ok {
			box.TotalEmails = c.total
			box.UnreadEmails = c.unread
			box.TotalThreads = c.total
			box.UnreadThreads = c.unread
		}
	}
}

// UpdateEmail applies RFC 8621 Section 4.3 patch objects to an existing Email.
