package nextcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav/carddav"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcontacts"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmappush"
	"imap-jmap/jmap/vcardconv"
)

// standardAddressBookProperties maps the standard (CardDAV) display properties of an
// AddressBook to upstream PROPPATCH fields so they are stored server-side.
func standardAddressBookProperties(ab *jmapcontacts.AddressBook) []AddressBookProperty {
	if ab == nil {
		return nil
	}
	var set []AddressBookProperty
	if ab.Name != "" {
		set = append(set, AddressBookProperty{Name: xml.Name{Space: nsDAV, Local: "displayname"}, Value: ab.Name})
	}
	if ab.Description != nil {
		set = append(set, AddressBookProperty{Name: xml.Name{Space: nsCardDAV, Local: "addressbook-description"}, Value: *ab.Description})
	}
	if ab.SortOrder != 0 {
		set = append(set, AddressBookProperty{Name: xml.Name{Space: nsApple, Local: "addressbook-order"}, Value: strconv.FormatUint(ab.SortOrder, 10)})
	}
	// JMAP-only preferences, stored as one JSON custom property upstream.
	prefs := jmapAddressBookPrefs{
		SortOrder:    &ab.SortOrder,
		IsSubscribed: &ab.IsSubscribed,
	}
	if data, err := json.Marshal(prefs); err == nil {
		set = append(set, AddressBookProperty{Name: xml.Name{Space: nsJMAPCont, Local: "prefs"}, Value: string(data)})
	}
	return set
}

type jmapAddressBookPrefs struct {
	SortOrder    *uint64 `json:"sortOrder,omitempty"`
	IsSubscribed *bool   `json:"isSubscribed,omitempty"`
}

// ContactsBackend implements jmapcontacts.ContactsBackend backed by Nextcloud CardDAV via github.com/emersion/go-webdav/carddav.
type ContactsBackend struct {
	client      *Client
	mu          sync.RWMutex
	trackersMu  sync.Mutex
	broadcaster *jmappush.Broadcaster

	abTrackers   map[string]*jmappush.ChangeTracker
	cardTrackers map[string]*jmappush.ChangeTracker
	abPaths      map[string]map[jmapcore.Id]string
	cardPaths    map[string]map[jmapcore.Id]string
	cardsCache   map[string]map[jmapcore.Id]*jmapcontacts.Card
}

var _ jmapcontacts.ContactsBackend = (*ContactsBackend)(nil)

// NewContactsBackend initializes a new Nextcloud-backed ContactsBackend.
func NewContactsBackend(client *Client) *ContactsBackend {
	return &ContactsBackend{
		client:       client,
		abTrackers:   make(map[string]*jmappush.ChangeTracker),
		cardTrackers: make(map[string]*jmappush.ChangeTracker),
		abPaths:      make(map[string]map[jmapcore.Id]string),
		cardPaths:    make(map[string]map[jmapcore.Id]string),
		cardsCache:   make(map[string]map[jmapcore.Id]*jmapcontacts.Card),
	}
}


func (b *ContactsBackend) SetBroadcaster(bc *jmappush.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *ContactsBackend) emitStateChange(u, typeName, newState string) {
	bc := b.broadcaster
	if bc != nil {
		accountID := jmapauth.AccountIDForSubject(u)
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

func (b *ContactsBackend) getABTracker(u string) *jmappush.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.abTrackers[u] == nil {
		b.abTrackers[u] = jmappush.NewChangeTracker(1000)
	}
	return b.abTrackers[u]
}

func (b *ContactsBackend) getCardTracker(u string) *jmappush.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.cardTrackers[u] == nil {
		b.cardTrackers[u] = jmappush.NewChangeTracker(1000)
	}
	return b.cardTrackers[u]
}

func addressBookStateMap(abs []*jmapcontacts.AddressBook) map[string]string {
	fps := make(map[string]string, len(abs))
	for _, ab := range abs {
		if ab != nil {
			fps[string(ab.ID)] = jmappush.ObjectFingerprint(ab)
		}
	}
	return fps
}

// AddressBookState returns an opaque JMAP state token for the user's address books.
func (b *ContactsBackend) AddressBookState(ctx context.Context) string {
	abs, _, err := b.GetAddressBooks(ctx, nil)
	if err != nil {
		return b.getABTracker(b.user(ctx)).State()
	}
	return jmappush.EncodeStateVector("ab-v1:", addressBookStateMap(abs))
}

