package jmap

// CoreCapabilityURI is the standard JMAP core capability URI defined in RFC 8620 Section 2.2.
const CoreCapabilityURI = "urn:ietf:params:jmap:core"

// MailCapabilityURI is the standard JMAP mail capability URI defined in RFC 8621 Section 2.
const MailCapabilityURI = "urn:ietf:params:jmap:mail"

// SmimeCapabilityURI is the standard JMAP S/MIME capability URI defined in RFC 9219 Section 2.
const SmimeCapabilityURI = "urn:ietf:params:jmap:smime"

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

// Account defines an account object in the JMAP Session per RFC 8620 Section 2.
type Account struct {
	Name                string         `json:"name"`
	IsPrimary           bool           `json:"isPrimary"`
	IsReadOnly          bool           `json:"isReadOnly"`
	AccountCapabilities map[string]any `json:"accountCapabilities"`
}

// Session represents the JMAP Session resource object per RFC 8620 Section 2.
type Session struct {
	Capabilities    map[string]any `json:"capabilities"`
	Accounts        map[string]Account     `json:"accounts"`
	PrimaryAccounts map[string]string      `json:"primaryAccounts"`
	Username        string                 `json:"username"`
	APIURL          string                 `json:"apiUrl"`
	DownloadURL     string                 `json:"downloadUrl"`
	UploadURL       string                 `json:"uploadUrl"`
	EventSourceURL  string                 `json:"eventSourceUrl"`
	State           string                 `json:"state"`
}

// DefaultSession creates a default RFC 8620 / RFC 8621 / RFC 9219 compliant Session object.
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
		},
		Accounts: map[string]Account{
			"primary": {
				Name:       "user@example.com",
				IsPrimary:  true,
				IsReadOnly: false,
				AccountCapabilities: map[string]any{
					CoreCapabilityURI:  struct{}{},
					MailCapabilityURI:  struct{}{},
					SmimeCapabilityURI: struct{}{},
				},
			},
		},
		PrimaryAccounts: map[string]string{
			CoreCapabilityURI:  "primary",
			MailCapabilityURI:  "primary",
			SmimeCapabilityURI: "primary",
		},
		Username:       "user@example.com",
		APIURL:         baseURL + "/jmap",
		DownloadURL:    baseURL + "/download/{accountId}/{blobId}/{name}?type={type}",
		UploadURL:      baseURL + "/upload/{accountId}/",
		EventSourceURL: baseURL + "/eventsource?types={types}&closeafter={closeafter}&ping={ping}",
		State:          "0",
	}
}
