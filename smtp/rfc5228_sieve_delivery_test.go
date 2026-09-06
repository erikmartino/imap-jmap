package smtp_test

import (
	"context"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/testmock"
	jmapsmtp "imap-jmap/smtp"
)

type sieveTestOutboundSender struct {
	mu   sync.Mutex
	sent []struct {
		from       string
		recipients []string
		data       []byte
	}
}

func (s *sieveTestOutboundSender) SendMail(ctx context.Context, from string, recipients []string, rawMessage []byte) map[string]jmap.OutboundDeliveryResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, struct {
		from       string
		recipients []string
		data       []byte
	}{
		from:       from,
		recipients: recipients,
		data:       rawMessage,
	})
	res := make(map[string]jmap.OutboundDeliveryResult, len(recipients))
	for _, r := range recipients {
		res[r] = jmap.OutboundDeliveryResult{Delivered: true, SmtpReply: "250 2.0.0 OK"}
	}
	return res
}

func setupSieveDeliveryServer(t *testing.T) (backend *imapsmtp.IMAPSMTPBackend, sieveBackend *testmock.MemorySieveBackend, outbound *sieveTestOutboundSender, addr string, cleanup func()) {
	t.Helper()
	embeddedBackend, imapCleanup := imapsmtp.NewEmbeddedBackend("alice@example.com", "bob@example.com")
	sieveBackend = testmock.NewMemorySieveBackend()
	outbound = &sieveTestOutboundSender{}
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		imapCleanup()
		t.Fatalf("listen: %v", err)
	}
	addr = l.Addr().String()
	_ = l.Close()

	srv := jmapsmtp.NewServer(addr, embeddedBackend, embeddedBackend, nil,
		jmapsmtp.WithAccountResolver(resolver),
		jmapsmtp.WithSieveBackend(sieveBackend),
		jmapsmtp.WithOutboundSender(outbound),
	)
	go func() { _ = srv.ListenAndServe() }()
	time.Sleep(50 * time.Millisecond)

	cleanup = func() {
		srv.Close()
		imapCleanup()
	}
	return embeddedBackend, sieveBackend, outbound, addr, cleanup
}

// TestRFC5228_SieveFileinto verifies that fileinto directs the incoming message
// into the specified folder and cancels implicit keep in INBOX (RFC 5228 Section 4.1).
func TestRFC5228_SieveFileinto(t *testing.T) {
	backend, sieveBackend, _, addr, cleanup := setupSieveDeliveryServer(t)
	defer cleanup()

	bobID := jmap.AccountIDForSubject("bob@example.com")
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)

	// Create an active Sieve script for bob
	script := `require ["fileinto"];
if header :contains "subject" "Receipt" {
    fileinto "Receipts";
}
`
	_, err := sieveBackend.CreateSieveScript(bobCtx, &jmap.SieveScript{
		Name:     "sort-receipts",
		IsActive: true,
		Content:  script,
	})
	if err != nil {
		t.Fatalf("CreateSieveScript failed: %v", err)
	}

	const sender = "alice@example.com"
	const recipient = "bob@example.com"
	msg := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: Your Payment Receipt #1234\r\n" +
		"Message-ID: <sieve-fileinto-1@example.com>\r\n" +
		"\r\n" +
		"Here is your receipt.\r\n")

	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msg); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}

	// Verify message arrived in Receipts and not in Inbox
	var delivered *jmap.Email
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		emails, _ := backend.GetAllEmails(bobCtx)
		for _, em := range emails {
			if strings.Contains(em.Subject, "Receipt") {
				delivered = em
				break
			}
		}
		if delivered != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if delivered == nil {
		t.Fatalf("message was not delivered to bob")
	}

	receiptsID := jmap.MailboxIDByName(bobCtx, backend, "Receipts")
	if receiptsID == "" {
		t.Fatalf("Receipts mailbox was not created")
	}
	if !delivered.MailboxIDs[receiptsID] {
		t.Errorf("expected email to be filed in Receipts mailbox %s, got mailboxes: %v", receiptsID, delivered.MailboxIDs)
	}
	inboxID := jmap.InboxMailboxID(bobCtx, backend)
	if delivered.MailboxIDs[inboxID] {
		t.Errorf("fileinto MUST cancel implicit keep in Inbox; email was also in Inbox")
	}
}

