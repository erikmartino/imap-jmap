package jmapcalendar

import (
	"strings"
)

// @spec RFC5545#3.3.11-p1-MUST
// escapeICalText applies RFC 5545 Section 3.3.11 TEXT escaping: backslash, newline,
// semicolon and comma are escaped so a value can never break the line/param structure.
func escapeICalText(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		"\r\n", `\n`,
		"\n", `\n`,
		"\r", `\n`,
		`;`, `\;`,
		`,`, `\,`,
	)
	return r.Replace(s)
}

// @spec RFC5545#3.3.11-p1-MUST
// unescapeICalText reverses escapeICalText for values read back from iCalendar.
func unescapeICalText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n', 'N':
				b.WriteByte('\n')
			case '\\', ';', ',':
				b.WriteByte(s[i+1])
			default:
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
