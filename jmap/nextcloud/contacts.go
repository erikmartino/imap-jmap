package nextcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav/carddav"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/vcardconv"
)

// ContactsBackend implements jmap.ContactsBackend backed by Nextcloud CardDAV via github.com/emersion/go-webdav/carddav.
type ContactsBackend struct {
	client      *Client
	mu          sync.RWMutex
	broadcaster *jmap.Broadcaster

	abStates   map[string]int
	cardStates map[string]int
	abPaths    map[string]map[jmap.Id]string
	cardsCache map[string]map[jmap.Id]*jmap.Card
}

var _ jmap.ContactsBackend = (*ContactsBackend)(nil)

// NewContactsBackend initializes a new Nextcloud-backed ContactsBackend.
func NewContactsBackend(client *Client) *ContactsBackend {
	return &ContactsBackend{
		client:     client,
		abStates:   make(map[string]int),
		cardStates: make(map[string]int),
		abPaths:    make(map[string]map[jmap.Id]string),
		cardsCache: make(map[string]map[jmap.Id]*jmap.Card),
	}
}

func (b *ContactsBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *ContactsBackend) emitStateChange(u, typeName, newState string) {
	b.mu.RLock()
	bc := b.broadcaster
	b.mu.RUnlock()
	if bc != nil {
		accountID := jmap.AccountIDForSubject(u)
		bc.PublishStateChange(accountID, typeName, newState)
		if accountID != u {
			bc.PublishStateChange(u, typeName, newState)
		}
	}
}

func (b *ContactsBackend) user(ctx context.Context) string {
	u, _ := b.client.getUserAndPass(ctx)
	return u
}

// AddressBookState
func (b *ContactsBackend) AddressBookState(ctx context.Context) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.user(ctx)
	st, ok := b.abStates[u]
	if !ok {
		st = 1
		b.abStates[u] = st
	}
	return strconv.Itoa(st)
}

func (b *ContactsBackend) AddressBookChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	cur := b.AddressBookState(ctx)
	if sinceState == cur {
		return nil, nil, nil, cur, false
	}
	abs, _ := b.GetAllAddressBooks(ctx)
	var created []jmap.Id
	for _, ab := range abs {
		created = append(created, ab.ID)
	}
	return created, nil, nil, cur, false
}

func (b *ContactsBackend) GetAllAddressBooks(ctx context.Context) ([]*jmap.AddressBook, error) {
	abs, _, err := b.GetAddressBooks(ctx, nil)
	return abs, err
}

func (b *ContactsBackend) getAddressBookHomeSet(ctx context.Context, cardClient *carddav.Client, u string) string {
	principal, err := cardClient.FindCurrentUserPrincipal(ctx)
	if err == nil && principal != "" {
		homeSet, err := cardClient.FindAddressBookHomeSet(ctx, principal)
		if err == nil && homeSet != "" {
			return homeSet
		}
	}
	return "addressbooks/users/" + u + "/"
}

func (b *ContactsBackend) getABPath(u string, abID jmap.Id, homeSet string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.abPaths[u] != nil {
		if p, ok := b.abPaths[u][abID]; ok && p != "" {
			return p
		}
	}
	if homeSet != "" {
		return strings.TrimRight(homeSet, "/") + "/" + string(abID) + "/"
	}
	return "addressbooks/users/" + u + "/" + string(abID) + "/"
}

