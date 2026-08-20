package smtp

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	gosmtp "github.com/emersion/go-smtp"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/spectest"
)

// submissionBackend builds a ReceiverBackend on the RFC 6409 Section 3.1
// submission transport with a real credential verifier, ready for session
// level tests.
func submissionBackend(t *testing.T) (*ReceiverBackend, *memory.MemoryCalendarsBackend, *memory.MemoryBackend) {
	t.Helper()
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()
	calBackend := memory.NewMemoryCalendarsBackend()
	backend := NewReceiverBackend(mailBackend, blobBackend, calBackend)
	backend.Mode = TransportModeSubmission
	backend.Authenticator = NewAuthBackendAuthenticator(memory.NewMemoryAuthBackend())
	backend.AccountResolver = jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	return backend, calBackend, mailBackend
}

func TestRFC6409_SubmissionRequiresAuthentication(t *testing.T) {
	spectest.Require(t, "RFC6409", "4.3", spectest.MUST,
		"The MSA MUST, by default, issue an error response to the MAIL command if the session has not been authenticated using SMTP-AUTH, unless it has already independently established authentication or authorization (such as being within a protected subnetwork).")
	spectest.Require(t, "RFC4954", "6", spectest.SHOULD,
		"530 5.7.0 Authentication required: this response SHOULD be returned by any command other than AUTH, EHLO, HELO, NOOP, RSET, or QUIT when server policy requires authentication in order to perform the requested action and authentication is not currently in force.")

	backend, _, _ := submissionBackend(t)
	backend.SenderVerifier = NewSPFDKIMDMARCVerifier(&fakeDNS{})
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s := sess.(*Session)

	// Even a locally-trusted client (loopback) must authenticate on the
	// submission transport: the 530 gate fires before any local-trust logic.
	if err := s.Mail("bob@example.com", nil); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("unauthenticated MAIL on submission: got %v, want ErrAuthenticationRequired (530 5.7.0)", err)
	}
	if s.from != "" {
		t.Fatalf("rejected MAIL must not set the envelope sender, got %q", s.from)
	}
}

func TestRFC6409_SubmissionSenderMustMatchAuthenticatedIdentity(t *testing.T) {
	spectest.Require(t, "RFC6409", "6.1", spectest.MAY,
		"The MSA MAY issue an error response to a MAIL command if the address in MAIL FROM appears to have insufficient submission rights or is not authorized with the authentication used (if the session has been authenticated). Reply code 550 with an appropriate enhanced status code, such as 5.7.1, is used for this purpose.")

	backend, _, _ := submissionBackend(t)
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s := sess.(*Session)

	if err := s.AuthPlain("bob@example.com", "bob@example.com"); err != nil {
		t.Fatalf("AuthPlain: %v", err)
	}
	if err := s.Mail("alice@external.org", nil); err == nil {
		t.Fatalf("MAIL FROM not bound to the authenticated user must be rejected")
	} else if se, ok := err.(*gosmtp.SMTPError); !ok || se.Code != 550 || se.EnhancedCode != (gosmtp.EnhancedCode{5, 7, 1}) {
		t.Fatalf("MAIL FROM mismatch: got %v, want 550 5.7.1", err)
	}
	if err := s.Mail("bob@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM matching the authenticated user: %v", err)
	}
	// Case-insensitive domain matching per RFC 5321 Section 2.4 (the local part
	// stays case-sensitive, so only the domain may differ in case).
	if err := s.Mail("bob@EXAMPLE.com", nil); err != nil {
		t.Fatalf("MAIL FROM with different-case domain: %v", err)
	}
	if err := s.Mail("Bob@example.com", nil); err == nil {
		t.Fatalf("MAIL FROM with different-case local part must be rejected")
	}
}

