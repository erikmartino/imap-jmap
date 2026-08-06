package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
}

func (b *MemoryContactsBackend) getStoreLocked(ctx context.Context) *userContactsStore {
	accountID := "primary"
	if ctxID, ok := jmap.AccountIDFromContext(ctx); ok && ctxID != "" {
		accountID = ctxID
	}

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
			MayReadItems:  true,
			MayWriteItems: true,
			MayAdmin:      true,
			MayDelete:     false,
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
		accountID := "primary"
		if ctxID, ok := jmap.AccountIDFromContext(ctx); ok && ctxID != "" {
			accountID = ctxID
		}
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
		MayReadItems:  true,
		MayWriteItems: true,
		MayAdmin:      true,
		MayDelete:     true,
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
	if isDefault, ok := patch["isDefault"].(bool); ok && isDefault {
		for _, other := range us.addressBooks {
			if other.ID != ab.ID {
				other.IsDefault = false
			}
		}
		ab.IsDefault = true
	}

	b.recordChange(ctx, us.abState, id, "update", "AddressBook")
	return ab, nil
}

func (b *MemoryContactsBackend) DeleteAddressBook(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.addressBooks[id]; !ok {
		return false, nil
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

func (b *MemoryContactsBackend) CreateCard(ctx context.Context, card *jmap.Card) (*jmap.Card, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if card.ID == "" {
		b.nextID++
		card.ID = jmap.Id(fmt.Sprintf("card-%d", b.nextID))
	}
	card.Type = "Card"
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

// decodeInto marshals src to JSON and unmarshals it into a typed destination,
// mirroring how JMAP /set patches arrive over the wire.
func decodeInto(src any, dest any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dest)
}

// setCardField applies a single RFC 9553 / RFC 9610 Section 3.5 card patch path.
// A null value removes the property; immutable fields (id, uid, created, kind)
// are deliberately not applied.
func setCardField(card *jmap.Card, path string, val any) {
	switch path {
	case "addressBookIds":
		if val == nil {
			card.AddressBookIDs = nil
			return
		}
		var ids map[string]bool
		if err := decodeJSONField(val, &ids); err != nil {
			return
		}
		card.AddressBookIDs = make(map[jmap.Id]bool)
		for k, v := range ids {
			if v {
				card.AddressBookIDs[jmap.Id(k)] = true
			}
		}
	case "name":
		if val == nil {
			card.Name = nil
			return
		}
		var name jmap.JSContactName
		if err := decodeJSONField(val, &name); err == nil {
			card.Name = &name
		}
	case "nicknames":
		if val == nil {
			card.Nicknames = nil
			return
		}
		var m map[string]*jmap.JSContactNickname
		if err := decodeJSONField(val, &m); err == nil {
			card.Nicknames = m
		}
	case "emails":
		if val == nil {
			card.Emails = nil
			return
		}
		var m map[string]*jmap.JSContactEmailAddress
		if err := decodeJSONField(val, &m); err == nil {
			card.Emails = m
		}
	case "phones":
		if val == nil {
			card.Phones = nil
			return
		}
		var m map[string]*jmap.JSContactPhone
		if err := decodeJSONField(val, &m); err == nil {
			card.Phones = m
		}
	case "addresses":
		if val == nil {
			card.Addresses = nil
			return
		}
		var m map[string]*jmap.JSContactAddress
		if err := decodeJSONField(val, &m); err == nil {
			card.Addresses = m
		}
	case "organizations":
		if val == nil {
			card.Organizations = nil
			return
		}
		var m map[string]*jmap.JSContactOrganization
		if err := decodeJSONField(val, &m); err == nil {
			card.Organizations = m
		}
	case "titles":
		if val == nil {
			card.Titles = nil
			return
		}
		var m map[string]*jmap.JSContactTitle
		if err := decodeJSONField(val, &m); err == nil {
			card.Titles = m
		}
	case "notes":
		if val == nil {
			card.Notes = nil
			return
		}
		var m map[string]*jmap.JSContactNote
		if err := decodeJSONField(val, &m); err == nil {
			card.Notes = m
		}
	case "onlineServices":
		if val == nil {
			card.OnlineServices = nil
			return
		}
		var m map[string]*jmap.JSContactOnlineService
		if err := decodeJSONField(val, &m); err == nil {
			card.OnlineServices = m
		}
	case "members":
		if val == nil {
			card.Members = nil
			return
		}
		var m map[string]bool
		if err := decodeJSONField(val, &m); err == nil {
			card.Members = m
		}
	case "keywords":
		if val == nil {
			card.Keywords = nil
			return
		}
		var m map[string]bool
		if err := decodeJSONField(val, &m); err == nil {
			card.Keywords = m
		}
	}
}

func (b *MemoryContactsBackend) UpdateCard(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Card, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	card, ok := us.cards[id]
	if !ok {
		return nil, fmt.Errorf("card not found: %s", id)
	}

	for path, val := range patch {
		setCardField(card, path, val)
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

// MatchCard reports whether a card satisfies an RFC 9610 Section 3.3.1 filter
// condition. All specified conditions must match (logical AND).
func MatchCard(card *jmap.Card, filter map[string]any) bool {
	for k, v := range filter {
		switch k {
		case "inAddressBook":
			ab, ok := v.(string)
			if !ok || !card.AddressBookIDs[jmap.Id(ab)] {
				return false
			}
		case "uid":
			s, _ := v.(string)
			if card.Uid != s {
				return false
			}
		case "hasMember":
			s, _ := v.(string)
			if !card.Members[s] {
				return false
			}
		case "kind":
			s, _ := v.(string)
			if card.Kind != s {
				return false
			}
		case "createdBefore":
			s, _ := v.(string)
			if card.Created == "" || card.Created >= s {
				return false
			}
		case "createdAfter":
			s, _ := v.(string)
			if card.Created == "" || card.Created < s {
				return false
			}
		case "updatedBefore":
			s, _ := v.(string)
			if card.Updated == "" || card.Updated >= s {
				return false
			}
		case "updatedAfter":
			s, _ := v.(string)
			if card.Updated == "" || card.Updated < s {
				return false
			}
		case "name":
			s, _ := v.(string)
			if !matchesCardName(card.Name, s) {
				return false
			}
		case "name/given", "name/surname", "name/surname2":
			s, _ := v.(string)
			if !matchesNameKind(card.Name, strings.TrimPrefix(k, "name/"), s) {
				return false
			}
		case "nickname":
			s, _ := v.(string)
			if !matchesNickname(card.Nicknames, s) {
				return false
			}
		case "organization":
			s, _ := v.(string)
			if !matchesOrganization(card.Organizations, s) {
				return false
			}
		case "email":
			s, _ := v.(string)
			if !matchesEmails(card.Emails, s) {
				return false
			}
		case "phone":
			s, _ := v.(string)
			if !matchesPhones(card.Phones, s) {
				return false
			}
		case "onlineService":
			s, _ := v.(string)
			if !matchesOnlineService(card.OnlineServices, s) {
				return false
			}
		case "address":
			s, _ := v.(string)
			if !matchesAddresses(card.Addresses, s) {
				return false
			}
		case "note":
			s, _ := v.(string)
			if !matchesNotes(card.Notes, s) {
				return false
			}
		case "text":
			s, _ := v.(string)
			if !matchesCardText(card, s) {
				return false
			}
		}
	}
	return true
}

func getCardNameComponent(card *jmap.Card, kind string) string {
	if card == nil || card.Name == nil {
		return ""
	}
	for _, comp := range card.Name.Components {
		if comp != nil && comp.Kind == kind {
			return comp.Value
		}
	}
	return ""
}

func sortCards(cards []*jmap.Card, comparators []jmap.Comparator) {
	if len(comparators) == 0 {
		return
	}
	sort.SliceStable(cards, func(i, j int) bool {
		c1, c2 := cards[i], cards[j]
		for _, comp := range comparators {
			var v1, v2 string
			switch comp.Property {
			case "created":
				v1, v2 = c1.Created, c2.Created
			case "updated":
				v1, v2 = c1.Updated, c2.Updated
			case "name/given":
				v1 = getCardNameComponent(c1, "given")
				v2 = getCardNameComponent(c2, "given")
			case "name/surname":
				v1 = getCardNameComponent(c1, "surname")
				v2 = getCardNameComponent(c2, "surname")
			case "name/surname2":
				v1 = getCardNameComponent(c1, "surname2")
				v2 = getCardNameComponent(c2, "surname2")
			default:
				continue
			}

			if v1 == v2 {
				continue
			}
			if comp.IsAscending {
				return v1 < v2
			}
			return v1 > v2
		}
		return string(c1.ID) < string(c2.ID)
	})
}

func (b *MemoryContactsBackend) QueryCards(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var matched []*jmap.Card
	for _, card := range us.cards {
		if MatchCard(card, filter) {
			matched = append(matched, card)
		}
	}

	total := len(matched)
	sortCards(matched, comparators)

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