func (b *ContactsBackend) GetAddressBooks(ctx context.Context, ids []jmap.Id) ([]*jmap.AddressBook, []jmap.Id, error) {
	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
	abList, err := cardClient.FindAddressBooks(ctx, homeSet)
	if err != nil {
		defaultAB := &jmap.AddressBook{
			ID:        jmap.Id("contacts"),
			Name:      "Contacts",
			IsDefault: true,
		}
		b.mu.Lock()
		if b.abPaths[u] == nil {
			b.abPaths[u] = make(map[jmap.Id]string)
		}
		b.abPaths[u]["contacts"] = strings.TrimRight(homeSet, "/") + "/contacts/"
		b.mu.Unlock()
		return []*jmap.AddressBook{defaultAB}, nil, nil
	}

	var list []*jmap.AddressBook
	pathMap := make(map[jmap.Id]string)
	idMap := make(map[jmap.Id]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	for _, ab := range abList {
		abID := path.Base(strings.TrimRight(ab.Path, "/"))
		if strings.HasPrefix(abID, "z-") {
			continue
		}

		aid := jmap.Id(abID)
		pathMap[aid] = ab.Path

		if len(ids) > 0 && !idMap[aid] {
			continue
		}

		name := ab.Name
		if name == "" {
			name = abID
		}

		isDefault := abID == "contacts" || strings.EqualFold(name, "Contacts")
		list = append(list, &jmap.AddressBook{
			ID:        aid,
			Name:      name,
			IsDefault: isDefault,
		})
	}

	if len(list) == 0 {
		aid := jmap.Id("contacts")
		pathMap[aid] = strings.TrimRight(homeSet, "/") + "/contacts/"
		list = append(list, &jmap.AddressBook{
			ID:        aid,
			Name:      "Contacts",
			IsDefault: true,
		})
	}

	b.mu.Lock()
	if b.abPaths[u] == nil {
		b.abPaths[u] = make(map[jmap.Id]string)
	}
	for k, v := range pathMap {
		b.abPaths[u][k] = v
	}
	b.mu.Unlock()

	var notFound []jmap.Id
	if len(ids) > 0 {
		foundMap := make(map[jmap.Id]bool)
		for _, ab := range list {
			foundMap[ab.ID] = true
		}
		for _, id := range ids {
			if !foundMap[id] {
				notFound = append(notFound, id)
			}
		}
	}

	return list, notFound, nil
}

func (b *ContactsBackend) CreateAddressBook(ctx context.Context, ab *jmap.AddressBook) (*jmap.AddressBook, error) {
	if ab == nil {
		return nil, fmt.Errorf("address book is nil")
	}
	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return nil, err
	}

	if ab.ID == "" {
		ab.ID = jmap.Id(fmt.Sprintf("ab-%d", time.Now().UnixNano()))
	}

	homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
	abPath := strings.TrimRight(homeSet, "/") + "/" + string(ab.ID) + "/"
	_ = cardClient.Mkdir(ctx, abPath)

	b.mu.Lock()
	if b.abPaths[u] == nil {
		b.abPaths[u] = make(map[jmap.Id]string)
	}
	b.abPaths[u][ab.ID] = abPath
	b.abStates[u]++
	st := strconv.Itoa(b.abStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "AddressBook", st)
	return ab, nil
}

func (b *ContactsBackend) UpdateAddressBook(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.AddressBook, error) {
	u := b.user(ctx)
	b.mu.Lock()
	b.abStates[u]++
	st := strconv.Itoa(b.abStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "AddressBook", st)
	return &jmap.AddressBook{ID: id, Name: "Contacts"}, nil
}

func (b *ContactsBackend) DeleteAddressBook(ctx context.Context, id jmap.Id, removeContents bool) (bool, error) {
	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return false, err
	}

	homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
	abPath := b.getABPath(u, id, homeSet)
	_ = cardClient.RemoveAll(ctx, abPath)

	b.mu.Lock()
	if b.abPaths[u] != nil {
		delete(b.abPaths[u], id)
	}
	b.abStates[u]++
	st := strconv.Itoa(b.abStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "AddressBook", st)
	return true, nil
}

func (b *ContactsBackend) SetDefaultAddressBook(ctx context.Context, id jmap.Id) error {
	return nil
}

func (b *ContactsBackend) AddressBookHasContents(ctx context.Context, id jmap.Id) (bool, error) {
	cards, _, err := b.GetCards(ctx, nil)
	if err != nil {
		return false, err
	}
	for _, c := range cards {
		if c.AddressBookIDs[id] {
			return true, nil
		}
	}
	return false, nil
}

// CardState
func (b *ContactsBackend) CardState(ctx context.Context) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.user(ctx)
	st, ok := b.cardStates[u]
	if !ok {
		st = 1
		b.cardStates[u] = st
	}
	return strconv.Itoa(st)
}

func (b *ContactsBackend) CardChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	cur := b.CardState(ctx)
	if sinceState == cur {
		return nil, nil, nil, cur, false
	}
	cards, _ := b.GetAllCards(ctx)
	var created []jmap.Id
	for _, c := range cards {
		created = append(created, c.ID)
	}
	return created, nil, nil, cur, false
}

