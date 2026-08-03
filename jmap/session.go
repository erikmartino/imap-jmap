package jmap

import "strings"

// CoreCapabilityURI is the standard JMAP core capability URI defined in RFC 8620 Section 2.2.
const CoreCapabilityURI = "urn:ietf:params:jmap:core"

// MailCapabilityURI is the standard JMAP mail capability URI defined in RFC 8621 Section 2.
const MailCapabilityURI = "urn:ietf:params:jmap:mail"

// SmimeCapabilityURI is the standard JMAP S/MIME capability URI defined in RFC 9219 Section 2.
const SmimeCapabilityURI = "urn:ietf:params:jmap:smime"

// BlobCapabilityURI is the standard JMAP Blob Management capability URI defined in RFC 9404 Section 2.
const BlobCapabilityURI = "urn:ietf:params:jmap:blob"

// QuotaCapabilityURI is the standard JMAP Quota capability URI defined in RFC 9425 Section 2.
const QuotaCapabilityURI = "urn:ietf:params:jmap:quota"

// MdnCapabilityURI is the standard JMAP MDN capability URI defined in RFC 9007 Section 2.
const MdnCapabilityURI = "urn:ietf:params:jmap:mdn"

// WebPushVapidCapabilityURI is the JMAP capability URI for VAPID Web Push per RFC 9749 Section 3.
const WebPushVapidCapabilityURI = "urn:ietf:params:jmap:webpush-vapid"

// WebSocketCapabilityURI is the JMAP capability URI for WebSocket transport per RFC 8887 Section 3.
const WebSocketCapabilityURI = "urn:ietf:params:jmap:websocket"

// ContactsCapabilityURI is the standard JMAP Contacts capability URI defined in RFC 9610 Section 2.
const ContactsCapabilityURI = "urn:ietf:params:jmap:contacts"

// CalendarsCapabilityURI is the standard JMAP Calendars capability URI.
const CalendarsCapabilityURI = "urn:ietf:params:jmap:calendars"

// SieveCapabilityURI is the standard JMAP Sieve capability URI defined in RFC 9661 Section 2.
const SieveCapabilityURI = "urn:ietf:params:jmap:sieve"

// ImapAccessCapabilityURI is the JMAPACCESS extension for IMAP capability URI defined in RFC 9698 Section 2.
const ImapAccessCapabilityURI = "urn:ietf:params:jmap:imapaccess"

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

// MailCapability defines the capability object for "urn:ietf:params:jmap:mail" per RFC 8621 Section 2.
type MailCapability struct {
	MaxMailboxesPerEmail     *uint64  `json:"maxMailboxesPerEmail"`
	MaxMailboxDepth          *uint64  `json:"maxMailboxDepth"`
	MaxSizeMailboxName       uint64   `json:"maxSizeMailboxName"`
	MaxSizeEmailHeaders      uint64   `json:"maxSizeEmailHeaders"`
	MaxObjectsInGet          uint64   `json:"maxObjectsInGet"`
	MaxObjectsInSet          uint64   `json:"maxObjectsInSet"`
	CollationAlgorithms      []string `json:"collationAlgorithms"`
	EmailQuerySortOptions    []string `json:"emailQuerySortOptions"`
	MayCreateTopLevelMailbox bool     `json:"mayCreateTopLevelMailbox"`
}

// SmimeCapability defines the capability object for "urn:ietf:params:jmap:smime" per RFC 9219 Section 2.
type SmimeCapability struct {
	SmimeVerificationSupported bool `json:"smimeVerificationSupported"`
}

