package jmap

import (
	"context"
	"testing"
)

func TestGroupParticipantInviteExpansion(t *testing.T) {
	principalsBackend := &mockPrincipalsBackend{
		principals: map[Id]*Principal{
			"p-team": {
				ID:      "p-team",
				Type:    "group",
				Name:    "Engineering Team",
				Email:   "team@example.com",
				Members: map[string]bool{"p-alice": true, "p-bob": true},
			},
			"p-alice": {
				ID:    "p-alice",
				Type:  "individual",
				Name:  "Alice Smith",
				Email: "alice@example.com",
			},
			"p-bob": {
				ID:    "p-bob",
				Type:  "individual",
				Name:  "Bob Jones",
				Email: "bob@example.com",
			},
		},
	}

	recipients := map[string]string{
		"p-team": "team@example.com",
	}

	expanded := expandGroupRecipients(context.Background(), principalsBackend, recipients)
	if len(expanded) != 2 {
		t.Fatalf("expected 2 expanded recipients, got %d: %v", len(expanded), expanded)
	}
	if expanded["p-alice"] != "alice@example.com" {
		t.Errorf("expected p-alice to be alice@example.com, got %s", expanded["p-alice"])
	}
	if expanded["p-bob"] != "bob@example.com" {
		t.Errorf("expected p-bob to be bob@example.com, got %s", expanded["p-bob"])
	}
}

type mockPrincipalsBackend struct {
	principals map[Id]*Principal
}

func (m *mockPrincipalsBackend) GetPrincipals(ctx context.Context, ids []Id) ([]*Principal, []Id, error) {
	var list []*Principal
	var notFound []Id
	for _, id := range ids {
		if p, ok := m.principals[id]; ok {
			list = append(list, p)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (m *mockPrincipalsBackend) GetAllPrincipals(ctx context.Context) ([]*Principal, error) {
	var list []*Principal
	for _, p := range m.principals {
		list = append(list, p)
	}
	return list, nil
}

func (m *mockPrincipalsBackend) CreatePrincipal(ctx context.Context, principal *Principal) (*Principal, error) {
	return principal, nil
}

func (m *mockPrincipalsBackend) UpdatePrincipal(ctx context.Context, id Id, patch map[string]any) (*Principal, error) {
	return nil, nil
}

func (m *mockPrincipalsBackend) DeletePrincipal(ctx context.Context, id Id) (bool, error) {
	return true, nil
}

func (m *mockPrincipalsBackend) QueryPrincipals(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]Id, int, error) {
	var ids []Id
	for id := range m.principals {
		ids = append(ids, id)
	}
	return ids, len(ids), nil
}

func (m *mockPrincipalsBackend) GetAvailability(ctx context.Context, principalID Id, utcStart, utcEnd string) ([]*AvailabilityWindow, error) {
	return nil, nil
}

func (m *mockPrincipalsBackend) PrincipalState(ctx context.Context) string {
	return "1"
}

func (m *mockPrincipalsBackend) PrincipalChanges(ctx context.Context, sinceState string) (created, updated, destroyed []Id, newState string, hasMore bool) {
	return nil, nil, nil, "1", false
}
