package smtp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"imap-jmap/jmap"
)

// TransportMode distinguishes the two SMTP transports defined by RFC 6409
// Section 3.1: the unauthenticated inbound relay path (port 25, MX) and the
// authenticated message submission path (port 587).
type TransportMode int

const (
	// TransportModeMX is the unauthenticated inbound relay transport (RFC 6409
	// Section 3.1). Messages received on this path MUST be sender-authenticated
	// (SPF/DKIM/DMARC) before iTIP is auto-applied.
	TransportModeMX TransportMode = iota
	// TransportModeSubmission is the authenticated message submission transport
	// (RFC 6409 Section 3.1, port 587). Clients MUST authenticate (RFC 6409
	// Section 4.3) and the authenticated identity is trusted on this boundary.
	TransportModeSubmission
)

// Authenticator validates SMTP AUTH credentials (RFC 4954) for the submission
// transport. Authenticate returns the authenticated user's email address when
// the credentials are valid, ok=false when they are rejected, and an error for
// a temporary authentication failure.
type Authenticator interface {
	Authenticate(ctx context.Context, username, password string) (email string, ok bool, err error)
}

// ReceiverBackend implements smtp.Backend for receiving emails and storing them into JMAP backends.
type ReceiverBackend struct {
	MailBackend      jmap.MailBackend
	BlobBackend      jmap.BlobBackend
	CalendarsBackend jmap.CalendarsBackend
	AccountResolver  jmap.AccountResolver
	AccountID        string
	// SenderVerifier authenticates the sender (SPF/DKIM/DMARC, SEC-1) before
	// iTIP scheduling messages are auto-applied. When nil (development mode)
	// the authentication gate is skipped; production deployments MUST set a
	// verifier so unauthenticated messages fail closed and never mutate
	// calendar state.
	SenderVerifier SenderVerifier
	// Mode selects the transport boundary this server is on (RFC 6409
	// Section 3.1). Defaults to TransportModeMX.
	Mode TransportMode
	// Authenticator validates SMTP AUTH credentials on the submission transport.
	// When nil (development mode) the submission server accepts any credentials.
	Authenticator Authenticator
	// AllowInsecureAuth mirrors the go-smtp server setting: when true, AUTH is
	// permitted without TLS (RFC 4954 Section 9 requires a secure layer for
	// plaintext password mechanisms unless the site accepts the risk).
	AllowInsecureAuth bool
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
	s := &Session{
		backend: b,
		mode:    b.Mode,
	}
	if c != nil {
		s.helo = c.Hostname()
		if conn := c.Conn(); conn != nil {
			s.remoteAddr = conn.RemoteAddr().String()
		}
		if _, tlsActive := c.TLSConnectionState(); tlsActive {
			s.tlsActive = true
		}
	}
	return s, nil
}

// Session handles individual SMTP transaction commands (MAIL FROM, RCPT TO, DATA).
type Session struct {
	backend         *ReceiverBackend
	mode            TransportMode
	from            string
	to              []string
	helo            string
	remoteAddr      string
	authenticated   bool
	authenticatedAs string
	tlsActive       bool
}

// AuthMechanisms advertises the supported SASL mechanism (RFC 4954 Section 3):
// PLAIN is mandatory-to-implement (RFC 4954 Section 14). AUTH is appropriate
// only for the submission protocol (RFC 4954 Section 3), so the unauthenticated
// inbound MX transport never advertises it. Without an encryption layer the
// server must not advertise a plaintext password mechanism (RFC 4954
// Section 9), unless the site has explicitly permitted insecure AUTH.
func (s *Session) AuthMechanisms() []string {
	if s.mode != TransportModeSubmission {
		return nil
	}
	if !s.tlsActive && !s.backend.AllowInsecureAuth {
		return nil
	}
	return []string{sasl.Plain}
}

// Auth starts an AUTH PLAIN exchange (RFC 4954 Section 4). The returned SASL
// server validates the credentials against the configured Authenticator; a
// failed exchange is rejected with 535 5.7.8 (invalid credentials) or a
// 4xx temporary error (RFC 4954 Section 6).
func (s *Session) Auth(mech string) (sasl.Server, error) {
	if s.backend.Authenticator == nil {
		return nil, smtp.ErrAuthUnsupported
	}
	if mech != sasl.Plain {
		return nil, smtp.ErrAuthUnknownMechanism
	}
	return sasl.NewPlainServer(func(identity, username, password string) error {
		email, ok, err := s.backend.Authenticator.Authenticate(context.Background(), username, password)
		if err != nil {
			return &smtp.SMTPError{
				Code:         454,
				EnhancedCode: smtp.EnhancedCode{4, 7, 0},
				Message:      "Temporary authentication failure",
			}
		}
		if !ok {
			return smtp.ErrAuthFailed
		}
		s.authenticated = true
		s.authenticatedAs = email
		return nil
	}), nil
}

