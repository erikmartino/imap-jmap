package testmock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// MemoryContactsBackend provides an in-memory implementation of jmap.ContactsBackend per RFC 9610.
type userContactsStore struct {
	addressBooks map[jmap.Id]*jmap.AddressBook
	cards        map[jmap.Id]*jmap.Card
	abState      *changeTracker
	cardState    *changeTracker
}

type MemoryContactsBackend struct {
	mu          sync.RWMutex
	users       map[string]*userContactsStore
	nextID      uint64
	broadcaster *jmap.Broadcaster
	blobBackend jmap.BlobBackend
}

func (b *MemoryContactsBackend) getStoreLocked(ctx context.Context) *userContactsStore {
	accountID, _ := jmap.AccountIDFromContext(ctx)

	us, ok := b.users[accountID]
	if !ok {
		us = newMemoryUserContactsStore()
		b.users[accountID] = us
	}
	return us
}

// Ensure MemoryContactsBackend implements jmap.ContactsBackend interface.
var _ jmap.ContactsBackend = (*MemoryContactsBackend)(nil)

// SetBroadcaster connects a Broadcaster so AddressBook and Card mutations emit
// RFC 8620 Section 7.1 StateChange push events.
func (b *MemoryContactsBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

// SetBlobBackend attaches a BlobBackend for media blob validations.
func (b *MemoryContactsBackend) SetBlobBackend(bb jmap.BlobBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.blobBackend = bb
}

func newMemoryUserContactsStore() *userContactsStore {
	us := &userContactsStore{
		addressBooks: make(map[jmap.Id]*jmap.AddressBook),
		cards:        make(map[jmap.Id]*jmap.Card),
		abState:      newChangeTracker(1000),
		cardState:    newChangeTracker(1000),
	}

	defaultAB := &jmap.AddressBook{
		ID:        "ab-default",
		Name:      "Personal Contacts",
		SortOrder: 0,
		IsDefault: true,
		MyRights: jmap.AddressBookRights{
			MayRead:   true,
			MayWrite:  true,
			MayShare:  true,
			MayDelete: false,
		},
	}
	us.addressBooks[defaultAB.ID] = defaultAB

	return us
}

// NewMemoryContactsBackend initializes a new MemoryContactsBackend with a default address book.
func NewMemoryContactsBackend() *MemoryContactsBackend {
	b := &MemoryContactsBackend{
		users:  make(map[string]*userContactsStore),
		nextID: 1,
	}
	_ = b.getStoreLocked(context.Background())
	return b
}

// AddressBookState returns the current change state token for AddressBooks per RFC 8620.
func (b *MemoryContactsBackend) AddressBookState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.abState.State()
}

// AddressBookChanges returns created/updated/destroyed AddressBooks since the given state per RFC 8620 Section 5.2.
func (b *MemoryContactsBackend) AddressBookChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.abState.Changes(sinceState)
}

// CardState returns the current change state token for Cards per RFC 8620.
func (b *MemoryContactsBackend) CardState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.cardState.State()
}

// CardChanges returns created/updated/destroyed Cards since the given state per RFC 8620 Section 5.2.
func (b *MemoryContactsBackend) CardChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.cardState.Changes(sinceState)
}

// recordChange records a mutation on the given tracker and publishes the new state token
// to push subscribers.
func (b *MemoryContactsBackend) recordChange(ctx context.Context, tracker *changeTracker, id jmap.Id, action string, typeName string) string {
	newState := tracker.record(id, action)
	if b.broadcaster != nil {
		accountID, _ := jmap.AccountIDFromContext(ctx)
		b.broadcaster.PublishStateChange(accountID, typeName, newState)
	}
	return newState
}