func (b *ContactsBackend) GetAllCards(ctx context.Context) ([]*jmap.Card, error) {
	cards, _, err := b.GetCards(ctx, nil)
	return cards, err
}

func (b *ContactsBackend) GetCards(ctx context.Context, ids []jmap.Id) ([]*jmap.Card, []jmap.Id, error) {
	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	b.mu.Lock()
	if b.cardsCache[u] == nil {
		b.cardsCache[u] = make(map[jmap.Id]*jmap.Card)
	}
	b.mu.Unlock()

	abs, _, _ := b.GetAddressBooks(ctx, nil)
	homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
	idMap := make(map[jmap.Id]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	for _, ab := range abs {
		abPath := b.getABPath(u, ab.ID, homeSet)
		objs, qErr := cardClient.QueryAddressBook(ctx, abPath, &carddav.AddressBookQuery{
			DataRequest: carddav.AddressDataRequest{
				AllProp: true,
			},
		})
		if qErr != nil {
			continue
		}

		for _, ao := range objs {
			if ao.Card == nil {
				continue
			}
			name := path.Base(ao.Path)
			rawID := strings.TrimSuffix(name, ".vcf")
			cardID := jmap.Id(rawID)

			if len(ids) > 0 && !idMap[cardID] {
				continue
			}

			var buf bytes.Buffer
			_ = vcard.NewEncoder(&buf).Encode(ao.Card)

			rawMap, vErr := vcardconv.FromVCard(buf.String())
			if vErr == nil && rawMap != nil {
				jsonBytes, _ := json.Marshal(rawMap)
				var card jmap.Card
				if err := json.Unmarshal(jsonBytes, &card); err == nil {
					card.ID = cardID
					if card.AddressBookIDs == nil {
						card.AddressBookIDs = make(map[jmap.Id]bool)
					}
					card.AddressBookIDs[ab.ID] = true
					b.mu.Lock()
					b.cardsCache[u][cardID] = &card
					b.mu.Unlock()
				}
			}
		}
	}

	b.mu.RLock()
	var list []*jmap.Card
	var notFound []jmap.Id
	if len(ids) == 0 {
		for _, c := range b.cardsCache[u] {
			list = append(list, c)
		}
	} else {
		for _, id := range ids {
			if c, ok := b.cardsCache[u][id]; ok {
				list = append(list, c)
			} else {
				notFound = append(notFound, id)
			}
		}
	}
	b.mu.RUnlock()

	return list, notFound, nil
}

func (b *ContactsBackend) CreateCard(ctx context.Context, card *jmap.Card) (*jmap.Card, error) {
	if card == nil {
		return nil, fmt.Errorf("card is nil")
	}
	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return nil, err
	}

	if card.ID == "" {
		card.ID = jmap.Id(fmt.Sprintf("card-%d", time.Now().UnixNano()))
	}
	if card.Uid == "" {
		card.Uid = string(card.ID)
	}
	if card.Type == "" {
		card.Type = "Card"
	}
	if card.Version == "" {
		card.Version = "1.0"
	}

	abs, _, _ := b.GetAddressBooks(ctx, nil)
	homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)

	abID := ""
	if len(card.AddressBookIDs) > 0 {
		for aid := range card.AddressBookIDs {
			if aid != "ab-default" && aid != "" {
				abID = string(aid)
				break
			}
		}
	}
	if abID == "" {
		if len(abs) > 0 {
			abID = string(abs[0].ID)
		} else {
			abID = "contacts"
		}
		if card.AddressBookIDs == nil {
			card.AddressBookIDs = make(map[jmap.Id]bool)
		}
		delete(card.AddressBookIDs, "ab-default")
		card.AddressBookIDs[jmap.Id(abID)] = true
	}

	abPath := b.getABPath(u, jmap.Id(abID), homeSet)
	_ = cardClient.Mkdir(ctx, abPath)

	cardBytes, _ := json.Marshal(card)
	var cardMap map[string]any
	_ = json.Unmarshal(cardBytes, &cardMap)

	vcfPayload, err := vcardconv.ToVCard(cardMap)
	if err != nil {
		name := ""
		if card.Name != nil && card.Name.Full != "" {
			name = card.Name.Full
		}
		vcfPayload = fmt.Sprintf("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:%s\r\nFN:%s\r\nEND:VCARD\r\n", card.Uid, name)
	}

	dec := vcard.NewDecoder(strings.NewReader(vcfPayload))
	cardObj, decErr := dec.Decode()
	if decErr != nil {
		return nil, fmt.Errorf("failed to decode vcard: %w", decErr)
	}

	cardPath := strings.TrimRight(abPath, "/") + "/" + string(card.ID) + ".vcf"
	_, putErr := cardClient.PutAddressObject(ctx, cardPath, cardObj)
	if putErr != nil {
		return nil, fmt.Errorf("failed to put address object via carddav client: %w", putErr)
	}

	b.mu.Lock()
	if b.cardsCache[u] == nil {
		b.cardsCache[u] = make(map[jmap.Id]*jmap.Card)
	}
	b.cardsCache[u][card.ID] = card
	b.cardStates[u]++
	st := strconv.Itoa(b.cardStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "ContactCard", st)
	return card, nil
}

