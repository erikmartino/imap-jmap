package smtp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-smtp"

	"imap-jmap/jmap"
)

// ReceiverBackend implements smtp.Backend for receiving emails and storing them into JMAP backends.
type ReceiverBackend struct {
	MailBackend      jmap.MailBackend
	BlobBackend      jmap.BlobBackend
	CalendarsBackend jmap.CalendarsBackend
	AccountResolver  jmap.AccountResolver
	AccountID        string
	// ServerName is the receiving host name used in the RFC 5321 Section 4.4
	// "Received:" trace header prepended to every accepted message.
	ServerName string
}

// NewReceiverBackend initializes a new SMTP ReceiverBackend linked to JMAP backends.
func NewReceiverBackend(mailBackend jmap.MailBackend, blobBackend jmap.BlobBackend, calBackend jmap.CalendarsBackend, resolver ...jmap.AccountResolver) *ReceiverBackend {
	var r jmap.AccountResolver
	if len(resolver) > 0 {
		r = resolver[0]
	}
	return &ReceiverBackend{
		MailBackend:      mailBackend,
		BlobBackend:      blobBackend,
		CalendarsBackend: calBackend,
		AccountResolver:  r,
		AccountID:        jmap.AccountIDForSubject("user@example.com"),
		ServerName:       "localhost",
	}
}

// NewSession starts a new SMTP receiving session per connection, capturing the
// client's HELO name and remote address for the Received trace header.
func (b *ReceiverBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	s := &Session{backend: b}
	if c != nil {
		s.helo = c.Hostname()
		if conn := c.Conn(); conn != nil {
			s.remoteAddr = conn.RemoteAddr().String()
		}
	}
	return s, nil
}

// Session handles individual SMTP transaction commands (MAIL FROM, RCPT TO, DATA).
type Session struct {
	backend    *ReceiverBackend
	from       string
	to         []string
	helo       string
	remoteAddr string
}

// AuthPlain handles PLAIN authentication (noop / accepts all for receiving server).
func (s *Session) AuthPlain(username, password string) error {
	return nil
}

// Mail handles MAIL FROM command per RFC 5321.
func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	s.from = from
	log.Printf("SMTP receiver: MAIL FROM <%s> from %s (helo=%q)", from, s.remoteAddr, s.helo)
	return nil
}

// Rcpt handles RCPT TO command per RFC 5321.
func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	log.Printf("SMTP receiver: RCPT TO <%s>", to)

	// A server that cannot deliver to an address MUST reject it rather than accept
	// the message and silently drop or misdeliver it. When an AccountResolver is
	// configured, recipients it does not resolve to a local account are refused with
	// 550 5.7.1 (RFC 3463: "Delivery not authorized, message refused" — the code real
	// MTAs use for relaying-denied). Without a resolver the server acts as a
	// catch-all receiver and accepts every recipient (legacy single-account mode).
	if s.backend.AccountResolver != nil {
		if _, local := s.backend.AccountResolver.ResolveAccountID(context.Background(), to); !local {
			log.Printf("SMTP receiver: rejecting RCPT TO <%s>: address is not local and no relay is configured", to)
			return &smtp.SMTPError{
				Code:         550,
				EnhancedCode: smtp.EnhancedCode{5, 7, 1},
				Message:      fmt.Sprintf("<%s>: Relaying denied. Address is not a local user of this server", to),
			}
		}
	}

	s.to = append(s.to, to)
	return nil
}