// AuthPlain handles PLAIN authentication (RFC 4954). It validates the
// credentials against the configured Authenticator when one is set; without an
// authenticator (development mode) it accepts the credentials so local testing
// and the delivery harness can submit without a credential store.
func (s *Session) AuthPlain(username, password string) error {
	if s.backend.Authenticator == nil {
		return nil
	}
	email, ok, err := s.backend.Authenticator.Authenticate(context.Background(), username, password)
	if err != nil {
		return &smtp.SMTPError{
			Code:         454,
			EnhancedCode: smtp.EnhancedCode{4, 7, 0},
			Message:      "Temporary authentication failure",
		}
	}
	if !ok {
		return smtp.ErrAuthFailed
	}
	s.authenticated = true
	s.authenticatedAs = email
	return nil
}

// ErrAuthenticationRequired is the RFC 6409 Section 4.3 / RFC 4954 Section 6
// reply for a command on the submission transport while authentication is not
// in force: 530 5.7.0. The go-smtp library's ErrAuthRequired uses 502, so the
// submission transport returns this code explicitly.
var ErrAuthenticationRequired = &smtp.SMTPError{
	Code:         530,
	EnhancedCode: smtp.EnhancedCode{5, 7, 0},
	Message:      "Authentication required",
}

// Mail handles MAIL FROM command per RFC 5321.
func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	if s.mode == TransportModeSubmission {
		// RFC 6409 Section 4.3 (MUST): on the submission transport the server
		// MUST require authentication before accepting a message, unless it has
		// independently established authorization (e.g. a protected subnetwork).
		if !s.authenticated {
			return ErrAuthenticationRequired
		}
		// RFC 6409 Section 6.1 (MAY): reject a MAIL command whose address is not
		// authorized with the authenticated identity (550 5.7.1). This is how the
		// submission boundary binds the sender to the authenticated user.
		if s.authenticatedAs != "" && !emailAddressMatches(from, s.authenticatedAs) {
			return &smtp.SMTPError{
				Code:         550,
				EnhancedCode: smtp.EnhancedCode{5, 7, 1},
				Message:      fmt.Sprintf("MAIL FROM does not match the authenticated user %s", s.authenticatedAs),
			}
		}
	}
	s.from = from
	log.Printf("SMTP receiver: MAIL FROM <%s> from %s (helo=%q, authenticated=%v)", from, s.remoteAddr, s.helo, s.authenticated)
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

const (
	// MaxSMTPMessageSize is the maximum size accepted in SMTP DATA (50MB).
	MaxSMTPMessageSize = 50 * 1024 * 1024
	// MaxMIMENestingDepth is the maximum allowed MIME multipart recursion depth.
	MaxMIMENestingDepth = 10
	// MaxMIMEParts is the maximum total number of MIME parts inspected for scheduling bodies.
	MaxMIMEParts = 100
)

