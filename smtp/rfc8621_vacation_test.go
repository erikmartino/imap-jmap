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
	jmapsmtp "imap-jmap/smtp"
)

type vacationTestOutboundSender struct {
	mu   sync.Mutex
	sent []struct {
		from       string
		recipients []string
		data       []byte
	}
}

func (s *vacationTestOutboundSender) SendMail(ctx context.Context, from string, recipients []string, rawMessage []byte) map[string]jmap.OutboundDeliveryResult {
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

func (s *vacationTestOutboundSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

func (s *vacationTestOutboundSender) get(idx int) (string, []string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx >= len(s.sent) {
		return "", nil, ""
	}
	return s.sent[idx].from, s.sent[idx].recipients, string(s.sent[idx].data)
}

func (s *vacationTestOutboundSender) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = nil
}

func setupVacationServer(t *testing.T) (backend *imapsmtp.IMAPSMTPBackend, outbound *vacationTestOutboundSender, addr string, cleanup func()) {
	t.Helper()
	embeddedBackend, imapCleanup := imapsmtp.NewEmbeddedBackend("alice@example.com", "bob@example.com")
	outbound = &vacationTestOutboundSender{}
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
		jmapsmtp.WithOutboundSender(outbound),
	)
	go func() { _ = srv.ListenAndServe() }()
	time.Sleep(50 * time.Millisecond)

	cleanup = func() {
		srv.Close()
		imapCleanup()
	}
	return embeddedBackend, outbound, addr, cleanup
}

// TestRFC8621_VacationResponse_AutoReplySent tests that an incoming email to a recipient
// with VacationResponse enabled receives an automatic reply per RFC 8621 §8.
func TestRFC8621_VacationResponse_AutoReplySent(t *testing.T) {
	backend, outbound, addr, cleanup := setupVacationServer(t)
	defer cleanup()

	bobID := jmap.AccountIDForSubject("bob@example.com")
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)

	// Enable VacationResponse for bob
	subj := "Out of Office: On Holiday"
	body := "Thank you for reaching out. I am currently away on holiday."
	_, err := backend.UpdateVacationResponse(bobCtx, map[string]any{
		"isEnabled": true,
		"subject":   subj,
		"textBody":  body,
	})
	if err != nil {
		t.Fatalf("UpdateVacationResponse failed: %v", err)
	}

	const sender = "alice@example.com"
	const recipient = "bob@example.com"
	msg := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: Important Project Discussion\r\n" +
		"Message-ID: <alice-project-msg-1@example.com>\r\n" +
		"\r\n" +
		"Can we meet tomorrow?\r\n")

	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msg); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if outbound.count() != 1 {
		t.Fatalf("expected 1 vacation auto-reply sent, got %d", outbound.count())
	}

	from, rcpts, raw := outbound.get(0)
	if from != "bob@example.com" {
		t.Errorf("expected auto-reply from bob@example.com, got %s", from)
	}
	if len(rcpts) != 1 || rcpts[0] != "alice@example.com" {
		t.Errorf("expected auto-reply to alice@example.com, got %v", rcpts)
	}
	if !strings.Contains(raw, "Subject: "+subj) {
		t.Errorf("expected Subject %q in auto-reply, got raw:\n%s", subj, raw)
	}
	if !strings.Contains(raw, "Auto-Submitted: auto-replied") {
		t.Errorf("expected Auto-Submitted: auto-replied header in auto-reply, got raw:\n%s", raw)
	}
	if !strings.Contains(raw, "In-Reply-To: <alice-project-msg-1@example.com>") {
		t.Errorf("expected In-Reply-To header matching sender's Message-ID, got raw:\n%s", raw)
	}
	if !strings.Contains(raw, body) {
		t.Errorf("expected body %q in auto-reply, got raw:\n%s", body, raw)
	}
}

// TestRFC8621_VacationResponse_Disabled tests that no auto-reply is sent when
// VacationResponse is disabled (isEnabled: false).
func TestRFC8621_VacationResponse_Disabled(t *testing.T) {
	backend, outbound, addr, cleanup := setupVacationServer(t)
	defer cleanup()

	bobID := jmap.AccountIDForSubject("bob@example.com")
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)

	_, err := backend.UpdateVacationResponse(bobCtx, map[string]any{
		"isEnabled": false,
		"subject":   "Out of Office",
		"textBody":  "I should not be sent",
	})
	if err != nil {
		t.Fatalf("UpdateVacationResponse failed: %v", err)
	}

	const sender = "alice@example.com"
	const recipient = "bob@example.com"
	msg := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: Hello Bob\r\n" +
		"Message-ID: <msg-disabled-1@example.com>\r\n" +
		"\r\n" +
		"Hello!\r\n")

	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msg); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if outbound.count() != 0 {
		t.Errorf("expected 0 auto-replies when isEnabled: false, got %d", outbound.count())
	}
}

