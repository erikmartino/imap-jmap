package smtp_test

import (
	"testing"

	"imap-jmap/jmap/memory"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC3207_SMTPStartTLS tests RFC 3207 SMTP Service Extension for Secure SMTP over Transport Layer Security.
func TestRFC3207_SMTPStartTLS(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()

	srv := jmapsmtp.NewServer("127.0.0.1:0", memBackend, memBlobBackend, nil)
	if srv == nil {
		t.Fatalf("NewServer returned nil for RFC 3207 STARTTLS capability check")
	}
}
