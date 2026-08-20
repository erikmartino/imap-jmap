package smtp

import (
	"log"
	"time"

	gosmtp "github.com/emersion/go-smtp"

	"imap-jmap/jmap"
)

// Server wraps the underlying go-smtp Server configured for JMAP mail intake.
type Server struct {
	server *gosmtp.Server
	addr   string
}

// Option defines a functional configuration option for SMTP Server.
type Option func(*Server)

// WithAccountResolver sets a custom AccountResolver on the SMTP receiver backend.
func WithAccountResolver(resolver jmap.AccountResolver) Option {
	return func(s *Server) {
		if receiver, ok := s.server.Backend.(*ReceiverBackend); ok {
			receiver.AccountResolver = resolver
		}
	}
}

// WithSenderVerifier sets the SenderVerifier (SPF/DKIM/DMARC, SEC-1) used to
// authenticate senders before iTIP scheduling messages are auto-applied.
// Without it the receiver runs in development mode and applies iTIP without
// sender authentication; production deployments MUST set a verifier.
func WithSenderVerifier(verifier SenderVerifier) Option {
	return func(s *Server) {
		if receiver, ok := s.server.Backend.(*ReceiverBackend); ok {
			receiver.SenderVerifier = verifier
		}
	}
}

// WithTransportMode sets the transport boundary (RFC 6409 Section 3.1) for the
// server: TransportModeMX for the unauthenticated inbound relay path (default)
// or TransportModeSubmission for the authenticated message submission path.
func WithTransportMode(mode TransportMode) Option {
	return func(s *Server) {
		if receiver, ok := s.server.Backend.(*ReceiverBackend); ok {
			receiver.Mode = mode
		}
	}
}

// WithAuthenticator sets the Authenticator (RFC 4954) used to validate SMTP
// AUTH credentials on the submission transport. Without it the server runs in
// development mode and accepts any credentials; production deployments MUST set
// an authenticator so the submission boundary can bind the sender identity.
func WithAuthenticator(auth Authenticator) Option {
	return func(s *Server) {
		if receiver, ok := s.server.Backend.(*ReceiverBackend); ok {
			receiver.Authenticator = auth
		}
	}
}

// WithAllowInsecureAuth controls whether AUTH is permitted without TLS
// (RFC 4954 Section 9). It is enabled by default for development; deployments
// that require a secure layer for plaintext password mechanisms MUST disable
// it so PLAIN is neither advertised nor usable without STARTTLS.
func WithAllowInsecureAuth(allowed bool) Option {
	return func(s *Server) {
		s.server.AllowInsecureAuth = allowed
		if receiver, ok := s.server.Backend.(*ReceiverBackend); ok {
			receiver.AllowInsecureAuth = allowed
		}
	}
}

// NewServer initializes a new SMTP server instance configured for receiving mail into JMAP storage.
func NewServer(addr string, mailBackend jmap.MailBackend, blobBackend jmap.BlobBackend, calBackend jmap.CalendarsBackend, opts ...Option) *Server {
	backend := NewReceiverBackend(mailBackend, blobBackend, calBackend)
	backend.ServerName = "localhost"

	s := gosmtp.NewServer(backend)
	s.Addr = addr
	s.Domain = "localhost"
	s.ReadTimeout = 30 * time.Second
	s.WriteTimeout = 30 * time.Second
	s.MaxMessageBytes = 32 * 1024 * 1024 // 32MB max message size
	s.MaxRecipients = 50
	s.AllowInsecureAuth = true

	srv := &Server{
		server: s,
		addr:   addr,
	}
	for _, opt := range opts {
		opt(srv)
	}
	backend.AllowInsecureAuth = s.AllowInsecureAuth
	return srv
}

// Addr returns the configured address of the SMTP server.
func (s *Server) Addr() string {
	return s.addr
}

// ListenAndServe starts the SMTP receiver server.
func (s *Server) ListenAndServe() error {
	log.Printf("Starting SMTP server on %s", s.addr)
	return s.server.ListenAndServe()
}

// Close gracefully stops the SMTP receiver server.
func (s *Server) Close() error {
	return s.server.Close()
}
