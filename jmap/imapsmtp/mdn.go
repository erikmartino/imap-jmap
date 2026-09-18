package imapsmtp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

// MDN (RFC 9007 Section 3)

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

	// Match Original-Message-ID to an existing email on the server (RFC 9007 §3.2)
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
