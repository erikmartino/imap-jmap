package smtp

import (
	"context"
	"io"
	"log"

	"github.com/emersion/go-smtp"

	"imap-jmap/jmap"
)

// ReceiverBackend implements smtp.Backend for receiving emails and storing them into JMAP backends.
type ReceiverBackend struct {
	MailBackend jmap.MailBackend
	BlobBackend jmap.BlobBackend
	AccountID   string
}

// NewReceiverBackend initializes a new SMTP ReceiverBackend linked to JMAP backends.
func NewReceiverBackend(mailBackend jmap.MailBackend, blobBackend jmap.BlobBackend) *ReceiverBackend {
	return &ReceiverBackend{
		MailBackend: mailBackend,
		BlobBackend: blobBackend,
		AccountID:   "primary",
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
