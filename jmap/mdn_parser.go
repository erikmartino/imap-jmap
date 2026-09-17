package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

// ParseMDNFromBytes decodes raw RFC 5322 MIME bytes into an MDN object per RFC 8098 and RFC 9007 Section 2.
func ParseMDNFromBytes(raw []byte) (*MDN, error) {
	return jmapmail.ParseMDNFromBytes(raw)
}
