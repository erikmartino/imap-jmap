package jmap_test

import (
	"testing"

	"imap-jmap/jmap"
)

// TestMatchesFilter_Comprehensive Coverage tests both positive matching AND negative filtering
// for all Email/query FilterCondition properties per AGENTS.md rule.
func TestMatchesFilter_Comprehensive(t *testing.T) {
	email := &jmap.Email{
		ID:         "em-1",
		Subject:    "Project Update Meeting",
		Preview:    "Discussion on Q3 deliverables",
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true, "mb-project": true},
		Keywords:   map[string]bool{"$seen": true, "$flagged": true},
		From:       []jmap.EmailAddress{{Name: "Alice Smith", Email: "alice@example.com"}},
		To:         []jmap.EmailAddress{{Name: "Bob Jones", Email: "bob@example.com"}},
		CC:         []jmap.EmailAddress{{Name: "Charlie Brown", Email: "charlie@example.com"}},
		BCC:        []jmap.EmailAddress{{Name: "David Miller", Email: "david@example.com"}},
		Size:       1500,
		ReceivedAt: "2026-08-01T12:00:00Z",
		HasAttachment: true,
		BodyValues: map[string]jmap.EmailBodyValue{
			"1": {Value: "Detailed agenda for the Q3 strategy meeting."},
		},
	}

	tests := []struct {
		name     string
		filter   map[string]any
		expected bool
	}{
		// inMailbox
		{"inMailbox positive", map[string]any{"inMailbox": "mb-inbox"}, true},
		{"inMailbox negative", map[string]any{"inMailbox": "mb-trash"}, false},

		// inMailboxOtherThan
		{"inMailboxOtherThan positive", map[string]any{"inMailboxOtherThan": []any{"mb-trash"}}, true},
		{"inMailboxOtherThan negative", map[string]any{"inMailboxOtherThan": []any{"mb-inbox", "mb-project"}}, false},

		// before
		{"before positive", map[string]any{"before": "2026-08-02T00:00:00Z"}, true},
		{"before negative", map[string]any{"before": "2026-08-01T00:00:00Z"}, false},

		// after
		{"after positive", map[string]any{"after": "2026-07-31T00:00:00Z"}, true},
		{"after negative", map[string]any{"after": "2026-08-02T00:00:00Z"}, false},

		// minSize / maxSize
		{"minSize positive", map[string]any{"minSize": float64(1000)}, true},
		{"minSize negative", map[string]any{"minSize": float64(2000)}, false},
		{"maxSize positive", map[string]any{"maxSize": float64(2000)}, true},
		{"maxSize negative", map[string]any{"maxSize": float64(1000)}, false},

		// hasAttachment
		{"hasAttachment positive", map[string]any{"hasAttachment": true}, true},
		{"hasAttachment negative", map[string]any{"hasAttachment": false}, false},

		// subject / cc / bcc / body
		{"subject positive", map[string]any{"subject": "Update"}, true},
		{"subject negative", map[string]any{"subject": "Invoice"}, false},
		{"cc positive", map[string]any{"cc": "charlie@example.com"}, true},
		{"cc negative", map[string]any{"cc": "unknown@example.com"}, false},
		{"bcc positive", map[string]any{"bcc": "david@example.com"}, true},
		{"bcc negative", map[string]any{"bcc": "unknown@example.com"}, false},
		{"body positive", map[string]any{"body": "agenda"}, true},
		{"body negative", map[string]any{"body": "confidential"}, false},

		// hasKeyword / notKeyword
		{"hasKeyword positive", map[string]any{"hasKeyword": "$seen"}, true},

		// text
		{"text positive", map[string]any{"text": "deliverables"}, true},
		{"text negative", map[string]any{"text": "unrelated"}, false},

		// FilterOperators AND / OR / NOT
		{"operator AND positive", map[string]any{
			"operator": "AND",
			"conditions": []any{
				map[string]any{"inMailbox": "mb-inbox"},
				map[string]any{"hasAttachment": true},
			},
		}, true},
		{"operator AND negative", map[string]any{
			"operator": "AND",
			"conditions": []any{
				map[string]any{"inMailbox": "mb-inbox"},
				map[string]any{"subject": "Invoice"},
			},
		}, false},
		{"operator OR positive", map[string]any{
			"operator": "OR",
			"conditions": []any{
				map[string]any{"subject": "Invoice"},
				map[string]any{"inMailbox": "mb-inbox"},
			},
		}, true},
		{"operator OR negative", map[string]any{
			"operator": "OR",
			"conditions": []any{
				map[string]any{"subject": "Invoice"},
				map[string]any{"inMailbox": "mb-trash"},
			},
		}, false},
		{"operator NOT positive", map[string]any{
			"operator": "NOT",
			"conditions": []any{
				map[string]any{"subject": "Invoice"},
			},
		}, true},
		{"operator NOT negative", map[string]any{
			"operator": "NOT",
			"conditions": []any{
				map[string]any{"inMailbox": "mb-inbox"},
			},
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jmap.MatchesFilter(email, tt.filter)
			if got != tt.expected {
				t.Errorf("MatchesFilter(%s) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}
