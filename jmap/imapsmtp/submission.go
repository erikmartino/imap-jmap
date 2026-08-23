package imapsmtp

import (
	"context"
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2"
	"imap-jmap/jmap"
)

// CreateSubmission sends an outbound email via SMTP and stores a sent copy in IMAP Sent folder.
func (b *IMAPSMTPBackend) CreateSubmission(ctx context.Context, sub *jmap.EmailSubmission) (*jmap.EmailSubmission, error) {
	emails, _, err := b.GetEmails(ctx, []jmap.Id{sub.EmailID})
	if err != nil || len(emails) == 0 {
		return nil, fmt.Errorf("referenced email not found: %s", sub.EmailID)
	}
	em := emails[0]

	rawBytes := jmap.FormatEmailRFC822(em)

	var from string
	if sub.Envelope != nil && sub.Envelope.MailFrom.Email != "" {
		from = sub.Envelope.MailFrom.Email
	} else if len(em.From) > 0 {
		from = em.From[0].Email
	}

	var recipients []string
	if sub.Envelope != nil && len(sub.Envelope.RcptTo) > 0 {
		for _, rcpt := range sub.Envelope.RcptTo {
			recipients = append(recipients, rcpt.Email)
		}
	} else {
		for _, addr := range em.To {
			recipients = append(recipients, addr.Email)
		}
		for _, addr := range em.CC {
			recipients = append(recipients, addr.Email)
		}
		for _, addr := range em.BCC {
			recipients = append(recipients, addr.Email)
		}
	}

	// Dispatch over SMTP
	if err := b.pool.SendMail(ctx, from, recipients, rawBytes); err != nil {
		return nil, fmt.Errorf("failed to send outbound email via SMTP: %w", err)
	}

	// Append to IMAP Sent folder if available
	client, err := b.pool.GetClientForContext(ctx)
	if err == nil {
		defer client.Close()
		appendCmd := client.Append("Sent", int64(len(rawBytes)), &imap.AppendOptions{
			Flags: []imap.Flag{imap.FlagSeen},
			Time:  time.Now(),
		})
		_, _ = appendCmd.Write(rawBytes)
		_ = appendCmd.Close()
		_, _ = appendCmd.Wait()
	}

	if sub.ID == "" {
		sub.ID = jmap.Id(fmt.Sprintf("sub-%d", time.Now().UnixNano()))
	}
	sub.SendAt = time.Now().UTC().Format(time.RFC3339)

	deliv := make(map[string]jmap.DeliveryStatus)
	for _, rcpt := range recipients {
		deliv[rcpt] = jmap.DeliveryStatus{
			Delivered: "yes",
			SmtpReply: "250 2.0.0 OK message queued",
		}
	}
	sub.DeliveryStatus = deliv

	return sub, nil
}

func (b *IMAPSMTPBackend) SubmissionState(ctx context.Context) string {
	return "1"
}

func (b *IMAPSMTPBackend) SubmissionChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return nil, nil, nil, "1", false
}

func (b *IMAPSMTPBackend) UpdateSubmission(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.EmailSubmission, error) {
	return nil, nil
}

func (b *IMAPSMTPBackend) DeleteSubmission(ctx context.Context, id jmap.Id) (bool, error) {
	return true, nil
}

func (b *IMAPSMTPBackend) GetSubmissions(ctx context.Context, ids []jmap.Id) ([]*jmap.EmailSubmission, []jmap.Id, error) {
	return nil, ids, nil
}

func (b *IMAPSMTPBackend) GetAllSubmissions(ctx context.Context) ([]*jmap.EmailSubmission, error) {
	return []*jmap.EmailSubmission{}, nil
}

func (b *IMAPSMTPBackend) QuerySubmissions(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	return nil, 0, nil
}
