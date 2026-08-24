package jmap

import (
	"fmt"
	"strings"
)

// JMAP tags are keywords of the form
//
//	$tag/<name>          a boolean tag: present on a message or not
//	$tag/<name>/<value>  a tag carrying a value
//
// They follow the same IMAP/JMAP keyword grammar as any other keyword
// (RFC 8621 Section 4.2.2): 1-255 bytes of ASCII %x21-%x7e, excluding
// ( ) { ] % * " \ and space/control, with servers returning keywords in
// lowercase. Because the keyword is shared 1:1 with IMAP, a tag is stored as
// the literal keyword on the message.
//
// Tag names and values are arbitrary user strings, so a raw name/value must be
// escaped into a keyword-safe form before being embedded in a keyword. '%' is
// itself forbidden in keywords, so it cannot be used for percent-encoding;
// '~' (keyword-safe) is used instead:
//
// literal:  [A-Za-z0-9$_-]
// escaped:  ~xx   (lowercase hex of any other byte, including '/', '~',
//                  space, control and UTF-8 bytes)
//
// The '/' between name and value is the literal, unescaped separator.

// tagKeywordPrefix is the reserved keyword prefix identifying a tag.
const tagKeywordPrefix = "$tag/"

// IsTagKeyword reports whether keyword is a tag keyword of the form $tag/...
func IsTagKeyword(keyword string) bool {
	return strings.HasPrefix(keyword, tagKeywordPrefix) && len(keyword) > len(tagKeywordPrefix)
}

// isTagSegmentLiteral reports whether b may appear literally inside an escaped
// tag segment. Every other byte is written as ~XX.
func isTagSegmentLiteral(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '$', '_', '-':
		return true
	}
	return false
}

const hexDigits = "0123456789abcdef"

func escapeTagSegment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isTagSegmentLiteral(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('~')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0x0f])
	}
	return b.String()
}

func unescapeTagSegment(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '~' {
			b.WriteByte(c)
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("invalid tag escape sequence in %q", s)
		}
		hi, err := hexValue(s[i+1])
		if err != nil {
			return "", err
		}
		lo, err := hexValue(s[i+2])
		if err != nil {
			return "", err
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	return b.String(), nil
}

func hexValue(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, fmt.Errorf("invalid hex digit %q", c)
}

// TagToKeyword builds the keyword for a tag name and optional value. Names and
// values are normalized to lowercase (RFC 8621: servers MUST return keywords
// in lowercase) and escaped into the keyword-safe form. It returns an error
// when the name is empty or the encoded keyword would exceed 255 bytes.
func TagToKeyword(name, value string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("tag name must not be empty")
	}
	kw := tagKeywordPrefix + escapeTagSegment(strings.ToLower(name))
	if value != "" {
		kw += "/" + escapeTagSegment(strings.ToLower(value))
	}
	if len(kw) > 255 {
		return "", fmt.Errorf("tag keyword exceeds 255 bytes")
	}
	return kw, nil
}

// KeywordToTag parses a tag keyword back into its decoded name and value.
// The value is empty for a boolean tag ($tag/<name>). ok is false when the
// keyword is not a well-formed tag.
func KeywordToTag(keyword string) (name, value string, ok bool) {
	kw := strings.ToLower(keyword)
	if !IsTagKeyword(kw) {
		return "", "", false
	}
	rest := strings.TrimPrefix(kw, tagKeywordPrefix)
	seg1 := rest
	seg2 := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		seg1, seg2 = rest[:i], rest[i+1:]
	}
	decodedName, err := unescapeTagSegment(seg1)
	if err != nil || decodedName == "" {
		return "", "", false
	}
	decodedValue, err := unescapeTagSegment(seg2)
	if err != nil {
		return "", "", false
	}
	return decodedName, decodedValue, true
}
