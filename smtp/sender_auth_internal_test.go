package smtp

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-msgauth/dmarc"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/spectest"
)

// stubVerifier is a SenderVerifier stub for gate tests.
type stubVerifier struct {
	auth   bool
	reason string
	called bool
}

func (s *stubVerifier) Verify(_ context.Context, _ *MessageToVerify) (*SenderAuthResult, error) {
	s.called = true
	return &SenderAuthResult{AuthAuthenticated: s.auth, Reason: s.reason}, nil
}

func newAuthSession(verifier SenderVerifier) *Session {
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()
	calBackend := memory.NewMemoryCalendarsBackend()
	backend := NewReceiverBackend(mailBackend, blobBackend, calBackend)
	backend.SenderVerifier = verifier
	backend.AccountResolver = jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	return &Session{backend: backend}
}

func TestCheckSenderAuth_NoVerifierDevelopmentMode(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Authentication gate: without a configured SenderVerifier (development mode) the gate is skipped.")
	sess := newAuthSession(nil)
	sess.remoteAddr = "192.0.2.10:54321"
	ok, _ := sess.checkSenderAuth([]byte("message"))
	if !ok {
		t.Fatal("expected dev mode (no verifier) to allow iTIP processing")
	}
}

func TestCheckSenderAuth_LoopbackClientTrusted(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Local trust exception: loopback clients are trusted without SPF/DKIM/DMARC validation so local delivery works.")
	deny := &stubVerifier{auth: false, reason: "no authentication"}
	sess := newAuthSession(deny)
	sess.remoteAddr = "127.0.0.1:54321"
	ok, reason := sess.checkSenderAuth([]byte("message"))
	if !ok {
		t.Fatalf("expected loopback client to be trusted, got: %s", reason)
	}
	if deny.called {
		t.Fatal("verifier must not be consulted for loopback clients")
	}
}

func TestCheckSenderAuth_LocalAccountSenderTrusted(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Local trust exception: envelope senders that are local accounts of this server are trusted without DNS validation.")
	deny := &stubVerifier{auth: false, reason: "no authentication"}
	sess := newAuthSession(deny)
	sess.remoteAddr = "192.0.2.10:54321"
	sess.from = "alice@example.com"
	ok, reason := sess.checkSenderAuth([]byte("message"))
	if !ok {
		t.Fatalf("expected local-account sender to be trusted, got: %s", reason)
	}
	if deny.called {
		t.Fatal("verifier must not be consulted for local-account senders")
	}
}

func TestCheckSenderAuth_ExternalSenderVerified(t *testing.T) {
	spectest.Require(t, "RFC6047", "3", spectest.MUST,
		"the originator of an iCalendar object must be authenticated by a recipient.")
	ok := &stubVerifier{auth: true, reason: "DMARC pass"}
	sess := newAuthSession(ok)
	sess.remoteAddr = "192.0.2.10:54321"
	sess.from = "bob@external.org"
	if got, _ := sess.checkSenderAuth([]byte("message")); !got {
		t.Fatal("expected authenticated external sender to pass the gate")
	}
	if !ok.called {
		t.Fatal("verifier must be consulted for external senders")
	}

	deny := &stubVerifier{auth: false, reason: "DMARC fail"}
	sess = newAuthSession(deny)
	sess.remoteAddr = "192.0.2.10:54321"
	sess.from = "bob@external.org"
	if got, reason := sess.checkSenderAuth([]byte("message")); got {
		t.Fatalf("expected unauthenticated external sender to be rejected, got: %s", reason)
	}
}

func TestCheckSenderAuth_VerifierErrorFailsClosed(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Unauthenticated messages may not be trusted: verification errors fail closed.")
	sess := newAuthSession(errorVerifier{})
	sess.remoteAddr = "192.0.2.10:54321"
	sess.from = "bob@external.org"
	if got, reason := sess.checkSenderAuth([]byte("message")); got {
		t.Fatalf("expected verifier error to fail closed, got: %s", reason)
	}
}

type errorVerifier struct{}

func (errorVerifier) Verify(context.Context, *MessageToVerify) (*SenderAuthResult, error) {
	return nil, errors.New("DNS failure")
}