func (b *ContactsBackend) AddressBookChanges(ctx context.Context, sinceState string) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	u := b.user(ctx)
	if !strings.HasPrefix(sinceState, "ab-v1:") {
		return b.getABTracker(u).Changes(sinceState)
	}
	old, err := jmappush.DecodeStateVector("ab-v1:", sinceState)
	if err != nil {
		return nil, nil, nil, "", false
	}
	abs, _, err := b.GetAddressBooks(ctx, nil)
	if err != nil {
		return nil, nil, nil, "", false
	}
	cur := addressBookStateMap(abs)
	newState := jmappush.EncodeStateVector("ab-v1:", cur)
	created, updated, destroyed := jmappush.DiffStateVectors(old, cur)
	return created, updated, destroyed, newState, false
}

func (b *ContactsBackend) GetAllAddressBooks(ctx context.Context) ([]*jmapcontacts.AddressBook, error) {
	abs, _, err := b.GetAddressBooks(ctx, nil)
	return abs, err
}

func (b *ContactsBackend) getAddressBookHomeSet(ctx context.Context, cardClient *carddav.Client, u string) string {
	return b.client.getAddressBookHomeSet(ctx, cardClient, u)
}

func (b *ContactsBackend) getABPath(u string, abID jmapcore.Id, homeSet string) string {
	b.mu.RLock()
	if b.abPaths[u] != nil {
		if p, ok := b.abPaths[u][abID]; ok && p != "" {
			b.mu.RUnlock()
			return p
		}
	}
	b.mu.RUnlock()
	if homeSet != "" {
		return strings.TrimRight(homeSet, "/") + "/" + string(abID) + "/"
	}
	return "/remote.php/dav/addressbooks/users/" + u + "/" + string(abID) + "/"
}