// Data handles DATA command per RFC 5321, storing raw blob and JMAP Email object per RFC 8620 & RFC 8621.
func (s *Session) Data(r io.Reader) error {
	lr := io.LimitReader(r, MaxSMTPMessageSize+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return err
	}
	if len(data) > MaxSMTPMessageSize {
		log.Printf("SMTP receiver: rejected oversized message from %s (%d bytes > %d bytes limit)",
			s.remoteAddr, len(data), MaxSMTPMessageSize)
		return &smtp.SMTPError{
			Code:         552,
			EnhancedCode: smtp.EnhancedCode{5, 3, 4},
			Message:      fmt.Sprintf("Message size exceeds maximum limit of %d bytes", MaxSMTPMessageSize),
		}
	}
	log.Printf("SMTP receiver: DATA from %s (helo=%q, envelope from=%q, recipients=%v, size=%d bytes)",
		s.remoteAddr, s.helo, s.from, s.to, len(data))

	// Keep the message exactly as received for DKIM signature verification:
	// DKIM signs the received bytes (RFC 6376 Section 6.1), so the server's own
	// Received: trace header must not be part of what is verified.
	rawData := data

	// RFC 6409 Section 8.3 (SHOULD): the MSA adds a valid Message-ID field to a
	// submitted message that lacks one, since a number of clients still do not
	// generate them. The addition applies only on the submission transport and
	// only to the stored copy: the bytes that DKIM verifies stay the client's
	// original bytes.
	if s.mode == TransportModeSubmission && !hasValidMessageID(data) {
		data = append([]byte(fmt.Sprintf("Message-ID: <%d.%d@%s>\r\n", time.Now().UnixNano(), os.Getpid(), s.backend.ServerName)), data...)
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
	// SEC-1: sender authentication is evaluated once per message (not once per
	// recipient account) and cached for the delivery loop.
	var authChecked bool
	var authOK bool
	var authReason string
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
				// SEC-1 sender authentication gate: SPF/DKIM/DMARC verification is
				// performed once per message and must pass before any iTIP is
				// auto-applied. Fail closed: unauthenticated or unverifiable senders
				// are still delivered to the mailbox but never mutate calendar state.
				if !authChecked {
					authChecked = true
					authOK, authReason = s.checkSenderAuth(rawData)
				}
				if !authOK {
					log.Printf("SMTP receiver: not applying iTIP %s for account %s: %s", msg.Method, targetAccountID, authReason)
					continue
				}

				senderClean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(s.from, "mailto:")))

				if strings.EqualFold(msg.Method, "REPLY") {
					// Envelope <-> iTIP identity binding (SEC-2)
					attendeeEmail := ""
					if len(msg.Attendees) > 0 && msg.Attendees[0].Email != "" {
						attendeeEmail = msg.Attendees[0].Email
					} else if s.from != "" {
						attendeeEmail = s.from
					}
					attendeeClean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(attendeeEmail, "mailto:")))
					if senderClean != "" && attendeeClean != "" && senderClean != attendeeClean {
						log.Printf("SMTP receiver warning: ignoring iTIP REPLY: envelope sender %q does not match attendee %q (SEC-2)", s.from, attendeeEmail)
						continue
					}

					ev := s.findEventByUID(rcptCtx, msg.UID)
					if ev != nil {
						// Participant authorization (SEC-3)
						partKey := findParticipantKey(ev, attendeeEmail)
						if partKey == "" {
							log.Printf("SMTP receiver warning: ignoring iTIP REPLY: attendee %q is not a participant on event %s (SEC-3)", attendeeEmail, ev.ID)
							continue
						}

						// Replay / out-of-order defence (SEC-5)
						if msg.Sequence > 0 && ev.Sequence > 0 && msg.Sequence < ev.Sequence {
							log.Printf("SMTP receiver warning: ignoring stale iTIP REPLY: message sequence %d < event sequence %d (SEC-5)", msg.Sequence, ev.Sequence)
							continue
						}

						status := strings.ToLower(msg.Status)
						if status == "" {
							status = "accepted"
						}

						patch := map[string]any{
							"participants/" + partKey + "/participationStatus": status,
							"participants/" + partKey + "/scheduleStatus":      "2.0;delivered",
						}
						if msg.Sequence > ev.Sequence {
							patch["sequence"] = msg.Sequence
						}

						if _, err := s.backend.CalendarsBackend.UpdateCalendarEvent(rcptCtx, ev.ID, patch); err == nil {
							log.Printf("SMTP receiver: applied iTIP REPLY to event %s: participant %s -> %s", ev.ID, attendeeEmail, status)
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
					// Envelope <-> iTIP identity binding (SEC-2)
					orgClean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(msg.Organizer, "mailto:")))
					if senderClean != "" && orgClean != "" && senderClean != orgClean {
						log.Printf("SMTP receiver warning: ignoring iTIP REQUEST: envelope sender %q does not match organizer %q (SEC-2)", s.from, msg.Organizer)
						continue
					}

					imported := parseImportedEvent(icsBody, msg)
					if existing := s.findEventByUID(rcptCtx, imported.UID); existing != nil {
						// Replay / out-of-order defence (SEC-5)
						if msg.Sequence > 0 && existing.Sequence > 0 && msg.Sequence < existing.Sequence {
							log.Printf("SMTP receiver warning: ignoring stale iTIP REQUEST: message sequence %d < event sequence %d (SEC-5)", msg.Sequence, existing.Sequence)
							continue
						}

						// Re-REQUEST: re-sync the mutable core details onto the copy.
						patch := map[string]any{"title": imported.Title, "start": imported.Start}
						if imported.Duration != "" {
							patch["duration"] = imported.Duration
						}
						if msg.Sequence >= existing.Sequence {
							patch["sequence"] = msg.Sequence
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
				} else if strings.EqualFold(msg.Method, "CANCEL") {
					// Envelope <-> iTIP identity binding & Participant authorization (SEC-2, SEC-3)
					ev := s.findEventByUID(rcptCtx, msg.UID)
					if ev != nil {
						orgClean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(msg.Organizer, "mailto:")))
						if senderClean != "" && orgClean != "" && senderClean != orgClean {
							log.Printf("SMTP receiver warning: ignoring iTIP CANCEL: envelope sender %q does not match organizer %q (SEC-2)", s.from, msg.Organizer)
							continue
						}
						if senderClean != "" && !isEventOrganizer(ev, s.from) && orgClean != "" && !isEventOrganizer(ev, orgClean) {
							log.Printf("SMTP receiver warning: ignoring iTIP CANCEL: sender %q is not the organizer of event %s (SEC-3)", s.from, ev.ID)
							continue
						}

						// Replay / out-of-order defence (SEC-5)
						if msg.Sequence > 0 && ev.Sequence > 0 && msg.Sequence < ev.Sequence {
							log.Printf("SMTP receiver warning: ignoring stale iTIP CANCEL: message sequence %d < event sequence %d (SEC-5)", msg.Sequence, ev.Sequence)
							continue
						}

						patch := map[string]any{"status": "cancelled"}
						if msg.Sequence >= ev.Sequence {
							patch["sequence"] = msg.Sequence
						}
						if _, err := s.backend.CalendarsBackend.UpdateCalendarEvent(rcptCtx, ev.ID, patch); err == nil {
							log.Printf("SMTP receiver: cancelled event %s from iTIP CANCEL", ev.ID)
							fromEmail := s.from
							s.backend.CalendarsBackend.CreateCalendarEventNotification(rcptCtx, &jmap.CalendarEventNotification{
								Type:            "deleted",
								CalendarEventID: ev.ID,
								ChangedBy: jmap.CalendarEventNotificationPerson{
									Email:           &fromEmail,
									CalendarAddress: &fromEmail,
								},
								Event:      ev,
								EventPatch: patch,
							})
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

// checkSenderAuth enforces the SEC-1 sender authentication gate: an iTIP
// scheduling message is only auto-applied when its sender is authenticated via
// SPF/DKIM/DMARC (RFC 7208 / RFC 6376 / RFC 7489). Fail closed: unauthenticated
// or unverifiable senders never mutate calendar state — the message is still
// delivered to the mailbox, exactly as a real server delivers it.
//
// Local trust exceptions keep local delivery working without DNS validation,
// mirroring real MTA trusted-network behavior (e.g. Postfix "mynetworks"):
//   - clients connected via the loopback interface (the server's own outbound
//     relay, local tooling, and the local delivery harness), and
//   - envelope senders whose address belongs to a local account of this server
//     (same-server users scheduling with each other).
//
// When no SenderVerifier is configured (development mode) the gate is skipped.
func (s *Session) checkSenderAuth(raw []byte) (bool, string) {
	if s.backend.SenderVerifier == nil {
		return true, "no sender verifier configured (development mode)"
	}
	// Transport-boundary trust (SEC-4): on the authenticated submission channel
	// the authenticated user IS the sender, established by RFC 4954 AUTH at the
	// transport layer (RFC 6409 Section 4.3). No SPF/DKIM/DMARC lookup is needed
	// on this boundary because the server authenticated the client directly.
	if s.mode == TransportModeSubmission && s.authenticated {
		return true, fmt.Sprintf("authenticated submission user %q (transport-boundary trust)", s.authenticatedAs)
	}
	if ip := remoteIP(s.remoteAddr); ip != nil && ip.IsLoopback() {
		return true, "locally trusted client (loopback)"
	}
	if s.from != "" && s.backend.AccountResolver != nil {
		if _, local := s.backend.AccountResolver.ResolveAccountID(context.Background(), s.from); local {
			return true, fmt.Sprintf("locally trusted sender %q (local account)", s.from)
		}
	}
	res, err := s.backend.SenderVerifier.Verify(context.Background(), &MessageToVerify{
		RawMessage:   raw,
		EnvelopeFrom: s.from,
		ClientIP:     remoteIP(s.remoteAddr),
		HeloName:     s.helo,
	})
	if err != nil {
		return false, "sender verification error: " + err.Error()
	}
	if !res.AuthAuthenticated {
		return false, res.Reason
	}
	return true, res.Reason
}

// remoteIP parses the "ip:port" remote address of a session into the client's
// IP address, returning nil when it cannot be parsed.
func remoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
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
	partsCount := 0
	for {
		if partsCount >= MaxMIMEParts {
			break
		}
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		partsCount++
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

func findParticipantKey(ev *jmap.CalendarEvent, attendeeEmail string) string {
	if ev == nil || attendeeEmail == "" {
		return ""
	}
	clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(attendeeEmail, "mailto:")))
	for key, p := range ev.Participants {
		if strings.ToLower(key) == clean || strings.ToLower(key) == "mailto:"+clean {
			return key
		}
		if p != nil {
			if strings.EqualFold(strings.TrimPrefix(p.Email, "mailto:"), clean) {
				return key
			}
			if p.SendTo != nil {
				for _, val := range p.SendTo {
					if strings.EqualFold(strings.TrimPrefix(val, "mailto:"), clean) {
						return key
					}
				}
			}
		}
	}
	return ""
}

func isEventOrganizer(ev *jmap.CalendarEvent, email string) bool {
	if ev == nil || email == "" {
		return false
	}
	clean := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(email, "mailto:")))
	for key, p := range ev.Participants {
		if p == nil {
			continue
		}
		isOrg := false
		if (p.Roles != nil && (p.Roles["owner"] || p.Roles["organizer"] || p.Roles["chair"])) ||
			p.Role == "owner" || p.Role == "organizer" || p.Role == "chair" {
			isOrg = true
		}
		if isOrg {
			if strings.ToLower(key) == clean || strings.EqualFold(strings.TrimPrefix(p.Email, "mailto:"), clean) {
				return true
			}
			if p.SendTo != nil {
				for _, val := range p.SendTo {
					if strings.EqualFold(strings.TrimPrefix(val, "mailto:"), clean) {
						return true
					}
				}
			}
		}
	}
	return false
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
// recipient, and the receipt time. When the session was authenticated the
// "with" clause is ESMTPA (ESMTPSA over TLS) per RFC 4954 Section 7.
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
	with := "ESMTP"
	if s.authenticated {
		if s.tlsActive {
			with = "ESMTPSA"
		} else {
			with = "ESMTPA"
		}
	}
	now := time.Now().UTC()
	forClause := ""
	if len(s.to) > 0 {
		forClause = fmt.Sprintf("\r\n\tfor <%s>", s.to[0])
	}
	return fmt.Sprintf("Received: from %s (%s)\r\n\tby %s with %s id %d%s;\r\n\t%s\r\n",
		from, remote, by, with, now.UnixNano(), forClause, now.Format(time.RFC1123Z))
}

