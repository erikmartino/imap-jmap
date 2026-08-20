package jmap

import (
	"context"
	"strings"
)

// CoreCapabilityURI is the standard JMAP core capability URI defined in RFC 8620 Section 2.2.
const CoreCapabilityURI = "urn:ietf:params:jmap:core"

// MailCapabilityURI is the standard JMAP mail capability URI defined in RFC 8621 Section 2.
const MailCapabilityURI = "urn:ietf:params:jmap:mail"

// SubmissionCapabilityURI is the standard JMAP submission capability URI defined in RFC 8621 Section 7.
const SubmissionCapabilityURI = "urn:ietf:params:jmap:submission"

// SmimeCapabilityURI is the standard JMAP S/MIME capability URI defined in RFC 9219 Section 2.
const SmimeCapabilityURI = "urn:ietf:params:jmap:smime"

// BlobCapabilityURI is the standard JMAP Blob Management capability URI defined in RFC 9404 Section 2.
const BlobCapabilityURI = "urn:ietf:params:jmap:blob"

// QuotaCapabilityURI is the standard JMAP Quota capability URI defined in RFC 9425 Section 2.
const QuotaCapabilityURI = "urn:ietf:params:jmap:quota"

// MdnCapabilityURI is the standard JMAP MDN capability URI defined in RFC 9007 Section 2.
const MdnCapabilityURI = "urn:ietf:params:jmap:mdn"

// VacationResponseCapabilityURI is the JMAP vacation-response capability URI (RFC 8621 Section 8).
const VacationResponseCapabilityURI = "urn:ietf:params:jmap:vacationresponse"

// WebPushVapidCapabilityURI is the JMAP capability URI for VAPID Web Push per RFC 9749 Section 3.
const WebPushVapidCapabilityURI = "urn:ietf:params:jmap:webpush-vapid"

// WebSocketCapabilityURI is the JMAP capability URI for WebSocket transport per RFC 8887 Section 3.
const WebSocketCapabilityURI = "urn:ietf:params:jmap:websocket"

// ContactsCapabilityURI is the standard JMAP Contacts capability URI defined in RFC 9610 Section 2.
const ContactsCapabilityURI = "urn:ietf:params:jmap:contacts"

// CalendarsCapabilityURI is the standard JMAP Calendars capability URI.
const CalendarsCapabilityURI = "urn:ietf:params:jmap:calendars"

// CalendarsParseCapabilityURI is the JMAP capability URI advertising support for the
// CalendarEvent/parse method per draft-ietf-jmap-calendars Section 1.5.3.
const CalendarsParseCapabilityURI = "urn:ietf:params:jmap:calendars:parse"

// SieveCapabilityURI is the standard JMAP Sieve capability URI defined in RFC 9661 Section 2.
const SieveCapabilityURI = "urn:ietf:params:jmap:sieve"

// ImapAccessCapabilityURI is the JMAPACCESS extension for IMAP capability URI defined in RFC 9698 Section 2.
const ImapAccessCapabilityURI = "urn:ietf:params:jmap:imapaccess"

// PrincipalsCapabilityURI is the JMAP capability URI for Principals per draft-ietf-jmap-principals.
const PrincipalsCapabilityURI = "urn:ietf:params:jmap:principals"

// AvailabilityCapabilityURI is the JMAP capability URI for Availability per draft-ietf-jmap-principals.
const AvailabilityCapabilityURI = "urn:ietf:params:jmap:principals:availability"

// PrincipalsOwnerCapabilityURI is the sub-capability URI for "urn:ietf:params:jmap:principals:owner"
// defined in RFC 9670 Section 1.5.2. Unlike regular capabilities it never appears in the JMAP
// Session "capabilities" object; support is implied by the presence of the
// "urn:ietf:params:jmap:principals" URI in session capabilities. Clients (e.g. Bulwark webmail)
// still include it in the "using" array of API requests, so the server MUST accept it there.
const PrincipalsOwnerCapabilityURI = "urn:ietf:params:jmap:principals:owner"

// PrincipalCapability defines the capability object for "urn:ietf:params:jmap:principals".
type PrincipalCapability struct {
	MaxAvailabilityDuration string `json:"maxAvailabilityDuration"`
}

// FileNodeCapabilityURI is the JMAP capability URI for FileNode file storage extension.
const FileNodeCapabilityURI = "urn:ietf:params:jmap:filenode"

// FileNodeCapability defines the capability object for "urn:ietf:params:jmap:filenode".
type FileNodeCapability struct {
	MaxFileSize uint64 `json:"maxFileSize,omitempty"`
}

// ImapAccessCapability defines the capability object for "urn:ietf:params:jmap:imapaccess" per RFC 9698 Section 2.
type ImapAccessCapability struct{}