// BlobCapability defines the capability object for "urn:ietf:params:jmap:blob" per RFC 9404 Section 2.
type BlobCapability struct {
	SupportedAlgorithms []string `json:"supportedAlgorithms"`
	MaxDataAsStream     uint64   `json:"maxDataAsStream"`
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

// CalendarsCapability defines the capability object for "urn:ietf:params:jmap:calendars".
type CalendarsCapability struct {
	MaxCalendarsPerEvent *uint64 `json:"maxCalendarsPerEvent"`
	MayCreateCalendar    bool    `json:"mayCreateCalendar"`
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

// DefaultSession creates a default RFC 8620 / 8621 / 9219 / 9404 / 9425 compliant Session object.
func DefaultSession(baseURL string) *Session {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
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
			MailCapabilityURI: MailCapability{
				MaxMailboxesPerEmail:     nil,
				MaxMailboxDepth:          nil,
				MaxSizeMailboxName:       255,
				MaxSizeEmailHeaders:      100000,
				MaxObjectsInGet:          500,
				MaxObjectsInSet:          500,
				CollationAlgorithms:      []string{"i;ascii-casemap", "i;octet"},
				EmailQuerySortOptions:    []string{"receivedAt", "sentAt"},
				MayCreateTopLevelMailbox: true,
			},
			SmimeCapabilityURI: SmimeCapability{
				SmimeVerificationSupported: true,
			},
			BlobCapabilityURI: BlobCapability{
				SupportedAlgorithms: []string{"sha-256"},
				MaxDataAsStream:     50000000,
			},
			QuotaCapabilityURI: QuotaCapability{
				MaxQuotaResources: 10,
			},
			MdnCapabilityURI: MdnCapability{},
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
				MaxCalendarsPerEvent: nil,
				MayCreateCalendar:    true,
			},
			// RFC 9661: JMAP for Sieve Scripts capability.
			SieveCapabilityURI: SieveCapability{
				MaxScriptSize:   1048576, // 1MB max script size
				SieveExtensions: []string{"fileinto", "reject", "vacation", "envelope", "subaddress", "encoded-character"},
			},
			// RFC 9698: JMAPACCESS Extension for IMAP.
			ImapAccessCapabilityURI: ImapAccessCapability{},
			// FileNode file storage extension capability.
			FileNodeCapabilityURI: FileNodeCapability{
				MaxFileSize: 50000000,
			},
		},
		Accounts: map[string]Account{
			"primary": {
				Name:       "user@example.com",
				IsPrimary:  true,
				IsReadOnly: false,
				AccountCapabilities: map[string]any{
					CoreCapabilityURI:         struct{}{},
					MailCapabilityURI:         struct{}{},
					SmimeCapabilityURI:        struct{}{},
					BlobCapabilityURI:         struct{}{},
					QuotaCapabilityURI:        struct{}{},
					MdnCapabilityURI:          struct{}{},
					WebPushVapidCapabilityURI: struct{}{},
					ContactsCapabilityURI:     struct{}{},
					CalendarsCapabilityURI:    struct{}{},
					SieveCapabilityURI:        struct{}{},
					ImapAccessCapabilityURI:   struct{}{},
					FileNodeCapabilityURI:     struct{}{},
				},
			},
		},
		PrimaryAccounts: map[string]string{
			CoreCapabilityURI:         "primary",
			MailCapabilityURI:         "primary",
			SmimeCapabilityURI:        "primary",
			BlobCapabilityURI:         "primary",
			QuotaCapabilityURI:        "primary",
			MdnCapabilityURI:          "primary",
			WebPushVapidCapabilityURI: "primary",
			ContactsCapabilityURI:     "primary",
			CalendarsCapabilityURI:    "primary",
			SieveCapabilityURI:        "primary",
			ImapAccessCapabilityURI:   "primary",
			FileNodeCapabilityURI:     "primary",
		},
		Username:       "user@example.com",
		APIURL:         baseURL + "/jmap",
		DownloadURL:    baseURL + "/download/{accountId}/{blobId}/{name}?type={type}",
		UploadURL:      baseURL + "/upload/{accountId}/",
		EventSourceURL: baseURL + "/eventsource?types={types}&closeafter={closeafter}&ping={ping}",
		State:          "0",
	}
}
