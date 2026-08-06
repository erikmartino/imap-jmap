package jmap

import (
	"testing"
)

func TestMatchPrincipal(t *testing.T) {
	desc := "Developer at ACME"
	email := "alice@example.com"
	tz := "America/New_York"

	p := &Principal{
		ID:          "p1",
		Type:        "individual",
		Name:        "Alice Smith",
		Description: &desc,
		Email:       &email,
		TimeZone:    &tz,
		Accounts: map[string]*Account{
			"acc1": {},
		},
	}

	tests := []struct {
		name   string
		filter map[string]any
		want   bool
	}{
		{
			name:   "accountIds match positive",
			filter: map[string]any{"accountIds": []any{"acc1"}},
			want:   true,
		},
		{
			name:   "accountIds match negative",
			filter: map[string]any{"accountIds": []any{"acc2"}},
			want:   false,
		},
		{
			name:   "email match positive",
			filter: map[string]any{"email": "alice@"},
			want:   true,
		},
		{
			name:   "email match negative",
			filter: map[string]any{"email": "bob@"},
			want:   false,
		},
		{
			name:   "name match positive",
			filter: map[string]any{"name": "Alice"},
			want:   true,
		},
		{
			name:   "name match negative",
			filter: map[string]any{"name": "Bob"},
			want:   false,
		},
		{
			name:   "text match in name positive",
			filter: map[string]any{"text": "Smith"},
			want:   true,
		},
		{
			name:   "text match in email positive",
			filter: map[string]any{"text": "example.com"},
			want:   true,
		},
		{
			name:   "text match in description positive",
			filter: map[string]any{"text": "ACME"},
			want:   true,
		},
		{
			name:   "text match negative",
			filter: map[string]any{"text": "NonExistent"},
			want:   false,
		},
		{
			name:   "type match positive",
			filter: map[string]any{"type": "individual"},
			want:   true,
		},
		{
			name:   "type match negative",
			filter: map[string]any{"type": "group"},
			want:   false,
		},
		{
			name:   "timeZone match positive",
			filter: map[string]any{"timeZone": "America/New_York"},
			want:   true,
		},
		{
			name:   "timeZone match negative",
			filter: map[string]any{"timeZone": "UTC"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchPrincipal(p, tt.filter)
			if got != tt.want {
				t.Errorf("MatchPrincipal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeNullEventBusyPeriods(t *testing.T) {
	ev := &CalendarEvent{ID: "e1"}
	periods := []*BusyPeriod{
		{UTCStart: "2026-08-06T10:00:00Z", UTCEnd: "2026-08-06T11:00:00Z", BusyStatus: "tentative", Event: nil},
		{UTCStart: "2026-08-06T10:30:00Z", UTCEnd: "2026-08-06T11:30:00Z", BusyStatus: "confirmed", Event: nil},
		{UTCStart: "2026-08-06T12:00:00Z", UTCEnd: "2026-08-06T13:00:00Z", BusyStatus: "unavailable", Event: ev},
	}

	merged := MergeNullEventBusyPeriods(periods)
	if len(merged) != 2 {
		t.Fatalf("expected 2 periods, got %d", len(merged))
	}

	// Non-null event should be preserved
	hasEv := false
	hasNullMerged := false
	for _, p := range merged {
		if p.Event != nil {
			hasEv = true
		} else {
			hasNullMerged = true
			if p.BusyStatus != "confirmed" {
				t.Errorf("expected merged busyStatus 'confirmed', got %q", p.BusyStatus)
			}
			if p.UTCStart != "2026-08-06T10:00:00Z" || p.UTCEnd != "2026-08-06T11:30:00Z" {
				t.Errorf("unexpected merged range: %s to %s", p.UTCStart, p.UTCEnd)
			}
		}
	}

	if !hasEv || !hasNullMerged {
		t.Errorf("missing expected periods in output: hasEv=%v, hasNullMerged=%v", hasEv, hasNullMerged)
	}
}

func TestParseRFC3339TimeAndCloneCalendarEvent(t *testing.T) {
	t.Run("ParseRFC3339Time", func(t *testing.T) {
		_, ok := ParseRFC3339Time("2026-08-06T12:00:00Z")
		if !ok {
			t.Error("expected valid RFC3339 parse")
		}
		_, ok = ParseRFC3339Time("invalid-time")
		if ok {
			t.Error("expected invalid parse for bogus time string")
		}
	})

	t.Run("CloneCalendarEvent", func(t *testing.T) {
		if CloneCalendarEvent(nil) != nil {
			t.Error("expected nil clone for nil event")
		}

		orig := &CalendarEvent{ID: "e1", Title: "Meeting"}
		cloned := CloneCalendarEvent(orig)
		if cloned == nil || cloned.ID != "e1" || cloned.Title != "Meeting" {
			t.Errorf("unexpected clone result: %+v", cloned)
		}
		if cloned == orig {
			t.Error("clone should be a new pointer")
		}
	})
}
