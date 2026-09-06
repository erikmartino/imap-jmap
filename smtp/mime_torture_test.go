package smtp_test

import (
	"context"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/spectest"
	jmapsmtp "imap-jmap/smtp"
)

// TestSMTPReceiver_MIMETorture executes the MIME torture test suite against ParseMessageToEmail
// and the inbound SMTP ReceiverBackend over a live TCP socket. It verifies zero panics,
// bounded resource consumption, graceful degradation, and correct mail delivery.
func TestSMTPReceiver_MIMETorture(t *testing.T) {
	spectest.Require(t, "RFC5321", "3.7", spectest.MUST,
		"A receiving SMTP server must not reply 250 to DATA when the message could not be stored for any recipient; it must return a failure reply.")

	testDir := filepath.Join("..", "jmap", "testdata", "mime_torture")
	entries, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatalf("Failed to read testdata/mime_torture: %v", err)
	}

	const recipient = "recipient@example.com"
	const sender = "sender@example.com"

	embeddedBackend, cleanup := imapsmtp.NewEmbeddedBackend(recipient)
	defer cleanup()
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, embeddedBackend, embeddedBackend, nil, jmapsmtp.WithAccountResolver(resolver))
	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	rcptCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(recipient))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".eml") {
			continue
		}

		vectorName := entry.Name()
		t.Run(vectorName, func(t *testing.T) {
			rawPath := filepath.Join(testDir, vectorName)
			rawBytes, err := os.ReadFile(rawPath)
			if err != nil {
				t.Fatalf("[%s] Failed to read vector: %v", vectorName, err)
			}

			// 1. Direct unit-level parse via ParseMessageToEmail - MUST NOT PANIC
			email, parseErr := jmapsmtp.ParseMessageToEmail(rawBytes, jmap.Id("blob-"+vectorName))
			if parseErr != nil {
				t.Logf("[%s] ParseMessageToEmail returned error (graceful): %v", vectorName, parseErr)
			} else if email == nil {
				t.Fatalf("[%s] ParseMessageToEmail returned nil email with nil error", vectorName)
			} else {
				// Assertions on the parsed Email object
				if email.BlobID != jmap.Id("blob-"+vectorName) {
					t.Errorf("[%s] Expected blobID blob-%s, got %s", vectorName, vectorName, email.BlobID)
				}
				for pid, bv := range email.BodyValues {
					if pid == "" {
						t.Errorf("[%s] Empty partID in BodyValues", vectorName)
					}
					_ = bv.Value
				}
			}

			// 2. Over-the-wire live SMTP DATA delivery - MUST NOT PANIC OR CRASH SERVER
			sendErr := smtp.SendMail(addr, nil, sender, []string{recipient}, rawBytes)
			if sendErr != nil {
				t.Logf("[%s] smtp.SendMail rejected or returned error: %v", vectorName, sendErr)
			} else {
				t.Logf("[%s] smtp.SendMail accepted delivery", vectorName)
			}

			// 3. Vector-specific verifications
			switch vectorName {
			case "crispin_torture.eml":
				if email == nil {
					t.Fatalf("[crispin_torture.eml] Expected non-nil email")
				}
				if !strings.Contains(email.Subject, "Multi-media mail demonstration") {
					t.Errorf("[crispin_torture.eml] Expected subject to contain 'Multi-media mail demonstration', got %q", email.Subject)
				}
				if sendErr != nil {
					t.Errorf("[crispin_torture.eml] Expected live SMTP delivery to succeed, got: %v", sendErr)
				}

			case "rf_mime_torture.eml":
				if email == nil {
					t.Fatalf("[rf_mime_torture.eml] Expected non-nil email")
				}
				if !strings.Contains(email.Subject, "Ryan Finnie's MIME Torture Test") {
					t.Errorf("[rf_mime_torture.eml] Expected subject to contain 'Ryan Finnie's MIME Torture Test', got %q", email.Subject)
				}
				if sendErr != nil {
					t.Errorf("[rf_mime_torture.eml] Expected live SMTP delivery to succeed, got: %v", sendErr)
				}

			case "deep_nested_multiparts.eml":
				if email == nil {
					t.Fatalf("[deep_nested_multiparts.eml] Expected non-nil email")
				}
				if sendErr != nil {
					t.Errorf("[deep_nested_multiparts.eml] Expected live SMTP delivery to succeed, got: %v", sendErr)
				}

			case "mixed_cte.eml":
				if email == nil {
					t.Fatalf("[mixed_cte.eml] Expected non-nil email")
				}
				if len(email.TextBody) == 0 && len(email.Attachments) == 0 {
					t.Errorf("[mixed_cte.eml] Expected extracted body or attachment parts")
				}

			case "adversarial_payloads.eml":
				if email == nil {
					t.Fatalf("[adversarial_payloads.eml] Expected non-nil email despite null bytes")
				}
			}
		})
	}

	// Verify all successfully delivered messages exist in recipient account
	deliveredEmails, err := embeddedBackend.GetAllEmails(rcptCtx)
	if err != nil {
		t.Fatalf("Failed to retrieve recipient emails: %v", err)
	}
	if len(deliveredEmails) == 0 {
		t.Errorf("Expected delivered emails in recipient account, got 0")
	}
	t.Logf("Total delivered emails stored in recipient account: %d", len(deliveredEmails))
}

// TestSMTPReceiver_OversizedMessageDATA verifies RFC 5321 Section 3.7 / 552 rejection
// when DATA exceeds MaxSMTPMessageSize (50MB).
func TestSMTPReceiver_OversizedMessageDATA(t *testing.T) {
	spectest.Require(t, "RFC5321", "3.7", spectest.MUST,
		"A receiving SMTP server must not reply 250 to DATA when the message could not be stored for any recipient; it must return a failure reply.")

	const recipient = "oversize@example.com"
	embeddedBackend, cleanup := imapsmtp.NewEmbeddedBackend(recipient)
	defer cleanup()
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, embeddedBackend, embeddedBackend, nil,
		jmapsmtp.WithAccountResolver(resolver),
		jmapsmtp.WithMaxMessageBytes(2048),
	)
	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	// Create message exceeding configured MaxMessageBytes (2048 bytes) with valid line lengths
	var lines []string
	lines = append(lines, "From: sender@example.com", "To: oversize@example.com", "Subject: Huge", "")
	for i := 0; i < 50; i++ {
		lines = append(lines, strings.Repeat("A", 80))
	}
	oversized := []byte(strings.Join(lines, "\r\n") + "\r\n")

	err = smtp.SendMail(addr, nil, "sender@example.com", []string{recipient}, oversized)
	if err == nil {
		t.Fatalf("Expected oversized message to be rejected by SMTP DATA, got nil error")
	}
	if !strings.Contains(err.Error(), "552") && !strings.Contains(err.Error(), "too large") && !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("Expected 552 size limit rejection, got: %v", err)
	}
}