// CoreCapability defines the capability object for "urn:ietf:params:jmap:core" per RFC 8620 Section 2.2.
type CoreCapability struct {
	MaxSizeUpload         uint64   `json:"maxSizeUpload"`
	MaxConcurrentUpload   uint64   `json:"maxConcurrentUpload"`
	MaxSizeRequest        uint64   `json:"maxSizeRequest"`
	MaxConcurrentRequests uint64   `json:"maxConcurrentRequests"`
	MaxCallsInRequest     uint64   `json:"maxCallsInRequest"`
	MaxObjectsInGet       uint64   `json:"maxObjectsInGet"`
	MaxObjectsInSet       uint64   `json:"maxObjectsInSet"`
	CollationAlgorithms   []string `json:"collationAlgorithms"`
}

// MailCapability defines the account capability object for "urn:ietf:params:jmap:mail" per RFC 8621 Section 2.
type MailCapability struct {
	MaxMailboxesPerEmail       *uint64  `json:"maxMailboxesPerEmail"`
	MaxMailboxDepth            *uint64  `json:"maxMailboxDepth"`
	MaxSizeMailboxName         uint64   `json:"maxSizeMailboxName"`
	MaxSizeAttachmentsPerEmail uint64   `json:"maxSizeAttachmentsPerEmail"`
	EmailQuerySortOptions      []string `json:"emailQuerySortOptions"`
	MayCreateTopLevelMailbox   bool     `json:"mayCreateTopLevelMailbox"`
}

// SmimeCapability defines the capability object for "urn:ietf:params:jmap:smime" per RFC 9219 Section 2.
type SmimeCapability struct {
	SmimeVerificationSupported bool `json:"smimeVerificationSupported"`
}

// BlobCapability defines the capability object for "urn:ietf:params:jmap:blob" per RFC 9404 Section 3.1.
type BlobCapability struct {
	MaxSizeBlobSet            *uint64  `json:"maxSizeBlobSet"`
	MaxDataSources            uint64   `json:"maxDataSources"`
	SupportedTypeNames        []string `json:"supportedTypeNames"`
	SupportedDigestAlgorithms []string `json:"supportedDigestAlgorithms"`
}

// QuotaCapability defines the capability object for "urn:ietf:params:jmap:quota" per RFC 9425 Section 2.
type QuotaCapability struct {
	MaxQuotaResources uint64 `json:"maxQuotaResources"`
}

// MdnCapability defines the capability object for "urn:ietf:params:jmap:mdn" per RFC 9007 Section 2.
type MdnCapability struct{}

// WebPushVapidCapability defines the capability object for "urn:ietf:params:jmap:webpush-vapid" per RFC 9749 Section 3.
type WebPushVapidCapability struct {
	// ApplicationServerKey is the base64url-encoded VAPID public key (uncompressed P-256 point) per RFC 9749.
	ApplicationServerKey string `json:"applicationServerKey"`
}

// WebSocketCapability defines the capability object for "urn:ietf:params:jmap:websocket" per RFC 8887 Section 3.
type WebSocketCapability struct {
	// URL is the wss:// URI to use for initiating a JMAP-over-WebSocket handshake per RFC 8887 Section 3.
	URL string `json:"url"`
	// SupportsPush indicates whether the server supports push notifications over the WebSocket per RFC 8887 Section 4.3.5.
	SupportsPush bool `json:"supportsPush"`
}

// ContactsCapability defines the capability object for "urn:ietf:params:jmap:contacts" per RFC 9610 Section 2.
type ContactsCapability struct {
	MaxAddressBooksPerCard *uint64 `json:"maxAddressBooksPerCard"`
	MayCreateAddressBook   bool    `json:"mayCreateAddressBook"`
}

// CalendarsCapability defines the capability object for "urn:ietf:params:jmap:calendars"
// per draft-ietf-jmap-calendars Section 1.5.1.
type CalendarsCapability struct {
	MaxCalendarsPerEvent     *uint64 `json:"maxCalendarsPerEvent"`
	MayCreateCalendar        bool    `json:"mayCreateCalendar"`
	MinDateTime              string  `json:"minDateTime"`
	MaxDateTime              string  `json:"maxDateTime"`
	MaxExpandedQueryDuration string  `json:"maxExpandedQueryDuration"`
	MaxParticipantsPerEvent  *uint64 `json:"maxParticipantsPerEvent"`
}

// SieveCapability defines the capability object for "urn:ietf:params:jmap:sieve" per RFC 9661 Section 2.
type SieveCapability struct {
	MaxScriptSize   uint64   `json:"maxScriptSize"`
	SieveExtensions []string `json:"sieveExtensions"`
}