func setNestedMapVal(m map[string]any, parts []string, val any) {
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
	setNestedMapVal(sub, parts[1:], val)
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
		setNestedMapVal(m, parts, val)
	}

	rawUpdated, err := json.Marshal(m)
	if err != nil {
		return err
	}

	origID := card.ID
	origUid := card.Uid
	origABIDs := card.AddressBookIDs
	var updatedCard jmap.Card
	if err := json.Unmarshal(rawUpdated, &updatedCard); err != nil {
		return err
	}
	updatedCard.ID = origID
	if updatedCard.Uid == "" {
		updatedCard.Uid = origUid
	}
	if len(updatedCard.AddressBookIDs) == 0 {
		updatedCard.AddressBookIDs = origABIDs
	}

	*card = updatedCard
	return nil
}

func (b *ContactsBackend) UpdateCard(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Card, error) {
	cards, _, err := b.GetCards(ctx, []jmap.Id{id})
	if err != nil || len(cards) == 0 {
		return nil, fmt.Errorf("card %s not found", id)
	}
	card := cards[0]
	oldAbID := ""
	for aid := range card.AddressBookIDs {
		oldAbID = string(aid)
		break
	}

	if err := applyCardPatch(card, patch); err != nil {
		return nil, err
	}

	newAbID := ""
	for aid := range card.AddressBookIDs {
		newAbID = string(aid)
		break
	}
	if oldAbID != "" && newAbID != "" && oldAbID != newAbID {
		cardClient, u, cErr := b.client.CardDAV(ctx)
		if cErr == nil {
			homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
			oldPath := strings.TrimRight(b.getABPath(u, jmap.Id(oldAbID), homeSet), "/") + "/" + string(id) + ".vcf"
			_ = cardClient.RemoveAll(ctx, oldPath)
		}
	}

	return b.CreateCard(ctx, card)
}

func (b *ContactsBackend) DeleteCard(ctx context.Context, id jmap.Id) (bool, error) {
	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return false, err
	}

	cards, _, _ := b.GetCards(ctx, []jmap.Id{id})
	abID := "contacts"
	if len(cards) > 0 {
		for aid := range cards[0].AddressBookIDs {
			abID = string(aid)
			break
		}
	}

	homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
	abPath := b.getABPath(u, jmap.Id(abID), homeSet)
	cardPath := strings.TrimRight(abPath, "/") + "/" + string(id) + ".vcf"
	_ = cardClient.RemoveAll(ctx, cardPath)

	b.mu.Lock()
	if b.cardsCache[u] != nil {
		delete(b.cardsCache[u], id)
	}
	b.cardStates[u]++
	st := strconv.Itoa(b.cardStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "ContactCard", st)
	return true, nil
}

func (b *ContactsBackend) QueryCards(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	cards, _, err := b.GetCards(ctx, nil)
	if err != nil {
		return nil, 0, err
	}

	var matched []*jmap.Card
	for _, c := range cards {
		if memory.MatchCard(c, filter) {
			matched = append(matched, c)
		}
	}

	total := len(matched)
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
