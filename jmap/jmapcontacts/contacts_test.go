package jmapcontacts

import (
	"testing"

	"imap-jmap/jmap/jmapcore"
)

func TestMatchCardText(t *testing.T) {
	card := &Card{
		ID: "c1",
		Name: &JSContactName{
			Full: "Alice Smith",
			Components: []*JSContactNameComponent{
				{Value: "Alice", Kind: "given"},
				{Value: "Smith", Kind: "surname"},
			},
		},
		Emails: map[string]*JSContactEmailAddress{
			"e1": {Address: "alice@example.com"},
		},
	}

	filterMatch := map[string]any{"text": "alice"}
	if !MatchCard(card, filterMatch) {
		t.Errorf("expected card to match text 'alice'")
	}

	filterMismatch := map[string]any{"text": "bob"}
	if MatchCard(card, filterMismatch) {
		t.Errorf("expected card NOT to match text 'bob'")
	}
}

func TestGetCardNameComponent(t *testing.T) {
	card := &Card{
		ID: "c1",
		Name: &JSContactName{
			Full: "Alice Smith",
			Components: []*JSContactNameComponent{
				{Value: "Alice", Kind: "given"},
				{Value: "Smith", Kind: "surname"},
			},
		},
	}

	if given := GetCardNameComponent(card, "given"); given != "Alice" {
		t.Errorf("expected Alice, got %s", given)
	}
	if surname := GetCardNameComponent(card, "surname"); surname != "Smith" {
		t.Errorf("expected Smith, got %s", surname)
	}
	if missing := GetCardNameComponent(card, "prefix"); missing != "" {
		t.Errorf("expected empty string for missing prefix, got %s", missing)
	}
}

func TestSortCards(t *testing.T) {
	c1 := &Card{
		ID: "c1",
		Name: &JSContactName{
			Components: []*JSContactNameComponent{{Value: "Bob", Kind: "given"}},
		},
	}
	c2 := &Card{
		ID: "c2",
		Name: &JSContactName{
			Components: []*JSContactNameComponent{{Value: "Alice", Kind: "given"}},
		},
	}

	cards := []*Card{c1, c2}
	SortCards(cards, []jmapcore.Comparator{{Property: "name/given", IsAscending: true}})

	if cards[0].ID != "c2" || cards[1].ID != "c1" {
		t.Errorf("expected [c2, c1], got [%s, %s]", cards[0].ID, cards[1].ID)
	}
}