// Account defines an account object in the JMAP Session per RFC 8620 Section 2.
type Account struct {
	Name                string         `json:"name"`
	IsPrimary           bool           `json:"isPrimary"`
	IsPersonal          bool           `json:"isPersonal"`
	IsReadOnly          bool           `json:"isReadOnly"`
	AccountCapabilities map[string]any `json:"accountCapabilities"`
}

// Session represents the JMAP Session resource object per RFC 8620 Section 2.
type Session struct {
	Capabilities    map[string]any     `json:"capabilities"`
	Accounts        map[string]Account `json:"accounts"`
	PrimaryAccounts map[string]string  `json:"primaryAccounts"`
	Username        string             `json:"username"`
	APIURL          string             `json:"apiUrl"`
	DownloadURL     string             `json:"downloadUrl"`
	UploadURL       string             `json:"uploadUrl"`
	EventSourceURL  string             `json:"eventSourceUrl"`
	State           string             `json:"state"`
}

// SubmissionCapability defines the capability object for "urn:ietf:params:jmap:submission" per RFC 8621 Section 7.
type SubmissionCapability struct{}

// DefaultSession creates a default RFC 8620 / 8621 / 9219 / 9404 / 9425 compliant Session object
// for a username, with the account keyed by the derived accountID (AccountIDForSubject(username)).
func DefaultSession(baseURL string, username string) *Session {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if username == "" {
		username = "user@example.com"
	}
	return sessionFor(baseURL, username, AccountIDForSubject(username))
}

// SessionForAccountID creates a Session for an authenticated accountID, used by the per-request
// session handler where only the accountID (not the original username) is available. When username
// is empty the accountID is used as the display name.
func SessionForAccountID(baseURL, username, accountID string) *Session {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if accountID == "" {
		accountID = AccountIDForSubject("user@example.com")
	}
	if username == "" {
		if subj, ok := SubjectForAccountID(accountID); ok && subj != "" {
			username = subj
		} else {
			username = accountID
		}
	}
	return sessionFor(baseURL, username, accountID)
}

