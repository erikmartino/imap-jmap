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

// NewServer initializes a new SMTP server instance configured for receiving mail into JMAP storage.
func NewServer(addr string, mailBackend jmap.MailBackend, blobBackend jmap.BlobBackend, calBackend jmap.CalendarsBackend, opts ...Option) *Server {
	backend := NewReceiverBackend(mailBackend, blobBackend, calBackend)

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
