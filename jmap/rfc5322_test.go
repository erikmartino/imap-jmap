package jmap_test

import (
	"testing"

	"imap-jmap/smtp"
)

// TestRFC5322_MessageParsing tests Internet Message Format header & body parsing per RFC 5322.
func TestRFC5322_MessageParsing(t *testing.T) {
	rawMsg := []byte("From: Alice <alice@example.com>\r\n" +
		"To: Bob <bob@example.com>\r\n" +
		"Subject: RFC 5322 Test\r\n" +
		"Date: Sun, 02 Aug 2026 18:00:00 +0000\r\n" +
		"Message-ID: <msg-5322-1@example.com>\r\n" +
		"\r\n" +
		"Hello Bob,\r\nThis is a standard RFC 5322 internet message.\r\n")

	email, err := smtp.ParseMessageToEmail(rawMsg, "blob-5322")
	if err != nil {
		t.Fatalf("ParseMessageToEmail failed: %v", err)
	}

	if email.Subject != "RFC 5322 Test" {
		t.Errorf("Expected Subject 'RFC 5322 Test', got %q", email.Subject)
	}

	if len(email.From) == 0 || email.From[0].Email != "alice@example.com" {
		t.Errorf("Expected From email 'alice@example.com', got %v", email.From)
	}

	if len(email.To) == 0 || email.To[0].Email != "bob@example.com" {
		t.Errorf("Expected To email 'bob@example.com', got %v", email.To)
	}

	if email.BlobID != "blob-5322" {
		t.Errorf("Expected BlobID 'blob-5322', got %q", email.BlobID)
	}
}