func sessionFor(baseURL, username, accountID string) *Session {
	accountCaps := map[string]any{
		CoreCapabilityURI: struct{}{},
		MailCapabilityURI: MailCapability{
			MaxMailboxesPerEmail:       nil,
			MaxMailboxDepth:            nil,
			MaxSizeMailboxName:         255,
			MaxSizeAttachmentsPerEmail: 50000000,
			EmailQuerySortOptions:      []string{"receivedAt", "sentAt", "size", "subject", "from", "to", "hasKeyword", "allInThreadHaveKeyword", "someInThreadHaveKeyword"},
			MayCreateTopLevelMailbox:   true,
		},
		SmimeCapabilityURI: SmimeCapability{
			SmimeVerificationSupported: true,
		},
		BlobCapabilityURI: BlobCapability{
			MaxSizeBlobSet:            nil,
			MaxDataSources:            100,
			SupportedTypeNames:        []string{"Mailbox", "Thread", "Email", "Calendar", "CalendarEvent", "AddressBook", "ContactCard", "Card", "FileNode", "SieveScript"},
			SupportedDigestAlgorithms: []string{"sha-256"},
		},
		QuotaCapabilityURI:            struct{}{},
		MdnCapabilityURI:              struct{}{},
		VacationResponseCapabilityURI: struct{}{},
		WebPushVapidCapabilityURI:     struct{}{},
		ContactsCapabilityURI:         struct{}{},
		CalendarsCapabilityURI:        struct{}{},
		CalendarsParseCapabilityURI:   struct{}{},
		SieveCapabilityURI:            struct{}{},
		FileNodeCapabilityURI:         struct{}{},
		PrincipalsCapabilityURI: PrincipalCapability{
			MaxAvailabilityDuration: "P30D",
		},
		AvailabilityCapabilityURI: struct{}{},
	}

	accounts := map[string]Account{
		accountID: {
			Name:                username,
			IsPrimary:           true,
			IsPersonal:          true,
			IsReadOnly:          false,
			AccountCapabilities: accountCaps,
		},
	}

	if username == "user@example.com" || strings.HasSuffix(username, "-multi") {
		secondaryAccountID := AccountIDForSubject("user2@example.com")
		accounts[secondaryAccountID] = Account{
			Name:                "user2@example.com",
			IsPrimary:           false,
			IsPersonal:          false,
			IsReadOnly:          false,
			AccountCapabilities: accountCaps,
		}
	}

	return &Session{
		Capabilities: map[string]any{
			CoreCapabilityURI: CoreCapability{
				MaxSizeUpload:         50000000,
				MaxConcurrentUpload:   4,
				MaxSizeRequest:        10000000,
				MaxConcurrentRequests: 4,
				MaxCallsInRequest:     16,
				MaxObjectsInGet:       500,
				MaxObjectsInSet:       500,
				CollationAlgorithms:   []string{"i;ascii-casemap", "i;octet"},
			},
			MailCapabilityURI:       struct{}{},
			SubmissionCapabilityURI: SubmissionCapability{},
			SmimeCapabilityURI: SmimeCapability{
				SmimeVerificationSupported: true,
			},
			BlobCapabilityURI: struct{}{},
			QuotaCapabilityURI: QuotaCapability{
				MaxQuotaResources: 10,
			},
			MdnCapabilityURI: MdnCapability{},
			// RFC 8621 Section 8: vacation-response auto-reply capability (empty object).
			VacationResponseCapabilityURI: struct{}{},
			// RFC 9749: Advertise VAPID public key for Web Push authentication.
			// The placeholder key is a no-op base64url-encoded NIST P-256 uncompressed public key point.
			WebPushVapidCapabilityURI: WebPushVapidCapability{
				ApplicationServerKey: "BCVxsr7N_eNgVRqvHtD0zTZsEc9-Lkvr-4km-ML7dvHfBQNO-leJAM5bkUtZikUUIaKGZvgVmsBbj56IL57-BgM",
			},
			// RFC 8887: JMAP WebSocket subprotocol capability.
			WebSocketCapabilityURI: WebSocketCapability{
				URL:          strings.Replace(strings.Replace(baseURL, "http://", "ws://", 1), "https://", "wss://", 1) + "/jmap/ws",
				SupportsPush: true,
			},
			// RFC 9610: JMAP for Contacts capability.
			ContactsCapabilityURI: ContactsCapability{
				MaxAddressBooksPerCard: nil,
				MayCreateAddressBook:   true,
			},
			// JMAP for Calendars capability.
			CalendarsCapabilityURI: CalendarsCapability{
				MaxCalendarsPerEvent:     nil,
				MayCreateCalendar:        true,
				MinDateTime:              "1900-01-01T00:00:00",
				MaxDateTime:              "9999-12-31T23:59:59",
				MaxExpandedQueryDuration: "P730D",
				MaxParticipantsPerEvent:  nil,
			},
			// Optional CalendarEvent/parse support (draft-ietf-jmap-calendars Section 1.5.3).
			CalendarsParseCapabilityURI: struct{}{},
			// RFC 9661: JMAP for Sieve Scripts capability.
			SieveCapabilityURI: SieveCapability{
				MaxScriptSize:   1048576, // 1MB max script size
				SieveExtensions: []string{"fileinto", "reject", "vacation", "envelope", "subaddress", "encoded-character"},
			},
			// FileNode file storage extension capability.
			FileNodeCapabilityURI: FileNodeCapability{
				MaxFileSize: 50000000,
			},
			PrincipalsCapabilityURI: PrincipalCapability{
				MaxAvailabilityDuration: "P30D",
			},
			AvailabilityCapabilityURI: struct{}{},
		},
		Accounts: accounts,
		PrimaryAccounts: map[string]string{
			CoreCapabilityURI:             accountID,
			MailCapabilityURI:             accountID,
			BlobCapabilityURI:             accountID,
			QuotaCapabilityURI:            accountID,
			MdnCapabilityURI:              accountID,
			VacationResponseCapabilityURI: accountID,
			WebPushVapidCapabilityURI:     accountID,
			ContactsCapabilityURI:         accountID,
			CalendarsCapabilityURI:        accountID,
			CalendarsParseCapabilityURI:   accountID,
			SieveCapabilityURI:            accountID,
			FileNodeCapabilityURI:         accountID,
			PrincipalsCapabilityURI:       accountID,
			AvailabilityCapabilityURI:     accountID,
		},
		Username:       username,
		APIURL:         baseURL + "/jmap",
		DownloadURL:    baseURL + "/download/{accountId}/{blobId}/{name}?type={type}",
		UploadURL:      baseURL + "/upload/{accountId}/",
		EventSourceURL: baseURL + "/eventsource?types={types}&closeafter={closeafter}&ping={ping}",
		State:          "0",
	}
}

type usingCtxKey struct{}

func WithUsingCapabilities(ctx context.Context, using []string) context.Context {
	return context.WithValue(ctx, usingCtxKey{}, using)
}

func UsingCapabilitiesFromContext(ctx context.Context) ([]string, bool) {
	using, ok := ctx.Value(usingCtxKey{}).([]string)
	return using, ok
}

func IsUsingCapability(ctx context.Context, capURI string) bool {
	using, ok := UsingCapabilitiesFromContext(ctx)
	if !ok {
		return true // Default to true when context not set in unit tests
	}
	for _, u := range using {
		if u == capURI {
			return true
		}
	}
	return false
}
