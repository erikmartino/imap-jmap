package jmap

import (
	"imap-jmap/jmap/jmapcontacts"
)

// RegisterContactsHandlers registers RFC 9610 JMAP for Contacts method handlers into MethodRegistry.
func RegisterContactsHandlers(r *MethodRegistry, backend ContactsBackend) {
	jmapcontacts.RegisterContactsHandlers(r, backend)
}

var (
	MatchCard            = jmapcontacts.MatchCard
	SortCards            = jmapcontacts.SortCards
	GetCardNameComponent = jmapcontacts.GetCardNameComponent
)
