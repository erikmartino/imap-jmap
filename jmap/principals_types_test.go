package jmap

import (
	"testing"
)

func TestMatchPrincipal(t *testing.T) {
	p := &Principal{
		ID:          "p1",
		Type:        "individual",
		Name:        "Alice Smith",
		Description: "Developer at ACME",
		Email:       "alice@example.com",
		AccountIDs: map[string]bool{
			"acc1": true,
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
