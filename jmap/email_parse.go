package jmap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
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

// parseRFC822 parses a raw RFC 5322 MIME message into a JMAP Email object (RFC 8621 Section 4.1).
func parseRFC822(raw []byte, blobBackend ...BlobBackend) (*Email, error) {
	return parseRFC822WithAccount("", raw, blobBackend...)
}

// parseRFC822WithAccount parses an RFC 5322 MIME message into a JMAP Email object with optional blob storage.
func parseRFC822WithAccount(accountID string, raw []byte, blobBackend ...BlobBackend) (*Email, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		return parseRFC822Simple(raw)
	}

	headers := parseRawHeaders(raw)
	now := time.Now().UTC().Format(time.RFC3339)

	var bb BlobBackend
	if len(blobBackend) > 0 {
		bb = blobBackend[0]
	}

	em := &Email{
		Size:        uint64(len(raw)),
		Headers:     headers,
		Keywords:    map[string]bool{},
		ReceivedAt:  now,
		BodyValues:  make(map[string]EmailBodyValue),
		HTMLBody:    []EmailBodyPart{},
		TextBody:    []EmailBodyPart{},
		Attachments: []EmailBodyPart{},
	}

	hdr := entity.Header
	if fromAddrs, err := mail.ParseAddressList(hdr.Get("From")); err == nil {
		em.From = convertMailAddresses(fromAddrs)
	}
	if senderAddrs, err := mail.ParseAddressList(hdr.Get("Sender")); err == nil {
		em.Sender = convertMailAddresses(senderAddrs)
	}
	if toAddrs, err := mail.ParseAddressList(hdr.Get("To")); err == nil {
		em.To = convertMailAddresses(toAddrs)
	}
	if ccAddrs, err := mail.ParseAddressList(hdr.Get("Cc")); err == nil {
		em.CC = convertMailAddresses(ccAddrs)
	}
	if bccAddrs, err := mail.ParseAddressList(hdr.Get("Bcc")); err == nil {
		em.BCC = convertMailAddresses(bccAddrs)
	}
	if replyToAddrs, err := mail.ParseAddressList(hdr.Get("Reply-To")); err == nil {
		em.ReplyTo = convertMailAddresses(replyToAddrs)
	}
	em.Subject = decodeHeader(hdr.Get("Subject"))
	if msgID := strings.TrimSpace(hdr.Get("Message-ID")); msgID != "" {
		em.MessageID = []string{strings.Trim(msgID, "<>")}
	}
	if inReplyTo := strings.TrimSpace(hdr.Get("In-Reply-To")); inReplyTo != "" {
		em.InReplyTo = parseReferencesList(inReplyTo)
	}
	if refs := strings.TrimSpace(hdr.Get("References")); refs != "" {
		em.References = parseReferencesList(refs)
	}
	if dateStr := hdr.Get("Date"); dateStr != "" {
		if d, err := mail.ParseDate(dateStr); err == nil {
			s := d.UTC().Format("2006-01-02T15:04:05Z")
			em.SentAt = &s
		}
	}

	partCounter := 0
	bodyStructure, err := parseMIMEEntity(context.Background(), accountID, entity, &partCounter, em.BodyValues, bb)
	if err != nil {
		return nil, err
	}
	if len(bodyStructure.SubParts) == 0 && !strings.HasPrefix(bodyStructure.Type, "multipart/") {
		bodyStructure.Headers = headers
	}
	em.BodyStructure = bodyStructure

	textBody, htmlBody, attachments := extractBodyStructureParts(&em.BodyStructure)
	em.TextBody = textBody
	em.HTMLBody = htmlBody
	em.Attachments = attachments
	em.HasAttachment = len(attachments) > 0

	if len(em.HTMLBody) == 0 && len(em.TextBody) > 0 {
		em.HTMLBody = em.TextBody
	} else if len(em.TextBody) == 0 && len(em.HTMLBody) > 0 {
		em.TextBody = em.HTMLBody
	}

	if len(em.TextBody) > 0 && em.TextBody[0].PartID != nil {
		if bv, ok := em.BodyValues[*em.TextBody[0].PartID]; ok {
			em.Preview = preview(bv.Value, 256)
		}
	} else if len(em.HTMLBody) > 0 && em.HTMLBody[0].PartID != nil {
		if bv, ok := em.BodyValues[*em.HTMLBody[0].PartID]; ok {
			em.Preview = preview(stripHTMLTags(bv.Value), 256)
		}
	}

	return em, nil
}