func TestAligned_StrictAndRelaxedModes(t *testing.T) {
	spectest.Require(t, "RFC7489", "3.1", spectest.MUST,
		"Identifier Alignment: strict mode requires an exact FQDN match; relaxed mode compares Organizational Domains.")
	cases := []struct {
		from, auth string
		mode       dmarc.AlignmentMode
		want       bool
	}{
		{"example.com", "example.com", dmarc.AlignmentStrict, true},
		{"example.com", "example.com", dmarc.AlignmentRelaxed, true},
		{"alerts@news.example.com", "example.com", dmarc.AlignmentRelaxed, true},
		{"alerts@news.example.com", "example.com", dmarc.AlignmentStrict, false},
		{"example.com", "evil.com", dmarc.AlignmentRelaxed, false},
		{"example.com", "example.com.evil.com", dmarc.AlignmentRelaxed, false},
		{"", "example.com", dmarc.AlignmentRelaxed, false},
		{"example.com", "", dmarc.AlignmentRelaxed, false},
	}
	for _, c := range cases {
		if got := aligned(c.from, c.auth, c.mode); got != c.want {
			t.Errorf("aligned(%q, %q, %v) = %v, want %v", c.from, c.auth, c.mode, got, c.want)
		}
	}
}

func TestOrganizationalDomain_Heuristic(t *testing.T) {
	spectest.Require(t, "RFC7489", "3.2", spectest.MUST,
		"Organizational Domain heuristic: the registered domain is the last two DNS labels.")
	cases := map[string]string{
		"example.com":          "example.com",
		"a.b.c.d.example.com":  "example.com",
		"example.com.":         "example.com",
		"EXAMPLE.COM":          "example.com",
		"sub.attacker.example": "attacker.example",
		"example.co.uk":        "co.uk", // last-two-labels heuristic; a PSL-backed implementation may be substituted (RFC 7489 §3.2)
	}
	for in, want := range cases {
		if got := organizationalDomain(in); got != want {
			t.Errorf("organizationalDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractFromDomain_FailClosedCases(t *testing.T) {
	spectest.Require(t, "RFC7489", "6.6.1", spectest.MUST,
		"Extract Author Domain: messages without a usable RFC5322.From domain are not processed.")
	valid := "From: Alice <alice@example.com>\r\nTo: bob@example.com\r\nSubject: x\r\n\r\nbody\r\n"
	if d, err := extractFromDomain([]byte(valid)); err != nil || d != "example.com" {
		t.Fatalf("expected example.com, got %q (err %v)", d, err)
	}
	bad := []string{
		"To: bob@example.com\r\n\r\nbody\r\n",                                 // no From
		"From: Alice <alice@example.com>, Bob <bob@example.com>\r\n\r\nx\r\n", // multiple addresses
		"From: =?utf-8?q?broken?=\r\n\r\nbody\r\n",                            // unparseable
		"From: <>\r\n\r\nbody\r\n",                                            // no address
	}
	for _, raw := range bad {
		if d, err := extractFromDomain([]byte(raw)); err == nil {
			t.Errorf("expected error for %q, got domain %q", strings.ReplaceAll(raw, "\r\n", " | "), d)
		}
	}
}

func TestVerify_FailClosedWithEmptyDNS(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Unauthenticated messages may not be trusted: no SPF/DKIM/DMARC records means not authenticated.")
	verifier := NewSPFDKIMDMARCVerifier(emptyDNS{})
	msg := []byte("From: Eve <eve@example.com>\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: Re: meeting\r\n" +
		"Content-Type: text/calendar; method=REPLY\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\nMETHOD:REPLY\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:u1@example.com\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	res, err := verifier.Verify(context.Background(), &MessageToVerify{
		RawMessage:   msg,
		EnvelopeFrom: "eve@example.com",
		ClientIP:     net.ParseIP("192.0.2.10"),
		HeloName:     "smtp.evil.example",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.AuthAuthenticated {
		t.Fatalf("expected fail-closed for unauthenticated sender, got: %s", res.Reason)
	}
	if !strings.Contains(res.Reason, "SPF=none") {
		t.Errorf("reason should report SPF=none, got: %s", res.Reason)
	}
}

// emptyDNS resolves nothing: every lookup is an NXDOMAIN answer.
type emptyDNS struct{}

func (emptyDNS) LookupTXT(context.Context, string) ([]string, error) {
	return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
}
func (emptyDNS) LookupHost(context.Context, string) ([]net.IP, error) {
	return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
}
func (emptyDNS) LookupMX(context.Context, string) ([]*net.MX, error) {
	return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
}
func (emptyDNS) LookupAddr(context.Context, string) ([]string, error) {
	return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
}
