package memory

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"

	"imap-jmap/jmap"
)

// CardDAVBackend implements carddav.Backend bridging WebDAV CardDAV requests to jmap.ContactsBackend (RFC 6352).
type CardDAVBackend struct {
	Backend jmap.ContactsBackend
}

// NewCardDAVBackend initializes a new CardDAVBackend.
func NewCardDAVBackend(backend jmap.ContactsBackend) *CardDAVBackend {
	return &CardDAVBackend{
		Backend: backend,
	}
}

func (b *CardDAVBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return "/carddav/principals/user", nil
}

func (b *CardDAVBackend) AddressBookHomeSetPath(ctx context.Context) (string, error) {
	return "/carddav/addressbooks/", nil
}

func (b *CardDAVBackend) ListAddressBooks(ctx context.Context) ([]carddav.AddressBook, error) {
	if b.Backend == nil {
		return []carddav.AddressBook{
			{
				Path:        "/carddav/addressbooks/default",
				Name:        "Personal Contacts",
				Description: "Default Personal AddressBook",
			},
		}, nil
	}

	abs, _, err := b.Backend.GetAddressBooks(ctx, nil)
	if err != nil {
		return nil, err
	}

	var list []carddav.AddressBook
	for _, ab := range abs {
		desc := ""
		if ab.Description != nil {
			desc = *ab.Description
		}
		list = append(list, carddav.AddressBook{
			Path:        "/carddav/addressbooks/" + string(ab.ID),
			Name:        ab.Name,
			Description: desc,
		})
	}
	return list, nil
}

func (b *CardDAVBackend) GetAddressBook(ctx context.Context, path string) (*carddav.AddressBook, error) {
	abs, err := b.ListAddressBooks(ctx)
	if err != nil {
		return nil, err
	}

	for _, ab := range abs {
		if ab.Path == path || strings.HasSuffix(path, ab.Path) {
			return &ab, nil
		}
	}
	return &carddav.AddressBook{
		Path:        path,
		Name:        "AddressBook",
		Description: "CardDAV AddressBook",
	}, nil
}

func (b *CardDAVBackend) CreateAddressBook(ctx context.Context, ab *carddav.AddressBook) error {
	if b.Backend == nil {
		return nil
	}
	desc := ab.Description
	_, err := b.Backend.CreateAddressBook(ctx, &jmap.AddressBook{
		Name:        ab.Name,
		Description: &desc,
	})
	return err
}

func (b *CardDAVBackend) DeleteAddressBook(ctx context.Context, path string) error {
	if b.Backend == nil {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		abID := parts[len(parts)-1]
		_, err := b.Backend.DeleteAddressBook(ctx, jmap.Id(abID))
		return err
	}
	return nil
}

func (b *CardDAVBackend) ListAddressObjects(ctx context.Context, path string, req *carddav.AddressDataRequest) ([]carddav.AddressObject, error) {
	if b.Backend == nil {
		return []carddav.AddressObject{}, nil
	}

	cards, _, err := b.Backend.GetCards(ctx, nil)
	if err != nil {
		return nil, err
	}

	var list []carddav.AddressObject
	for _, card := range cards {
		name := ""
		if card.Name != nil {
			name = card.Name.Full
		}

		vcardObj := make(vcard.Card)
		vcardObj.SetValue(vcard.FieldFormattedName, name)
		vcardObj.SetValue(vcard.FieldVersion, "4.0")

		for _, em := range card.Emails {
			vcardObj.AddValue(vcard.FieldEmail, em.Address)
		}
		for _, ph := range card.Phones {
			vcardObj.AddValue(vcard.FieldTelephone, ph.Number)
		}

		list = append(list, carddav.AddressObject{
			Path:    path + "/" + string(card.ID) + ".vcf",
			Card:    vcardObj,
			ModTime: time.Now(),
		})
	}
	return list, nil
}

func (b *CardDAVBackend) GetAddressObject(ctx context.Context, path string, req *carddav.AddressDataRequest) (*carddav.AddressObject, error) {
	objs, err := b.ListAddressObjects(ctx, "/carddav/addressbooks/default", req)
	if err != nil {
		return nil, err
	}

	for _, obj := range objs {
		if obj.Path == path || strings.HasSuffix(path, obj.Path) {
			return &obj, nil
		}
	}
	return nil, webdav.NewHTTPError(http.StatusNotFound, nil)
}

func (b *CardDAVBackend) PutAddressObject(ctx context.Context, path string, card vcard.Card, opts *carddav.PutAddressObjectOptions) (*carddav.AddressObject, error) {
	if b.Backend != nil && card != nil {
		fn := card.Value(vcard.FieldFormattedName)
		email := card.Value(vcard.FieldEmail)
		phone := card.Value(vcard.FieldTelephone)

		jCard := &jmap.Card{
			Name: &jmap.JSContactName{Full: fn},
		}
		if nick := card.Value(vcard.FieldNickname); nick != "" {
			jCard.Nicknames = map[string]*jmap.JSContactNickname{"n1": {Name: nick}}
		}
		if org := card.Value(vcard.FieldOrganization); org != "" {
			jCard.Organizations = map[string]*jmap.JSContactOrganization{"o1": {Name: org}}
		}
		if email != "" {
			jCard.Emails = map[string]*jmap.JSContactEmailAddress{"e1": {Address: email}}
		}
		if phone != "" {
			jCard.Phones = map[string]*jmap.JSContactPhone{"p1": {Number: phone}}
		}
		_, _ = b.Backend.CreateCard(ctx, jCard)
	}
	return &carddav.AddressObject{
		Path: path,
		Card: card,
	}, nil
}

func (b *CardDAVBackend) QueryAddressObjects(ctx context.Context, path string, query *carddav.AddressBookQuery) ([]carddav.AddressObject, error) {
	objs, err := b.ListAddressObjects(ctx, path, nil)
	if err != nil || query == nil {
		return objs, err
	}

	var filtered []carddav.AddressObject
	for _, obj := range objs {
		if len(query.PropFilters) == 0 {
			filtered = append(filtered, obj)
			continue
		}

		matchAll := true
		for _, pf := range query.PropFilters {
			if obj.Card == nil {
				matchAll = false
				break
			}
			val := obj.Card.Value(pf.Name)
			if val == "" {
				matchAll = false
				break
			}
			if len(pf.TextMatches) > 0 {
				for _, tm := range pf.TextMatches {
					if tm.Text != "" && !strings.Contains(strings.ToLower(val), strings.ToLower(tm.Text)) {
						matchAll = false
						break
					}
				}
			}
		}
		if matchAll {
			filtered = append(filtered, obj)
		}
	}
	return filtered, nil
}

func (b *CardDAVBackend) DeleteAddressObject(ctx context.Context, path string) error {
	if b.Backend == nil {
		return nil
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 {
		filename := parts[len(parts)-1]
		cardID := strings.TrimSuffix(filename, ".vcf")
		_, err := b.Backend.DeleteCard(ctx, jmap.Id(cardID))
		return err
	}
	return nil
}
