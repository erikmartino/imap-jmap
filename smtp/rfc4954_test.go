package smtp_test

import (
	"context"
	"io"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/spectest"
	jmapsmtp "imap-jmap/smtp"
)

// startSMTPServer starts a real SMTP server on an ephemeral port and returns
// its address plus the memory backends it stores into. The test waits until
// the listener actually accepts connections.
func startSMTPServer(t *testing.T, opts ...jmapsmtp.Option) (string, *memory.MemoryBackend, *memory.MemoryBlobBackend) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := jmapsmtp.NewServer(addr, mailBackend, blobBackend, calBackend, opts...)
	go func() {
		_ = srv.ListenAndServe()
	}()
	t.Cleanup(func() { _ = srv.Close() })

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("SMTP server did not start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return addr, mailBackend, blobBackend
}

func dialClient(t *testing.T, addr string) *smtp.Client {
	t.Helper()
	client, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("smtp.Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Hello("client.example.org"); err != nil {
		t.Fatalf("EHLO: %v", err)
	}
	return client
}

// plainAuth returns a net/smtp PlainAuth whose host matches the one the
// client dialed, satisfying the net/smtp client's host check.
func plainAuth(addr, user, pass string) smtp.Auth {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return smtp.PlainAuth("", user, pass, host)
}

func storedRawMessage(t *testing.T, mailBackend *memory.MemoryBackend, blobBackend *memory.MemoryBlobBackend, recipient string) string {
	t.Helper()
	ctx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(recipient))
	ids, _, err := mailBackend.QueryEmails(ctx, nil, nil, 0, nil)
	if err != nil {
		t.Fatalf("QueryEmails: %v", err)
	}
	if len(ids) == 0 {
		t.Fatalf("no message stored for %s", recipient)
	}
	emails, _, err := mailBackend.GetEmails(ctx, ids)
	if err != nil {
		t.Fatalf("GetEmails: %v", err)
	}
	blob, ok, err := blobBackend.GetBlob(ctx, jmap.AccountIDForSubject(recipient), string(emails[0].BlobID))
	if err != nil || !ok {
		t.Fatalf("GetBlob: ok=%v err=%v", ok, err)
	}
	return string(blob.Data)
}

func submissionServer(t *testing.T) (string, *memory.MemoryBackend, *memory.MemoryBlobBackend) {
	return startSMTPServer(t,
		jmapsmtp.WithTransportMode(jmapsmtp.TransportModeSubmission),
		jmapsmtp.WithAuthenticator(jmapsmtp.NewAuthBackendAuthenticator(memory.NewMemoryAuthBackend())),
		jmapsmtp.WithAccountResolver(jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}),
	)
}

func TestRFC4954_SMTPAuthOverWire(t *testing.T) {
	spectest.Require(t, "RFC4954", "3", spectest.MUST,
		"The EHLO keyword value associated with this extension is AUTH, and the AUTH EHLO keyword contains as a parameter a space-separated list of the names of available SASL mechanisms.")
	spectest.Require(t, "RFC4954", "4", spectest.MUST,
		"Should the client successfully complete the exchange, the SMTP server issues a 235 reply.")

	addr, _, _ := submissionServer(t)
	client := dialClient(t, addr)

	ok, mechs := client.Extension("AUTH")
	if !ok {
		t.Fatalf("submission server must advertise the AUTH extension")
	}
	if !strings.Contains(mechs, "PLAIN") {
		t.Fatalf("AUTH extension must list PLAIN, got %q", mechs)
	}

	if err := client.Auth(plainAuth(addr, "bob@example.com", "bob@example.com")); err != nil {
		t.Fatalf("AUTH PLAIN with valid credentials: %v", err)
	}
	if err := client.Mail("bob@example.com"); err != nil {
		t.Fatalf("MAIL after authentication: %v", err)
	}
}

