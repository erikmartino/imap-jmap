package jmap

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/textproto"
	"strings"

	"github.com/emersion/go-message"
)

// ParseMDNFromBytes decodes raw RFC 5322 MIME bytes into an MDN object per RFC 8098 and RFC 9007 Section 2.
func ParseMDNFromBytes(raw []byte) (*MDN, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty MDN payload")
	}

	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		// Try parsing as raw disposition-notification header block if entity parsing failed
		return parseRawDispositionBlock(raw, "")
	}

	subject := entity.Header.Get("Subject")
	textBody, dispBytes, err := extractMDNComponents(entity)
	if err != nil {
		return nil, err
	}

	if len(dispBytes) == 0 {
		// If the entity itself has Content-Type: message/disposition-notification
		ct := entity.Header.Get("Content-Type")
		if strings.Contains(strings.ToLower(ct), "disposition-notification") {
			body, _ := io.ReadAll(entity.Body)
			return parseRawDispositionBlock(body, subject)
		}
		// If raw contains Disposition header directly
		if bytes.Contains(raw, []byte("Disposition:")) {
			return parseRawDispositionBlock(raw, subject)
		}
		return nil, fmt.Errorf("not an MDN: message/disposition-notification part not found")
	}

	mdn, err := parseRawDispositionBlock(dispBytes, subject)
	if err != nil {
		return nil, err
	}
	if textBody != "" && mdn.TextBody == "" {
		mdn.TextBody = strings.TrimSpace(textBody)
	}
	return mdn, nil
}

// extractMDNComponents recursively walks MIME entities to extract textBody and the disposition bytes.
func extractMDNComponents(entity *message.Entity) (textBody string, dispBytes []byte, err error) {
	var walk func(e *message.Entity) error
	walk = func(e *message.Entity) error {
		ct := strings.ToLower(strings.TrimSpace(e.Header.Get("Content-Type")))

		if mr := e.MultipartReader(); mr != nil {
			for {
				part, partErr := mr.NextPart()
				if partErr == io.EOF {
					break
				}
				if partErr != nil {
					return partErr
				}
				if err := walk(part); err != nil {
					return err
				}
			}
			return nil
		}

		if strings.Contains(ct, "message/disposition-notification") || strings.Contains(ct, "disposition-notification") {
			b, readErr := io.ReadAll(e.Body)
			if readErr == nil && len(b) > 0 {
				dispBytes = b
			}
			return nil
		}

		if (strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ct, "text/html")) && textBody == "" {
			b, readErr := io.ReadAll(e.Body)
			if readErr == nil {
				textBody = string(b)
			}
			return nil
		}

		return nil
	}

	err = walk(entity)
	return textBody, dispBytes, err
}

// parseRawDispositionBlock parses the RFC 8098 Section 3.2 machine-readable disposition-notification fields.
func parseRawDispositionBlock(data []byte, subject string) (*MDN, error) {
	tp := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	hdr, err := tp.ReadMIMEHeader()
	if err != nil && len(hdr) == 0 {
		return nil, fmt.Errorf("failed to parse disposition notification headers: %w", err)
	}

	dispStr := hdr.Get("Disposition")
	if dispStr == "" {
		return nil, fmt.Errorf("missing Disposition header field")
	}

	disp, err := parseMDNDisposition(dispStr)
	if err != nil {
		return nil, err
	}

	mdn := &MDN{
		Subject:           subject,
		ReportingUA:       strings.TrimSpace(hdr.Get("Reporting-UA")),
		MDNGateway:        strings.TrimSpace(hdr.Get("MDN-Gateway")),
		OriginalRecipient: strings.TrimSpace(hdr.Get("Original-Recipient")),
		FinalRecipient:    strings.TrimSpace(hdr.Get("Final-Recipient")),
		OriginalMessageID: strings.TrimSpace(hdr.Get("Original-Message-ID")),
		Disposition:       *disp,
	}
	mdn.Recipient = mdn.FinalRecipient

	if errVals := hdr["Error"]; len(errVals) > 0 {
		mdn.Error = errVals
	}

	known := map[string]bool{
		"reporting-ua":        true,
		"mdn-gateway":         true,
		"original-recipient":  true,
		"final-recipient":     true,
		"original-message-id": true,
		"disposition":         true,
		"error":               true,
		"failure":             true,
		"warning":             true,
	}

	for k, v := range hdr {
		lowerK := strings.ToLower(k)
		if !known[lowerK] {
			if mdn.ExtensionFields == nil {
				mdn.ExtensionFields = make(map[string]string)
			}
			mdn.ExtensionFields[k] = strings.Join(v, ", ")
		}
	}

	return mdn, nil
}

// parseMDNDisposition parses and normalizes the Disposition header per RFC 8098 §3.2.6 and RFC 9007 §2.
// Syntax: disposition-mode ';' disposition-type [ ';' disposition-modifier-list ]
// disposition-mode = action-mode '/' sending-mode
func parseMDNDisposition(dispStr string) (*MDNDisposition, error) {
	parts := strings.Split(dispStr, ";")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty Disposition field")
	}

	modePart := strings.TrimSpace(parts[0])
	typePart := ""
	if len(parts) > 1 {
		typePart = strings.TrimSpace(parts[1])
	}

	modeParts := strings.Split(modePart, "/")
	actionMode := strings.ToLower(strings.TrimSpace(modeParts[0]))
	sendingMode := ""
	if len(modeParts) > 1 {
		sendingMode = strings.ToLower(strings.TrimSpace(modeParts[1]))
	}

	typeParts := strings.Split(typePart, "/")
	dispType := strings.ToLower(strings.TrimSpace(typeParts[0]))

	// Normalize and validate per RFC 9007 Section 2
	if actionMode != "manual-action" && actionMode != "automatic-action" {
		actionMode = "automatic-action"
	}

	if sendingMode != "mdn-sent-manually" && sendingMode != "mdn-sent-automatically" {
		if actionMode == "manual-action" {
			sendingMode = "mdn-sent-manually"
		} else {
			sendingMode = "mdn-sent-automatically"
		}
	}

	validTypes := map[string]bool{
		"deleted":    true,
		"dispatched": true,
		"displayed":  true,
		"processed":   true,
	}
	if !validTypes[dispType] {
		if dispType == "" {
			return nil, fmt.Errorf("missing disposition type")
		}
		dispType = "displayed"
	}

	return &MDNDisposition{
		ActionMode:  actionMode,
		SendingMode: sendingMode,
		Type:        dispType,
	}, nil
}
