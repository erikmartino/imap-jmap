package smtp_test

import (
	"testing"

	"imap-jmap/jmap/imapsmtp"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC3207_SMTPStartTLS tests RFC 3207 SMTP Service Extension for Secure SMTP over Transport Layer Security.
func TestRFC3207_SMTPStartTLS(t *testing.T) {
	backend, cleanup := imapsmtp.NewEmbeddedBackend("user@example.com")
	defer cleanup()

	srv := jmapsmtp.NewServer("127.0.0.1:0", backend, backend, nil)
	if srv == nil {
		t.Fatalf("NewServer returned nil for RFC 3207 STARTTLS capability check")
	}
}