func TestRFC4954_AuthFailureOverWire(t *testing.T) {
	spectest.Require(t, "RFC4954", "6", spectest.SHOULD,
		"535 5.7.8 Authentication credentials invalid: the authentication failed due to invalid or insufficient authentication credentials.")

	addr, _, _ := submissionServer(t)
	client := dialClient(t, addr)

	err := client.Auth(plainAuth(addr, "bob@example.com", "wrong-password"))
	if err == nil {
		t.Fatalf("AUTH with wrong credentials must fail")
	}
	var protoErr *textproto.Error
	if !errorsAs(err, &protoErr) || protoErr.Code != 535 {
		t.Fatalf("AUTH failure: got %v, want a 535 reply", err)
	}
	// A failed AUTH must not have authenticated the session: MAIL is still gated.
	if err := client.Mail("bob@example.com"); err == nil {
		t.Fatalf("MAIL after failed AUTH must still be rejected on the submission transport")
	}
}

func TestRFC6409_SubmissionRequiresAuthenticationOverWire(t *testing.T) {
	spectest.Require(t, "RFC6409", "3.1", spectest.MUST,
		"Port 587 is reserved for email message submission as specified in this document. Messages received on this port are defined to be submissions. The protocol used is ESMTP, with additional restrictions or allowances as specified here.")
	spectest.Require(t, "RFC6409", "4.3", spectest.MUST,
		"The MSA MUST, by default, issue an error response to the MAIL command if the session has not been authenticated using SMTP-AUTH, unless it has already independently established authentication or authorization (such as being within a protected subnetwork).")
	spectest.Require(t, "RFC4954", "6", spectest.SHOULD,
		"530 5.7.0 Authentication required: this response SHOULD be returned by any command other than AUTH, EHLO, HELO, NOOP, RSET, or QUIT when server policy requires authentication in order to perform the requested action and authentication is not currently in force.")

	addr, _, _ := submissionServer(t)
	client := dialClient(t, addr)

	err := client.Mail("bob@example.com")
	if err == nil {
		t.Fatalf("unauthenticated MAIL on the submission transport must be rejected")
	}
	var protoErr *textproto.Error
	if !errorsAs(err, &protoErr) || protoErr.Code != 530 {
		t.Fatalf("unauthenticated MAIL: got %v, want a 530 reply", err)
	}
}

func TestRFC6409_SubmissionSenderMustMatchAuthenticatedUserOverWire(t *testing.T) {
	spectest.Require(t, "RFC6409", "6.1", spectest.MAY,
		"The MSA MAY issue an error response to a MAIL command if the address in MAIL FROM appears to have insufficient submission rights or is not authorized with the authentication used (if the session has been authenticated). Reply code 550 with an appropriate enhanced status code, such as 5.7.1, is used for this purpose.")

	addr, _, _ := submissionServer(t)
	client := dialClient(t, addr)
	if err := client.Auth(plainAuth(addr, "bob@example.com", "bob@example.com")); err != nil {
		t.Fatalf("AUTH: %v", err)
	}

	err := client.Mail("alice@external.org")
	if err == nil {
		t.Fatalf("MAIL FROM not matching the authenticated user must be rejected")
	}
	var protoErr *textproto.Error
	if !errorsAs(err, &protoErr) || protoErr.Code != 550 {
		t.Fatalf("MAIL FROM mismatch: got %v, want a 550 reply", err)
	}
	if err := client.Mail("bob@example.com"); err != nil {
		t.Fatalf("MAIL FROM matching the authenticated user: %v", err)
	}
}

