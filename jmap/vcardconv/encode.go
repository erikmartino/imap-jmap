// Package vcardconv implements the bidirectional JSContact (RFC 9553) to
// vCard (RFC 6350) conversion defined by RFC 9555 and the vCard extensions
// defined by RFC 9554, used by the jscontact-tests conformance suite
// (github.com/jmapio/jscontact-tests).
//
// vCard parsing is delegated to github.com/emersion/go-vcard, the repository's
// standard vCard parser. This package provides its own serializer because the
// go-vcard encoder cannot express the grammar features RFC 9554/9555 require:
// quoted parameter values (JSCOMPS must always be quoted), RFC 6868 caret
// encoding of parameter values, and unescaped SEMICOLON characters inside
// structured property values (the suite's reference parser treats them as raw
// value bytes).
package vcardconv

import (
	"strconv"
	"strings"
)

// vcardField is a single vCard property line (group, name, parameters, value).
type vcardField struct {
	Group  string
	Name   string
	Params []vcardParam
	Value  string
}

// paramMode controls how a parameter value is rendered.
type paramMode int

const (
	// paramAuto caret-encodes the value (RFC 6868) and quotes it whenever the
	// encoded value contains a character that is not a SAFE-CHAR.
	paramAuto paramMode = iota
	// paramQuotedAlways emits the value verbatim inside DQUOTEs. Used for
	// JSCOMPS, whose value already carries the escapes required by RFC 9555
	// §3.3.1 (RFC 6868 caret encoding plus backslash-escaped COMMA/SEMICOLON).
	paramQuotedAlways
	// paramUnquoted emits the value verbatim without quoting. Used for
	// multi-valued TYPE parameters (e.g. TYPE=HOME,VOICE), which the reference
	// parser of the jscontact-tests suite captures as a single raw token.
	paramUnquoted
)

// vcardParam is a single vCard parameter value.
type vcardParam struct {
	Name  string
	Value string
	Mode  paramMode
}

// isSafeParamChar reports whether c is a vCard 4.0 SAFE-CHAR (RFC 6350 §3.3):
// WSP / %x21 / %x23-7E / NON-ASCII.
func isSafeParamChar(c rune) bool {
	switch {
	case c == ' ' || c == '\t':
		return true
	case c == 0x21:
		return true
	case c >= 0x23 && c <= 0x7E:
		return true
	case c >= 0x80:
		return true
	}
	return false
}

// caretEncode applies the RFC 6868 encoding to a parameter value: ^ -> ^^,
// LF -> ^n, DQUOTE -> ^'.
func caretEncode(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '^':
			b.WriteString("^^")
		case '\n':
			b.WriteString("^n")
		case '"':
			b.WriteString("^'")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// caretDecode reverses caretEncode.
func caretDecode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '^' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case '^':
			b.WriteByte('^')
		case 'n':
			b.WriteByte('\n')
		case '\'':
			b.WriteByte('"')
		default:
			b.WriteByte('^')
			continue
		}
		i++
	}
	return b.String()
}

// escapeText escapes a vCard text value per RFC 6350 §3.4: BACKSLASH becomes
// "\\", newline becomes "\n", and COMMA becomes "\,". Semicolons are left raw
// because the reference parser used by the jscontact-tests suite treats them
// as literal value bytes (e.g. tel URIs such as "tel:+1-555-555-5555;ext=1").
func escapeText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case ',':
			b.WriteString("\\,")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeJSCOMPSVerb encodes a JSCOMPS separator value: the verb is first
// RFC 6868 caret-encoded, then COMMA and SEMICOLON are backslash-escaped per
// RFC 6350 §3.4 as required by RFC 9555 §3.3.1.
func escapeJSCOMPSVerb(s string) string {
	enc := caretEncode(s)
	var b strings.Builder
	for _, r := range enc {
		switch r {
		case ',':
			b.WriteString("\\,")
		case ';':
			b.WriteString("\\;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// unescapeJSCOMPSVerb reverses escapeJSCOMPSVerb.
func unescapeJSCOMPSVerb(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == ',' || s[i+1] == ';') {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return caretDecode(b.String())
}

// encodeValue renders a parameter value per its mode.
func encodeValue(s string, mode paramMode) string {
	switch mode {
	case paramQuotedAlways:
		return `"` + s + `"`
	case paramUnquoted:
		return s
	}
	enc := caretEncode(s)
	for _, r := range enc {
		if !isSafeParamChar(r) {
			return `"` + enc + `"`
		}
	}
	return enc
}

const foldLen = 75

// encodeLine renders a single vCard property line with folding at 75 octets
// per RFC 6350 §3.2.
func encodeLine(f vcardField) string {
	var b strings.Builder
	if f.Group != "" {
		b.WriteString(f.Group)
		b.WriteString(".")
	}
	b.WriteString(f.Name)
	for _, p := range f.Params {
		b.WriteByte(';')
		b.WriteString(p.Name)
		b.WriteByte('=')
		b.WriteString(encodeValue(p.Value, p.Mode))
	}
	b.WriteByte(':')
	b.WriteString(escapeText(f.Value))

	line := b.String()
	if len(line) <= foldLen {
		return line
	}
	// Fold at 75 octets: a CRLF followed by a single space continues the line.
	var folded strings.Builder
	for len(line) > foldLen {
		folded.WriteString(line[:foldLen])
		folded.WriteString("\r\n ")
		line = line[foldLen:]
	}
	folded.WriteString(line)
	return folded.String()
}

// encodeVCard serializes a set of properties into a vCard 4.0 document.
func encodeVCard(fields []vcardField) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\n")
	b.WriteString("VERSION:4.0\r\n")
	for _, f := range fields {
		b.WriteString(encodeLine(f))
		b.WriteString("\r\n")
	}
	b.WriteString("END:VCARD\r\n")
	return b.String()
}

// strconv is retained for the timestamp helpers used by the mapping.
var _ = strconv.Itoa