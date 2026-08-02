package jmap_test

import (
	"strings"
	"testing"

	"imap-jmap/smtp"
)

// TestRFC2045_MIMEPartStructure tests RFC 2045 MIME part structure & content type handling.
func TestRFC2045_MIMEPartStructure(t *testing.T) {
	mimeMsg := []byte("From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: RFC 2045 MIME Test\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 7bit\r\n" +
		"\r\n" +
		"This is a UTF-8 encoded text body.\r\n")

	email, err := smtp.ParseMessageToEmail(mimeMsg, "blob-2045")
	if err != nil {
		t.Fatalf("ParseMessageToEmail failed: %v", err)
	}

	if len(email.TextBody) > 0 && email.TextBody[0].Type != "text/plain" {
		t.Errorf("Expected TextBody Type 'text/plain', got %q", email.TextBody[0].Type)
	}

	val, ok := email.BodyValues["1"]
	if !ok || !strings.Contains(val.Value, "UTF-8 encoded text body") {
		t.Errorf("Expected body value snippet in Email bodyValues, got %v", email.BodyValues)
	}
}