// Data handles DATA command per RFC 5321, storing raw blob and JMAP Email object per RFC 8620 & RFC 8621.
func (s *Session) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	log.Printf("SMTP receiver: DATA from %s (helo=%q, envelope from=%q, recipients=%v, size=%d bytes)",
		s.remoteAddr, s.helo, s.from, s.to, len(data))

	// Prepend an RFC 5321 Section 4.4 trace ("Received:") header. A receiving SMTP
	// server MUST insert this at the top of the message so delivery is auditable in
	// the message headers, recording where it came from, the receiving host, and when.
	data = append([]byte(s.buildReceivedHeader()), data...)

	// 1. Determine target accountIDs per recipient
	targetAccountIDs := make(map[string]bool)
	if s.backend.AccountResolver != nil {
		for _, rcpt := range s.to {
			accountID, local := s.backend.AccountResolver.ResolveAccountID(context.Background(), rcpt)
			if local && accountID != "" {
				targetAccountIDs[accountID] = true
				log.Printf("SMTP receiver: recipient %q resolved to local account %s", rcpt, accountID)
			} else {
				log.Printf("SMTP receiver: recipient %q is NOT local (no account resolved)", rcpt)
			}
		}
	} else {
		log.Printf("SMTP receiver: no AccountResolver configured; skipping per-recipient resolution")
	}
	if len(targetAccountIDs) == 0 {
		if s.backend.AccountID != "" {
			targetAccountIDs[s.backend.AccountID] = true
			log.Printf("SMTP receiver: no local recipient; delivering to fallback account %s", s.backend.AccountID)
		} else if len(s.to) > 0 {
			targetAccountIDs[jmap.AccountIDForSubject(s.to[0])] = true
			log.Printf("SMTP receiver: no local recipient; delivering to account derived from first recipient %q", s.to[0])
		} else {
			log.Printf("SMTP receiver: message has no recipients and no fallback account; dropping message")
			return nil
		}
	}

	// 2. Deliver message copy for each target accountID
	deliveredAny := false
	var firstFailure error
	for targetAccountID := range targetAccountIDs {
		rcptCtx := jmap.ContextWithAccountID(context.Background(), targetAccountID)
		log.Printf("SMTP receiver: delivering message to account %s", targetAccountID)

		var blobID jmap.Id = "blob-unknown"
		blobStored := false
		if s.backend.BlobBackend == nil {
			log.Printf("SMTP receiver: warning: no BlobBackend configured; blob not stored for account %s", targetAccountID)
			if firstFailure == nil {
				firstFailure = fmt.Errorf("no BlobBackend configured for account %s", targetAccountID)
			}
		} else {
			blob, err := s.backend.BlobBackend.PutBlob(rcptCtx, targetAccountID, "message/rfc822", data)
			if err != nil {
				log.Printf("SMTP receiver warning: failed to store blob for account %s: %v", targetAccountID, err)
				if firstFailure == nil {
					firstFailure = err
				}
			} else {
				blobID = jmap.Id(blob.ID)
				blobStored = true
			}
		}

		email, err := ParseMessageToEmail(data, blobID)
		if err != nil {
			log.Printf("SMTP receiver warning: parsing email error for account %s: %v", targetAccountID, err)
			if firstFailure == nil {
				firstFailure = err
			}
		} else if email == nil {
			log.Printf("SMTP receiver warning: parser returned no email for account %s", targetAccountID)
			if firstFailure == nil {
				firstFailure = fmt.Errorf("message could not be parsed for account %s", targetAccountID)
			}
		}

		if s.backend.MailBackend != nil && email != nil && blobStored {
			created, err := s.backend.MailBackend.CreateEmail(rcptCtx, email)
			if err != nil {
				log.Printf("SMTP receiver error: failed to create email in backend for account %s: %v", targetAccountID, err)
				if firstFailure == nil {
					firstFailure = err
				}
			} else {
				log.Printf("SMTP receiver: stored email %s (account %s, blob %s, size %d bytes)", created.ID, targetAccountID, created.BlobID, created.Size)
				deliveredAny = true
			}
		} else if email != nil && !blobStored {
			log.Printf("SMTP receiver: warning: skipping email creation for account %s because its blob could not be stored", targetAccountID)
		} else if email != nil {
			log.Printf("SMTP receiver: warning: no MailBackend configured; email not stored for account %s", targetAccountID)
			if firstFailure == nil {
				firstFailure = fmt.Errorf("no MailBackend configured for account %s", targetAccountID)
			}
		}

		// 3. Auto-process iMIP invitation responses and incoming invitations (RFC 6047 /
		//    RFC 5546). The text/calendar part is extracted with a real MIME parser (which
		//    also decodes any Content-Transfer-Encoding) rather than scanning the raw bytes.
		if s.backend.CalendarsBackend != nil {
			icsBody := extractCalendarBody(data)
			if icsBody == "" {
				continue
			}
			msg, err := jmap.ParseITIPMessage(icsBody)
			if err == nil && msg != nil && msg.UID != "" {
				if strings.EqualFold(msg.Method, "REPLY") {
					// An inbound REPLY (RFC 5546 Section 3.2.3 / RFC 6047) updates the
					// replying attendee's participationStatus on the event identified by
					// UID (RFC 5546 Section 2.1.5) — never the event-level status.
					ev := s.findEventByUID(rcptCtx, msg.UID)
					if ev != nil {
						attendeeEmail := s.from
						if len(msg.Attendees) > 0 && msg.Attendees[0].Email != "" {
							attendeeEmail = msg.Attendees[0].Email
						}
						status := strings.ToLower(msg.Status)
						if status == "" {
							status = "accepted"
						}

						patch := map[string]any{
							"participants/" + attendeeEmail + "/participationStatus": status,
						}
						if _, err := s.backend.CalendarsBackend.UpdateCalendarEvent(rcptCtx, ev.ID, patch); err == nil {
							log.Printf("SMTP receiver: applied iTIP REPLY to event %s: participant %s -> %s", ev.ID, attendeeEmail, status)
							// The change was made by an external party, so record a
							// CalendarEventNotification (draft-ietf-jmap-calendars-27 Section 7).
							replyEmail := attendeeEmail
							s.backend.CalendarsBackend.CreateCalendarEventNotification(rcptCtx, &jmap.CalendarEventNotification{
								Type:            "updated",
								CalendarEventID: ev.ID,
								ChangedBy: jmap.CalendarEventNotificationPerson{
									Email:           &replyEmail,
									CalendarAddress: &replyEmail,
								},
								Event:      ev,
								EventPatch: patch,
							})
						}
					}
				} else if strings.EqualFold(msg.Method, "REQUEST") {
					// Full-fidelity import: parse the entire VEVENT (participants,
					// duration, recurrence, location, alerts) rather than a title+start
					// stub, so the invitee's calendar copy matches the organizer's.
					imported := parseImportedEvent(icsBody, msg)
					if existing := s.findEventByUID(rcptCtx, imported.UID); existing != nil {
						// Re-REQUEST: re-sync the mutable core details onto the copy.
						patch := map[string]any{"title": imported.Title, "start": imported.Start}
						if imported.Duration != "" {
							patch["duration"] = imported.Duration
						}
						_, _ = s.backend.CalendarsBackend.UpdateCalendarEvent(rcptCtx, existing.ID, patch)
					} else {
						imported.ID = ""
						imported.CalendarIDs = map[jmap.Id]bool{"cal-default": true}
						if imported.Status == "" {
							imported.Status = "tentative"
						}
						ensureOwnerParticipant(imported, s.from)
						createdEv, err := s.backend.CalendarsBackend.CreateCalendarEvent(rcptCtx, imported)
						if err == nil && createdEv != nil {
							log.Printf("SMTP receiver: auto-imported incoming invitation into calendar event %s (%s)", createdEv.ID, createdEv.Title)
						}
					}
				}
			}
		}
	}

	// A message the server could not store for any recipient MUST NOT be acknowledged
	// with a success reply: the client would believe it was accepted ("does not show
	// up" with 250 OK). Reply with a transient system error (RFC 3463 4.3.0) so the
	// client retries later.
	if !deliveredAny {
		reason := "message could not be stored for any recipient"
		if firstFailure != nil {
			reason = firstFailure.Error()
		}
		log.Printf("SMTP receiver error: rejecting DATA for recipients %v: %s", s.to, reason)
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 3, 0},
			Message:      "temporary local delivery failure: " + reason,
		}
	}

	return nil
}