func parseMIMEEntity(ctx context.Context, accountID string, e *message.Entity, partCounter *int, bodyValues map[string]EmailBodyValue, bb BlobBackend) (EmailBodyPart, error) {
	mediaType := "text/plain"
	var mediaParams map[string]string
	ctHeader := e.Header.Get("Content-Type")
	if ctHeader != "" {
		mt, mp, err := mime.ParseMediaType(ctHeader)
		if err == nil {
			mediaType = strings.ToLower(mt)
			mediaParams = mp
		}
	}

	var dispPtr *string
	var dispParams map[string]string
	cdHeader := e.Header.Get("Content-Disposition")
	if cdHeader != "" {
		d, dp, err := mime.ParseMediaType(cdHeader)
		if err == nil {
			dispLower := strings.ToLower(d)
			dispPtr = &dispLower
			dispParams = dp
		}
	}

	var namePtr *string
	if dispParams != nil && dispParams["filename"] != "" {
		fn := dispParams["filename"]
		namePtr = &fn
	} else if mediaParams != nil && mediaParams["name"] != "" {
		n := mediaParams["name"]
		namePtr = &n
	}

	var cidPtr *string
	if cid := e.Header.Get("Content-ID"); cid != "" {
		c := strings.Trim(strings.TrimSpace(cid), "<>")
		if c != "" {
			cidPtr = &c
		}
	}

	var locPtr *string
	if loc := e.Header.Get("Content-Location"); loc != "" {
		l := strings.TrimSpace(loc)
		if l != "" {
			locPtr = &l
		}
	}

	var langList []string
	if lang := e.Header.Get("Content-Language"); lang != "" {
		for _, part := range strings.FieldsFunc(lang, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			part = strings.TrimSpace(part)
			if part != "" {
				langList = append(langList, part)
			}
		}
	}

	var charsetPtr *string
	if strings.HasPrefix(mediaType, "text/") {
		if mediaParams != nil && mediaParams["charset"] != "" {
			cs := strings.ToLower(mediaParams["charset"])
			charsetPtr = &cs
		} else {
			defCS := "us-ascii"
			charsetPtr = &defCS
		}
	}

	var partHeaders []EmailHeader
	fields := e.Header.Fields()
	for fields.Next() {
		partHeaders = append(partHeaders, EmailHeader{
			Name:  fields.Key(),
			Value: fields.Value(),
		})
	}

	mr := e.MultipartReader()
	if mr != nil {
		var subParts []EmailBodyPart
		for {
			subEntity, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			subPart, err := parseMIMEEntity(ctx, accountID, subEntity, partCounter, bodyValues, bb)
			if err != nil {
				continue
			}
			subParts = append(subParts, subPart)
		}

		return EmailBodyPart{
			Type:        mediaType,
			SubParts:    subParts,
			Disposition: dispPtr,
			Name:        namePtr,
			CID:         cidPtr,
			Location:    locPtr,
			Language:    langList,
			Headers:     partHeaders,
		}, nil
	}

	*partCounter++
	pID := fmt.Sprintf("%d", *partCounter)

	bodyBytes, _ := io.ReadAll(e.Body)
	size := uint64(len(bodyBytes))

	sum := sha256.Sum256(bodyBytes)
	blobIDStr := hex.EncodeToString(sum[:])
	blobID := Id(blobIDStr)

	if bb != nil && accountID != "" {
		_, _ = bb.PutBlob(ctx, accountID, mediaType, bodyBytes)
	}

	bodyValues[pID] = EmailBodyValue{
		Value: normalizeToLF(string(bodyBytes)),
	}

	return EmailBodyPart{
		PartID:      &pID,
		BlobID:      &blobID,
		Size:        size,
		Type:        mediaType,
		Charset:     charsetPtr,
		Disposition: dispPtr,
		Name:        namePtr,
		CID:         cidPtr,
		Location:    locPtr,
		Language:    langList,
		Headers:     partHeaders,
	}, nil
}

