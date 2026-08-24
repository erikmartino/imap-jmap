package memory

import (
	"context"
	"imap-jmap/jmap"
)

// SeedSampleData populates realistic sample emails, calendars, and events for server execution.
func SeedSampleData(mb *MemoryBackend, cb *MemoryCalendarsBackend) {
	ctx := context.Background()

	if mb != nil {
		p1 := "1"
		p2 := "2"
		s3 := "2026-08-02T09:14:30Z"
		s4 := "2026-08-02T18:44:00Z"
		sDraft := "2026-08-03T07:00:00Z"
		sSent := "2026-08-02T10:05:00Z"
		nInvoice := "Invoice-2026-8891.pdf"

		// Populate realistic emails
		stub3 := &jmap.Email{
			Subject:    "Q3 Product Architecture & Roadmap Review",
			From:       []jmap.EmailAddress{{Name: "Alex Vance", Email: "alex.architect@example.com"}},
			To:         []jmap.EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
			CC:         []jmap.EmailAddress{{Name: "Engineering Team", Email: "eng-team@example.com"}},
			MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
			Keywords:   map[string]bool{"$seen": true, "Work": true},
			Size:       3580,
			ReceivedAt: "2026-08-02T09:15:00Z",
			SentAt:     &s3,
			Preview:    "Hi Team, please find attached the proposal for the Q3 architecture review meeting scheduled for Wednesday.",
			BlobID:     "blob-stub-3",
			BodyStructure: jmap.EmailBodyPart{
				PartID: &p1,
				Type:   "text/plain",
				Size:   180,
			},
			BodyValues: map[string]jmap.EmailBodyValue{
				"1": {Value: "Hi Team,\n\nPlease review the attached proposal for the Q3 architecture review meeting scheduled for Wednesday at 14:00 UTC.\n\nBest regards,\nAlex Vance"},
			},
		}
		_, _ = mb.CreateEmail(ctx, stub3)

		stub4 := &jmap.Email{
			Subject:       "Invoice #2026-8891 - Cloud Hosting Services",
			From:          []jmap.EmailAddress{{Name: "Cloud Billing", Email: "billing@cloudprovider.example.com"}},
			To:            []jmap.EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
			MailboxIDs:    map[jmap.Id]bool{"mb-inbox": true},
			Keywords:      map[string]bool{"$seen": false, "Finance": true},
			HasAttachment: true,
			Size:          12450,
			ReceivedAt:    "2026-08-02T18:45:00Z",
			SentAt:        &s4,
			Preview:       "Your cloud hosting invoice for July 2026 is attached ($149.00 USD). Due date: August 15, 2026.",
			BlobID:        "blob-stub-4",
			Attachments: []jmap.EmailBodyPart{
				{PartID: &p2, Type: "application/pdf", Name: &nInvoice, Size: 10240},
			},
			BodyStructure: jmap.EmailBodyPart{
				PartID: &p1,
				Type:   "text/plain",
				Size:   120,
			},
			BodyValues: map[string]jmap.EmailBodyValue{
				"1": {Value: "Dear Customer,\n\nYour monthly hosting invoice for July 2026 is now available. Amount: $149.00 USD.\n\nThank you for choosing CloudProvider."},
			},
		}
		_, _ = mb.CreateEmail(ctx, stub4)

		stubDraft := &jmap.Email{
			Subject:    "Draft: High-Availability Cluster Deployment Plan",
			From:       []jmap.EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
			To:         []jmap.EmailAddress{{Name: "DevOps Team", Email: "devops@example.com"}},
			MailboxIDs: map[jmap.Id]bool{"mb-drafts": true},
			Keywords:   map[string]bool{"$draft": true},
			Size:       1500,
			ReceivedAt: "2026-08-03T07:00:00Z",
			SentAt:     &sDraft,
			Preview:    "Outline for deploying the multi-region Kubernetes cluster with failover routing.",
			BlobID:     "blob-stub-draft",
			BodyStructure: jmap.EmailBodyPart{
				PartID: &p1,
				Type:   "text/plain",
				Size:   110,
			},
			BodyValues: map[string]jmap.EmailBodyValue{
				"1": {Value: "Team,\n\nHere is the initial outline for our multi-region Kubernetes cluster deployment plan..."},
			},
		}
		_, _ = mb.CreateEmail(ctx, stubDraft)

		stubSent := &jmap.Email{
			Subject:    "Re: Q3 Product Architecture & Roadmap Review",
			From:       []jmap.EmailAddress{{Name: "Primary User", Email: "user@example.com"}},
			To:         []jmap.EmailAddress{{Name: "Alex Vance", Email: "alex.architect@example.com"}},
			MailboxIDs: map[jmap.Id]bool{"mb-sent": true},
			Keywords:   map[string]bool{"$seen": true},
			Size:       1800,
			ReceivedAt: "2026-08-02T10:05:00Z",
			SentAt:     &sSent,
			Preview:    "Thanks Alex, I reviewed the proposal and accepted the calendar invitation.",
			BlobID:     "blob-stub-sent",
			BodyStructure: jmap.EmailBodyPart{
				PartID: &p1,
				Type:   "text/plain",
				Size:   100,
			},
			BodyValues: map[string]jmap.EmailBodyValue{
				"1": {Value: "Thanks Alex,\n\nI reviewed the proposal and accepted the calendar invitation for Wednesday."},
			},
		}
		_, _ = mb.CreateEmail(ctx, stubSent)
	}

	if cb != nil {
		workCalColor := "#3b82f6"
		workCalDesc := "Work events and team meetings"
		workCal, _ := cb.CreateCalendar(ctx, &jmap.Calendar{
			ID:          "cal-work",
			Name:        "Work Calendar",
			Color:       &workCalColor,
			Description: &workCalDesc,
			SortOrder:   10,
			IsDefault:   false,
			IsVisible:   true,
			MyRights:    jmap.FullCalendarRights(),
		})

		calID := jmap.Id("cal-default")
		if workCal != nil {
			calID = workCal.ID
		}

		_, _ = cb.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
			ID:          "ev-arch-review",
			CalendarIDs: map[jmap.Id]bool{calID: true},
			Type:        "Event",
			Title:       "Q3 Product Architecture & Roadmap Review",
			Description: "Detailed design review for upcoming JMAP extensions and high-availability cluster topology.",
			Start:       "2026-08-05T14:00:00Z",
			Duration:    "PT1H30M",
			TimeZone:    "UTC",
			Locations: map[string]*jmap.JSCalendarLocation{
				"loc-1": {
					Name:        "Virtual Meeting Room A",
					Description: "Join via Video Link",
				},
			},
			VirtualLocations: map[string]*jmap.JSCalendarVirtualLocation{
				"v1": {
					URI:         "https://meet.example.com/arch-review",
					Name:        "Video Call",
					Description: "Browser link",
				},
			},
			Status:         "confirmed",
			FreeBusyStatus: "busy",
			Privacy:        "public",
			Participants: map[string]*jmap.JSCalendarParticipant{
				"alex.architect@example.com": {
					Name:   "Alex Vance",
					Email:  "alex.architect@example.com",
					Role:   "chair",
					Status: "accepted",
				},
				"user@example.com": {
					Name:   "Primary User",
					Email:  "user@example.com",
					Role:   "attendee",
					Status: "accepted",
				},
			},
			Alerts: map[string]*jmap.JSCalendarAlert{
				"a1": {
					Trigger:     "-PT15M",
					Action:      "display",
					Description: "15 minute reminder",
				},
			},
		})

		_, _ = cb.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
			ID:          "ev-weekly-sync",
			CalendarIDs: map[jmap.Id]bool{calID: true},
			Type:        "Event",
			Title:       "Engineering Team Weekly Standup",
			Description: "Weekly progress updates, blocker triage, and task coordination.",
			Start:       "2026-08-05T09:00:00Z",
			Duration:    "PT30M",
			TimeZone:    "UTC",
			Status:      "confirmed",
			RecurrenceRules: []*jmap.JSCalendarRecurrenceRule{
				{
					Frequency: "weekly",
					Interval:  1,
					ByDay:     []*jmap.NDay{{Day: "we"}},
				},
			},
		})

		_, _ = cb.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
			ID:          "ev-lunch-personal",
			CalendarIDs: map[jmap.Id]bool{"cal-default": true},
			Type:        "Event",
			Title:       "Team Lunch at Bistro 42",
			Start:       "2026-08-06T12:00:00Z",
			Duration:    "PT1H",
			TimeZone:    "UTC",
			Locations: map[string]*jmap.JSCalendarLocation{
				"loc-1": {
					Name: "Bistro 42 (Downtown)",
				},
			},
			Status: "confirmed",
		})
	}
}

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

		// Resolve the real mailbox IDs by role. The memory backend keeps the
		// hardcoded "mb-*" ids, while gateway backends (e.g. IMAP/SMTP) derive
		// ids from the underlying folder names — so never assume a mailbox id
		// here; look it up from the backend that actually owns the data.
		roleIDs := make(map[string]jmap.Id)
		if mbs, err := mb.GetAllMailboxes(accountCtx); err == nil {
			for _, m := range mbs {
				if m != nil && m.Role != nil && *m.Role != "" {
					roleIDs[*m.Role] = m.ID
				}
			}
		}
		inboxID := roleIDs[jmap.RoleInbox]
		sentID := roleIDs[jmap.RoleSent]
		draftsID := roleIDs[jmap.RoleDrafts]
		archiveID := roleIDs[jmap.RoleArchive]

		emails, _ := mb.GetAllEmails(accountCtx)
		if len(emails) == 0 && inboxID != "" {
			p1 := "1"
			s1 := "2026-08-01T11:59:00Z"
			s2 := "2026-08-02T10:29:00Z"
			sSent := "2026-08-01T12:05:00Z"
			sDraft := "2026-08-03T07:00:00Z"
			sArch := "2026-08-01T10:00:00Z"
			if blobB != nil {
				_, _ = blobB.PutBlob(accountCtx, accountID, "text/plain", []byte("Welcome to your new JMAP mail server."))
			}
			stub1 := &jmap.Email{
				Subject:       "Welcome to JMAP Server",
				From:          []jmap.EmailAddress{{Name: "JMAP Admin", Email: "admin@example.com"}},
				To:            []jmap.EmailAddress{{Name: userEmail, Email: userEmail}},
				MailboxIDs:    map[jmap.Id]bool{inboxID: true},
				Keywords:      map[string]bool{"$seen": false},
				Size:          1024,
				ReceivedAt:    "2026-08-01T12:00:00Z",
				SentAt:        &s1,
				Preview:       "Welcome to your new JMAP mail server.",
				BlobID:        "blob-stub-1",
				BodyStructure: jmap.EmailBodyPart{PartID: &p1, Type: "text/plain", Size: 40},
				BodyValues:    map[string]jmap.EmailBodyValue{"1": {Value: "Welcome to your new JMAP mail server."}},
			}
			_, _ = mb.CreateEmail(accountCtx, stub1)

			stub2 := &jmap.Email{
				Subject:       "JMAP Core and Mail Specifications",
				From:          []jmap.EmailAddress{{Name: "IETF JMAP Working Group", Email: "jmap-wg@ietf.example.org"}},
				To:            []jmap.EmailAddress{{Name: userEmail, Email: userEmail}},
				MailboxIDs:    map[jmap.Id]bool{inboxID: true},
				Keywords:      map[string]bool{"$seen": true, "$flagged": true},
				Size:          4096,
				ReceivedAt:    "2026-08-02T10:30:00Z",
				SentAt:        &s2,
				Preview:       "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail).",
				BlobID:        "blob-stub-2",
				BodyStructure: jmap.EmailBodyPart{PartID: &p1, Type: "text/plain", Size: 88},
				BodyValues:    map[string]jmap.EmailBodyValue{"1": {Value: "This email verifies that your server supports RFC 8620 (JMAP Core) and RFC 8621 (JMAP Mail)."}},
			}
			_, _ = mb.CreateEmail(accountCtx, stub2)

			if sentID != "" {
				stubSent := &jmap.Email{
					Subject:       "Re: Welcome to JMAP Server",
					From:          []jmap.EmailAddress{{Name: userEmail, Email: userEmail}},
					To:            []jmap.EmailAddress{{Name: "JMAP Admin", Email: "admin@example.com"}},
					MailboxIDs:    map[jmap.Id]bool{sentID: true},
					Keywords:      map[string]bool{"$seen": true},
					Size:          512,
					ReceivedAt:    "2026-08-01T12:05:00Z",
					SentAt:        &sSent,
					Preview:       "Thank you! Glad to be using JMAP.",
					BlobID:        "blob-stub-sent",
					BodyStructure: jmap.EmailBodyPart{PartID: &p1, Type: "text/plain", Size: 34},
					BodyValues:    map[string]jmap.EmailBodyValue{"1": {Value: "Thank you! Glad to be using JMAP."}},
				}
				_, _ = mb.CreateEmail(accountCtx, stubSent)
			}

			if draftsID != "" {
				stubDraft := &jmap.Email{
					Subject:       "Draft: High-Availability Cluster Deployment Plan",
					From:          []jmap.EmailAddress{{Name: userEmail, Email: userEmail}},
					To:            []jmap.EmailAddress{{Name: "DevOps Team", Email: "devops@example.com"}},
					MailboxIDs:    map[jmap.Id]bool{draftsID: true},
					Keywords:      map[string]bool{"$draft": true},
					Size:          1500,
					ReceivedAt:    "2026-08-03T07:00:00Z",
					SentAt:        &sDraft,
					Preview:       "Initial draft outline for HA cluster deployment...",
					BlobID:        "blob-stub-draft",
					BodyStructure: jmap.EmailBodyPart{PartID: &p1, Type: "text/plain", Size: 48},
					BodyValues:    map[string]jmap.EmailBodyValue{"1": {Value: "Initial draft outline for HA cluster deployment..."}},
				}
				_, _ = mb.CreateEmail(accountCtx, stubDraft)
			}

			if archiveID != "" {
				stubArchive := &jmap.Email{
					Subject:       "Welcome to your account",
					From:          []jmap.EmailAddress{{Name: "System", Email: "system@example.com"}},
					To:            []jmap.EmailAddress{{Name: userEmail, Email: userEmail}},
					MailboxIDs:    map[jmap.Id]bool{archiveID: true},
					Keywords:      map[string]bool{"$seen": true},
					Size:          384,
					ReceivedAt:    "2026-08-01T10:00:00Z",
					SentAt:        &sArch,
					Preview:       "Account setup completed successfully.",
					BlobID:        "blob-stub-archive",
					BodyStructure: jmap.EmailBodyPart{PartID: &p1, Type: "text/plain", Size: 37},
					BodyValues:    map[string]jmap.EmailBodyValue{"1": {Value: "Account setup completed successfully."}},
				}
				_, _ = mb.CreateEmail(accountCtx, stubArchive)
			}
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
				Kind:           "individual",
				Name:           &jmap.JSContactName{Full: "Alice Smith"},
				Emails:         map[string]*jmap.JSContactEmailAddress{"e1": {Address: "alice@example.com"}},
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
