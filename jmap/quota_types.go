package jmap

// Quota represents a Quota data object per RFC 9425 Section 4.
type Quota struct {
	ID           Id      `json:"id"`
	Name         string  `json:"name"`
	ResourceType string  `json:"resourceType"` // "octets" or "messages"
	Used         uint64  `json:"used"`
	HardLimit    uint64  `json:"hardLimit"`
	WarnLimit    *uint64 `json:"warnLimit,omitempty"`
	SoftLimit    *uint64 `json:"softLimit,omitempty"`
	Scope        string  `json:"scope"` // "account", "domain", "user"
	Description  *string `json:"description,omitempty"`
}