func extractBodyStructureParts(root *EmailBodyPart) (textBody []EmailBodyPart, htmlBody []EmailBodyPart, attachments []EmailBodyPart) {
	if root == nil {
		return
	}

	textBody = extractTextBody(root, false)
	htmlBody = extractHTMLBody(root, false)

	if len(textBody) == 0 && len(htmlBody) > 0 {
		textBody = htmlBody
	} else if len(htmlBody) == 0 && len(textBody) > 0 {
		htmlBody = textBody
	}

	inTextMap := make(map[string]bool)
	for _, p := range textBody {
		if p.PartID != nil {
			inTextMap[*p.PartID] = true
		}
	}
	inHTMLMap := make(map[string]bool)
	for _, p := range htmlBody {
		if p.PartID != nil {
			inHTMLMap[*p.PartID] = true
		}
	}

	var collectAttachments func(p *EmailBodyPart)
	collectAttachments = func(p *EmailBodyPart) {
		if p == nil {
			return
		}
		if strings.HasPrefix(p.Type, "multipart/") {
			for i := range p.SubParts {
				collectAttachments(&p.SubParts[i])
			}
			return
		}

		pID := ""
		if p.PartID != nil {
			pID = *p.PartID
		}
		inText := inTextMap[pID]
		inHTML := inHTMLMap[pID]

		isAtt := false
		if p.Disposition != nil && *p.Disposition == "attachment" {
			isAtt = true
		} else if p.Type == "text/plain" || p.Type == "text/html" {
			if !inText && !inHTML {
				isAtt = true
			}
		} else {
			if !(inText && inHTML) {
				isAtt = true
			}
		}

		if isAtt {
			attachments = append(attachments, *p)
		}
	}

	collectAttachments(root)
	return
}

func extractTextBody(p *EmailBodyPart, isFallback bool) []EmailBodyPart {
	if p == nil {
		return nil
	}

	if p.Type == "multipart/alternative" {
		textChild := findAlternativeChild(p.SubParts, "text/plain")
		if textChild != nil {
			return extractTextBody(textChild, false)
		}
		htmlChild := findAlternativeChild(p.SubParts, "text/html")
		if htmlChild != nil {
			return extractTextBody(htmlChild, true)
		}
		return nil
	}

	if p.Type == "multipart/related" {
		if len(p.SubParts) > 0 {
			return extractTextBody(&p.SubParts[0], isFallback)
		}
		return nil
	}

	if strings.HasPrefix(p.Type, "multipart/") {
		var out []EmailBodyPart
		for i := range p.SubParts {
			out = append(out, extractTextBody(&p.SubParts[i], isFallback)...)
		}
		return out
	}

	if p.Disposition != nil && *p.Disposition == "attachment" {
		return nil
	}

	if p.Type == "text/plain" {
		return []EmailBodyPart{*p}
	}
	if p.Type == "text/html" && isFallback {
		return []EmailBodyPart{*p}
	}
	if strings.HasPrefix(p.Type, "image/") || strings.HasPrefix(p.Type, "audio/") || strings.HasPrefix(p.Type, "video/") {
		return []EmailBodyPart{*p}
	}

	return nil
}

func extractHTMLBody(p *EmailBodyPart, isFallback bool) []EmailBodyPart {
	if p == nil {
		return nil
	}

	if p.Type == "multipart/alternative" {
		htmlChild := findAlternativeChild(p.SubParts, "text/html")
		if htmlChild != nil {
			return extractHTMLBody(htmlChild, false)
		}
		textChild := findAlternativeChild(p.SubParts, "text/plain")
		if textChild != nil {
			return extractHTMLBody(textChild, true)
		}
		return nil
	}

	if p.Type == "multipart/related" {
		if len(p.SubParts) > 0 {
			return extractHTMLBody(&p.SubParts[0], isFallback)
		}
		return nil
	}

	if strings.HasPrefix(p.Type, "multipart/") {
		var out []EmailBodyPart
		for i := range p.SubParts {
			out = append(out, extractHTMLBody(&p.SubParts[i], isFallback)...)
		}
		return out
	}

	if p.Disposition != nil && *p.Disposition == "attachment" {
		return nil
	}

	if p.Type == "text/html" {
		return []EmailBodyPart{*p}
	}
	if p.Type == "text/plain" {
		return []EmailBodyPart{*p}
	}
	if strings.HasPrefix(p.Type, "image/") || strings.HasPrefix(p.Type, "audio/") || strings.HasPrefix(p.Type, "video/") {
		return []EmailBodyPart{*p}
	}

	return nil
}