// TestRFC8621_VacationResponse_DateWindow tests that auto-reply respects
// fromDate and toDate boundaries (RFC 8621 §8).
func TestRFC8621_VacationResponse_DateWindow(t *testing.T) {
	backend, outbound, addr, cleanup := setupVacationServer(t)
	defer cleanup()

	bobID := jmap.AccountIDForSubject("bob@example.com")
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)

	pastFrom := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	pastTo := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)

	// Past window (expired)
	_, _ = backend.UpdateVacationResponse(bobCtx, map[string]any{
		"isEnabled": true,
		"fromDate":  pastFrom,
		"toDate":    pastTo,
		"textBody":  "I was away yesterday",
	})

	const sender = "alice@example.com"
	const recipient = "bob@example.com"
	msgExpired := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: Hello after vacation\r\n" +
		"Message-ID: <msg-expired-1@example.com>\r\n" +
		"\r\n" +
		"Are you back?\r\n")

	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msgExpired); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if outbound.count() != 0 {
		t.Fatalf("expected 0 auto-replies for expired window, got %d", outbound.count())
	}

	// Future window (not yet started)
	futureFrom := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	futureTo := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	_, _ = backend.UpdateVacationResponse(bobCtx, map[string]any{
		"isEnabled": true,
		"fromDate":  futureFrom,
		"toDate":    futureTo,
		"textBody":  "I will be away tomorrow",
	})

	msgFuture := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: Hello before vacation\r\n" +
		"Message-ID: <msg-future-1@example.com>\r\n" +
		"\r\n" +
		"Before you leave...\r\n")

	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msgFuture); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if outbound.count() != 0 {
		t.Fatalf("expected 0 auto-replies for future window, got %d", outbound.count())
	}

	// Active window (now is between fromDate and toDate)
	activeFrom := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	activeTo := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	_, _ = backend.UpdateVacationResponse(bobCtx, map[string]any{
		"isEnabled": true,
		"fromDate":  activeFrom,
		"toDate":    activeTo,
		"textBody":  "I am currently away",
	})

	msgActive := []byte("From: Alice <" + sender + ">\r\n" +
		"To: Bob <" + recipient + ">\r\n" +
		"Subject: Hello during vacation\r\n" +
		"Message-ID: <msg-active-1@example.com>\r\n" +
		"\r\n" +
		"Checking in...\r\n")

	if err := smtp.SendMail(addr, nil, sender, []string{recipient}, msgActive); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if outbound.count() != 1 {
		t.Fatalf("expected 1 auto-reply for active window, got %d", outbound.count())
	}
}

// TestRFC8621_VacationResponse_AntiLoopPrevention tests loop and bounce prevention
// per RFC 3834 / RFC 5230 (Auto-Submitted, Precedence, List-Id, mailer-daemon).
func TestRFC8621_VacationResponse_AntiLoopPrevention(t *testing.T) {
	backend, outbound, addr, cleanup := setupVacationServer(t)
	defer cleanup()

	bobID := jmap.AccountIDForSubject("bob@example.com")
	bobCtx := jmap.ContextWithAccountID(context.Background(), bobID)

	_, err := backend.UpdateVacationResponse(bobCtx, map[string]any{
		"isEnabled": true,
		"subject":   "Away",
		"textBody":  "Away on trip",
	})
	if err != nil {
		t.Fatalf("UpdateVacationResponse failed: %v", err)
	}

	const recipient = "bob@example.com"

	cases := []struct {
		name       string
		from       string
		headers    string
		shouldSkip bool
	}{
		{
			name:       "Auto-Submitted auto-generated header",
			from:       "alice@example.com",
			headers:    "Auto-Submitted: auto-generated\r\n",
			shouldSkip: true,
		},
		{
			name:       "Auto-Submitted auto-replied header",
			from:       "alice@example.com",
			headers:    "Auto-Submitted: auto-replied\r\n",
			shouldSkip: true,
		},
		{
			name:       "Precedence bulk header",
			from:       "alice@example.com",
			headers:    "Precedence: bulk\r\n",
			shouldSkip: true,
		},
		{
			name:       "Precedence list header",
			from:       "alice@example.com",
			headers:    "Precedence: list\r\n",
			shouldSkip: true,
		},
		{
			name:       "List-Id header",
			from:       "alice@example.com",
			headers:    "List-Id: <announcements.example.com>\r\n",
			shouldSkip: true,
		},
		{
			name:       "List-Unsubscribe header",
			from:       "alice@example.com",
			headers:    "List-Unsubscribe: <mailto:unsub@example.com>\r\n",
			shouldSkip: true,
		},
		{
			name:       "Sender mailer-daemon",
			from:       "mailer-daemon@example.com",
			headers:    "",
			shouldSkip: true,
		},
		{
			name:       "Sender postmaster",
			from:       "postmaster@example.com",
			headers:    "",
			shouldSkip: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outbound.reset()

			msg := []byte("From: " + tc.from + "\r\n" +
				"To: Bob <" + recipient + ">\r\n" +
				"Subject: Test " + tc.name + "\r\n" +
				tc.headers +
				"Message-ID: <loop-check-" + tc.name + "@example.com>\r\n" +
				"\r\n" +
				"Loop check test body\r\n")

			if err := smtp.SendMail(addr, nil, tc.from, []string{recipient}, msg); err != nil {
				t.Fatalf("smtp.SendMail failed: %v", err)
			}
			time.Sleep(80 * time.Millisecond)

			if tc.shouldSkip && outbound.count() > 0 {
				t.Errorf("expected auto-reply to be suppressed for %s, but sent %d message(s)", tc.name, outbound.count())
			}
		})
	}
}
