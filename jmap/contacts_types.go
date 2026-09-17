package jmap

// This file re-exports all contacts domain types from the jmapcontacts sub-package
// as type aliases, preserving full backward compatibility for all existing callers.

import "imap-jmap/jmap/jmapcontacts"

type (
	AddressBookRights           = jmapcontacts.AddressBookRights
	AddressBook                 = jmapcontacts.AddressBook
	JSContactName               = jmapcontacts.JSContactName
	JSContactNameComponent      = jmapcontacts.JSContactNameComponent
	JSContactEmailAddress       = jmapcontacts.JSContactEmailAddress
	JSContactPhone              = jmapcontacts.JSContactPhone
	JSContactAddressComponent   = jmapcontacts.JSContactAddressComponent
	JSContactAddress            = jmapcontacts.JSContactAddress
	JSContactOrganization       = jmapcontacts.JSContactOrganization
	JSContactTitle              = jmapcontacts.JSContactTitle
	JSContactNote               = jmapcontacts.JSContactNote
	JSContactNickname           = jmapcontacts.JSContactNickname
	JSContactOnlineService      = jmapcontacts.JSContactOnlineService
	JSContactLink               = jmapcontacts.JSContactLink
	JSContactMedia              = jmapcontacts.JSContactMedia
	JSContactSpeakToAs          = jmapcontacts.JSContactSpeakToAs
	JSContactAnniversary        = jmapcontacts.JSContactAnniversary
	JSContactRelation           = jmapcontacts.JSContactRelation
	JSContactLanguagePref       = jmapcontacts.JSContactLanguagePref
	JSContactCalendar           = jmapcontacts.JSContactCalendar
	JSContactSchedulingAddress  = jmapcontacts.JSContactSchedulingAddress
	JSContactCryptoKey          = jmapcontacts.JSContactCryptoKey
	JSContactDirectory          = jmapcontacts.JSContactDirectory
	JSContactPersonalInfo       = jmapcontacts.JSContactPersonalInfo
	Card                        = jmapcontacts.Card
)
