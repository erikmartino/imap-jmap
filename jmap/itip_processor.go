package jmap

import (
	"context"
	"log"
	"strings"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
)

// itipMarker is an IMAP keyword imap-jmap sets on a message once it has been examined
// for iTIP content, so the same message is not rescanned on every sync. It is managed
// entirely by imap-jmap (no Sieve/ManageSieve) and hidden from clients.
const itipMarker = "$itip"

// itipMailScanner is implemented by mail backends that can locate iMIP/iTIP mail with a
// server-side IMAP SEARCH. Backends that do not implement it fall back to QueryEmails.
type itipMailScanner interface {
	ITIPEmails(ctx context.Context, marker string) ([]jmapcore.Id, error)
}

// itipTriggerMethods are the client sync methods that cause the mailbox to be scanned
// for iTIP mail. Processing is request-driven because there is no background job (and no
// credentials outside a request).
var itipTriggerMethods = map[string]bool{
	"Email/changes":                     true,
	"Email/query":                       true,
	"Email/get":                         true,
	"CalendarEvent/changes":             true,
	"CalendarEvent/query":               true,
	"CalendarEvent/get":                 true,
	"Calendar/get":                      true,
	"CalendarEventNotification/changes": true,
	"CalendarEventNotification/query":   true,
	"CalendarEventNotification/get":     true,
}

// ProcessMailboxITIP applies iMIP/iTIP mail found in the mailbox and marks each message
// processed, so replies/requests/cancels delivered by the real mail server (postfix ->
// dovecot) are reflected in the calendar. It uses the caller's credentials and is
// idempotent; a message whose body cannot be fetched is left unmarked for the next sync.
func (s *Server) ProcessMailboxITIP(ctx context.Context) {
	if s.MailBackend == nil || s.CalendarsBackend == nil || s.BlobBackend == nil {
		return
	}

	var ids []jmapcore.Id
	if scanner, ok := s.MailBackend.(itipMailScanner); ok {
		found, err := scanner.ITIPEmails(ctx, itipMarker)
		if err != nil {
			return
		}
		ids = found
	} else {
		found, _, err := s.MailBackend.QueryEmails(ctx, map[string]any{"text": "text/calendar"}, nil, 0, nil)
		if err != nil {
			return
		}
		ids = found
	}
	if len(ids) == 0 {
		return
	}

	skipMailboxIDs := make(map[jmapcore.Id]bool)
	if mbs, err := s.MailBackend.GetAllMailboxes(ctx); err == nil {
		for _, mb := range mbs {
			if mb == nil {
				continue
			}
			if mb.Role != nil {
				r := strings.ToLower(*mb.Role)
				if r == "sent" || r == "drafts" || r == "trash" {
					skipMailboxIDs[mb.ID] = true
				}
			}
			lower := strings.ToLower(mb.Name)
			if lower == "sent" || lower == "drafts" || lower == "trash" {
				skipMailboxIDs[mb.ID] = true
			}
		}
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
		if len(em.MailboxIDs) > 0 {
			allSkipped := true
			for mbID := range em.MailboxIDs {
				if !skipMailboxIDs[mbID] {
					allSkipped = false
					break
				}
			}
			if allSkipped {
				s.markITIPProcessed(ctx, em)
				continue
			}
		}
		blob, found, berr := s.BlobBackend.GetBlob(ctx, accountID, string(em.BlobID))
		if berr != nil || !found || blob == nil {
			// Transient failure: leave the message unmarked so the next sync retries.
			continue
		}
		icsBody, icsMethod := jmapcalendar.ExtractCalendarPart(blob.Data)
		if icsBody == "" {
			// Not a calendar message (or not parseable): mark it so it is not rescanned.
			s.markITIPProcessed(ctx, em)
			continue
		}
		sender := ""
		if len(em.From) > 0 {
			sender = em.From[0].Email
		}
		// Mark regardless of the outcome: re-applying is idempotent, and marking avoids
		// rescanning (and re-importing a deleted invitation) on every sync.
		_ = jmapcalendar.ApplyITIP(ctx, s.CalendarsBackend, icsBody, sender, icsMethod)
		s.markITIPProcessed(ctx, em)
	}
}

func (s *Server) markITIPProcessed(ctx context.Context, em *jmapmail.Email) {
	if em == nil {
		return
	}
	if _, err := s.MailBackend.UpdateEmail(ctx, em.ID, map[string]any{"keywords/" + itipMarker: true}); err != nil {
		log.Printf("iTIP: failed to mark %s as processed: %v", em.ID, err)
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
