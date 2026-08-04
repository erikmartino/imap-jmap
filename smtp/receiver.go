package smtp

import (
	"context"
	"io"
	"log"
	"strings"

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
}

// NewReceiverBackend initializes a new SMTP ReceiverBackend linked to JMAP backends.
func NewReceiverBackend(mailBackend jmap.MailBackend, blobBackend jmap.BlobBackend, calBackend jmap.CalendarsBackend, resolver ...jmap.AccountResolver) *ReceiverBackend {
	var r jmap.AccountResolver
	if len(resolver) > 0 && resolver[0] != nil {
		r = resolver[0]
	} else {
		r = jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	}
	return &ReceiverBackend{
		MailBackend:      mailBackend,
		BlobBackend:      blobBackend,
		CalendarsBackend: calBackend,
		AccountResolver:  r,
		AccountID:        "primary",
	}
}

// NewSession starts a new SMTP receiving session per connection.
func (b *ReceiverBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &Session{
		backend: b,
	}, nil
}

// Session handles individual SMTP transaction commands (MAIL FROM, RCPT TO, DATA).
type Session struct {
	backend *ReceiverBackend
	from    string
	to      []string
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

	ctx := context.Background()
	if s.backend.AccountID != "" {
		ctx = jmap.ContextWithAccountID(ctx, s.backend.AccountID)
	}

	// 1. Store raw RFC 5322 byte stream as a Blob in BlobBackend (RFC 8620 Section 6)
	var blobID jmap.Id = "blob-unknown"
	if s.backend.BlobBackend != nil {
		blob, err := s.backend.BlobBackend.PutBlob(ctx, s.backend.AccountID, "message/rfc822", data)
		if err != nil {
			log.Printf("SMTP receiver warning: failed to store blob: %v", err)
		} else {
			blobID = jmap.Id(blob.ID)
		}
	}

	// 2. Parse raw bytes into JMAP Email struct (RFC 8621 Section 4)
	email, err := ParseMessageToEmail(data, blobID)
	if err != nil {
		log.Printf("SMTP receiver warning: parsing email error: %v", err)
	}

	// 3. Store Email in MailBackend (RFC 8621 Section 4)
	if s.backend.MailBackend != nil {
		created, err := s.backend.MailBackend.CreateEmail(ctx, email)
		if err != nil {
			log.Printf("SMTP receiver error: failed to create email in backend: %v", err)
			return err
		}
		log.Printf("SMTP receiver: stored email %s (blob %s, size %d bytes)", created.ID, created.BlobID, created.Size)
	}

	// 4. Auto-process iMIP invitation responses and incoming invitations (RFC 6047 / RFC 5546)
	dataStr := string(data)
	if s.backend.CalendarsBackend != nil && (strings.Contains(dataStr, "BEGIN:VCALENDAR") || strings.Contains(dataStr, "text/calendar")) {
		msg, err := jmap.ParseITIPMessage(dataStr)
		if err == nil && msg != nil && msg.UID != "" {
			eventID := jmap.Id(msg.UID)
			if strings.EqualFold(msg.Method, "REPLY") {
				events, _, err := s.backend.CalendarsBackend.GetCalendarEvents(ctx, []jmap.Id{eventID})
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

					_, _ = s.backend.CalendarsBackend.UpdateCalendarEvent(ctx, eventID, map[string]any{
						"status": status,
					})
					log.Printf("SMTP receiver: auto-updated calendar event %s participant %s status to %s", eventID, attendeeEmail, status)
				}
			} else if strings.EqualFold(msg.Method, "REQUEST") {
				// Auto-create pending calendar event from incoming external invitation
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
				createdEv, err := s.backend.CalendarsBackend.CreateCalendarEvent(ctx, newEvent)
				if err == nil && createdEv != nil {
					log.Printf("SMTP receiver: auto-imported incoming external invitation into calendar event %s (%s)", createdEv.ID, createdEv.Title)
				}
			}
		}
	}

	return nil
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