// extractCalendarBody returns the decoded body of the message's text/calendar MIME part
// (RFC 6047 Section 2.4), using a real MIME reader that also decodes any
// Content-Transfer-Encoding (base64 / quoted-printable). Only a genuine text/calendar
// part is honoured, so scheduling logic can never be driven by iCalendar-looking text
// smuggled into an unrelated part. Returns "" when the message carries no calendar part.
func extractCalendarBody(raw []byte) string {
	mr, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		mediaType, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		if strings.EqualFold(mediaType, "text/calendar") {
			body, err := io.ReadAll(p.Body)
			if err != nil {
				return ""
			}
			return string(body)
		}
	}
	return ""
}

// parseImportedEvent parses the (already MIME-extracted) text/calendar body into a full
// CalendarEvent (RFC 5545 → RFC 8984), preferring the VEVENT whose UID matches the iTIP
// message and falling back to a title+start event from the scanned iTIP fields.
func parseImportedEvent(ics string, msg *jmap.ITIPMessage) *jmap.CalendarEvent {
	if events, err := jmap.ParseICalendar([]byte(ics)); err == nil {
		for _, e := range events {
			if e != nil && e.UID == msg.UID {
				return e
			}
		}
		if len(events) > 0 && events[0] != nil {
			return events[0]
		}
	}
	title := msg.Summary
	if title == "" {
		title = "External Meeting Invitation"
	}
	return &jmap.CalendarEvent{UID: msg.UID, Title: title, Start: msg.Start}
}

