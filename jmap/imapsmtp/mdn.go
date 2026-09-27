package imapsmtp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/textproto"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

// MDN (RFC 9007 Section 2)

func (b *IMAPSMTPBackend) SendMDN(ctx context.Context, mdn *jmapmail.MDN) (*jmapmail.MDN, error) {
	if mdn.ForEmailID == "" {
		return nil, fmt.Errorf("email ID is required")
	}
	emails, notFound, err := b.GetEmails(ctx, []jmapcore.Id{mdn.ForEmailID})
	if err != nil || len(notFound) > 0 || len(emails) == 0 {
		return nil, fmt.Errorf("email %s not found", mdn.ForEmailID)
	}
	targetEmail := emails[0]
	if mdn.ID == "" {
		mdn.ID = jmapcore.Id(fmt.Sprintf("mdn-%d", time.Now().UnixNano()))
	}
	if mdn.Subject == "" {
		mdn.Subject = fmt.Sprintf("Disposition Notification: %s", targetEmail.Subject)
	}
	if mdn.ReportingUA == "" {
		mdn.ReportingUA = "imap-jmap-server/1.0"
	}

	// Find recipient address from Disposition-Notification-To header
	var dispTo string
	for _, h := range targetEmail.Headers {
		if strings.EqualFold(h.Name, "Disposition-Notification-To") {
			dispTo = strings.TrimSpace(h.Value)
			break
		}
	}

	fromAddr := ""
	if mdn.FinalRecipient != "" {
		fromAddr = mdn.FinalRecipient
		if strings.HasPrefix(strings.ToLower(fromAddr), "rfc822;") {
			fromAddr = strings.TrimSpace(fromAddr[7:])
		}
	}

	// Build raw RFC 8098 MDN MIME message using structured encoders
	rawBytes, buildErr := buildMDNMIMEMessage(mdn, fromAddr, dispTo)
	if buildErr == nil && len(rawBytes) > 0 {
		accountID, _ := jmapauth.AccountIDFromContext(ctx)
		// Store blob for MDN/parse access
		_, _ = b.PutBlob(ctx, accountID, "multipart/report", rawBytes)

		// Dispatch via SMTP if configured and valid recipient address is found
		if b.smtpHost != "" && dispTo != "" {
			if parsed, pErr := mail.ParseAddress(dispTo); pErr == nil && parsed.Address != "" {
				_ = b.pool.SendMail(ctx, fromAddr, []string{parsed.Address}, rawBytes)
			}
		}
	}

	return mdn, nil
}

func (b *IMAPSMTPBackend) ParseMDN(ctx context.Context, blobID jmapcore.Id) (*jmapmail.MDN, error) {
	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	blob, found, err := b.GetBlob(ctx, accountID, string(blobID))
	if err != nil || !found || blob == nil {
		return nil, jmapblob.ErrBlobNotFound
	}
	mdn, err := jmapmail.ParseMDNFromBytes(blob.Data)
	if err != nil {
		return nil, err
	}
	mdn.ID = jmapcore.Id("mdn-parsed-" + string(blobID))

	// Match Original-Message-ID to an existing email on the server (RFC 9007 §2.2)
	if mdn.OriginalMessageID != "" {
		origClean := strings.Trim(strings.TrimSpace(mdn.OriginalMessageID), "<>")
		if emails, err := b.GetAllEmails(ctx); err == nil {
			for _, em := range emails {
				for _, mid := range em.MessageID {
					if strings.Trim(strings.TrimSpace(mid), "<>") == origClean {
						mdn.ForEmailID = em.ID
						break
					}
				}
				if mdn.ForEmailID != "" {
					break
				}
			}
		}
	}
	return mdn, nil
}

func buildMDNMIMEMessage(mdn *jmapmail.MDN, from string, to string) ([]byte, error) {
	var buf bytes.Buffer
	var h message.Header
	h.Set("Subject", mdn.Subject)
	h.Set("MIME-Version", "1.0")
	if from != "" {
		h.Set("From", from)
	}
	if to != "" {
		h.Set("To", to)
	}
	h.SetContentType("multipart/report", map[string]string{
		"report-type": "disposition-notification",
	})

	mw, err := message.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}

	// Part 1: Human-readable text
	var textH message.Header
	textH.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	pw, err := mw.CreatePart(textH)
	if err != nil {
		return nil, err
	}
	bodyText := mdn.TextBody
	if bodyText == "" {
		bodyText = "The message has been displayed."
	}
	if _, err := io.WriteString(pw, bodyText); err != nil {
		return nil, err
	}
	_ = pw.Close()

	// Part 2: message/disposition-notification
	var dispH message.Header
	dispH.SetContentType("message/disposition-notification", nil)
	dw, err := mw.CreatePart(dispH)
	if err != nil {
		return nil, err
	}

	var tpHeader textproto.Header
	if mdn.ReportingUA != "" {
		tpHeader.Set("Reporting-UA", mdn.ReportingUA)
	}
	if mdn.MDNGateway != "" {
		tpHeader.Set("MDN-Gateway", mdn.MDNGateway)
	}
	if mdn.OriginalRecipient != "" {
		tpHeader.Set("Original-Recipient", mdn.OriginalRecipient)
	}
	if mdn.FinalRecipient != "" {
		tpHeader.Set("Final-Recipient", mdn.FinalRecipient)
	}
	if mdn.OriginalMessageID != "" {
		tpHeader.Set("Original-Message-ID", mdn.OriginalMessageID)
	}
	dispVal := mdn.Disposition.ActionMode + "/" + mdn.Disposition.SendingMode + "; " + mdn.Disposition.Type
	tpHeader.Set("Disposition", dispVal)
	for _, errStr := range mdn.Error {
		tpHeader.Add("Error", errStr)
	}
	for k, v := range mdn.ExtensionFields {
		tpHeader.Set(k, v)
	}

	if err := textproto.WriteHeader(dw, tpHeader); err != nil {
		return nil, err
	}
	_ = dw.Close()
	_ = mw.Close()

	return buf.Bytes(), nil
}
