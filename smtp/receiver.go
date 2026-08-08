package smtp

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

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
	return nil
}

// Rcpt handles RCPT TO command per RFC 5321.
func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

// Data handles DATA command per RFC 5321, storing raw blob and JMAP Email object per RFC 8620 & RFC 8621.
func (s *Session) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

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
			}
		}
	}
	if len(targetAccountIDs) == 0 {
		fallbackID := s.backend.AccountID
		if fallbackID == "" {
			fallbackID = jmap.AccountIDForSubject("user@example.com")
		}
		targetAccountIDs[fallbackID] = true
	}

	// 2. Deliver message copy for each target accountID
	for targetAccountID := range targetAccountIDs {
		rcptCtx := jmap.ContextWithAccountID(context.Background(), targetAccountID)

		var blobID jmap.Id = "blob-unknown"
		if s.backend.BlobBackend != nil {
			blob, err := s.backend.BlobBackend.PutBlob(rcptCtx, targetAccountID, "message/rfc822", data)
			if err != nil {
				log.Printf("SMTP receiver warning: failed to store blob: %v", err)
			} else {
				blobID = jmap.Id(blob.ID)
			}
		}

		email, err := ParseMessageToEmail(data, blobID)
		if err != nil {
			log.Printf("SMTP receiver warning: parsing email error: %v", err)
		}

		if s.backend.MailBackend != nil && email != nil {
			created, err := s.backend.MailBackend.CreateEmail(rcptCtx, email)
			if err != nil {
				log.Printf("SMTP receiver error: failed to create email in backend: %v", err)
			} else {
				log.Printf("SMTP receiver: stored email %s (account %s, blob %s, size %d bytes)", created.ID, targetAccountID, created.BlobID, created.Size)
			}
		}

		// 3. Auto-process iMIP invitation responses and incoming invitations (RFC 6047 / RFC 5546)
		dataStr := string(data)
		if s.backend.CalendarsBackend != nil && (strings.Contains(dataStr, "BEGIN:VCALENDAR") || strings.Contains(dataStr, "text/calendar")) {
			msg, err := jmap.ParseITIPMessage(dataStr)
			if err == nil && msg != nil && msg.UID != "" {
				eventID := jmap.Id(msg.UID)
				if strings.EqualFold(msg.Method, "REPLY") {
					events, _, err := s.backend.CalendarsBackend.GetCalendarEvents(rcptCtx, []jmap.Id{eventID})
					if err == nil && len(events) > 0 {
						ev := events[0]
						attendeeEmail := s.from
						if len(msg.Attendees) > 0 && msg.Attendees[0].Email != "" {
							attendeeEmail = msg.Attendees[0].Email
						}
						status := strings.ToLower(msg.Status)
						if status == "" {
							status = "accepted"
						}

						if ev.Participants == nil {
							ev.Participants = make(map[string]*jmap.JSCalendarParticipant)
						}
						if p, ok := ev.Participants[attendeeEmail]; ok && p != nil {
							p.Status = status
						} else {
							ev.Participants[attendeeEmail] = &jmap.JSCalendarParticipant{
								Email:  attendeeEmail,
								Status: status,
							}
						}

						_, _ = s.backend.CalendarsBackend.UpdateCalendarEvent(rcptCtx, eventID, map[string]any{
							"status": status,
						})
						log.Printf("SMTP receiver: auto-updated calendar event %s participant %s status to %s", eventID, attendeeEmail, status)
					}
				} else if strings.EqualFold(msg.Method, "REQUEST") {
					title := msg.Summary
					if title == "" {
						title = "External Meeting Invitation"
					}
					newEvent := &jmap.CalendarEvent{
						ID:          eventID,
						Title:       title,
						Start:       msg.Start,
						Status:      "tentative",
						CalendarIDs: map[jmap.Id]bool{"cal-default": true},
						Participants: map[string]*jmap.JSCalendarParticipant{
							s.from: {
								Email: s.from,
								Role:  "owner",
							},
						},
					}
					createdEv, err := s.backend.CalendarsBackend.CreateCalendarEvent(rcptCtx, newEvent)
					if err == nil && createdEv != nil {
						log.Printf("SMTP receiver: auto-imported incoming external invitation into calendar event %s (%s)", createdEv.ID, createdEv.Title)
					}
				}
			}
		}
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
