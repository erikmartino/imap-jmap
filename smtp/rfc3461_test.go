package smtp_test

import (
	"testing"

	"imap-jmap/jmap/memory"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC3461_SMTPDSN tests RFC 3461 SMTP Service Extension for Delivery Status Notifications.
func TestRFC3461_SMTPDSN(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()

	backend := jmapsmtp.NewReceiverBackend(memBackend, memBlobBackend, nil)
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	err = sess.Mail("sender@example.com", nil)
	if err != nil {
		t.Errorf("Mail failed per RFC 3461: %v", err)
	}
}
