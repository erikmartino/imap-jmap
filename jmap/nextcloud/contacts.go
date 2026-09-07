package nextcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav/carddav"

	"imap-jmap/jmap"
	"imap-jmap/jmap/vcardconv"
)

// ContactsBackend implements jmap.ContactsBackend backed by Nextcloud CardDAV via github.com/emersion/go-webdav/carddav.
type ContactsBackend struct {
	client      *Client
	mu          sync.RWMutex
	trackersMu  sync.Mutex
	broadcaster *jmap.Broadcaster

	abTrackers          map[string]*jmap.ChangeTracker
	cardTrackers        map[string]*jmap.ChangeTracker
	abPaths             map[string]map[jmap.Id]string
	cardPaths           map[string]map[jmap.Id]string
	homeSets            map[string]string
	defaultAddressBooks map[string]jmap.Id
	absCache            map[string][]*jmap.AddressBook
	cardsCache          map[string]map[jmap.Id]*jmap.Card
}

var _ jmap.ContactsBackend = (*ContactsBackend)(nil)

// NewContactsBackend initializes a new Nextcloud-backed ContactsBackend.
func NewContactsBackend(client *Client) *ContactsBackend {
	return &ContactsBackend{
		client:              client,
		abTrackers:          make(map[string]*jmap.ChangeTracker),
		cardTrackers:        make(map[string]*jmap.ChangeTracker),
		abPaths:             make(map[string]map[jmap.Id]string),
		cardPaths:           make(map[string]map[jmap.Id]string),
		homeSets:            make(map[string]string),
		defaultAddressBooks: make(map[string]jmap.Id),
		absCache:            make(map[string][]*jmap.AddressBook),
		cardsCache:          make(map[string]map[jmap.Id]*jmap.Card),
	}
}

func (b *ContactsBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *ContactsBackend) emitStateChange(u, typeName, newState string) {
	bc := b.broadcaster
	if bc != nil {
		accountID := jmap.AccountIDForSubject(u)
		bc.PublishStateChange(accountID, typeName, newState)
		if typeName == "Card" {
			bc.PublishStateChange(accountID, "ContactCard", newState)
		} else if typeName == "ContactCard" {
			bc.PublishStateChange(accountID, "Card", newState)
		}
		if accountID != u {
			bc.PublishStateChange(u, typeName, newState)
			if typeName == "Card" {
				bc.PublishStateChange(u, "ContactCard", newState)
			} else if typeName == "ContactCard" {
				bc.PublishStateChange(u, "Card", newState)
			}
		}
	}
}

func (b *ContactsBackend) user(ctx context.Context) string {
	u, _ := b.client.getUserAndPass(ctx)
	return u
}

func (b *ContactsBackend) getABTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.abTrackers[u] == nil {
		b.abTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.abTrackers[u]
}

func (b *ContactsBackend) getCardTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.cardTrackers[u] == nil {
		b.cardTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.cardTrackers[u]
}

// AddressBookState
func (b *ContactsBackend) AddressBookState(ctx context.Context) string {
	return b.getABTracker(b.user(ctx)).State()
}

func (b *ContactsBackend) AddressBookChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return b.getABTracker(b.user(ctx)).Changes(sinceState)
}

func (b *ContactsBackend) GetAllAddressBooks(ctx context.Context) ([]*jmap.AddressBook, error) {
	abs, _, err := b.GetAddressBooks(ctx, nil)
	return abs, err
}

