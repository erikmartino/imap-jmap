package jmap

import (
	"context"
	"log"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

// itipKeyword is the IMAP keyword the upstream Sieve filter applies to incoming
// iMIP/iTIP mail. imap-jmap processes tagged messages and removes the keyword.
const itipKeyword = "$itip"

// itipMailScanner is implemented by mail backends that can find messages by keyword
// server-side. Backends that do not implement it fall back to QueryEmails.
type itipMailScanner interface {
	EmailsWithKeyword(ctx context.Context, keyword string) ([]jmapcore.Id, error)
}

// itipTriggerMethods are the client sync methods that cause the mailbox to be scanned
// for Sieve-tagged iTIP mail. Processing is request-driven because there is no
// background job (and no credentials outside a request).
var itipTriggerMethods = map[string]bool{
	"Email/changes":         true,
	"Email/query":           true,
	"CalendarEvent/changes": true,
	"CalendarEvent/query":   true,
}

// ProcessMailboxITIP applies pending iTIP messages tagged by the upstream Sieve filter
// and clears the tag, so replies/requests/cancels delivered by the real mail server
// (postfix -> dovecot) are reflected in the calendar. It uses the caller's credentials
// and is idempotent; a message that fails to apply keeps its tag for the next sync.
func (s *Server) ProcessMailboxITIP(ctx context.Context) {
	if s.MailBackend == nil || s.CalendarsBackend == nil || s.BlobBackend == nil {
		return
	}

	var ids []jmapcore.Id
	if scanner, ok := s.MailBackend.(itipMailScanner); ok {
		found, err := scanner.EmailsWithKeyword(ctx, itipKeyword)
		if err != nil {
			return
		}
		ids = found
	} else {
		found, _, err := s.MailBackend.QueryEmails(ctx, map[string]any{"hasKeyword": itipKeyword}, nil, 0, nil)
		if err != nil {
			return
		}
		ids = found
	}
	if len(ids) == 0 {
		return
	}

	accountID, _ := jmapauth.AccountIDFromContext(ctx)
	emails, _, err := s.MailBackend.GetEmails(ctx, ids)
	if err != nil {
		return
	}
	for _, em := range emails {
		if em == nil {
			continue
		}
		blob, found, berr := s.BlobBackend.GetBlob(ctx, accountID, string(em.BlobID))
		if berr != nil || !found || blob == nil {
			// Transient failure: leave the tag so the next sync retries.
			continue
		}
		icsBody := jmapcalendar.ExtractCalendarBody(blob.Data)
		if icsBody == "" {
			// Not a calendar message: clear the tag so it is not rescanned forever.
			s.clearITIPKeyword(ctx, em)
			continue
		}
		sender := ""
		if len(em.From) > 0 {
			sender = em.From[0].Email
		}
		if jmapcalendar.ApplyITIP(ctx, s.CalendarsBackend, icsBody, sender) {
			s.clearITIPKeyword(ctx, em)
		}
	}
}

func (s *Server) clearITIPKeyword(ctx context.Context, em *jmapmail.Email) {
	if em == nil {
		return
	}
	if _, err := s.MailBackend.UpdateEmail(ctx, em.ID, map[string]any{"keywords/" + itipKeyword: nil}); err != nil {
		log.Printf("iTIP: failed to clear %s on %s: %v", itipKeyword, em.ID, err)
	}
}

// itipTriggered reports whether the request contains a sync method that should cause a
// mailbox iTIP scan.
func itipTriggered(calls []Invocation) bool {
	for _, c := range calls {
		if itipTriggerMethods[c.Name] {
			return true
		}
	}
	return false
}