// TestRFC5228_SieveDiscard verifies that discard quietly drops the incoming message
// without an error reply and without storing it (RFC 5228 Section 4.3).
func TestRFC5228_SieveDiscard(t *testing.T) {
	backend, sieveBackend, _, addr, cleanup := setupSieveDeliveryServer(t)
	defer cleanup()

	bobID := jmap.AccountIDForSubject("bob@example.com")
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)

	script := `if header :contains "subject" "SpamMessage" {
    discard;
}
`
	_, err := sieveBackend.CreateSieveScript(bobCtx, &jmap.SieveScript{
		Name:     "discard-spam",
		IsActive: true,
		Content:  script,
	})
	if err != nil {
		t.Fatalf("CreateSieveScript failed: %v", err)
	}

	const sender = "alice@example.com"
	const recipient = "bob@example.com"
	msg := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: SpamMessage Buy Now\r\n" +
		"Message-ID: <sieve-discard-1@example.com>\r\n" +
		"\r\n" +
		"Buy cheap medications.\r\n")

	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msg); err != nil {
		t.Fatalf("discard action must return 250 OK to client, but got error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	emails, _ := backend.GetAllEmails(bobCtx)
	for _, em := range emails {
		if strings.Contains(em.Subject, "SpamMessage") {
			t.Fatalf("expected message to be silently dropped by discard, but found in bob's mail: %v", em.Subject)
		}
	}
}

// TestRFC5228_SieveRedirect verifies that redirect forwards the incoming message
// via OutboundSender and cancels implicit keep in recipient's Inbox (RFC 5228 Section 4.2).
func TestRFC5228_SieveRedirect(t *testing.T) {
	backend, sieveBackend, outbound, addr, cleanup := setupSieveDeliveryServer(t)
	defer cleanup()

	bobID := jmap.AccountIDForSubject("bob@example.com")
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)

	script := `if header :contains "subject" "ForwardExternal" {
    redirect "external-dest@otherdomain.com";
}
`
	_, err := sieveBackend.CreateSieveScript(bobCtx, &jmap.SieveScript{
		Name:     "forward-external",
		IsActive: true,
		Content:  script,
	})
	if err != nil {
		t.Fatalf("CreateSieveScript failed: %v", err)
	}

	const sender = "alice@example.com"
	const recipient = "bob@example.com"
	msg := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: ForwardExternal Urgent Notice\r\n" +
		"Message-ID: <sieve-redirect-1@example.com>\r\n" +
		"\r\n" +
		"Forward me away.\r\n")

	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msg); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	outbound.mu.Lock()
	sentCount := len(outbound.sent)
	var forwardedRecipients []string
	if sentCount > 0 {
		forwardedRecipients = outbound.sent[0].recipients
	}
	outbound.mu.Unlock()

	if sentCount == 0 {
		t.Fatalf("expected redirect message to be dispatched via OutboundSender, but none sent")
	}
	if len(forwardedRecipients) != 1 || forwardedRecipients[0] != "external-dest@otherdomain.com" {
		t.Errorf("expected forwarded recipient to be external-dest@otherdomain.com, got %v", forwardedRecipients)
	}

	// Sieve redirect without explicit keep cancels implicit keep (RFC 5228 §4.2)
	emails, _ := backend.GetAllEmails(bobCtx)
	for _, em := range emails {
		if strings.Contains(em.Subject, "ForwardExternal") {
			t.Errorf("redirect MUST cancel implicit keep in bob's Inbox, but message was found: %v", em.Subject)
		}
	}
}

// TestRFC5228_SieveReject verifies that reject returns a permanent 550 SMTP rejection
// (RFC 5429 Section 2.1).
func TestRFC5228_SieveReject(t *testing.T) {
	_, sieveBackend, _, addr, cleanup := setupSieveDeliveryServer(t)
	defer cleanup()

	bobID := jmap.AccountIDForSubject("bob@example.com")
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)

	script := `require ["reject"];
if header :contains "subject" "BlockedContent" {
    reject "we refuse this blocked content";
}
`
	_, err := sieveBackend.CreateSieveScript(bobCtx, &jmap.SieveScript{
		Name:     "reject-script",
		IsActive: true,
		Content:  script,
	})
	if err != nil {
		t.Fatalf("CreateSieveScript failed: %v", err)
	}

	const sender = "alice@example.com"
	const recipient = "bob@example.com"
	msg := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: BlockedContent should be rejected\r\n" +
		"Message-ID: <sieve-reject-1@example.com>\r\n" +
		"\r\n" +
		"This message should get 550.\r\n")

	err = smtp.SendMail(addr, nil, sender, []string{recipient}, msg)
	if err == nil {
		t.Fatalf("expected 550 rejection from Sieve reject, got success")
	}
	if !strings.Contains(err.Error(), "550") {
		t.Errorf("expected 550 SMTP rejection error, got: %v", err)
	}
}

// TestRFC5228_SieveAddFlag verifies that flags added by Sieve script are applied
// as keywords to the delivered message (RFC 5232 imap4flags).
func TestRFC5228_SieveAddFlag(t *testing.T) {
	backend, sieveBackend, _, addr, cleanup := setupSieveDeliveryServer(t)
	defer cleanup()

	bobID := jmap.AccountIDForSubject("bob@example.com")
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)

	script := `require ["imap4flags"];
if header :contains "subject" "HighPriority" {
    addflag "\\Flagged";
}
`
	_, err := sieveBackend.CreateSieveScript(bobCtx, &jmap.SieveScript{
		Name:     "flag-priority",
		IsActive: true,
		Content:  script,
	})
	if err != nil {
		t.Fatalf("CreateSieveScript failed: %v", err)
	}

	const sender = "alice@example.com"
	const recipient = "bob@example.com"
	msg := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: HighPriority Action Needed\r\n" +
		"Message-ID: <sieve-flags-1@example.com>\r\n" +
		"\r\n" +
		"Please check this urgently.\r\n")

	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msg); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}

	var delivered *jmap.Email
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		emails, _ := backend.GetAllEmails(bobCtx)
		for _, em := range emails {
			if strings.Contains(em.Subject, "HighPriority") {
				delivered = em
				break
			}
		}
		if delivered != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if delivered == nil {
		t.Fatalf("message was not delivered to bob")
	}

	if !delivered.Keywords["$flagged"] {
		t.Errorf("expected message to have $flagged keyword, got keywords: %v", delivered.Keywords)
	}
}
