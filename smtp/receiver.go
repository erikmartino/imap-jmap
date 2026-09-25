package smtp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/foxcpp/go-sieve"
	"github.com/foxcpp/go-sieve/interp"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmapsieve"
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
	MailBackend      jmapmail.MailBackend
	BlobBackend      jmapblob.BlobBackend
	CalendarsBackend jmapcalendar.CalendarsBackend
	SieveBackend     jmapsieve.SieveBackend
	OutboundSender   jmapmail.OutboundMailSender
	AccountResolver  jmapauth.AccountResolver
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
	// MaxMessageSize sets the maximum accepted message size in DATA (default MaxSMTPMessageSize).
	MaxMessageSize int64
}

// NewReceiverBackend initializes a new SMTP ReceiverBackend linked to JMAP backends.
func NewReceiverBackend(mailBackend jmapmail.MailBackend, blobBackend jmapblob.BlobBackend, calBackend jmapcalendar.CalendarsBackend, resolver ...jmapauth.AccountResolver) *ReceiverBackend {
	var r jmapauth.AccountResolver
	if len(resolver) > 0 {
		r = resolver[0]
	}
	return &ReceiverBackend{
		MailBackend:      mailBackend,
		BlobBackend:      blobBackend,
		CalendarsBackend: calBackend,
		AccountResolver:  r,
		AccountID:        "",
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

	// senderAuth caches the sender-authentication outcome for the message so the
	// Authentication-Results header and the iTIP gate share one evaluation.
	senderAuth *SenderAuthResult
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
	if s.mode != TransportModeSubmission {
		return nil, smtp.ErrAuthUnsupported
	}
	if mech != sasl.Plain {
		return nil, smtp.ErrAuthUnknownMechanism
	}
	return sasl.NewPlainServer(func(identity, username, password string) error {
		if s.backend.Authenticator == nil {
			s.authenticated = true
			s.authenticatedAs = username
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
	// A new transaction starts here: the previous message's cached sender
	// authentication result must not leak into this one.
	s.senderAuth = nil
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
	maxSize := int64(MaxSMTPMessageSize)
	if s.backend != nil && s.backend.MaxMessageSize > 0 {
		maxSize = s.backend.MaxMessageSize
	}
	lr := io.LimitReader(r, maxSize+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return err
	}
	if int64(len(data)) > maxSize {
		log.Printf("SMTP receiver: rejected oversized message from %s (%d bytes > %d bytes limit)",
			s.remoteAddr, len(data), maxSize)
		return &smtp.SMTPError{
			Code:         552,
			EnhancedCode: smtp.EnhancedCode{5, 3, 4},
			Message:      fmt.Sprintf("Message size exceeds maximum limit of %d bytes", maxSize),
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
		data = jmapmail.EnsureValidMessageID(data, s.backend.ServerName)
	}

	// Prepend an RFC 5321 Section 4.4 trace ("Received:") header. A receiving SMTP
	// server MUST insert this at the top of the message so delivery is auditable in
	// the message headers, recording where it came from, the receiving host, and when.
	data = append([]byte(s.buildReceivedHeader()), data...)

	// Prepend an RFC 8601 Section 3 trace ("Authentication-Results:") header for
	// senders that actually underwent SPF/DKIM/DMARC evaluation. Locally trusted
	// senders (and the no-verifier development mode) bypass DNS and get no header.
	// The evaluation is shared with the iTIP gate below, so a message is never
	// verified more than once.
	if res := s.senderAuthResult(rawData); res != nil && (res.SPF != "" || res.DKIM != "" || res.DMARC != "") {
		fromDom, _ := extractFromDomain(rawData)
		authServ := s.backend.ServerName
		if authServ == "" {
			authServ = "localhost"
		}
		data = append([]byte(res.AuthenticationResultsHeader(authServ, fromDom)), data...)
	}

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
			targetAccountIDs[jmapauth.AccountIDForSubject(s.to[0])] = true
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
		rcptSubject, ok := jmapauth.SubjectForAccountID(targetAccountID)
		if !ok || rcptSubject == "" {
			rcptSubject = targetAccountID
		}
		rcptCtx := jmapauth.ContextWithAccountID(context.Background(), targetAccountID)
		rcptCtx = jmapauth.ContextWithSubject(rcptCtx, rcptSubject)
		rcptCtx = jmapauth.ContextWithCredentials(rcptCtx, rcptSubject, rcptSubject)
		log.Printf("SMTP receiver: delivering message to account %s (%s)", targetAccountID, rcptSubject)

		var blobID jmapcore.Id = "blob-unknown"
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
				blobID = jmapcore.Id(blob.ID)
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

		// 2. Evaluate Sieve script per RFC 5228 / RFC 9661
		sieveRes, sieveErr := s.evaluateSieve(context.Background(), rcptCtx, s.from, rcptSubject, data)
		if sieveErr != nil {
			log.Printf("SMTP receiver: Sieve evaluation error for account %s: %v", targetAccountID, sieveErr)
		}

		if sieveRes != nil && sieveRes.reject {
			reason := sieveRes.rejectReason
			if reason == "" {
				reason = "rejected by recipient filter"
			}
			log.Printf("SMTP receiver: Sieve script rejected message for %s: %s", targetAccountID, reason)
			if firstFailure == nil {
				firstFailure = &smtp.SMTPError{
					Code:         550,
					EnhancedCode: smtp.EnhancedCode{5, 7, 1},
					Message:      "message rejected: " + reason,
				}
			}
			continue
		}

		if sieveRes != nil && sieveRes.discard {
			log.Printf("SMTP receiver: Sieve script discarded message for %s", targetAccountID)
			deliveredAny = true
			continue
		}

		if s.backend.MailBackend != nil && email != nil && blobStored {
			// Apply Sieve flags / keywords
			if sieveRes != nil && len(sieveRes.flags) > 0 {
				if email.Keywords == nil {
					email.Keywords = make(map[string]bool)
				}
				for _, f := range sieveRes.flags {
					kw := strings.ToLower(f)
					if strings.HasPrefix(kw, "\\") {
						kw = "$" + strings.TrimPrefix(kw, "\\")
					}
					email.Keywords[kw] = true
				}
			}

			// Apply Sieve fileinto or fallback to INBOX
			if sieveRes != nil && len(sieveRes.targetMailboxes) > 0 {
				email.MailboxIDs = make(map[jmapcore.Id]bool)
				for _, mbName := range sieveRes.targetMailboxes {
					mbID := jmapmail.MailboxIDByName(rcptCtx, s.backend.MailBackend, mbName)
					if mbID == "" {
						if newMb, err := s.backend.MailBackend.CreateMailbox(rcptCtx, &jmapmail.Mailbox{Name: mbName}); err == nil && newMb != nil {
							mbID = newMb.ID
						} else {
							mbID = jmapcore.Id("mb-" + strings.ToLower(mbName))
						}
					}
					email.MailboxIDs[mbID] = true
				}
			} else if sieveRes != nil && len(sieveRes.redirectAddrs) > 0 {
				// Sieve redirect without explicit keep or fileinto cancels implicit keep (RFC 5228 §4.2)
				email = nil
			} else {
				// Deliver into the recipient's INBOX.
				if inboxID := jmapmail.InboxMailboxID(rcptCtx, s.backend.MailBackend); inboxID != "" {
					email.MailboxIDs = map[jmapcore.Id]bool{inboxID: true}
				}
			}

			// Handle Sieve redirect forwarding
			if sieveRes != nil && len(sieveRes.redirectAddrs) > 0 {
				for _, redirAddr := range sieveRes.redirectAddrs {
					if s.backend.OutboundSender != nil {
						_ = s.backend.OutboundSender.SendMail(context.Background(), s.from, []string{redirAddr}, data)
					}
				}
				deliveredAny = true
			}

			if email != nil {
				created, err := s.backend.MailBackend.CreateEmail(rcptCtx, email)
				if err != nil {
					log.Printf("[MAIL INBOUND ERROR] Failed to store email for account %s: %v", targetAccountID, err)
					if firstFailure == nil {
						firstFailure = err
					}
				} else {
					log.Printf("[MAIL INBOUND] From: <%s> To: <%s> Subject: %q Size: %d bytes -> Account: %s EmailId: %s (Status: DELIVERED)",
						s.from, strings.Join(s.to, ", "), email.Subject, len(data), targetAccountID, created.ID)
					deliveredAny = true
				}
			}
		} else if email != nil && !blobStored {
			log.Printf("SMTP receiver: warning: skipping email creation for account %s because its blob could not be stored", targetAccountID)
		} else if email != nil {
			log.Printf("SMTP receiver: warning: no MailBackend configured; email not stored for account %s", targetAccountID)
			if firstFailure == nil {
				firstFailure = fmt.Errorf("no MailBackend configured for account %s", targetAccountID)
			}
		}

		// Evaluate VacationResponse auto-reply (RFC 8621 Section 8)
		if s.backend.MailBackend != nil && s.backend.OutboundSender != nil && (sieveRes == nil || (!sieveRes.discard && !sieveRes.reject)) {
			s.handleVacationResponse(rcptCtx, s.from, rcptSubject, email, data)
		}

		// 3. Auto-process iMIP invitation responses and incoming invitations (RFC 6047 /
		//    RFC 5546). The text/calendar part is extracted with a real MIME parser (which
		//    also decodes any Content-Transfer-Encoding) rather than scanning the raw bytes.
		//    SEC-1 sender authentication is evaluated once per message and must pass before
		//    any iTIP is auto-applied; fail closed.
		if s.backend.CalendarsBackend != nil {
			if icsBody := jmapcalendar.ExtractCalendarBody(data); icsBody != "" {
				if !authChecked {
					authChecked = true
					authOK, authReason = s.checkSenderAuth(rawData)
				}
				if !authOK {
					log.Printf("SMTP receiver: not applying iTIP for account %s: %s", targetAccountID, authReason)
				} else {
					jmapcalendar.ApplyITIP(rcptCtx, s.backend.CalendarsBackend, icsBody, s.from)
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
		log.Printf("[MAIL INBOUND REJECTED] From: <%s> To: <%s> Size: %d bytes -> Reason: %s", s.from, strings.Join(s.to, ", "), len(data), reason)
		if smtpErr, ok := firstFailure.(*smtp.SMTPError); ok {
			return smtpErr
		}
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
	res := s.senderAuthResult(raw)
	if res == nil {
		return false, "sender verification produced no result"
	}
	return res.AuthAuthenticated, res.Reason
}

// senderTrustedLocally reports whether the sender is trusted at the transport
// boundary, in which case no SPF/DKIM/DMARC lookup is needed:
//   - an authenticated submission client (RFC 6409 Section 4.3), or
//   - a client on the loopback or a private network (MTA "mynetworks" trust), or
//   - an envelope sender that belongs to a local account of this server.
func (s *Session) senderTrustedLocally() (bool, string) {
	if s.mode == TransportModeSubmission && s.authenticated {
		return true, fmt.Sprintf("authenticated submission user %q (transport-boundary trust)", s.authenticatedAs)
	}
	if ip := remoteIP(s.remoteAddr); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return true, "locally trusted client (loopback/private network)"
	}
	if s.from != "" && s.backend.AccountResolver != nil {
		if _, local := s.backend.AccountResolver.ResolveAccountID(context.Background(), s.from); local {
			return true, fmt.Sprintf("locally trusted sender %q (local account)", s.from)
		}
	}
	return false, ""
}

// senderAuthResult evaluates sender authentication once per message, caching the
// outcome for the Authentication-Results header and the iTIP gate. Locally
// trusted senders bypass DNS; when no verifier is configured the gate is skipped
// (development mode). The evaluation itself fails closed and is time-bounded.
func (s *Session) senderAuthResult(raw []byte) *SenderAuthResult {
	if s.senderAuth != nil {
		return s.senderAuth
	}
	if trusted, reason := s.senderTrustedLocally(); trusted {
		s.senderAuth = &SenderAuthResult{AuthAuthenticated: true, Reason: reason}
		return s.senderAuth
	}
	if s.backend.SenderVerifier == nil {
		s.senderAuth = &SenderAuthResult{AuthAuthenticated: true, Reason: "no sender verifier configured (development mode)"}
		return s.senderAuth
	}
	res, err := s.backend.SenderVerifier.Verify(context.Background(), &MessageToVerify{
		RawMessage:   raw,
		EnvelopeFrom: s.from,
		ClientIP:     remoteIP(s.remoteAddr),
		HeloName:     s.helo,
	})
	if err != nil || res == nil {
		reason := "sender verification error"
		if err != nil {
			reason += ": " + err.Error()
		}
		s.senderAuth = &SenderAuthResult{AuthAuthenticated: false, Reason: reason}
		return s.senderAuth
	}
	s.senderAuth = res
	return s.senderAuth
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
var msgIDRe = jmapmail.MsgIDRegex

// hasValidMessageID reports whether the message carries a Message-ID header
// field whose value conforms to the RFC 5322 Section 3.6.4 msg-id syntax.
func hasValidMessageID(data []byte) bool {
	return jmapmail.HasValidMessageID(data)
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
	s.senderAuth = nil
}

// Logout closes session (QUIT command).
func (s *Session) Logout() error {
	return nil
}

// sieveResult represents the evaluated outcome of an RFC 5228 Sieve script.
type sieveResult struct {
	discard         bool
	reject          bool
	rejectReason    string
	targetMailboxes []string
	redirectAddrs   []string
	flags           []string
}

func (s *Session) evaluateSieve(ctx context.Context, rcptCtx context.Context, fromAddr, rcptAddr string, data []byte) (*sieveResult, error) {
	if s.backend.SieveBackend == nil {
		return nil, nil
	}
	scripts, err := s.backend.SieveBackend.GetAllSieveScripts(rcptCtx)
	if err != nil || len(scripts) == 0 {
		return nil, nil
	}
	var activeScript *jmapsieve.SieveScript
	for _, sc := range scripts {
		if sc != nil && sc.IsActive {
			activeScript = sc
			break
		}
	}
	if activeScript == nil || strings.TrimSpace(activeScript.Content) == "" {
		return nil, nil
	}

	parsedScript, err := sieve.Load(strings.NewReader(activeScript.Content), sieve.DefaultOptions())
	if err != nil {
		log.Printf("SMTP receiver: failed to parse active Sieve script for %s: %v", rcptAddr, err)
		return nil, err
	}

	parsedMail, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	env := interp.EnvelopeStatic{
		From: fromAddr,
		To:   rcptAddr,
	}
	msgStatic := interp.MessageStatic{
		Size:       len(data),
		Header:     textproto.MIMEHeader(parsedMail.Header),
		RawMessage: data,
	}

	runtimeData := sieve.NewRuntimeData(parsedScript, interp.DummyPolicy{}, env, msgStatic)
	if err := parsedScript.Execute(ctx, runtimeData); err != nil {
		log.Printf("SMTP receiver: Sieve script execution error for %s: %v", rcptAddr, err)
		return nil, err
	}

	res := &sieveResult{}
	for _, action := range runtimeData.AppliedActions {
		switch act := action.(type) {
		case interp.ActionDiscard:
			res.discard = true
		case interp.ActionFileInto:
			res.targetMailboxes = append(res.targetMailboxes, act.Mailbox)
		case interp.ActionRedirect:
			res.redirectAddrs = append(res.redirectAddrs, act.Address)
		case interp.ActionReject:
			res.reject = true
			res.rejectReason = act.Reason
		case interp.ActionEReject:
			res.reject = true
			res.rejectReason = act.Reason
		}
	}
	res.flags = runtimeData.Flags
	return res, nil
}

func (s *Session) handleVacationResponse(rcptCtx context.Context, senderAddr, rcptAddr string, email *jmapmail.Email, data []byte) {
	if senderAddr == "" || senderAddr == "<>" {
		return
	}
	senderLower := strings.ToLower(senderAddr)
	if strings.Contains(senderLower, "mailer-daemon") || strings.Contains(senderLower, "postmaster") || strings.Contains(senderLower, "noreply") || strings.Contains(senderLower, "no-reply") {
		return
	}

	vr, err := s.backend.MailBackend.GetVacationResponse(rcptCtx)
	if err != nil || vr == nil || !vr.IsEnabled {
		return
	}

	now := time.Now().UTC()
	if vr.FromDate != nil && *vr.FromDate != "" {
		if t, err := time.Parse(time.RFC3339, *vr.FromDate); err == nil && now.Before(t) {
			return
		}
	}
	if vr.ToDate != nil && *vr.ToDate != "" {
		if t, err := time.Parse(time.RFC3339, *vr.ToDate); err == nil && now.After(t) {
			return
		}
	}

	// Anti-loop checks per RFC 3834 / RFC 5230:
	// If message has Auto-Submitted header (other than "no") or Precedence (bulk, junk, list), do not reply
	parsedMail, err := mail.ReadMessage(bytes.NewReader(data))
	if err == nil {
		if as := parsedMail.Header.Get("Auto-Submitted"); as != "" && !strings.EqualFold(as, "no") {
			return
		}
		if prec := strings.ToLower(parsedMail.Header.Get("Precedence")); prec == "bulk" || prec == "junk" || prec == "list" {
			return
		}
		if parsedMail.Header.Get("List-Id") != "" || parsedMail.Header.Get("List-Unsubscribe") != "" {
			return
		}
	}

	subj := "Auto: Vacation Response"
	if vr.Subject != nil && *vr.Subject != "" {
		subj = *vr.Subject
	} else if email != nil && email.Subject != "" {
		subj = "Auto: " + email.Subject
	}

	body := ""
	if vr.TextBody != nil && *vr.TextBody != "" {
		body = *vr.TextBody
	} else if vr.HTMLBody != nil && *vr.HTMLBody != "" {
		body = *vr.HTMLBody
	}
	if body == "" {
		body = "I am currently away and will respond when I return."
	}

	var msgIDHeader string
	if email != nil && len(email.MessageID) > 0 {
		msgIDHeader = fmt.Sprintf("In-Reply-To: <%s>\r\nReferences: <%s>\r\n", email.MessageID[0], email.MessageID[0])
	}

	rawReply := fmt.Sprintf("From: <%s>\r\nTo: <%s>\r\nSubject: %s\r\nDate: %s\r\nAuto-Submitted: auto-replied\r\n%sContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		rcptAddr, senderAddr, subj, now.Format(time.RFC1123Z), msgIDHeader, body)

	if s.backend.OutboundSender != nil {
		_ = s.backend.OutboundSender.SendMail(context.Background(), rcptAddr, []string{senderAddr}, []byte(rawReply))
	}
}
