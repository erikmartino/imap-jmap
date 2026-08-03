package jmap

// AddressBookRights defines access rights for an AddressBook per RFC 9610 Section 2.
type AddressBookRights struct {
	MayReadItems  bool `json:"mayReadItems"`
	MayWriteItems bool `json:"mayWriteItems"`
	MayAdmin      bool `json:"mayAdmin"`
	MayDelete     bool `json:"mayDelete"`
}

// AddressBook represents an AddressBook object per RFC 9610 Section 2.
type AddressBook struct {
	ID          Id                `json:"id"`
	Name        string            `json:"name"`
	Description *string           `json:"description,omitempty"`
	SortOrder   uint64            `json:"sortOrder"`
	IsDefault   bool              `json:"isDefault"`
	MyRights    AddressBookRights `json:"myRights"`
}

// JSContactName defines the Name property object per RFC 9553 Section 2.2.1.
type JSContactName struct {
	Components []*JSContactNameComponent `json:"components,omitempty"`
	Full       string                    `json:"full,omitempty"`
	SortAs     string                    `json:"sortAs,omitempty"`
}

// JSContactNameComponent defines a name component per RFC 9553 Section 2.2.1.
type JSContactNameComponent struct {
	Value string `json:"value"`
	Kind  string `json:"kind"` // "prefix", "given", "surname", "generational", "suffix"
}

// JSContactEmailAddress defines an email address property object per RFC 9553 Section 2.3.1.
type JSContactEmailAddress struct {
	Address  string          `json:"address"`
	Contexts map[string]bool `json:"contexts,omitempty"` // "work", "private"
	Label    string          `json:"label,omitempty"`
	Pref     uint32          `json:"pref,omitempty"`
}

// JSContactPhone defines a phone property object per RFC 9553 Section 2.3.3.
type JSContactPhone struct {
	Number   string          `json:"number"`
	Contexts map[string]bool `json:"contexts,omitempty"`
	Features map[string]bool `json:"features,omitempty"` // "voice", "fax", "cell", "text"
	Label    string          `json:"label,omitempty"`
	Pref     uint32          `json:"pref,omitempty"`
}

// JSContactAddress defines a postal address property object per RFC 9553 Section 2.5.1.
type JSContactAddress struct {
	Full        string          `json:"full,omitempty"`
	Street      string          `json:"street,omitempty"`
	Locality    string          `json:"locality,omitempty"`
	Region      string          `json:"region,omitempty"`
	Postcode    string          `json:"postcode,omitempty"`
	Country     string          `json:"country,omitempty"`
	CountryCode string          `json:"countryCode,omitempty"`
	Contexts    map[string]bool `json:"contexts,omitempty"`
	Pref        uint32          `json:"pref,omitempty"`
}

// JSContactOrganization defines an organization property object per RFC 9553 Section 2.2.3.
type JSContactOrganization struct {
	Name     string          `json:"name"`
	Units    []string        `json:"units,omitempty"`
	Contexts map[string]bool `json:"contexts,omitempty"`
}

// JSContactTitle defines a job title or role per RFC 9553 Section 2.2.4.
type JSContactTitle struct {
	Title    string          `json:"title"`
	Contexts map[string]bool `json:"contexts,omitempty"`
}

// JSContactNote defines a text note per RFC 9553 Section 2.6.4.
type JSContactNote struct {
	Note string `json:"note"`
}

// JSContactNickname defines a nickname property object per RFC 9553 Section 2.2.2.
type JSContactNickname struct {
	Name     string          `json:"name"`
	Contexts map[string]bool `json:"contexts,omitempty"`
}

// JSContactOnlineService defines an online service / IM handle per RFC 9553 Section 2.3.2.
type JSContactOnlineService struct {
	Service  string          `json:"service,omitempty"`
	URI      string          `json:"uri,omitempty"`
	Contexts map[string]bool `json:"contexts,omitempty"`
}

// JSContactLink defines a web link or URI per RFC 9553 Section 2.3.4.
type JSContactLink struct {
	URI      string          `json:"uri"`
	Contexts map[string]bool `json:"contexts,omitempty"`
	Label    string          `json:"label,omitempty"`
	Pref     uint32          `json:"pref,omitempty"`
}

// JSContactMedia defines a photo, video, or audio media attachment per RFC 9553 Section 2.6.2.
type JSContactMedia struct {
	URI      string          `json:"uri"`
	Contexts map[string]bool `json:"contexts,omitempty"`
	Kind     string          `json:"kind,omitempty"` // "photo", "logo", "sound"
	Pref     uint32          `json:"pref,omitempty"`
}

// JSContactGender defines gender information per RFC 9553 Section 2.2.5.
type JSContactGender struct {
	Grammatical string `json:"grammatical,omitempty"`
	Identity    string `json:"identity,omitempty"`
}

// JSContactSpeakToAs defines grammatical gender / pronouns per RFC 9553 Section 2.2.6.
type JSContactSpeakToAs struct {
	Pronouns map[string]bool `json:"pronouns,omitempty"`
}

// JSContactAnniversary defines an anniversary date per RFC 9553 Section 2.6.1.
type JSContactAnniversary struct {
	Date  string `json:"date"`
	Kind  string `json:"kind,omitempty"` // "birth", "wedding", "other"
	Label string `json:"label,omitempty"`
}

// Card represents a JSContact Card object per RFC 9553 & RFC 9610 Section 3.
type Card struct {
	ID             Id                                  `json:"id"`
	AddressBookIDs map[Id]bool                         `json:"addressBookIds"`
	Type           string                              `json:"@type"` // Always "Card"
	Kind           string                              `json:"kind,omitempty"`
	Uid            string                              `json:"uid,omitempty"`
	Created        string                              `json:"created,omitempty"`
	Updated        string                              `json:"updated,omitempty"`
	Name           *JSContactName                      `json:"name,omitempty"`
	Nicknames      map[string]*JSContactNickname       `json:"nicknames,omitempty"`
	Emails         map[string]*JSContactEmailAddress   `json:"emails,omitempty"`
	Phones         map[string]*JSContactPhone          `json:"phones,omitempty"`
	Addresses      map[string]*JSContactAddress        `json:"addresses,omitempty"`
	Organizations  map[string]*JSContactOrganization   `json:"organizations,omitempty"`
	Titles         map[string]*JSContactTitle          `json:"titles,omitempty"`
	Notes          map[string]*JSContactNote           `json:"notes,omitempty"`
	OnlineServices map[string]*JSContactOnlineService  `json:"onlineServices,omitempty"`
	Links          map[string]*JSContactLink           `json:"links,omitempty"`
	Media          map[string]*JSContactMedia          `json:"media,omitempty"`
	Gender         *JSContactGender                    `json:"gender,omitempty"`
	SpeakToAs      *JSContactSpeakToAs                 `json:"speakToAs,omitempty"`
	Anniversaries  map[string]*JSContactAnniversary    `json:"anniversaries,omitempty"`
	Members        map[string]bool                     `json:"members,omitempty"` // UIDs of group members (RFC 9553 Section 2.1.6)
	Keywords       map[string]bool                     `json:"keywords,omitempty"`
}
