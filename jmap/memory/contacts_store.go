package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// MemoryContactsBackend provides an in-memory implementation of jmap.ContactsBackend per RFC 9610.
type MemoryContactsBackend struct {
	mu           sync.RWMutex
	addressBooks map[jmap.Id]*jmap.AddressBook
	cards        map[jmap.Id]*jmap.Card
	nextID       uint64
}

// NewMemoryContactsBackend initializes a new MemoryContactsBackend with a default address book.
func NewMemoryContactsBackend() *MemoryContactsBackend {
	b := &MemoryContactsBackend{
		addressBooks: make(map[jmap.Id]*jmap.AddressBook),
		cards:        make(map[jmap.Id]*jmap.Card),
		nextID:       1,
	}

	defaultAB := &jmap.AddressBook{
		ID:        "ab-default",
		Name:      "Personal Contacts",
		SortOrder: 0,
		IsDefault: true,
		MyRights: jmap.AddressBookRights{
			MayReadItems:  true,
			MayWriteItems: true,
			MayAdmin:      true,
			MayDelete:     false,
		},
	}
	b.addressBooks[defaultAB.ID] = defaultAB

	return b
}

func (b *MemoryContactsBackend) GetAddressBooks(ctx context.Context, ids []jmap.Id) ([]*jmap.AddressBook, []jmap.Id, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.AddressBook
	var notFound []jmap.Id

	if len(ids) == 0 {
		for _, ab := range b.addressBooks {
			list = append(list, ab)
		}
		return list, nil, nil
	}

	for _, id := range ids {
		if ab, ok := b.addressBooks[id]; ok {
			list = append(list, ab)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryContactsBackend) GetAllAddressBooks(ctx context.Context) ([]*jmap.AddressBook, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.AddressBook
	for _, ab := range b.addressBooks {
		list = append(list, ab)
	}
	return list, nil
}

func (b *MemoryContactsBackend) CreateAddressBook(ctx context.Context, ab *jmap.AddressBook) (*jmap.AddressBook, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ab.ID == "" {
		b.nextID++
		ab.ID = jmap.Id(fmt.Sprintf("ab-%d", b.nextID))
	}
	ab.MyRights = jmap.AddressBookRights{
		MayReadItems:  true,
		MayWriteItems: true,
		MayAdmin:      true,
		MayDelete:     true,
	}
	b.addressBooks[ab.ID] = ab
	return ab, nil
}

func (b *MemoryContactsBackend) UpdateAddressBook(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.AddressBook, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ab, ok := b.addressBooks[id]
	if !ok {
		return nil, fmt.Errorf("addressbook not found: %s", id)
	}

	if name, ok := patch["name"].(string); ok {
		ab.Name = name
	}
	if desc, ok := patch["description"].(string); ok {
		ab.Description = &desc
	}
	return ab, nil
}

func (b *MemoryContactsBackend) DeleteAddressBook(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.addressBooks[id]; !ok {
		return false, nil
	}
	delete(b.addressBooks, id)
	return true, nil
}

func (b *MemoryContactsBackend) GetCards(ctx context.Context, ids []jmap.Id) ([]*jmap.Card, []jmap.Id, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.Card
	var notFound []jmap.Id

	if len(ids) == 0 {
		for _, card := range b.cards {
			list = append(list, card)
		}
		return list, nil, nil
	}

	for _, id := range ids {
		if card, ok := b.cards[id]; ok {
			list = append(list, card)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryContactsBackend) GetAllCards(ctx context.Context) ([]*jmap.Card, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.Card
	for _, card := range b.cards {
		list = append(list, card)
	}
	return list, nil
}

func (b *MemoryContactsBackend) CreateCard(ctx context.Context, card *jmap.Card) (*jmap.Card, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if card.ID == "" {
		b.nextID++
		card.ID = jmap.Id(fmt.Sprintf("card-%d", b.nextID))
	}
	card.Type = "Card"
	if card.Created == "" {
		card.Created = time.Now().UTC().Format(time.RFC3339)
	}
	card.Updated = time.Now().UTC().Format(time.RFC3339)
	b.cards[card.ID] = card
	return card, nil
}

func (b *MemoryContactsBackend) UpdateCard(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Card, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	card, ok := b.cards[id]
	if !ok {
		return nil, fmt.Errorf("card not found: %s", id)
	}

	card.Updated = time.Now().UTC().Format(time.RFC3339)
	return card, nil
}

func (b *MemoryContactsBackend) DeleteCard(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.cards[id]; !ok {
		return false, nil
	}
	delete(b.cards, id)
	return true, nil
}

func (b *MemoryContactsBackend) QueryCards(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var matched []jmap.Id
	for id, card := range b.cards {
		if filter != nil {
			if abID, ok := filter["inAddressBook"].(string); ok {
				if !card.AddressBookIDs[jmap.Id(abID)] {
					continue
				}
			}
		}
		matched = append(matched, id)
	}

	total := len(matched)
	if position >= total {
		return []jmap.Id{}, total, nil
	}

	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}

	return matched[position:end], total, nil
}
