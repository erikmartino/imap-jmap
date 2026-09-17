package jmapsession


// Standard Capability URIs defined across RFC specifications per RFC 8620 Section 2.
// @spec RFC8620#2-p1-MUST
const (
	CoreCapabilityURI             = "urn:ietf:params:jmap:core"
	MailCapabilityURI             = "urn:ietf:params:jmap:mail"
	SubmissionCapabilityURI       = "urn:ietf:params:jmap:submission"
	SmimeCapabilityURI            = "urn:ietf:params:jmap:smime"
	BlobCapabilityURI             = "urn:ietf:params:jmap:blob"
	QuotaCapabilityURI            = "urn:ietf:params:jmap:quota"
	MdnCapabilityURI              = "urn:ietf:params:jmap:mdn"
	VacationResponseCapabilityURI = "urn:ietf:params:jmap:vacationresponse"
	WebPushVapidCapabilityURI     = "urn:ietf:params:jmap:webpush-vapid"
	WebSocketCapabilityURI        = "urn:ietf:params:jmap:websocket"
	ContactsCapabilityURI         = "urn:ietf:params:jmap:contacts"
	CalendarsCapabilityURI        = "urn:ietf:params:jmap:calendars"
	CalendarsParseCapabilityURI   = "urn:ietf:params:jmap:calendars:parse"
	SieveCapabilityURI            = "urn:ietf:params:jmap:sieve"
	ImapAccessCapabilityURI       = "urn:ietf:params:jmap:imapaccess"
	PrincipalsCapabilityURI       = "urn:ietf:params:jmap:principals"
	AvailabilityCapabilityURI     = "urn:ietf:params:jmap:principals:availability"
	PrincipalsOwnerCapabilityURI  = "urn:ietf:params:jmap:principals:owner"
	FileNodeCapabilityURI         = "urn:ietf:params:jmap:filenode"
)

// Default CoreCapability limits per RFC 8620 Section 2.2.
// @spec RFC8620#2.2-p1-MUST
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
// @spec RFC8620#2.2-p1-MUST
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

// Account defines an account object in the JMAP Session per RFC 8620 Section 2.
// @spec RFC8620#2-p2-MUST
type Account struct {
	Name                string         `json:"name"`
	IsPrimary           bool           `json:"isPrimary"`
	IsPersonal          bool           `json:"isPersonal"`
	IsReadOnly          bool           `json:"isReadOnly"`
	AccountCapabilities map[string]any `json:"accountCapabilities"`
}

// Session represents the JMAP Session resource object per RFC 8620 Section 2.
// @spec RFC8620#2-p3-MUST
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
