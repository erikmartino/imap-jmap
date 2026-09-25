package jmapmail

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net/mail"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MsgIDRegex matches the RFC 5322 Section 3.6.4 msg-id ABNF (id-left "@" id-right
// enclosed in angle brackets) without whitespace.
var MsgIDRegex = regexp.MustCompile(`^<[^<>@\s]+@[^<>@\s]+>$`)

// GenerateMessageID creates an RFC 5322 Section 3.6.4 compliant Message-ID
// value without enclosing angle brackets (in conformance with JMAP RFC 8621 Section 4.1.2).
// The domain is extracted from the provided email address or domain string;
// if none is provided or if it lacks a valid hostname, it falls back to "localhost".
func GenerateMessageID(domainOrAddress string) string {
	domain := ""
	if idx := strings.LastIndex(domainOrAddress, "@"); idx != -1 && idx+1 < len(domainOrAddress) {
		domain = domainOrAddress[idx+1:]
	} else if domainOrAddress != "" && !strings.ContainsAny(domainOrAddress, "@ \t\r\n<>") {
		domain = domainOrAddress
	}
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
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return false
	}
	v := strings.TrimSpace(msg.Header.Get("Message-ID"))
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
	newHeader := fmt.Sprintf("Message-ID: <%s>\r\n", genID)

	if len(data) == 0 {
		return []byte(newHeader + "\r\n")
	}

	// In RFC 5322, the header block is separated from the body by \r\n\r\n (or \n\n).
	headerEnd := bytes.Index(data, []byte("\r\n\r\n"))
	delimLen := 4
	if headerEnd == -1 {
		headerEnd = bytes.Index(data, []byte("\n\n"))
		delimLen = 2
	}

	if headerEnd == -1 {
		// Bare headers without body separator
		return append([]byte(newHeader), data...)
	}

	headerBytes := data[:headerEnd]
	bodyBytes := data[headerEnd+delimLen:]

	// Filter out any existing invalid Message-ID header lines (including folded continuation lines)
	lines := strings.Split(string(headerBytes), "\n")
	var filteredHeader strings.Builder
	inInvalidMsgID := false
	hasExistingMsgID := false

	for _, rawLine := range lines {
		trimmedLine := strings.TrimRight(rawLine, "\r")
		if strings.HasPrefix(strings.ToLower(trimmedLine), "message-id:") {
			hasExistingMsgID = true
			inInvalidMsgID = true
			continue
		}
		if inInvalidMsgID {
			if strings.HasPrefix(trimmedLine, " ") || strings.HasPrefix(trimmedLine, "\t") {
				continue
			}
			inInvalidMsgID = false
		}
		filteredHeader.WriteString(trimmedLine)
		filteredHeader.WriteString("\r\n")
	}

	var buf bytes.Buffer
	buf.WriteString(newHeader)
	if hasExistingMsgID {
		buf.WriteString(filteredHeader.String())
	} else {
		buf.Write(headerBytes)
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")
	buf.Write(bodyBytes)
	return buf.Bytes()
}