func (b *ContactsBackend) getAddressBookHomeSet(ctx context.Context, cardClient *carddav.Client, u string) string {
	b.mu.RLock()
	if hs, ok := b.homeSets[u]; ok && hs != "" {
		b.mu.RUnlock()
		return hs
	}
	b.mu.RUnlock()

	principal, err := cardClient.FindCurrentUserPrincipal(ctx)
	if err == nil && principal != "" {
		homeSet, err := cardClient.FindAddressBookHomeSet(ctx, principal)
		if err == nil && homeSet != "" {
			b.mu.Lock()
			b.homeSets[u] = homeSet
			b.mu.Unlock()
			return homeSet
		}
	}
	defaultHS := "addressbooks/users/" + u + "/"
	b.mu.Lock()
	b.homeSets[u] = defaultHS
	b.mu.Unlock()
	return defaultHS
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

	b.mu.RLock()
	defID := b.defaultAddressBooks[u]
	b.mu.RUnlock()

	homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
	abList, _ := cardClient.FindAddressBooks(ctx, homeSet)

	var list []*jmap.AddressBook
	pathMap := make(map[jmap.Id]string)
	idMap := make(map[jmap.Id]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	if abList != nil {
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

			list = append(list, &jmap.AddressBook{
				ID:   aid,
				Name: name,
			})
		}
	}

	b.mu.Lock()
	if b.absCache[u] != nil {
		for _, cachedAB := range b.absCache[u] {
			found := false
			for _, existing := range list {
				if existing.ID == cachedAB.ID {
					found = true
					break
				}
			}
			if !found && (len(ids) == 0 || idMap[cachedAB.ID]) {
				copyAB := *cachedAB
				list = append(list, &copyAB)
			}
		}
	}
	b.mu.Unlock()

	if len(list) == 0 && (len(ids) == 0 || idMap["contacts"] || idMap["ab-default"]) {
		aid := jmap.Id("contacts")
		pathMap[aid] = strings.TrimRight(homeSet, "/") + "/contacts/"
		list = append(list, &jmap.AddressBook{
			ID:   aid,
			Name: "Contacts",
		})
	}

	for _, ab := range list {
		if defID != "" {
			ab.IsDefault = (ab.ID == defID || (defID == "ab-default" && ab.ID == "contacts"))
		} else {
			ab.IsDefault = (ab.ID == "contacts" || ab.ID == "ab-default" || strings.EqualFold(ab.Name, "Contacts"))
		}
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
			if ab.ID == "contacts" {
				foundMap["ab-default"] = true
			}
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
	if b.absCache[u] == nil {
		b.absCache[u] = make([]*jmap.AddressBook, 0)
	}
	abCopy := *ab
	b.absCache[u] = append(b.absCache[u], &abCopy)
	st := b.getABTracker(u).Record(ab.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "AddressBook", st)
	return ab, nil
}

func (b *ContactsBackend) UpdateAddressBook(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.AddressBook, error) {
	abs, notFound, err := b.GetAddressBooks(ctx, []jmap.Id{id})
	if err != nil {
		return nil, err
	}
	if len(notFound) > 0 || len(abs) == 0 {
		return nil, jmap.ErrNotFound
	}
	ab := abs[0]
	if name, ok := patch["name"].(string); ok && name != "" {
		ab.Name = name
	}
	u := b.user(ctx)
	b.mu.Lock()
	st := b.getABTracker(u).Record(id, "update")
	b.mu.Unlock()

	b.emitStateChange(u, "AddressBook", st)
	return ab, nil
}

func (b *ContactsBackend) DeleteAddressBook(ctx context.Context, id jmap.Id, removeContents bool) (bool, error) {
	abs, notFound, err := b.GetAddressBooks(ctx, []jmap.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(abs) == 0 {
		return false, nil
	}

	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return false, err
	}

	if removeContents {
		cards, _, _ := b.GetCards(ctx, nil)
		for _, c := range cards {
			if c.AddressBookIDs[id] {
				delete(c.AddressBookIDs, id)
				if len(c.AddressBookIDs) == 0 {
					_, _ = b.DeleteCard(ctx, c.ID)
				}
			}
		}
	}

	homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
	abPath := b.getABPath(u, id, homeSet)
	_ = cardClient.RemoveAll(ctx, abPath)

	b.mu.Lock()
	if b.abPaths[u] != nil {
		delete(b.abPaths[u], id)
	}
	if b.absCache[u] != nil {
		var filtered []*jmap.AddressBook
		for _, a := range b.absCache[u] {
			if a.ID != id {
				filtered = append(filtered, a)
			}
		}
		b.absCache[u] = filtered
	}
	st := b.getABTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "AddressBook", st)
	return true, nil
}

func (b *ContactsBackend) SetDefaultAddressBook(ctx context.Context, id jmap.Id) error {
	u := b.user(ctx)
	b.mu.Lock()
	if b.defaultAddressBooks == nil {
		b.defaultAddressBooks = make(map[string]jmap.Id)
	}
	b.defaultAddressBooks[u] = id
	b.mu.Unlock()
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
	return b.getCardTracker(b.user(ctx)).State()
}

