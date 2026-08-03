package memory

import (
	"context"
	"imap-jmap/jmap"
)

// SeedSampleData populates realistic sample emails, calendars, and events for server execution.
func SeedSampleData(mb *MemoryBackend, cb *MemoryCalendarsBackend) {
	ctx := context.Background()

	if mb != nil {
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
			SentAt:     "2026-08-02T09:14:30Z",
			Preview:    "Hi Team, please find attached the proposal for the Q3 architecture review meeting scheduled for Wednesday.",
			BlobID:     "blob-stub-3",
			BodyStructure: jmap.EmailBodyPart{
				PartID: "1",
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
			SentAt:        "2026-08-02T18:44:00Z",
			Preview:       "Your cloud hosting invoice for July 2026 is attached ($149.00 USD). Due date: August 15, 2026.",
			BlobID:        "blob-stub-4",
			Attachments: []jmap.EmailBodyPart{
				{PartID: "2", Type: "application/pdf", Name: "Invoice-2026-8891.pdf", Size: 10240},
			},
			BodyStructure: jmap.EmailBodyPart{
				PartID: "1",
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
			SentAt:     "2026-08-03T07:00:00Z",
			Preview:    "Outline for deploying the multi-region Kubernetes cluster with failover routing.",
			BlobID:     "blob-stub-draft",
			BodyStructure: jmap.EmailBodyPart{
				PartID: "1",
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
			SentAt:     "2026-08-02T10:05:00Z",
			Preview:    "Thanks Alex, I reviewed the proposal and accepted the calendar invitation.",
			BlobID:     "blob-stub-sent",
			BodyStructure: jmap.EmailBodyPart{
				PartID: "1",
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
			MyRights: jmap.CalendarRights{
				MayReadItems:  true,
				MayWriteItems: true,
				MayAdmin:      true,
				MayDelete:     true,
			},
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
			Location: &jmap.JSCalendarLocation{
				Name:        "Virtual Meeting Room A",
				Description: "Join via Video Link",
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
					ByDay:     []string{"we"},
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
			Location: &jmap.JSCalendarLocation{
				Name: "Bistro 42 (Downtown)",
			},
			Status: "confirmed",
		})
	}
}
