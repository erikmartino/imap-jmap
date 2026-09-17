package jmapcontacts

import (
	"context"

	"imap-jmap/jmap/jmapcore"
)

// ContactsBackend defines the storage interface for JMAP Contacts resources per RFC 9610.
type ContactsBackend interface {
	// AddressBooks (RFC 9610 Section 2)
	AddressBookState(ctx context.Context) string
	AddressBookChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetAddressBooks(ctx context.Context, ids []jmapcore.Id) (list []*AddressBook, notFound []jmapcore.Id, err error)
	GetAllAddressBooks(ctx context.Context) ([]*AddressBook, error)
	CreateAddressBook(ctx context.Context, ab *AddressBook) (*AddressBook, error)
	UpdateAddressBook(ctx context.Context, id jmapcore.Id, patch map[string]any) (*AddressBook, error)
	DeleteAddressBook(ctx context.Context, id jmapcore.Id, removeContents bool) (bool, error)
	SetDefaultAddressBook(ctx context.Context, id jmapcore.Id) error
	AddressBookHasContents(ctx context.Context, id jmapcore.Id) (bool, error)

	// Cards (RFC 9610 Section 3)
	CardState(ctx context.Context) string
	CardChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetCards(ctx context.Context, ids []jmapcore.Id) (list []*Card, notFound []jmapcore.Id, err error)
	GetAllCards(ctx context.Context) ([]*Card, error)
	CreateCard(ctx context.Context, card *Card) (*Card, error)
	UpdateCard(ctx context.Context, id jmapcore.Id, patch map[string]any) (*Card, error)
	DeleteCard(ctx context.Context, id jmapcore.Id) (bool, error)
	QueryCards(ctx context.Context, filter map[string]any, comparators []jmapcore.Comparator, position int, limit *uint64) (ids []jmapcore.Id, total int, err error)
}
