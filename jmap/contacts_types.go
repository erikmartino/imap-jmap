package jmap

// This file re-exports all contacts domain types from the jmapcontacts sub-package
// as type aliases, preserving full backward compatibility for all existing callers.

import "imap-jmap/jmap/jmapcontacts"

type (
	AddressBookRights      = jmapcontacts.AddressBookRights
	AddressBook            = jmapcontacts.AddressBook
	JSContactName          = jmapcontacts.JSContactName
	JSContactNameComponent = jmapcontacts.JSContactNameComponent
	JSContactEmailAddress  = jmapcontacts.JSContactEmailAddress
	Card                   = jmapcontacts.Card
)
