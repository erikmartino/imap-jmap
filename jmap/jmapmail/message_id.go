package jmapmail

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-message/textproto"
	"github.com/mcnijman/go-emailaddress"
)

// MsgIDRegex matches the RFC 5322 Section 3.6.4 msg-id ABNF (id-left "@" id-right
// enclosed in angle brackets) without whitespace.
var MsgIDRegex = regexp.MustCompile(`^<[^<>@\s]+@[^<>@\s]+>$`)

// domainFromMailboxOrDomain derives a domain from a mailbox (optionally wrapped
// in a mailto: URI or angle brackets) or a bare hostname. It parses the input
// with the standard libraries and never guesses a domain from malformed input.
func domainFromMailboxOrDomain(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if u, err := url.Parse(s); err == nil && strings.EqualFold(u.Scheme, "mailto") {
		if u.Opaque != "" {
			s = u.Opaque
		} else {
			s = u.Path
		}
	}
	s = strings.TrimSpace(strings.Trim(s, "<>"))
	if addr, err := mail.ParseAddress(s); err == nil {
		s = addr.Address
	}
	if email, err := emailaddress.Parse(s); err == nil && email.Domain != "" {
		return email.Domain
	}
	// A bare host name (no local part) is accepted only when it parses as a
	// URL host with no user-info or port.
	if u, err := url.Parse("//" + s); err == nil && s != "" && u.User == nil && u.Port() == "" && u.Host == s {
		return s
	}
	return ""
}

// GenerateMessageID creates an RFC 5322 Section 3.6.4 compliant Message-ID
// value without enclosing angle brackets (in conformance with JMAP RFC 8621 Section 4.1.2).
// The domain is extracted from the provided email address or domain string;
// if none is provided or if it lacks a valid hostname, it falls back to "localhost".
func GenerateMessageID(domainOrAddress string) string {
	domain := domainFromMailboxOrDomain(domainOrAddress)
	domain = strings.Trim(domain, "<> .")

	// Sanitize domain to valid hostname characters (RFC 1123 / RFC 5322 dot-atom)
	domain = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return -1
	}, domain)
	domain = strings.Trim(domain, ".-")
	if domain == "" {
		domain = "localhost"
	}

	now := uint64(time.Now().UnixNano())
	nonceByte := make([]byte, 8)
	if _, err := rand.Read(nonceByte); err != nil {
		// Improbable fallback for crypto/rand error
		binary.BigEndian.PutUint64(nonceByte, uint64(os.Getpid())^now)
	}
	nonce := binary.BigEndian.Uint64(nonceByte)

	return fmt.Sprintf("%s.%s@%s", strconv.FormatUint(now, 36), strconv.FormatUint(nonce, 36), domain)
}

// HasValidMessageID reports whether the message data carries a Message-ID header
// field whose value conforms to the RFC 5322 Section 3.6.4 msg-id syntax.
func HasValidMessageID(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	br := bufio.NewReader(bytes.NewReader(data))
	hdr, err := textproto.ReadHeader(br)
	if err != nil {
		return false
	}
	v := strings.TrimSpace(hdr.Get("Message-ID"))
	return v != "" && MsgIDRegex.MatchString(v)
}

// EnsureValidMessageID inspects raw RFC 822/5322 message bytes for a valid Message-ID header.
// If the Message-ID is missing or syntactically invalid, it adds or replaces the Message-ID field
// in conformance with RFC 6409 Section 8.3 and RFC 5322 Section 3.6.4.
func EnsureValidMessageID(data []byte, domainOrAddress string) []byte {
	if HasValidMessageID(data) {
		return data
	}

	genID := GenerateMessageID(domainOrAddress)
	msgIDVal := "<" + genID + ">"

	if len(data) == 0 {
		var h textproto.Header
		h.Set("Message-ID", msgIDVal)
		var buf bytes.Buffer
		_ = textproto.WriteHeader(&buf, h)
		return buf.Bytes()
	}

	br := bufio.NewReader(bytes.NewReader(data))
	hdr, err := textproto.ReadHeader(br)
	if err != nil {
		var h textproto.Header
		h.Set("Message-ID", msgIDVal)
		var buf bytes.Buffer
		_ = textproto.WriteHeader(&buf, h)
		buf.Write(data)
		return buf.Bytes()
	}

	hdr.Set("Message-ID", msgIDVal)
	var buf bytes.Buffer
	if err := textproto.WriteHeader(&buf, hdr); err != nil {
		return data
	}
	_, _ = io.Copy(&buf, br)
	return buf.Bytes()
}

// StripBCCHeader removes any Bcc header field from raw message bytes during transmission
// per RFC 8621 Section 7.5.
func StripBCCHeader(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	br := bufio.NewReader(bytes.NewReader(data))
	hdr, err := textproto.ReadHeader(br)
	if err != nil {
		return data
	}

	if !hdr.Has("Bcc") {
		return data
	}

	hdr.Del("Bcc")
	var buf bytes.Buffer
	if err := textproto.WriteHeader(&buf, hdr); err != nil {
		return data
	}
	_, _ = io.Copy(&buf, br)
	return buf.Bytes()
}
