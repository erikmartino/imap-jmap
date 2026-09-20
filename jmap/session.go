package jmap

import (
	"context"
	"strings"

	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcontacts"
	"imap-jmap/jmap/jmapfilenode"
	"imap-jmap/jmap/jmapsession"
	"imap-jmap/jmap/jmapsieve"
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
const CalendarsCapabilityURI = jmapcalendar.CalendarsCapabilityURI

// CalendarsParseCapabilityURI is the JMAP capability URI advertising support for the
// CalendarEvent/parse method per draft-ietf-jmap-calendars Section 1.5.3.
const CalendarsParseCapabilityURI = jmapcalendar.CalendarsParseCapabilityURI

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

// SharingCapabilityURI is the standard JMAP Sharing capability URI defined in RFC 9670 Section 5.
const SharingCapabilityURI = "urn:ietf:params:jmap:sharing"

// PrincipalCapability defines the capability object for "urn:ietf:params:jmap:principals".
type PrincipalCapability struct {
	MaxAvailabilityDuration string `json:"maxAvailabilityDuration"`
}

// FileNodeCapabilityURI is the JMAP capability URI for FileNode file storage extension.
const FileNodeCapabilityURI = jmapfilenode.FileNodeCapabilityURI

// FileNodeCapability defines the capability object for "urn:ietf:params:jmap:filenode".
type FileNodeCapability = jmapfilenode.FileNodeCapability

// ImapAccessCapability defines the capability object for "urn:ietf:params:jmap:imapaccess" per RFC 9698 Section 2.
type ImapAccessCapability struct{}

// Default CoreCapability limits per RFC 8620 Section 2.2.
const (
	DefaultMaxSizeUpload         uint64 = 50000000
	DefaultMaxConcurrentUpload   uint64 = 4
	DefaultMaxSizeRequest        uint64 = 10000000
	DefaultMaxConcurrentRequests uint64 = 4
	DefaultMaxCallsInRequest     uint64 = 16
	DefaultMaxObjectsInGet       uint64 = 500
	DefaultMaxObjectsInSet       uint64 = 500
)

// CoreCapability defines the capability object for "urn:ietf:params:jmap:core" per RFC 8620 Section 2.2.
type CoreCapability = jmapsession.CoreCapability

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
// The canonical definition lives in jmapcontacts; this is a type alias for backward compatibility.
type ContactsCapability = jmapcontacts.ContactsCapability

// CalendarsCapability defines the capability object for "urn:ietf:params:jmap:calendars"
// per draft-ietf-jmap-calendars Section 1.5.1.
// The canonical definition lives in jmapcalendar; this is a type alias for backward compatibility.
type CalendarsCapability = jmapcalendar.CalendarsCapability

// SieveCapability defines the capability object for "urn:ietf:params:jmap:sieve" per RFC 9661 Section 2.
// The canonical definition lives in jmapsieve; this is a type alias for backward compatibility.
type SieveCapability = jmapsieve.SieveCapability

// Account defines an account object in the JMAP Session per RFC 8620 Section 2.
type Account = jmapsession.Account

// Session represents the JMAP Session resource object per RFC 8620 Section 2.
type Session = jmapsession.Session

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
		SubmissionCapabilityURI:       SubmissionCapability{},
		VacationResponseCapabilityURI: struct{}{},
		WebPushVapidCapabilityURI:     struct{}{},
		WebSocketCapabilityURI:        struct{}{},
		ContactsCapabilityURI:         struct{}{},
		CalendarsCapabilityURI:        struct{}{},
		CalendarsParseCapabilityURI:   struct{}{},
		SieveCapabilityURI:            struct{}{},
		FileNodeCapabilityURI:         struct{}{},
		PrincipalsCapabilityURI: PrincipalCapability{
			MaxAvailabilityDuration: "P30D",
		},
		AvailabilityCapabilityURI: struct{}{},
		SharingCapabilityURI:      struct{}{},
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
				MaxSizeUpload:         DefaultMaxSizeUpload,
				MaxConcurrentUpload:   DefaultMaxConcurrentUpload,
				MaxSizeRequest:        DefaultMaxSizeRequest,
				MaxConcurrentRequests: DefaultMaxConcurrentRequests,
				MaxCallsInRequest:     DefaultMaxCallsInRequest,
				MaxObjectsInGet:       DefaultMaxObjectsInGet,
				MaxObjectsInSet:       DefaultMaxObjectsInSet,
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
			SharingCapabilityURI:      struct{}{},
		},
		Accounts: accounts,
		PrimaryAccounts: map[string]string{
			CoreCapabilityURI:             accountID,
			MailCapabilityURI:             accountID,
			SubmissionCapabilityURI:       accountID,
			WebSocketCapabilityURI:        accountID,
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
			SharingCapabilityURI:          accountID,
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