func (b *ContactsBackend) GetAddressBooks(ctx context.Context, ids []jmapcore.Id) ([]*jmapcontacts.AddressBook, []jmapcore.Id, error) {
	collections, err := b.client.getAddressBookCollections(ctx)
	if err != nil {
		return nil, nil, err
	}
	u := b.user(ctx)
	defID := collections.defaultAddressBook

	idMap := make(map[jmapcore.Id]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	var list []*jmapcontacts.AddressBook
	pathMap := make(map[jmapcore.Id]string)

	for _, c := range collections.ordered {
		aid := jmapcore.Id(c.ID)
		pathMap[aid] = c.Path

		if len(ids) > 0 && !idMap[aid] {
			if !(aid == "contacts" && idMap["ab-default"]) {
				continue
			}
		}

		name := c.Name
		if name == "" {
			name = c.ID
		}

		ab := &jmapcontacts.AddressBook{
			ID:           aid,
			Name:         name,
			IsSubscribed: true,
			MyRights:     jmapcontacts.FullAddressBookRights(),
		}
		if c.Description != "" {
			desc := c.Description
			ab.Description = &desc
		}
		if c.Order != "" {
			if n, parseErr := strconv.ParseUint(strings.TrimSpace(c.Order), 10, 64); parseErr == nil {
				ab.SortOrder = n
			}
		}
		if c.PrefsJSON != "" {
			var prefs jmapAddressBookPrefs
			if json.Unmarshal([]byte(c.PrefsJSON), &prefs) == nil {
				if prefs.SortOrder != nil {
					ab.SortOrder = *prefs.SortOrder
				}
				if prefs.IsSubscribed != nil {
					ab.IsSubscribed = *prefs.IsSubscribed
				}
			}
		}
		list = append(list, ab)
	}

	if len(list) == 0 && (len(ids) == 0 || idMap["contacts"] || idMap["ab-default"]) {
		cardClient, _, cardErr := b.client.CardDAV(ctx)
		homeSet := ""
		if cardErr == nil {
			homeSet = b.client.getAddressBookHomeSet(ctx, cardClient, u)
		}
		aid := jmapcore.Id("contacts")
		pathMap[aid] = strings.TrimRight(homeSet, "/") + "/contacts/"
		list = append(list, &jmapcontacts.AddressBook{
			ID:           aid,
			Name:         "Contacts",
			IsSubscribed: true,
			MyRights:     jmapcontacts.FullAddressBookRights(),
		})
	}

	for _, ab := range list {
		if defID != "" {
			ab.IsDefault = (string(ab.ID) == defID || (defID == "ab-default" && ab.ID == "contacts"))
		} else {
			ab.IsDefault = (ab.ID == "contacts" || ab.ID == "ab-default" || strings.EqualFold(ab.Name, "Contacts"))
		}
	}
	// RFC 9610 Section 2: there SHOULD be exactly one default AddressBook. If no
	// stored default matches and no book is the synthetic "Contacts", promote the
	// first book so clients always have a default.
	hasDefault := false
	for _, ab := range list {
		if ab.IsDefault {
			hasDefault = true
			break
		}
	}
	if !hasDefault && len(list) > 0 {
		list[0].IsDefault = true
	}

	b.mu.Lock()
	if b.abPaths[u] == nil {
		b.abPaths[u] = make(map[jmapcore.Id]string)
	}
	for k, v := range pathMap {
		b.abPaths[u][k] = v
	}
	b.mu.Unlock()

	var notFound []jmapcore.Id
	if len(ids) > 0 {
		foundMap := make(map[jmapcore.Id]bool)
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

func (b *ContactsBackend) CreateAddressBook(ctx context.Context, ab *jmapcontacts.AddressBook) (*jmapcontacts.AddressBook, error) {
	if ab == nil {
		return nil, fmt.Errorf("address book is nil")
	}
	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return nil, err
	}

	if ab.ID == "" {
		ab.ID = jmapcore.Id(fmt.Sprintf("ab-%d", time.Now().UnixNano()))
	}
	ab.MyRights = jmapcontacts.FullAddressBookRights()

	homeSet := b.client.getAddressBookHomeSet(ctx, cardClient, u)
	abPath := strings.TrimRight(homeSet, "/") + "/" + string(ab.ID) + "/"

	if err := b.client.CreateAddressBook(ctx, string(ab.ID)); err != nil {
		return nil, err
	}
	if err := b.client.PatchAddressBookProperties(ctx, string(ab.ID), standardAddressBookProperties(ab), nil); err != nil {
		return nil, err
	}

	b.mu.Lock()
	if b.abPaths[u] == nil {
		b.abPaths[u] = make(map[jmapcore.Id]string)
	}
	b.abPaths[u][ab.ID] = abPath
	b.getABTracker(u).Record(ab.ID, "create")
	b.mu.Unlock()

	st := b.AddressBookState(ctx)
	b.emitStateChange(u, "AddressBook", st)
	return ab, nil
}

func (b *ContactsBackend) UpdateAddressBook(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapcontacts.AddressBook, error) {
	abs, notFound, err := b.GetAddressBooks(ctx, []jmapcore.Id{id})
	if err != nil {
		return nil, err
	}
	if len(notFound) > 0 || len(abs) == 0 {
		return nil, jmapcore.ErrNotFound
	}
	ab := abs[0]
	var remove []xml.Name

	if name, ok := patch["name"].(string); ok && name != "" {
		ab.Name = name
	}
	if descVal, ok := patch["description"]; ok {
		if descVal == nil {
			ab.Description = nil
			remove = append(remove, xml.Name{Space: nsCardDAV, Local: "addressbook-description"})
		} else if descStr, ok := descVal.(string); ok {
			ab.Description = &descStr
		}
	}
	if so, ok := patch["sortOrder"].(float64); ok {
		ab.SortOrder = uint64(so)
	} else if soInt, ok := patch["sortOrder"].(uint64); ok {
		ab.SortOrder = soInt
	} else if soInt2, ok := patch["sortOrder"].(int); ok && soInt2 >= 0 {
		ab.SortOrder = uint64(soInt2)
	}
	if sub, ok := patch["isSubscribed"].(bool); ok {
		ab.IsSubscribed = sub
	}

	if err := b.client.PatchAddressBookProperties(ctx, string(id), standardAddressBookProperties(ab), remove); err != nil {
		return nil, err
	}

	u := b.user(ctx)
	b.mu.Lock()
	b.getABTracker(u).Record(id, "update")
	b.mu.Unlock()

	st := b.AddressBookState(ctx)
	b.emitStateChange(u, "AddressBook", st)
	return ab, nil
}

func (b *ContactsBackend) DeleteAddressBook(ctx context.Context, id jmapcore.Id, removeContents bool) (bool, error) {
	abs, notFound, err := b.GetAddressBooks(ctx, []jmapcore.Id{id})
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

	if err := b.client.DeleteAddressBook(ctx, string(id)); err != nil {
		return false, err
	}

	// If the deleted address book was the default, remove the home-set default-addressbook property.
	collections, _ := b.client.getAddressBookCollections(ctx)
	if collections != nil && collections.defaultAddressBook == string(id) {
		homeSet := b.client.getAddressBookHomeSet(ctx, cardClient, u)
		_ = b.client.PatchAddressBookProperties(ctx, homeSet, nil, []xml.Name{{Space: nsJMAPCont, Local: "default-addressbook"}})
	}

	b.mu.Lock()
	if b.abPaths[u] != nil {
		delete(b.abPaths[u], id)
	}
	b.getABTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	st := b.AddressBookState(ctx)
	b.emitStateChange(u, "AddressBook", st)
	return true, nil
}

func (b *ContactsBackend) SetDefaultAddressBook(ctx context.Context, id jmapcore.Id) error {
	// RFC 9610 Section 2.3: an unknown id (or one the server does not permit) is
	// ignored, leaving the current default unchanged and returning no error.
	abs, _, err := b.GetAddressBooks(ctx, []jmapcore.Id{id})
	if err != nil || len(abs) == 0 {
		return nil
	}
	if err := b.client.SetDefaultAddressBookUpstream(ctx, string(id)); err != nil {
		return err
	}
	return nil
}


func (b *ContactsBackend) AddressBookHasContents(ctx context.Context, id jmapcore.Id) (bool, error) {
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

func cardStateMap(cards []*jmapcontacts.Card) map[string]string {
	fps := make(map[string]string, len(cards))
	for _, c := range cards {
		if c != nil {
			fps[string(c.ID)] = jmappush.ObjectFingerprint(c)
		}
	}
	return fps
}

// CardState returns an opaque JMAP state token for the user's cards.
func (b *ContactsBackend) CardState(ctx context.Context) string {
	cards, _, err := b.GetCards(ctx, nil)
	if err != nil {
		return b.getCardTracker(b.user(ctx)).State()
	}
	return jmappush.EncodeStateVector("card-v1:", cardStateMap(cards))
}

func (b *ContactsBackend) CardChanges(ctx context.Context, sinceState string) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	u := b.user(ctx)
	if !strings.HasPrefix(sinceState, "card-v1:") {
		return b.getCardTracker(u).Changes(sinceState)
	}
	old, err := jmappush.DecodeStateVector("card-v1:", sinceState)
	if err != nil {
		return nil, nil, nil, "", false
	}
	cards, _, err := b.GetCards(ctx, nil)
	if err != nil {
		return nil, nil, nil, "", false
	}
	cur := cardStateMap(cards)
	newState := jmappush.EncodeStateVector("card-v1:", cur)
	created, updated, destroyed := jmappush.DiffStateVectors(old, cur)
	return created, updated, destroyed, newState, false
}

func (b *ContactsBackend) GetAllCards(ctx context.Context) ([]*jmapcontacts.Card, error) {
	cards, _, err := b.GetCards(ctx, nil)
	return cards, err
}

func (b *ContactsBackend) GetCards(ctx context.Context, ids []jmapcore.Id) ([]*jmapcontacts.Card, []jmapcore.Id, error) {
	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	b.mu.Lock()
	if b.cardsCache[u] == nil {
		b.cardsCache[u] = make(map[jmapcore.Id]*jmapcontacts.Card)
	}
	b.mu.Unlock()

	abs, _, _ := b.GetAddressBooks(ctx, nil)
	homeSet := b.getAddressBookHomeSet(ctx, cardClient, u)
	idMap := make(map[jmapcore.Id]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	type abResult struct {
		abID jmapcore.Id
		objs []carddav.AddressObject
	}
	resChan := make(chan abResult, len(abs))
	var wg sync.WaitGroup

	for _, ab := range abs {
		wg.Add(1)
		go func(ab *jmapcontacts.AddressBook) {
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
			cardID := jmapcore.Id(path.Base(ao.Path))

			if len(ids) > 0 && !idMap[cardID] {
				continue
			}

			var buf bytes.Buffer
			_ = vcard.NewEncoder(&buf).Encode(ao.Card)

			rawMap, vErr := vcardconv.FromVCard(buf.String())
			if vErr == nil && rawMap != nil {
				jsonBytes, _ := json.Marshal(rawMap)
				var card jmapcontacts.Card
				if err := json.Unmarshal(jsonBytes, &card); err == nil {
					card.ID = cardID
					if card.AddressBookIDs == nil {
						card.AddressBookIDs = make(map[jmapcore.Id]bool)
					}
					card.AddressBookIDs[res.abID] = true
					if res.abID == "contacts" {
						card.AddressBookIDs["ab-default"] = true
					}
					b.mu.Lock()
					if b.cardPaths[u] == nil {
						b.cardPaths[u] = make(map[jmapcore.Id]string)
					}
					b.cardPaths[u][cardID] = ao.Path
					b.cardsCache[u][cardID] = &card
					b.mu.Unlock()
				}
			}
		}
	}

	b.mu.RLock()
	var list []*jmapcontacts.Card
	var notFound []jmapcore.Id
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

func (b *ContactsBackend) CreateCard(ctx context.Context, card *jmapcontacts.Card) (*jmapcontacts.Card, error) {
	if card == nil {
		return nil, fmt.Errorf("card is nil")
	}
	cardClient, u, err := b.client.CardDAV(ctx)
	if err != nil {
		return nil, err
	}

	if card.ID == "" {
		card.ID = jmapcore.Id(fmt.Sprintf("card-%d", time.Now().UnixNano()))
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
			card.AddressBookIDs = make(map[jmapcore.Id]bool)
		}
		card.AddressBookIDs[jmapcore.Id(abID)] = true
		if abID == "contacts" {
			card.AddressBookIDs["ab-default"] = true
		}
	}

	abPath := b.getABPath(u, jmapcore.Id(abID), homeSet)
	_ = cardClient.Mkdir(ctx, abPath)

	cardBytes, _ := json.Marshal(card)
	var cardMap map[string]any
	_ = json.Unmarshal(cardBytes, &cardMap)

	var cardObj vcard.Card
	vcfPayload, err := vcardconv.ToVCard(cardMap)
	if err != nil {
		name := ""
		if card.Name != nil && card.Name.Full != "" {
			name = card.Name.Full
		}
		cardObj = make(vcard.Card)
		cardObj.SetValue(vcard.FieldVersion, "3.0")
		cardObj.SetValue(vcard.FieldUID, card.Uid)
		cardObj.SetValue(vcard.FieldFormattedName, name)
	} else {
		dec := vcard.NewDecoder(strings.NewReader(vcfPayload))
		var decErr error
		cardObj, decErr = dec.Decode()
		if decErr != nil {
			return nil, fmt.Errorf("failed to decode vcard: %w", decErr)
		}
	}

	b.mu.RLock()
	var cardPath string
	if b.cardPaths[u] != nil {
		cardPath = b.cardPaths[u][card.ID]
	}
	b.mu.RUnlock()

	if cardPath == "" {
		cardPath = strings.TrimRight(abPath, "/") + "/" + string(card.ID)
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
		b.cardPaths[u] = make(map[jmapcore.Id]string)
	}
	b.cardPaths[u][card.ID] = cardPath
	if b.cardsCache[u] == nil {
		b.cardsCache[u] = make(map[jmapcore.Id]*jmapcontacts.Card)
	}
	action := "create"
	if _, exists := b.cardsCache[u][card.ID]; exists {
		action = "update"
	}
	b.cardsCache[u][card.ID] = card
	b.getCardTracker(u).Record(card.ID, action)
	b.mu.Unlock()

	st := b.CardState(ctx)
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

func applyCardPatch(card *jmapcontacts.Card, patch map[string]any) error {
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
	var updatedCard jmapcontacts.Card
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

func (b *ContactsBackend) UpdateCard(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapcontacts.Card, error) {
	cards, notFound, err := b.GetCards(ctx, []jmapcore.Id{id})
	if err != nil {
		return nil, err
	}
	if len(notFound) > 0 || len(cards) == 0 {
		return nil, jmapcore.ErrNotFound
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
				oldPath = strings.TrimRight(b.getABPath(u, jmapcore.Id(oldAbID), homeSet), "/") + "/" + string(id)
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

func (b *ContactsBackend) DeleteCard(ctx context.Context, id jmapcore.Id) (bool, error) {
	cards, notFound, err := b.GetCards(ctx, []jmapcore.Id{id})
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
		abPath := b.getABPath(u, jmapcore.Id(abID), homeSet)
		cardPath = strings.TrimRight(abPath, "/") + "/" + string(id)
	}
	_ = cardClient.RemoveAll(ctx, cardPath)

	b.mu.Lock()
	if b.cardPaths[u] != nil {
		delete(b.cardPaths[u], id)
	}
	if b.cardsCache[u] != nil {
		delete(b.cardsCache[u], id)
	}
	b.getCardTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	st := b.CardState(ctx)
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

func (b *ContactsBackend) QueryCards(ctx context.Context, filter map[string]any, comparators []jmapcore.Comparator, position int, limit *uint64) ([]jmapcore.Id, int, error) {
	cards, _, err := b.GetCards(ctx, nil)
	if err != nil {
		return nil, 0, err
	}

	var matched []*jmapcontacts.Card
	for _, c := range cards {
		if jmapcontacts.MatchCard(c, filter) {
			matched = append(matched, c)
		}
	}

	jmapcontacts.SortCards(matched, comparators)

	total := len(matched)
	position = jmapcore.NormalizePosition(position, total)
	if position >= total {
		return []jmapcore.Id{}, total, nil
	}

	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}

	ids := make([]jmapcore.Id, 0, end-position)
	for i := position; i < end; i++ {
		ids = append(ids, matched[i].ID)
	}

	return ids, total, nil
}