func TestRFC6409_SubmissionDeliversMessageWithESMTPAReceived(t *testing.T) {
	spectest.Require(t, "RFC4954", "7", spectest.SHOULD,
		"Upon successful authentication, a server SHOULD use the ESMTPA or the ESMTPSA (when appropriate) keyword in the with clause of the Received header field.")
	spectest.Require(t, "RFC6409", "8.3", spectest.SHOULD,
		"The MSA SHOULD add or replace the Message-ID field, if it lacks it, or it is not valid syntax.")

	addr, mailBackend, blobBackend := submissionServer(t)
	client := dialClient(t, addr)
	if err := client.Auth(plainAuth(addr, "bob@example.com", "bob@example.com")); err != nil {
		t.Fatalf("AUTH: %v", err)
	}
	if err := client.Mail("bob@example.com"); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := client.Rcpt("bob@example.com"); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	// Deliberately no Message-ID header: the MSA must add one (RFC 6409 8.3).
	body := "From: Bob <bob@example.com>\r\n" +
		"To: Bob <bob@example.com>\r\n" +
		"Subject: submission test\r\n" +
		"\r\n" +
		"hello from the submission transport\r\n"
	if _, err := io.WriteString(wc, body); err != nil {
		t.Fatalf("write DATA: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("DATA close: %v", err)
	}

	raw := storedRawMessage(t, mailBackend, blobBackend, "bob@example.com")
	if !strings.Contains(raw, "with ESMTPA id") {
		t.Fatalf("authenticated submission message must carry a Received header with the ESMTPA keyword:\n%s", raw)
	}
	msgID := messageIDFromRaw(t, raw)
	if !strings.HasPrefix(msgID, "<") || !strings.Contains(msgID, "@") || !strings.HasSuffix(msgID, ">") {
		t.Fatalf("submitted message without Message-ID must get one added by the MSA, got %q", msgID)
	}
}

func TestRFC6409_MXServerDoesNotAdvertiseAUTH(t *testing.T) {
	spectest.Require(t, "RFC4954", "3", spectest.MUST,
		"The AUTH extension is appropriate for the submission protocol, not for the unauthenticated inbound MX relay path.")

	addr, _, _ := startSMTPServer(t) // default TransportModeMX, no authenticator

	// The MX receiver must not advertise AUTH: EHLO lists no AUTH keyword.
	client := dialClient(t, addr)
	if ok, _ := client.Extension("AUTH"); ok {
		t.Fatalf("the MX receiver must not advertise AUTH")
	}

	// An AUTH attempt is refused (the mechanism is unsupported on this path).
	authClient := dialClient(t, addr)
	if err := authClient.Auth(plainAuth(addr, "bob@example.com", "bob@example.com")); err == nil {
		t.Fatalf("AUTH on the MX receiver must fail")
	}

	// A fresh connection can still submit unauthenticated mail: the MX
	// transport has no authentication requirement (RFC 6409 Section 3.1).
	mailClient := dialClient(t, addr)
	if err := mailClient.Mail("bob@example.com"); err != nil {
		t.Fatalf("unauthenticated MAIL on the MX transport: %v", err)
	}
}

func TestRFC4954_NoInsecureAuthConfigurationOverWire(t *testing.T) {
	spectest.Require(t, "RFC4954", "9", spectest.MUST,
		"If an implementation supports SASL mechanisms that are vulnerable to passive eavesdropping attacks (such as PLAIN), then the implementation MUST support at least one configuration where these SASL mechanisms are not advertised or used without the presence of an external security layer such as TLS.")

	addr, _, _ := startSMTPServer(t,
		jmapsmtp.WithTransportMode(jmapsmtp.TransportModeSubmission),
		jmapsmtp.WithAuthenticator(jmapsmtp.NewAuthBackendAuthenticator(memory.NewMemoryAuthBackend())),
		jmapsmtp.WithAllowInsecureAuth(false),
	)
	client := dialClient(t, addr)

	if ok, _ := client.Extension("AUTH"); ok {
		t.Fatalf("PLAIN must not be advertised without TLS when insecure AUTH is disabled")
	}
	if err := client.Auth(plainAuth(addr, "bob@example.com", "bob@example.com")); err == nil {
		t.Fatalf("AUTH over plaintext must fail when insecure AUTH is disabled")
	}
}

// errorsAs is a tiny wrapper so this file needs no import cycle with errors.As
// type parameters; it reports whether err can be assigned to target.
func errorsAs(err error, target **textproto.Error) bool {
	te, ok := err.(*textproto.Error)
	if !ok {
		return false
	}
	*target = te
	return true
}

func messageIDFromRaw(t *testing.T, raw string) string {
	t.Helper()
	for _, line := range strings.Split(raw, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "message-id:") {
			return strings.TrimSpace(strings.TrimPrefix(line[11:], " "))
		}
	}
	t.Fatalf("no Message-ID header found in:\n%s", raw)
	return ""
}
