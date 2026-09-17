package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

// HeaderForm specifies the format of a header property.
type HeaderForm = jmapmail.HeaderForm

const (
	HeaderFormRaw              = jmapmail.HeaderFormRaw
	HeaderFormText             = jmapmail.HeaderFormText
	HeaderFormAddresses        = jmapmail.HeaderFormAddresses
	HeaderFormGroupedAddresses = jmapmail.HeaderFormGroupedAddresses
	HeaderFormMessageIDs       = jmapmail.HeaderFormMessageIDs
	HeaderFormDate             = jmapmail.HeaderFormDate
	HeaderFormURLs             = jmapmail.HeaderFormURLs
)

// ParsedHeaderProperty holds parsed header:* property metadata.
type ParsedHeaderProperty = jmapmail.ParsedHeaderProperty

// ParseHeaderProperty parses header:name[:form][:all] per RFC 8621 Section 4.1.3.
func ParseHeaderProperty(prop string) (*ParsedHeaderProperty, error) {
	return jmapmail.ParseHeaderProperty(prop)
}

// EvaluateHeaderProperty evaluates a parsed header property on an Email object.
func EvaluateHeaderProperty(em *Email, hp *ParsedHeaderProperty) any {
	return jmapmail.EvaluateHeaderProperty(em, hp)
}

// FilterEmailBodyPart filters an EmailBodyPart to only include requested bodyProperties.
func FilterEmailBodyPart(part EmailBodyPart, bodyProperties []string) map[string]any {
	return jmapmail.FilterEmailBodyPart(part, bodyProperties)
}