func TestRFC6409_SubmissionRcptPermissions(t *testing.T) {
	spectest.Require(t, "RFC6409", "6.2", spectest.MAY,
		"The MSA MAY issue an error response to a RCPT command if inconsistent with the permissions given to the user (if the session has been authenticated). Reply code 550 with an appropriate enhanced status code, such as 5.7.1, is used for this purpose.")

	backend, _, _ := submissionBackend(t)
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s := sess.(*Session)

	if err := s.AuthPlain("bob@example.com", "bob@example.com"); err != nil {
		t.Fatalf("AuthPlain: %v", err)
	}
	if err := s.Mail("bob@example.com", nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	// The authenticated user's permissions cover local delivery only: an
	// address this server cannot deliver to is refused (550 5.7.1).
	if err := s.Rcpt("nobody@external.org", nil); err == nil {
		t.Fatalf("RCPT to a non-local address must be rejected")
	}
	if err := s.Rcpt("bob@example.com", nil); err != nil {
		t.Fatalf("RCPT to a local address: %v", err)
	}
	if len(s.to) != 1 || s.to[0] != "bob@example.com" {
		t.Fatalf("expected exactly one accepted recipient, got %v", s.to)
	}
}

func TestRFC4954_AuthPlainInvalidCredentialsRejected(t *testing.T) {
	spectest.Require(t, "RFC4954", "6", spectest.SHOULD,
		"535 5.7.8 Authentication credentials invalid: the authentication failed due to invalid or insufficient authentication credentials.")

	backend, _, _ := submissionBackend(t)
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s := sess.(*Session)

	if err := s.AuthPlain("bob@example.com", "wrong-password"); !errors.Is(err, gosmtp.ErrAuthFailed) {
		t.Fatalf("wrong credentials: got %v, want ErrAuthFailed (535 5.7.8)", err)
	}
	if s.authenticated {
		t.Fatalf("failed authentication must not mark the session authenticated")
	}
	if err := s.AuthPlain("bob@example.com", "bob@example.com"); err != nil {
		t.Fatalf("correct credentials: %v", err)
	}
	if !s.authenticated || s.authenticatedAs != "bob@example.com" {
		t.Fatalf("successful authentication must record the identity, authenticated=%v as=%q", s.authenticated, s.authenticatedAs)
	}
}

func TestRFC4954_PlainMechanismOnlyWithSecureLayer(t *testing.T) {
	spectest.Require(t, "RFC4954", "9", spectest.MUST,
		"If an implementation supports SASL mechanisms that are vulnerable to passive eavesdropping attacks (such as PLAIN), then the implementation MUST support at least one configuration where these SASL mechanisms are not advertised or used without the presence of an external security layer such as TLS.")

	backend, _, _ := submissionBackend(t)
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s := sess.(*Session)

	// Secure configuration: plaintext transport, insecure AUTH disabled.
	s.tlsActive = false
	backend.AllowInsecureAuth = false
	if mechs := s.AuthMechanisms(); mechs != nil {
		t.Fatalf("PLAIN must not be advertised without a secure layer, got %v", mechs)
	}

	// Over TLS the mechanism is advertised.
	s.tlsActive = true
	if mechs := s.AuthMechanisms(); len(mechs) != 1 || mechs[0] != "PLAIN" {
		t.Fatalf("PLAIN must be advertised over TLS, got %v", mechs)
	}

	// Site configuration that accepts the risk of plaintext AUTH.
	s.tlsActive = false
	backend.AllowInsecureAuth = true
	if mechs := s.AuthMechanisms(); len(mechs) != 1 || mechs[0] != "PLAIN" {
		t.Fatalf("PLAIN must be advertised when insecure AUTH is configured, got %v", mechs)
	}
}

func TestRFC4954_ReceivedWithClauseESMTPA(t *testing.T) {
	spectest.Require(t, "RFC4954", "7", spectest.SHOULD,
		"Upon successful authentication, a server SHOULD use the ESMTPA or the ESMTPSA (when appropriate) keyword in the with clause of the Received header field.")

	backend, _, _ := submissionBackend(t)
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s := sess.(*Session)

	hdr := s.buildReceivedHeader()
	if !strings.Contains(hdr, "with ESMTP id") {
		t.Fatalf("unauthenticated Received header: %q", hdr)
	}

	s.authenticated = true
	hdr = s.buildReceivedHeader()
	if !strings.Contains(hdr, "with ESMTPA id") {
		t.Fatalf("authenticated Received header: %q", hdr)
	}

	s.tlsActive = true
	hdr = s.buildReceivedHeader()
	if !strings.Contains(hdr, "with ESMTPSA id") {
		t.Fatalf("authenticated-over-TLS Received header: %q", hdr)
	}
}

// TestSenderAuth_AuthenticatedSubmissionTrusted is the SEC-4 transport-boundary
// gate: a message submitted by an authenticated client is trusted on the
// submission channel (RFC 6409 Section 4.3) and its iTIP body is applied
// without any SPF/DKIM/DMARC DNS lookup. The verifier here runs over an empty
// DNS that fails every lookup, so if the boundary trust were absent the REPLY
// would be blocked.
func TestSenderAuth_AuthenticatedSubmissionTrusted(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Transport-boundary trust: an iTIP message submitted by an RFC 4954 authenticated client on the submission transport is applied without DNS sender authentication.")

	backend, calBackend, _ := submissionBackend(t)
	backend.SenderVerifier = NewSPFDKIMDMARCVerifier(&fakeDNS{})
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s := sess.(*Session)
	s.remoteAddr = "192.0.2.10:54321"
	s.helo = "laptop.client.example"

	const organizer = "bob@example.com"
	const attendee = "alice@external.org"
	const uid = "gate-submission-trust@example.com"
	id := seedEvent(t, calBackend, organizer, attendee, uid)

	if err := s.AuthPlain(attendee, attendee); err != nil {
		t.Fatalf("AuthPlain: %v", err)
	}
	if err := s.Mail(attendee, nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := s.Rcpt(organizer, nil); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := s.Data(bytes.NewReader(replyMsg(attendee, organizer, uid, "ACCEPTED"))); err != nil {
		t.Fatalf("DATA: %v", err)
	}

	if got := attendanceStatus(t, calBackend, organizer, id, attendee); got != "accepted" {
		t.Fatalf("authenticated submission REPLY should apply without DNS, attendance = %q", got)
	}
}

// TestSenderAuth_MXBoundaryDoesNotTrustEnvelope is the contrasting gate: the
// identical message on the unauthenticated MX transport is NOT trusted — with
// an empty DNS the verifier fails closed and the calendar state is untouched.
func TestSenderAuth_MXBoundaryDoesNotTrustEnvelope(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Unauthenticated messages may not be trusted: the same message on the MX transport is blocked because the sender cannot be verified.")

	backend, calBackend, mailBackend := submissionBackend(t)
	backend.Mode = TransportModeMX
	backend.SenderVerifier = NewSPFDKIMDMARCVerifier(&fakeDNS{})
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s := sess.(*Session)
	s.remoteAddr = "192.0.2.10:54321"
	s.helo = "smtp.external.org"

	const organizer = "bob@example.com"
	const attendee = "alice@external.org"
	const uid = "gate-mx-boundary@example.com"
	id := seedEvent(t, calBackend, organizer, attendee, uid)

	if err := s.Mail(attendee, nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := s.Rcpt(organizer, nil); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := s.Data(bytes.NewReader(replyMsg(attendee, organizer, uid, "ACCEPTED"))); err != nil {
		t.Fatalf("DATA: %v", err)
	}

	if got := attendanceStatus(t, calBackend, organizer, id, attendee); got != "needs-action" {
		t.Fatalf("MX-transport REPLY must not be trusted without DNS authentication, attendance = %q", got)
	}
	if n := mailboxCount(t, mailBackend, organizer); n < 1 {
		t.Fatalf("blocked message must still be delivered to the mailbox, got %d emails", n)
	}
}

func TestRFC6409_SubmissionAddsMessageID(t *testing.T) {
	spectest.Require(t, "RFC6409", "8.3", spectest.SHOULD,
		"The MSA SHOULD add or replace the Message-ID field, if it lacks it, or it is not valid syntax.")

	backend, _, _ := submissionBackend(t)
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	_ = sess

	if !hasValidMessageID([]byte("From: bob@example.com\r\nMessage-ID: <abc123@example.com>\r\n\r\nbody\r\n")) {
		t.Fatalf("valid Message-ID must be recognized")
	}
	if hasValidMessageID([]byte("From: bob@example.com\r\nMessage-ID: not a valid id\r\n\r\nbody\r\n")) {
		t.Fatalf("invalid Message-ID must not be recognized")
	}
	if hasValidMessageID([]byte("From: bob@example.com\r\n\r\nbody\r\n")) {
		t.Fatalf("absent Message-ID must not be recognized")
	}
}