// msgIDRe matches the RFC 5322 Section 3.6.4 msg-id ABNF (id-left "@" id-right
// wrapped in angle brackets) without whitespace, used to decide whether a
// submitted message already carries a valid Message-ID (RFC 6409 Section 8.3).
var msgIDRe = regexp.MustCompile(`^<[^<>@\s]+@[^<>@\s]+>$`)

// hasValidMessageID reports whether the message carries a Message-ID header
// field whose value conforms to the RFC 5322 Section 3.6.4 msg-id syntax.
func hasValidMessageID(data []byte) bool {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return false
	}
	v := strings.TrimSpace(msg.Header.Get("Message-ID"))
	return v != "" && msgIDRe.MatchString(v)
}

// emailAddressMatches reports whether the envelope sender address matches the
// authenticated identity (RFC 6409 Section 6.1). The local part is compared
// case-sensitively and the domain case-insensitively per RFC 5321 Section 2.4.
func emailAddressMatches(from, authenticatedAs string) bool {
	if from == "" || authenticatedAs == "" {
		return false
	}
	fromLocal, fromDomain, okFrom := strings.Cut(from, "@")
	authLocal, authDomain, okAuth := strings.Cut(authenticatedAs, "@")
	if !okFrom || !okAuth {
		return false
	}
	return fromLocal == authLocal && strings.EqualFold(fromDomain, authDomain)
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
