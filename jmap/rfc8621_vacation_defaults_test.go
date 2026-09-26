package jmap_test

import (
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/spectest"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC8621_VacationResponse_DefaultsGenerated verifies the RFC 8621 Section 8 SHOULD
// clauses for a VacationResponse with no subject and no body: the server sets an
// appropriate subject and generates a default body for the automatic reply.
func TestRFC8621_VacationResponse_DefaultsGenerated(t *testing.T) {
	spectest.Require(t, "RFC8621", "8", spectest.SHOULD,
		"appropriate subject SHOULD be set by the server")
	spectest.Require(t, "RFC8621", "8", spectest.SHOULD,
		"default body SHOULD be generated for responses by the server")

	srv := newTestServer()
	if _, err := srv.MailBackend.UpdateVacationResponse(seedCtx(), map[string]any{"isEnabled": true}); err != nil {
		t.Fatalf("UpdateVacationResponse: %v", err)
	}

	mock := &mockOutboundSender{}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	smtpServer := jmapsmtp.NewServer(addr, srv.MailBackend, srv.BlobBackend, nil,
		jmapsmtp.WithAccountResolver(jmapauth.PrimaryDomainResolver{PrimaryDomain: "example.com"}),
		jmapsmtp.WithOutboundSender(mock),
	)
	go func() { _ = smtpServer.ListenAndServe() }()
	defer smtpServer.Close()
	time.Sleep(100 * time.Millisecond)

	msg := []byte("From: Ext <ext@external.org>\r\nTo: User <user@example.com>\r\nSubject: Ping\r\n\r\nHello\r\n")
	if err := smtp.SendMail(addr, nil, "ext@external.org", []string{"user@example.com"}, msg); err != nil {
		t.Fatalf("smtp.SendMail: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if !mock.called {
		t.Fatalf("expected an outbound vacation auto-reply")
	}
	raw := string(mock.rawBytes)
	if !strings.Contains(raw, "Subject: ") || strings.Contains(raw, "Subject: \r\n") {
		t.Errorf("expected a server-generated subject, got:\n%s", raw)
	}
	headerEnd := strings.Index(raw, "\r\n\r\n")
	if headerEnd < 0 || strings.TrimSpace(raw[headerEnd+4:]) == "" {
		t.Errorf("expected a default body when both textBody and htmlBody are null, got:\n%s", raw)
	}
}