func (b *MemoryContactsBackend) GetAddressBooks(ctx context.Context, ids []jmap.Id) ([]*jmap.AddressBook, []jmap.Id, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.AddressBook
	var notFound []jmap.Id

	if len(ids) == 0 {
		for _, ab := range us.addressBooks {
			list = append(list, ab)
		}
		return list, nil, nil
	}

	for _, id := range ids {
		if ab, ok := us.addressBooks[id]; ok {
			list = append(list, ab)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryContactsBackend) GetAllAddressBooks(ctx context.Context) ([]*jmap.AddressBook, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.AddressBook
	for _, ab := range us.addressBooks {
		list = append(list, ab)
	}
	return list, nil
}

func (b *MemoryContactsBackend) CreateAddressBook(ctx context.Context, ab *jmap.AddressBook) (*jmap.AddressBook, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if ab.ID == "" {
		b.nextID++
		ab.ID = jmap.Id(fmt.Sprintf("ab-%d", b.nextID))
	}
	ab.MyRights = jmap.AddressBookRights{
		MayRead:   true,
		MayWrite:  true,
		MayShare:  true,
		MayDelete: true,
	}
	if ab.IsDefault {
		for _, other := range us.addressBooks {
			other.IsDefault = false
		}
	}
	us.addressBooks[ab.ID] = ab
	b.recordChange(ctx, us.abState, ab.ID, "create", "AddressBook")
	return ab, nil
}

func (b *MemoryContactsBackend) UpdateAddressBook(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.AddressBook, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	ab, ok := us.addressBooks[id]
	if !ok {
		return nil, fmt.Errorf("addressbook not found: %s", id)
	}

	if name, ok := patch["name"].(string); ok && name != "" {
		ab.Name = name
	}
	if desc, ok := patch["description"].(string); ok {
		ab.Description = &desc
	} else if _, present := patch["description"]; present {
		ab.Description = nil
	}
	if sortOrder, ok := patch["sortOrder"].(float64); ok {
		ab.SortOrder = uint64(sortOrder)
	}
	if isSubscribed, ok := patch["isSubscribed"].(bool); ok {
		ab.IsSubscribed = isSubscribed
	}
	if rawShare, present := patch["shareWith"]; present {
		if !ab.MyRights.MayShare {
			return nil, fmt.Errorf("forbidden: user does not have mayShare right")
		}
		if rawShare == nil {
			ab.ShareWith = nil
		} else {
			rawBytes, _ := json.Marshal(rawShare)
			var sw map[string]*jmap.AddressBookRights
			if err := json.Unmarshal(rawBytes, &sw); err == nil {
				ab.ShareWith = sw
			}
		}
	}

	b.recordChange(ctx, us.abState, id, "update", "AddressBook")
	return ab, nil
}

func (b *MemoryContactsBackend) SetDefaultAddressBook(ctx context.Context, id jmap.Id) error {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.addressBooks[id]; !ok {
		return fmt.Errorf("addressbook not found: %s", id)
	}

	for _, ab := range us.addressBooks {
		if ab.ID == id {
			ab.IsDefault = true
		} else {
			ab.IsDefault = false
		}
	}
	b.recordChange(ctx, us.abState, id, "update", "AddressBook")
	return nil
}

func (b *MemoryContactsBackend) AddressBookHasContents(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	for _, card := range us.cards {
		if card.AddressBookIDs != nil && card.AddressBookIDs[id] {
			return true, nil
		}
	}
	return false, nil
}

func (b *MemoryContactsBackend) DeleteAddressBook(ctx context.Context, id jmap.Id, removeContents bool) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.addressBooks[id]; !ok {
		return false, nil
	}

	for cardID, card := range us.cards {
		if card.AddressBookIDs != nil && card.AddressBookIDs[id] {
			delete(card.AddressBookIDs, id)
			if len(card.AddressBookIDs) == 0 {
				if removeContents {
					delete(us.cards, cardID)
					b.recordChange(ctx, us.cardState, cardID, "destroy", "Card")
				}
			} else {
				b.recordChange(ctx, us.cardState, cardID, "update", "Card")
			}
		}
	}

	delete(us.addressBooks, id)
	b.recordChange(ctx, us.abState, id, "destroy", "AddressBook")
	return true, nil
}

func (b *MemoryContactsBackend) GetCards(ctx context.Context, ids []jmap.Id) ([]*jmap.Card, []jmap.Id, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.Card
	var notFound []jmap.Id

	if len(ids) == 0 {
		for _, card := range us.cards {
			list = append(list, card)
		}
		return list, nil, nil
	}

	for _, id := range ids {
		if card, ok := us.cards[id]; ok {
			list = append(list, card)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryContactsBackend) GetAllCards(ctx context.Context) ([]*jmap.Card, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.Card
	for _, card := range us.cards {
		list = append(list, card)
	}
	return list, nil
}

func (b *MemoryContactsBackend) validateCard(ctx context.Context, us *userContactsStore, card *jmap.Card) error {
	if len(card.AddressBookIDs) == 0 {
		return fmt.Errorf("invalidProperties: card must belong to at least one address book")
	}
	hasTrue := false
	for abID, val := range card.AddressBookIDs {
		if val {
			hasTrue = true
			if _, exists := us.addressBooks[abID]; !exists {
				return fmt.Errorf("invalidProperties: addressbook %s does not exist", abID)
			}
		}
	}
	if !hasTrue {
		return fmt.Errorf("invalidProperties: card must belong to at least one address book")
	}

	if card.Media != nil {
		accountID := jmap.AccountIDForSubject("user@example.com")
		if ctxID, ok := jmap.AccountIDFromContext(ctx); ok && ctxID != "" {
			accountID = ctxID
		}
		for _, m := range card.Media {
			if m != nil && m.Kind == "photo" && m.BlobID != "" && b.blobBackend != nil {
				blob, found, err := b.blobBackend.GetBlob(ctx, accountID, string(m.BlobID))
				if err != nil || !found || blob == nil {
					return fmt.Errorf("invalidProperties: photo media blob %s not found", m.BlobID)
				}
			}
		}
	}
	return nil
}

func (b *MemoryContactsBackend) CreateCard(ctx context.Context, card *jmap.Card) (*jmap.Card, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if card.AddressBookIDs == nil {
		card.AddressBookIDs = make(map[jmap.Id]bool)
		for id, ab := range us.addressBooks {
			if ab.IsDefault {
				card.AddressBookIDs[id] = true
				break
			}
		}
		if len(card.AddressBookIDs) == 0 {
			for id := range us.addressBooks {
				card.AddressBookIDs[id] = true
				break
			}
		}
	}

	if err := b.validateCard(ctx, us, card); err != nil {
		return nil, err
	}

	if card.ID == "" {
		b.nextID++
		card.ID = jmap.Id(fmt.Sprintf("card-%d", b.nextID))
	}
	card.Type = "Card"
	if card.Version == "" {
		card.Version = "1.0"
	}
	if card.Uid == "" {
		card.Uid = fmt.Sprintf("urn:uuid:card-%d-%d", b.nextID, time.Now().UnixNano())
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if card.Created == "" {
		card.Created = now
	}
	if card.Updated == "" {
		card.Updated = now
	}
	us.cards[card.ID] = card
	b.recordChange(ctx, us.cardState, card.ID, "create", "Card")
	return card, nil
}

func setNestedMapValue(m map[string]any, parts []string, val any) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		if val == nil {
			delete(m, parts[0])
		} else {
			m[parts[0]] = val
		}
		return
	}
	key := parts[0]
	sub, ok := m[key].(map[string]any)
	if !ok {
		if val == nil {
			return
		}
		sub = make(map[string]any)
		m[key] = sub
	}
	setNestedMapValue(sub, parts[1:], val)
}

func applyCardPatch(card *jmap.Card, patch map[string]any) error {
	raw, err := json.Marshal(card)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if m == nil {
		m = make(map[string]any)
	}

	for path, val := range patch {
		parts := strings.Split(path, "/")
		setNestedMapValue(m, parts, val)
	}

	rawUpdated, err := json.Marshal(m)
	if err != nil {
		return err
	}

	origID := card.ID
	origCreated := card.Created
	var updatedCard jmap.Card
	if err := json.Unmarshal(rawUpdated, &updatedCard); err != nil {
		return err
	}
	updatedCard.ID = origID
	updatedCard.Created = origCreated

	*card = updatedCard
	return nil
}

func (b *MemoryContactsBackend) UpdateCard(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Card, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	card, ok := us.cards[id]
	if !ok {
		return nil, fmt.Errorf("card not found: %s", id)
	}

	if err := applyCardPatch(card, patch); err != nil {
		return nil, err
	}

	if err := b.validateCard(ctx, us, card); err != nil {
		return nil, err
	}

	card.Updated = time.Now().UTC().Format(time.RFC3339)
	b.recordChange(ctx, us.cardState, id, "update", "Card")
	return card, nil
}

func (b *MemoryContactsBackend) DeleteCard(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.cards[id]; !ok {
		return false, nil
	}
	delete(us.cards, id)
	b.recordChange(ctx, us.cardState, id, "destroy", "Card")
	return true, nil
}

func (b *MemoryContactsBackend) QueryCards(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var matched []*jmap.Card
	for _, card := range us.cards {
		if jmap.MatchCard(card, filter) {
			matched = append(matched, card)
		}
	}

	total := len(matched)
	jmap.SortCards(matched, comparators)

	position = jmap.NormalizePosition(position, total)
	if position >= total {
		return []jmap.Id{}, total, nil
	}

	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}

	ids := make([]jmap.Id, 0, end-position)
	for i := position; i < end; i++ {
		ids = append(ids, matched[i].ID)
	}
	return ids, total, nil
}
