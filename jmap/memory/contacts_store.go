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
	blobBackend jmap.BlobBackend
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
		accountID := "primary"
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

// MatchCard reports whether a card satisfies an RFC 9610 Section 3.3.1 filter condition or FilterOperator.
func MatchCard(card *jmap.Card, filter map[string]any) bool {
	if filter == nil {
		return true
	}
	if opVal, ok := filter["operator"]; ok {
		op, _ := opVal.(string)
		var conds []map[string]any
		if rawConds, ok := filter["conditions"].([]any); ok {
			for _, c := range rawConds {
				if cm, ok := c.(map[string]any); ok {
					conds = append(conds, cm)
				}
			}
		} else if rawConds, ok := filter["conditions"].([]map[string]any); ok {
			conds = rawConds
		}

		switch strings.ToUpper(op) {
		case "AND":
			for _, c := range conds {
				if !MatchCard(card, c) {
					return false
				}
			}
			return true
		case "OR":
			for _, c := range conds {
				if MatchCard(card, c) {
					return true
				}
			}
			return false
		case "NOT":
			for _, c := range conds {
				if MatchCard(card, c) {
					return false
				}
			}
			return true
		}
	}

	for k, v := range filter {
		if k == "operator" || k == "conditions" {
			continue
		}
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