func findAlternativeChild(subParts []EmailBodyPart, targetType string) *EmailBodyPart {
	for i := range subParts {
		p := &subParts[i]
		if p.Type == targetType {
			return p
		}
		if strings.HasPrefix(p.Type, "multipart/") {
			if hasPartType(p, targetType) {
				return p
			}
		}
	}
	return nil
}

func hasPartType(p *EmailBodyPart, targetType string) bool {
	if p.Type == targetType {
		return true
	}
	for i := range p.SubParts {
		if hasPartType(&p.SubParts[i], targetType) {
			return true
		}
	}
	return false
}

func convertMailAddresses(addrs []*mail.Address) []EmailAddress {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]EmailAddress, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, EmailAddress{Name: a.Name, Email: a.Address})
	}
	return out
}

func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
		} else if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseRFC822Simple(raw []byte) (*Email, error) {
	headers := parseRawHeaders(raw)
	now := time.Now().UTC().Format(time.RFC3339)

	var subject string
	var from, sender, to, cc, bcc, replyTo []EmailAddress
	var msgIDs, inReplyTo, references []string
	var sentAt *string
	var bodyStr string

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err == nil {
		hdr := msg.Header
		subject = decodeHeader(hdr.Get("Subject"))
		from = parseAddressList(hdr.Get("From"))
		sender = parseAddressList(hdr.Get("Sender"))
		to = parseAddressList(hdr.Get("To"))
		cc = parseAddressList(hdr.Get("Cc"))
		bcc = parseAddressList(hdr.Get("Bcc"))
		replyTo = parseAddressList(hdr.Get("Reply-To"))
		if msgID := strings.TrimSpace(hdr.Get("Message-ID")); msgID != "" {
			msgIDs = []string{strings.Trim(msgID, "<>")}
		}
		if irt := strings.TrimSpace(hdr.Get("In-Reply-To")); irt != "" {
			inReplyTo = parseReferencesList(irt)
		}
		if refs := strings.TrimSpace(hdr.Get("References")); refs != "" {
			references = parseReferencesList(refs)
		}
		if date, err := hdr.Date(); err == nil {
			s := date.UTC().Format("2006-01-02T15:04:05Z")
			sentAt = &s
		}
		body, _ := io.ReadAll(msg.Body)
		bodyStr = string(body)
	} else {
		bodyStr = string(raw)
	}

	partID := "1"
	cs := "us-ascii"
	contentType := "text/plain"

	em := &Email{
		Size:        uint64(len(raw)),
		Subject:     subject,
		From:        from,
		Sender:      sender,
		To:          to,
		CC:          cc,
		BCC:         bcc,
		ReplyTo:     replyTo,
		Headers:     headers,
		MessageID:   msgIDs,
		InReplyTo:   inReplyTo,
		References:  references,
		SentAt:      sentAt,
		Keywords:    map[string]bool{},
		HTMLBody:    []EmailBodyPart{},
		Attachments: []EmailBodyPart{},
		ReceivedAt:  now,
		BodyValues:  map[string]EmailBodyValue{"1": {Value: bodyStr}},
		Preview:     preview(bodyStr, 256),
	}

	em.BodyStructure = EmailBodyPart{
		PartID:  &partID,
		Type:    contentType,
		Charset: &cs,
		Size:    uint64(len(bodyStr)),
		Headers: headers,
	}
	em.TextBody = []EmailBodyPart{{
		PartID:  &partID,
		Type:    "text/plain",
		Charset: &cs,
		Size:    uint64(len(bodyStr)),
		Headers: headers,
	}}
	em.HTMLBody = em.TextBody

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

// normalizeToLF standardizes line endings to LF per RFC 8621 Section 4.1.4.
func normalizeToLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}