func (b *ContactsBackend) CardChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return b.getCardTracker(b.user(ctx)).Changes(sinceState)
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

	type abResult struct {
		abID jmap.Id
		objs []carddav.AddressObject
	}
	resChan := make(chan abResult, len(abs))
	var wg sync.WaitGroup

	for _, ab := range abs {
		wg.Add(1)
		go func(ab *jmap.AddressBook) {
			defer wg.Done()
			abPath := b.getABPath(u, ab.ID, homeSet)
			objs, qErr := cardClient.QueryAddressBook(ctx, abPath, &carddav.AddressBookQuery{
				DataRequest: carddav.AddressDataRequest{
					AllProp: true,
				},
			})
			if qErr == nil {
				resChan <- abResult{abID: ab.ID, objs: objs}
			}
		}(ab)
	}
	wg.Wait()
	close(resChan)

	for res := range resChan {
		for _, ao := range res.objs {
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
					card.AddressBookIDs[res.abID] = true
					if res.abID == "contacts" {
						card.AddressBookIDs["ab-default"] = true
					}
					b.mu.Lock()
					if b.cardPaths[u] == nil {
						b.cardPaths[u] = make(map[jmap.Id]string)
					}
					b.cardPaths[u][cardID] = ao.Path
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
		card.AddressBookIDs[jmap.Id(abID)] = true
		if abID == "contacts" {
			card.AddressBookIDs["ab-default"] = true
		}
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

	b.mu.RLock()
	var cardPath string
	if b.cardPaths[u] != nil {
		cardPath = b.cardPaths[u][card.ID]
	}
	b.mu.RUnlock()

	if cardPath == "" {
		filename := string(card.ID)
		if !strings.HasSuffix(filename, ".vcf") {
			filename += ".vcf"
		}
		cardPath = strings.TrimRight(abPath, "/") + "/" + filename
	}

	_, putErr := cardClient.PutAddressObject(ctx, cardPath, cardObj)
	if putErr != nil {
		// If CardDAV returns a UID conflict (e.g. 409 Conflict with <no-uid-conflict><href>...</href>),
		// a card with this UID already exists at that specific href in Nextcloud CardDAV.
		// Retry the PUT directly to that conflicting href to update the existing card.
		if strings.Contains(putErr.Error(), "no-uid-conflict") || strings.Contains(putErr.Error(), "UidConflict") {
			conflictHref := extractConflictHref(putErr.Error())
			if conflictHref != "" {
				_, retryErr := cardClient.PutAddressObject(ctx, conflictHref, cardObj)
				if retryErr == nil {
					putErr = nil
					cardPath = conflictHref
				}
			}
		}
	}
	if putErr != nil {
		return nil, fmt.Errorf("failed to put address object via carddav client: %w", putErr)
	}

	b.mu.Lock()
	if b.cardPaths[u] == nil {
		b.cardPaths[u] = make(map[jmap.Id]string)
	}
	b.cardPaths[u][card.ID] = cardPath
	if b.cardsCache[u] == nil {
		b.cardsCache[u] = make(map[jmap.Id]*jmap.Card)
	}
	action := "create"
	if _, exists := b.cardsCache[u][card.ID]; exists {
		action = "update"
	}
	b.cardsCache[u][card.ID] = card
	st := b.getCardTracker(u).Record(card.ID, action)
	b.mu.Unlock()

	b.emitStateChange(u, "Card", st)
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
		cleanPath := strings.TrimPrefix(path, "/")
		parts := strings.Split(cleanPath, "/")
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
	cards, notFound, err := b.GetCards(ctx, []jmap.Id{id})
	if err != nil {
		return nil, err
	}
	if len(notFound) > 0 || len(cards) == 0 {
		return nil, jmap.ErrNotFound
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
			b.mu.RLock()
			oldPath := ""
			if b.cardPaths[u] != nil {
				oldPath = b.cardPaths[u][id]
			}
			b.mu.RUnlock()
			if oldPath == "" {
				homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
				oldPath = strings.TrimRight(b.getABPath(u, jmap.Id(oldAbID), homeSet), "/") + "/" + string(id) + ".vcf"
			}
			_ = cardClient.RemoveAll(ctx, oldPath)
			b.mu.Lock()
			if b.cardPaths[u] != nil {
				delete(b.cardPaths[u], id)
			}
			b.mu.Unlock()
		}
	}

	return b.CreateCard(ctx, card)
}

func (b *ContactsBackend) DeleteCard(ctx context.Context, id jmap.Id) (bool, error) {
	cards, notFound, err := b.GetCards(ctx, []jmap.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(cards) == 0 {
		return false, nil
	}

	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return false, err
	}

	abID := "contacts"
	for aid := range cards[0].AddressBookIDs {
		abID = string(aid)
		break
	}

	b.mu.RLock()
	var cardPath string
	if b.cardPaths[u] != nil {
		cardPath = b.cardPaths[u][id]
	}
	b.mu.RUnlock()

	if cardPath == "" {
		homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
		abPath := b.getABPath(u, jmap.Id(abID), homeSet)
		cardPath = strings.TrimRight(abPath, "/") + "/" + string(id) + ".vcf"
	}
	_ = cardClient.RemoveAll(ctx, cardPath)

	b.mu.Lock()
	if b.cardPaths[u] != nil {
		delete(b.cardPaths[u], id)
	}
	if b.cardsCache[u] != nil {
		delete(b.cardsCache[u], id)
	}
	st := b.getCardTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "Card", st)
	return true, nil
}

func extractConflictHref(errStr string) string {
	sIdx := strings.Index(errStr, "<href")
	if sIdx == -1 {
		sIdx = strings.Index(errStr, ":href")
		if sIdx != -1 {
			open := strings.LastIndex(errStr[:sIdx], "<")
			if open != -1 {
				sIdx = open
			}
		}
	}
	if sIdx == -1 {
		return ""
	}
	closeTag := strings.Index(errStr[sIdx:], ">")
	if closeTag == -1 {
		return ""
	}
	valStart := sIdx + closeTag + 1
	eIdx := strings.Index(errStr[valStart:], "</")
	if eIdx == -1 {
		return ""
	}
	return strings.TrimSpace(errStr[valStart : valStart+eIdx])
}

func (b *ContactsBackend) QueryCards(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	cards, _, err := b.GetCards(ctx, nil)
	if err != nil {
		return nil, 0, err
	}

	var matched []*jmap.Card
	for _, c := range cards {
		if jmap.MatchCard(c, filter) {
			matched = append(matched, c)
		}
	}

	jmap.SortCards(matched, comparators)

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
