package jmap

import (
	"imap-jmap/jmap/jmapcontacts"
)

// RegisterContactsHandlers registers RFC 9610 JMAP for Contacts method handlers into MethodRegistry.
func RegisterContactsHandlers(r *MethodRegistry, backend ContactsBackend, blobBackend ...BlobBackend) {
	if len(blobBackend) > 0 {
		jmapcontacts.RegisterContactsHandlers(r, backend, blobBackend[0])
	} else {
		jmapcontacts.RegisterContactsHandlers(r, backend)
	}
}

var (
	MatchCard            = jmapcontacts.MatchCard
	SortCards            = jmapcontacts.SortCards
	GetCardNameComponent = jmapcontacts.GetCardNameComponent
)