// ensureOwnerParticipant guarantees the imported event has an owner participant (the
// organizer), adding the SMTP envelope sender as owner when the ICS carried none.
func ensureOwnerParticipant(ev *jmap.CalendarEvent, from string) {
	for _, p := range ev.Participants {
		if p != nil && ((p.Roles != nil && p.Roles["owner"]) || p.Role == "owner") {
			return
		}
	}
	if from == "" {
		return
	}
	if ev.Participants == nil {
		ev.Participants = make(map[string]*jmap.JSCalendarParticipant)
	}
	ev.Participants[from] = &jmap.JSCalendarParticipant{
		Email: from,
		Role:  "owner",
		Roles: map[string]bool{"owner": true},
	}
}

// findEventByUID locates the calendar event whose iCalendar UID (RFC 5546 Section
// 2.1.5) matches uid. It scans the account's events by their "uid" property, and
// falls back to treating uid as a JMAP id for events imported before uid tracking.
func (s *Session) findEventByUID(ctx context.Context, uid string) *jmap.CalendarEvent {
	if s.backend.CalendarsBackend == nil || uid == "" {
		return nil
	}
	if all, err := s.backend.CalendarsBackend.GetAllCalendarEvents(ctx); err == nil {
		for _, ev := range all {
			if ev != nil && ev.UID == uid {
				return ev
			}
		}
	}
	events, _, err := s.backend.CalendarsBackend.GetCalendarEvents(ctx, []jmap.Id{jmap.Id(uid)})
	if err == nil && len(events) > 0 {
		return events[0]
	}
	return nil
}

// buildReceivedHeader constructs an RFC 5321 Section 4.4 / RFC 5322 Section 3.6.7
// "Received:" trace header for the current transaction, recording the client (HELO
// name and remote address), the receiving host, a delivery id, the envelope
// recipient, and the receipt time.
func (s *Session) buildReceivedHeader() string {
	from := s.helo
	if from == "" {
		from = "unknown"
	}
	remote := s.remoteAddr
	if remote == "" {
		remote = "unknown"
	}
	by := s.backend.ServerName
	if by == "" {
		by = "localhost"
	}
	now := time.Now().UTC()
	forClause := ""
	if len(s.to) > 0 {
		forClause = fmt.Sprintf("\r\n\tfor <%s>", s.to[0])
	}
	return fmt.Sprintf("Received: from %s (%s)\r\n\tby %s with ESMTP id %d%s;\r\n\t%s\r\n",
		from, remote, by, now.UnixNano(), forClause, now.Format(time.RFC1123Z))
}

// Reset clears transaction state (RSET command).
func (s *Session) Reset() {
	s.from = ""
	s.to = nil
}

// Logout closes session (QUIT command).
func (s *Session) Logout() error {
	return nil
}
