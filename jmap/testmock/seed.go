package testmock

import (
	"context"
	"imap-jmap/jmap"
)

// SeedAccountSampleData populates sample emails, calendars, contacts, and filenodes for an account on first use.
func SeedAccountSampleData(ctx context.Context, accountID string, mb jmap.MailBackend, blobB jmap.BlobBackend, cb jmap.CalendarsBackend, contactsB jmap.ContactsBackend, fnB jmap.FileNodeBackend) {
	if ctx == nil {
		ctx = context.Background()
	}
	accountCtx := jmap.ContextWithAccountID(ctx, accountID)
	userEmail, _ := jmap.SubjectForAccountID(accountID)
	if userEmail == "" {
		userEmail = accountID
	}

	if mb != nil {
		seedStandardMailboxes(accountCtx, mb)

		// Resolve the real mailbox IDs by role.
		roleIDs := make(map[string]jmap.Id)
		if mbs, err := mb.GetAllMailboxes(accountCtx); err == nil {
			for _, m := range mbs {
				if m != nil && m.Role != nil && *m.Role != "" {
					roleIDs[*m.Role] = m.ID
				}
			}
		}
		inboxID := roleIDs[jmap.RoleInbox]

		emails, _ := mb.GetAllEmails(accountCtx)
		if len(emails) == 0 && inboxID != "" {
			p1 := "1"
			s1 := "2026-08-01T11:59:00Z"
			s2 := "2026-08-02T10:29:00Z"
			if blobB != nil {
				_, _ = blobB.PutBlob(accountCtx, accountID, "text/plain", []byte("Welcome to your new JMAP mail server."))
			}
			stubStatus := "signed"
			stubVerifiedWith := "admin@example.com"
			stub1 := &jmap.Email{
				Subject:           "Welcome to JMAP Server",
				From:              []jmap.EmailAddress{{Name: "JMAP Admin", Email: "admin@example.com"}},
				To:                []jmap.EmailAddress{{Name: userEmail, Email: userEmail}},
				MailboxIDs:        map[jmap.Id]bool{inboxID: true},
				Keywords:          map[string]bool{"$seen": true},
				Size:              1024,
				ReceivedAt:        "2026-08-01T12:00:00Z",
				SentAt:            &s1,
				Preview:           "Welcome to your new JMAP mail server.",
				BlobID:            "blob-stub-1",
				SMIMEStatus:       &stubStatus,
				SMIMEVerifiedWith: &stubVerifiedWith,
				BodyStructure:     jmap.EmailBodyPart{PartID: &p1, Type: "text/plain", Size: 40},
				BodyValues:        map[string]jmap.EmailBodyValue{"1": {Value: "Welcome to your new JMAP mail server."}},
			}
			_, _ = mb.CreateEmail(accountCtx, stub1)

			stub2 := &jmap.Email{
				Subject:       "JMAP Core and Mail Specifications",
				From:          []jmap.EmailAddress{{Name: "IETF JMAP Working Group", Email: "noreply@ietf.org"}},
				To:            []jmap.EmailAddress{{Name: userEmail, Email: userEmail}},
				MailboxIDs:    map[jmap.Id]bool{inboxID: true},
				Keywords:      map[string]bool{"$flagged": true},
				Size:          4096,
				ReceivedAt:    "2026-08-02T10:30:00Z",
				SentAt:        &s2,
				Preview:       "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail).",
				BlobID:        "blob-stub-2",
				BodyStructure: jmap.EmailBodyPart{PartID: &p1, Type: "text/plain", Size: 88},
				BodyValues:    map[string]jmap.EmailBodyValue{"1": {Value: "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail)."}},
			}
			_, _ = mb.CreateEmail(accountCtx, stub2)
		}
	}

	if cb != nil {
		events, _, _ := cb.GetCalendarEvents(accountCtx, nil)
		if len(events) == 0 {
			ev1 := &jmap.CalendarEvent{
				ID:          "ev-seed-1",
				CalendarIDs: map[jmap.Id]bool{"cal-default": true},
				Type:        "Event",
				Title:       "Welcome & Onboarding",
				Description: "Initial team onboarding and platform overview.",
				Start:       "2026-08-05T10:00:00Z",
				Duration:    "PT1H",
				TimeZone:    "UTC",
				Status:      "confirmed",
			}
			_, _ = cb.CreateCalendarEvent(accountCtx, ev1)
		}
	}

	if contactsB != nil {
		cards, _, _ := contactsB.GetCards(accountCtx, nil)
		if len(cards) == 0 {
			card1 := &jmap.Card{
				ID:             "card-seed-1",
				AddressBookIDs: map[jmap.Id]bool{"ab-default": true},
				Type:           "Card",
				Version:        "1.0",
				Kind:           "individual",
				Name: &jmap.JSContactName{
					Full: "Alice Smith",
					Components: []*jmap.JSContactNameComponent{
						{Value: "Alice", Kind: "given"},
						{Value: "Smith", Kind: "surname"},
					},
				},
				Emails: map[string]*jmap.JSContactEmailAddress{"e1": {Address: "alice@example.com"}},
			}
			_, _ = contactsB.CreateCard(accountCtx, card1)
		}
	}

	if fnB != nil {
		nodes, _ := fnB.GetAllFileNodes(accountCtx)
		if len(nodes) == 0 {
			folderID := jmap.Id("fn-folder-documents")
			file1BlobID := jmap.Id("blob-seed-welcome")
			file2BlobID := jmap.Id("blob-seed-notes")

			folder := &jmap.FileNode{
				ID:       folderID,
				Name:     "Documents",
				IsFolder: true,
			}
			file1 := &jmap.FileNode{
				ID:       "fn-file-welcome",
				Name:     "welcome.txt",
				ParentID: &folderID,
				BlobID:   &file1BlobID,
				Size:     28,
				IsFolder: false,
			}
			file2 := &jmap.FileNode{
				ID:       "fn-file-notes",
				Name:     "notes.pdf",
				ParentID: &folderID,
				BlobID:   &file2BlobID,
				Size:     1024,
				IsFolder: false,
			}
			_, _ = fnB.CreateFileNode(accountCtx, folder)
			_, _ = fnB.CreateFileNode(accountCtx, file1)
			_, _ = fnB.CreateFileNode(accountCtx, file2)
		}
	}
}

