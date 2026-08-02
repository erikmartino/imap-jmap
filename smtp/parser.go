package smtp

import (
	"bytes"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"

	"imap-jmap/jmap"
)

// ParseMessageToEmail converts raw RFC 5322 MIME message bytes and a blob ID into a JMAP Email object (RFC 8621).
func ParseMessageToEmail(raw []byte, blobID jmap.Id) (*jmap.Email, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	email := &jmap.Email{
		BlobID:     blobID,
		MailboxIDs: map[jmap.Id]bool{"mb-inbox": true},
		Keywords:   map[string]bool{"$unread": true},
		Size:       uint64(len(raw)),
		ReceivedAt: now,
	}

	mr, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		// Fallback for simple/malformed messages using standard net/mail
		return parseFallback(raw, email)
	}

	header := mr.Header
	if fromAddrs, err := header.AddressList("From"); err == nil {
		email.From = convertAddresses(fromAddrs)
	}
	if toAddrs, err := header.AddressList("To"); err == nil {
		email.To = convertAddresses(toAddrs)
	}
	if ccAddrs, err := header.AddressList("Cc"); err == nil {
		email.CC = convertAddresses(ccAddrs)
	}
	if bccAddrs, err := header.AddressList("Bcc"); err == nil {
		email.BCC = convertAddresses(bccAddrs)
	}
	if replyToAddrs, err := header.AddressList("Reply-To"); err == nil {
		email.ReplyTo = convertAddresses(replyToAddrs)
	}

	if subj, err := header.Subject(); err == nil {
		email.Subject = subj
	}
	if msgID, err := header.MessageID(); err == nil && msgID != "" {
		email.MessageID = []string{msgID}
	}
	if inReplyTo := header.Get("In-Reply-To"); inReplyTo != "" {
		email.InReplyTo = splitHeaderIDs(inReplyTo)
	}
	if refs := header.Get("References"); refs != "" {
		email.References = splitHeaderIDs(refs)
	}
	if date, err := header.Date(); err == nil {
		email.SentAt = date.UTC().Format(time.RFC3339)
	}

	email.BodyValues = make(map[string]jmap.EmailBodyValue)
	partCounter := 0

	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}

		partCounter++
		partID := string(rune('0' + partCounter))

		dispHeader := p.Header.Get("Content-Disposition")
		disp, dispParams, _ := mime.ParseMediaType(dispHeader)

		typeHeader := p.Header.Get("Content-Type")
		mediaType, mediaParams, _ := mime.ParseMediaType(typeHeader)

		isAttachment := strings.EqualFold(disp, "attachment")

		bodyBytes, readErr := io.ReadAll(p.Body)
		if readErr != nil {
			continue
		}

		typeParts := strings.SplitN(mediaType, "/", 2)
		mainType := typeParts[0]
		subType := ""
		if len(typeParts) > 1 {
			subType = typeParts[1]
		}

		part := jmap.EmailBodyPart{
			PartID:      partID,
			Size:        uint64(len(bodyBytes)),
			Type:        mediaType,
			Subtype:     subType,
			Name:        mediaParams["name"],
			Disposition: disp,
		}
		if part.Name == "" && dispParams["filename"] != "" {
			part.Name = dispParams["filename"]
		}

		if isAttachment {
			email.Attachments = append(email.Attachments, part)
			email.HasAttachment = true
		} else if strings.EqualFold(mainType, "text") {
			email.BodyValues[partID] = jmap.EmailBodyValue{
				Value: string(bodyBytes),
			}
			if strings.EqualFold(subType, "plain") {
				email.TextBody = append(email.TextBody, part)
				if email.Preview == "" {
					email.Preview = makePreview(string(bodyBytes))
				}
			} else if strings.EqualFold(subType, "html") {
				email.HTMLBody = append(email.HTMLBody, part)
				if email.Preview == "" {
					email.Preview = makePreview(stripHTML(string(bodyBytes)))
				}
			} else {
				email.TextBody = append(email.TextBody, part)
			}
		} else {
			if disp != "" {
				email.Attachments = append(email.Attachments, part)
				email.HasAttachment = true
			}
		}
	}

	if email.BodyStructure.Type == "" {
		email.BodyStructure = jmap.EmailBodyPart{
			PartID: "1",
			Type:   "multipart/mixed",
		}
	}

	return email, nil
}

func parseFallback(raw []byte, email *jmap.Email) (*jmap.Email, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		email.Subject = "(No Subject)"
		email.BodyValues = map[string]jmap.EmailBodyValue{
			"1": {Value: string(raw)},
		}
		email.TextBody = []jmap.EmailBodyPart{{PartID: "1", Type: "text/plain", Size: uint64(len(raw))}}
		return email, nil
	}

	email.Subject = msg.Header.Get("Subject")
	if msgID := msg.Header.Get("Message-ID"); msgID != "" {
		email.MessageID = []string{msgID}
	}
	if fromStr := msg.Header.Get("From"); fromStr != "" {
		if addrs, err := mail.ParseAddressList(fromStr); err == nil {
			email.From = convertStdAddresses(addrs)
		}
	}
	if toStr := msg.Header.Get("To"); toStr != "" {
		if addrs, err := mail.ParseAddressList(toStr); err == nil {
			email.To = convertStdAddresses(addrs)
		}
	}

	bodyBytes, _ := io.ReadAll(msg.Body)
	email.BodyValues = map[string]jmap.EmailBodyValue{
		"1": {Value: string(bodyBytes)},
	}
	email.TextBody = []jmap.EmailBodyPart{{PartID: "1", Type: "text/plain", Size: uint64(len(bodyBytes))}}
	email.Preview = makePreview(string(bodyBytes))
	return email, nil
}

func convertAddresses(addrs []*gomail.Address) []jmap.EmailAddress {
	res := make([]jmap.EmailAddress, 0, len(addrs))
	for _, a := range addrs {
		if a != nil {
			res = append(res, jmap.EmailAddress{
				Name:  a.Name,
				Email: a.Address,
			})
		}
	}
	return res
}

func convertStdAddresses(addrs []*mail.Address) []jmap.EmailAddress {
	res := make([]jmap.EmailAddress, 0, len(addrs))
	for _, a := range addrs {
		if a != nil {
			res = append(res, jmap.EmailAddress{
				Name:  a.Name,
				Email: a.Address,
			})
		}
	}
	return res
}

func splitHeaderIDs(raw string) []string {
	parts := strings.Fields(raw)
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		cleaned := strings.Trim(p, "<>")
		if cleaned != "" {
			res = append(res, cleaned)
		}
	}
	return res
}

func makePreview(body string) string {
	cleaned := strings.ReplaceAll(body, "\r\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if len(cleaned) > 256 {
		return cleaned[:256]
	}
	return cleaned
}

func stripHTML(html string) string {
	var buf strings.Builder
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}
