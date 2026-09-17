package jmapsieve

import (
	"imap-jmap/jmap/jmapcore"
)

// SieveCapabilityURI is the standard JMAP Sieve capability URI per RFC 9661 Section 2.
const SieveCapabilityURI = "urn:ietf:params:jmap:sieve"

// SieveCapability defines the capability object for "urn:ietf:params:jmap:sieve" per RFC 9661 Section 2.
type SieveCapability struct {
	MaxScriptSize   uint64   `json:"maxScriptSize"`
	SieveExtensions []string `json:"sieveExtensions"`
}

// SieveScript represents a SieveScript object per RFC 9661 Section 1.4.
type SieveScript struct {
	ID       jmapcore.Id `json:"id"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	IsActive bool   `json:"isActive"`
	IsValid  bool   `json:"isValid"`
}
