package jmap

import (
	"bytes"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
)

func parseRawHeaders(raw []byte) []EmailHeader {
	var headers []EmailHeader
	lines := strings.Split(string(raw), "\n")
	var currentName, currentValue string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			// end of headers
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			// continuation line
			if currentName != "" {
				currentValue += "\r\n" + line
			}
		} else {
			if currentName != "" {
				headers = append(headers, EmailHeader{Name: currentName, Value: currentValue})
			}
			idx := strings.Index(line, ":")
			if idx >= 0 {
				currentName = strings.TrimSpace(line[:idx])
				currentValue = line[idx+1:]
			} else {
				currentName = ""
				currentValue = ""
			}
		}
	}
	if currentName != "" {
		headers = append(headers, EmailHeader{Name: currentName, Value: currentValue})
	}
	return headers
}

func parseReferencesList(val string) []string {
	matches := midRegex.FindAllStringSubmatch(val, -1)
	if len(matches) == 0 {
		return nil
	}
	var out []string
	for _, m := range matches {
		s := m[1]
		if s == "" {
			s = m[2]
		}
		s = strings.Trim(s, "<> \t")
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// parseRFC822 parses a raw RFC 5322 message into a JMAP Email object (RFC 8621 Section 4.1.1),
// extracting envelope headers and a text body so the result is indistinguishable from what a
// real server would return for Email/parse and Email/import.
func parseRFC822(raw []byte) (*Email, error) {
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}

	hdr := msg.Header
	headers := parseRawHeaders(raw)

	em := &Email{
		Size:        uint64(len(raw)),
		Subject:     decodeHeader(hdr.Get("Subject")),
		From:        parseAddressList(hdr.Get("From")),
		Sender:      parseAddressList(hdr.Get("Sender")),
		To:          parseAddressList(hdr.Get("To")),
		CC:          parseAddressList(hdr.Get("Cc")),
		BCC:         parseAddressList(hdr.Get("Bcc")),
		ReplyTo:     parseAddressList(hdr.Get("Reply-To")),
		Headers:     headers,
		References:  parseReferencesList(hdr.Get("References")),
		Keywords:    map[string]bool{},
		HTMLBody:    []EmailBodyPart{},
		Attachments: []EmailBodyPart{},
	}

	if msgID := strings.TrimSpace(hdr.Get("Message-ID")); msgID != "" {
		em.MessageID = []string{strings.Trim(msgID, "<>")}
	}
	if inReplyTo := strings.TrimSpace(hdr.Get("In-Reply-To")); inReplyTo != "" {
		em.InReplyTo = parseReferencesList(inReplyTo)
	}
	if date, err := hdr.Date(); err == nil {
		s := date.UTC().Format("2006-01-02T15:04:05Z")
		em.SentAt = &s
	}

	body, _ := io.ReadAll(msg.Body)
	bodyStr := string(body)
	contentType := hdr.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}
	partID := "1"
	em.BodyStructure = EmailBodyPart{
		PartID:  &partID,
		Type:    contentType,
		Size:    uint64(len(body)),
		Headers: headers,
	}
	em.TextBody = []EmailBodyPart{{
		PartID:  &partID,
		Type:    "text/plain",
		Size:    uint64(len(body)),
		Headers: headers,
	}}
	em.BodyValues = map[string]EmailBodyValue{"1": {Value: bodyStr}}
	em.Preview = preview(bodyStr, 256)

	return em, nil
}

// FormatEmailRFC822 serializes an Email object to raw RFC 5322 MIME message bytes.
func FormatEmailRFC822(em *Email) []byte {
	if em == nil {
		return nil
	}
	var buf bytes.Buffer
	var h gomail.Header
	if em.Subject != "" {
		h.SetSubject(em.Subject)
	}
	if em.SentAt != nil && *em.SentAt != "" {
		if t, err := time.Parse(time.RFC3339, *em.SentAt); err == nil {
			h.SetDate(t)
		} else {
			h.SetDate(time.Now().UTC())
		}
	} else {
		h.SetDate(time.Now().UTC())
	}
	if len(em.From) > 0 {
		var addrs []*gomail.Address
		for _, a := range em.From {
			addrs = append(addrs, &gomail.Address{Name: a.Name, Address: a.Email})
		}
		h.SetAddressList("From", addrs)
	}
	if len(em.To) > 0 {
		var addrs []*gomail.Address
		for _, a := range em.To {
			addrs = append(addrs, &gomail.Address{Name: a.Name, Address: a.Email})
		}
		h.SetAddressList("To", addrs)
	}
	if len(em.CC) > 0 {
		var addrs []*gomail.Address
		for _, a := range em.CC {
			addrs = append(addrs, &gomail.Address{Name: a.Name, Address: a.Email})
		}
		h.SetAddressList("Cc", addrs)
	}
	if len(em.BCC) > 0 {
		var addrs []*gomail.Address
		for _, a := range em.BCC {
			addrs = append(addrs, &gomail.Address{Name: a.Name, Address: a.Email})
		}
		h.SetAddressList("Bcc", addrs)
	}
	if len(em.ReplyTo) > 0 {
		var addrs []*gomail.Address
		for _, a := range em.ReplyTo {
			addrs = append(addrs, &gomail.Address{Name: a.Name, Address: a.Email})
		}
		h.SetAddressList("Reply-To", addrs)
	}
	if len(em.MessageID) > 0 && em.MessageID[0] != "" {
		h.SetMessageID(em.MessageID[0])
	}
	if len(em.InReplyTo) > 0 && em.InReplyTo[0] != "" {
		h.Set("In-Reply-To", "<"+strings.Trim(em.InReplyTo[0], "<>")+">")
	}
	if len(em.References) > 0 {
		var refs []string
		for _, r := range em.References {
			if r != "" {
				refs = append(refs, "<"+strings.Trim(r, "<>")+">")
			}
		}
		if len(refs) > 0 {
			h.Set("References", strings.Join(refs, " "))
		}
	}

	mw, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil
	}

	bodyText := ""
	for _, v := range em.BodyValues {
		if v.Value != "" {
			bodyText = v.Value
			break
		}
	}
	var inlineH gomail.InlineHeader
	inlineH.Set("Content-Type", "text/plain; charset=utf-8")
	pw, err := mw.CreateSingleInline(inlineH)
	if err == nil {
		_, _ = io.WriteString(pw, bodyText)
		_ = pw.Close()
	}
	_ = mw.Close()
	return buf.Bytes()
}

// decodeHeader trims a header value; RFC 2047 encoded-word decoding is applied by net/mail
// for address fields, and plain subjects pass through unchanged.
func decodeHeader(v string) string {
	dec := new(mime.WordDecoder)
	if decoded, err := dec.DecodeHeader(v); err == nil {
		return decoded
	}
	return strings.TrimSpace(v)
}

// parseAddressList parses a header address list, returning nil when absent or unparseable.
func parseAddressList(v string) []EmailAddress {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(v)
	if err != nil {
		return nil
	}
	out := make([]EmailAddress, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, EmailAddress{Name: a.Name, Email: a.Address})
	}
	return out
}

// preview returns a single-line snippet of the body, truncated to max runes.
func preview(body string, max int) string {
	s := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", " "), "\n", " "))
	if len(s) > max {
		return s[:max]
	}
	return s
}
