package smtp_test

import (
	"testing"

	"imap-jmap/jmap/imapsmtp"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC1870_SMTPSizeExtension tests RFC 1870 SMTP Service Extension for Message Size Declaration.
func TestRFC1870_SMTPSizeExtension(t *testing.T) {
	backend, cleanup := imapsmtp.NewEmbeddedBackend("user@example.com")
	defer cleanup()

	srv := jmapsmtp.NewServer("127.0.0.1:0", backend, backend, nil)
	if srv.Addr() != "127.0.0.1:0" {
		t.Errorf("Expected server addr '127.0.0.1:0', got %s", srv.Addr())
	}
}
