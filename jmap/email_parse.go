package jmap

import (
	"io"
	"mime"
	"net/mail"
	"strings"
)

// parseRFC822 parses a raw RFC 5322 message into a JMAP Email object (RFC 8621 Section 4.1.1),
// extracting envelope headers and a text body so the result is indistinguishable from what a
// real server would return for Email/parse and Email/import.
func parseRFC822(raw []byte) (*Email, error) {
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}

	hdr := msg.Header
	em := &Email{
		Size:     uint64(len(raw)),
		Subject:  decodeHeader(hdr.Get("Subject")),
		From:     parseAddressList(hdr.Get("From")),
		Sender:   parseAddressList(hdr.Get("Sender")),
		To:       parseAddressList(hdr.Get("To")),
		CC:       parseAddressList(hdr.Get("Cc")),
		BCC:      parseAddressList(hdr.Get("Bcc")),
		ReplyTo:  parseAddressList(hdr.Get("Reply-To")),
		Keywords: map[string]bool{},
	}

	if msgID := strings.TrimSpace(hdr.Get("Message-ID")); msgID != "" {
		em.MessageID = []string{strings.Trim(msgID, "<>")}
	}
	if inReplyTo := strings.TrimSpace(hdr.Get("In-Reply-To")); inReplyTo != "" {
		em.InReplyTo = []string{strings.Trim(inReplyTo, "<>")}
	}
	if date, err := hdr.Date(); err == nil {
		em.SentAt = date.UTC().Format("2006-01-02T15:04:05Z")
	}

	body, _ := io.ReadAll(msg.Body)
	bodyStr := string(body)
	contentType := hdr.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}
	em.BodyStructure = EmailBodyPart{
		PartID: "1",
		Type:   contentType,
		Size:   uint64(len(body)),
	}
	em.TextBody = []EmailBodyPart{{PartID: "1", Type: "text/plain", Size: uint64(len(body))}}
	em.BodyValues = map[string]EmailBodyValue{"1": {Value: bodyStr}}
	em.Preview = preview(bodyStr, 256)

	return em, nil
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
