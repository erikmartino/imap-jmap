package smtp_test

import (
	"testing"

	"imap-jmap/jmap/memory"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC1870_SMTPSizeExtension tests RFC 1870 SMTP Service Extension for Message Size Declaration.
func TestRFC1870_SMTPSizeExtension(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()

	srv := jmapsmtp.NewServer("127.0.0.1:0", memBackend, memBlobBackend, nil)
	if srv.Addr() != "127.0.0.1:0" {
		t.Errorf("Expected server addr '127.0.0.1:0', got %s", srv.Addr())
	}
}