// seedStandardMailboxes ensures all standard role mailboxes expected by a mail
// client exist on a newly seeded account (idempotent helper).
func seedStandardMailboxes(ctx context.Context, mb jmap.MailBackend) {
	existing, err := mb.GetAllMailboxes(ctx)
	if err != nil {
		return
	}
	hasRole := make(map[string]bool, len(existing))
	for _, mailbox := range existing {
		if mailbox != nil && mailbox.Role != nil {
			hasRole[*mailbox.Role] = true
		}
	}

	for _, spec := range []struct {
		id, name, role string
		sortOrder      uint64
	}{
		{id: "mb-sent", name: "Sent", role: jmap.RoleSent, sortOrder: 20},
		{id: "mb-drafts", name: "Drafts", role: jmap.RoleDrafts, sortOrder: 30},
		{id: "mb-junk", name: "Junk", role: jmap.RoleJunk, sortOrder: 40},
		{id: "mb-trash", name: "Trash", role: jmap.RoleTrash, sortOrder: 50},
		{id: "mb-archive", name: "Archive", role: jmap.RoleArchive, sortOrder: 60},
	} {
		if hasRole[spec.role] {
			continue
		}
		role := spec.role
		_, _ = mb.CreateMailbox(ctx, &jmap.Mailbox{
			ID:           jmap.Id(spec.id),
			Name:         spec.name,
			Role:         &role,
			SortOrder:    spec.sortOrder,
			IsSubscribed: true,
		})
	}
}
