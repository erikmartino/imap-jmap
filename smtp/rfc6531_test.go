package smtp_test

import (
	"testing"

	"imap-jmap/smtp"
)

// TestRFC6531_SMTPUTF8 tests RFC 6531 SMTP Extension for Internationalized Email (UTF-8).
func TestRFC6531_SMTPUTF8(t *testing.T) {
	utf8Msg := []byte("From: User <user@example.com>\r\n" +
		"To: User <user@example.com>\r\n" +
		"Subject: =?UTF-8?B?VVRGLTggU3ViamVjdA==?=\r\n" +
		"\r\n" +
		"Internationalized UTF-8 content: ñ, é, ü, 🥳\r\n")

	email, err := smtp.ParseMessageToEmail(utf8Msg, "blob-utf8")
	if err != nil {
		t.Fatalf("ParseMessageToEmail failed for RFC 6531 UTF-8 email: %v", err)
	}

	if email.Subject != "UTF-8 Subject" {
		t.Errorf("Expected Subject 'UTF-8 Subject', got %q", email.Subject)
	}
}
