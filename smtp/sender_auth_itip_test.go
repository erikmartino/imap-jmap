package smtp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-msgauth/dkim"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/spectest"
)

// fakeDNS is an in-memory DNSResolver for sender-authentication tests. TXT
// records are keyed by the exact query name (e.g. "example.com" for SPF,
// "sec1._domainkey.example.com" for DKIM, "_dmarc.example.com" for DMARC).
// Unknown names resolve to NXDOMAIN; names listed in errs return that error.
type fakeDNS struct {
	txt   map[string][]string
	hosts map[string][]net.IP
	mx    map[string][]*net.MX
	ptr   map[string][]string
	errs  map[string]error
}

func nxdomain(name string) error {
	return &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f *fakeDNS) LookupTXT(_ context.Context, name string) ([]string, error) {
	if err, ok := f.errs[name]; ok {
		return nil, err
	}
	if v, ok := f.txt[name]; ok {
		return v, nil
	}
	return nil, nxdomain(name)
}

func (f *fakeDNS) LookupHost(_ context.Context, name string) ([]net.IP, error) {
	if err, ok := f.errs[name]; ok {
		return nil, err
	}
	if v, ok := f.hosts[name]; ok {
		return v, nil
	}
	return nil, nxdomain(name)
}

func (f *fakeDNS) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	if err, ok := f.errs[name]; ok {
		return nil, err
	}
	if v, ok := f.mx[name]; ok {
		return v, nil
	}
	return nil, nxdomain(name)
}

func (f *fakeDNS) LookupAddr(_ context.Context, name string) ([]string, error) {
	if err, ok := f.errs[name]; ok {
		return nil, err
	}
	if v, ok := f.ptr[name]; ok {
		return v, nil
	}
	return nil, nxdomain(name)
}

// startAuthSession builds a ReceiverBackend with the real SPF/DKIM/DMARC
// verifier over the given DNS, plus real memory backends, and returns a
// session whose peer address is a non-loopback IP so the authentication gate
// is actually exercised (loopback/local-account senders bypass it by design).
func startAuthSession(t *testing.T, dns DNSResolver) (*Session, *memory.MemoryCalendarsBackend, *memory.MemoryBackend) {
	t.Helper()
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()
	calBackend := memory.NewMemoryCalendarsBackend()
	backend := NewReceiverBackend(mailBackend, blobBackend, calBackend)
	backend.SenderVerifier = NewSPFDKIMDMARCVerifier(dns)
	backend.AccountResolver = jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	s := sess.(*Session)
	s.remoteAddr = "192.0.2.10:54321"
	s.helo = "client.external.org"
	return s, calBackend, mailBackend
}

func replyMsg(attendee, organizer, uid, partstat string) []byte {
	return []byte("From: Attendee <" + attendee + ">\r\n" +
		"To: Organizer <" + organizer + ">\r\n" +
		"Subject: Re: meeting\r\n" +
		"Content-Type: text/calendar; method=REPLY; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REPLY\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:" + uid + "\r\n" +
		"SEQUENCE:0\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=" + partstat + ":mailto:" + attendee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")
}

func requestMsg(organizer, invitee, uid string) []byte {
	return []byte("From: Organizer <" + organizer + ">\r\n" +
		"To: Invitee <" + invitee + ">\r\n" +
		"Subject: Invitation\r\n" +
		"Content-Type: text/calendar; method=REQUEST; charset=UTF-8\r\n" +
		"\r\n" +
		"BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REQUEST\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:" + uid + "\r\n" +
		"SEQUENCE:0\r\n" +
		"DTSTART:20260925T100000Z\r\n" +
		"DTEND:20260925T110000Z\r\n" +
		"SUMMARY:New meeting\r\n" +
		"ORGANIZER:mailto:" + organizer + "\r\n" +
		"ATTENDEE;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:" + invitee + "\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")
}

// seedEvent creates an event on Bob's (local) account with an external attendee.
func seedEvent(t *testing.T, calBackend *memory.MemoryCalendarsBackend, organizer, attendee, uid string) jmap.Id {
	t.Helper()
	bobCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(organizer))
	ev, err := calBackend.CreateCalendarEvent(bobCtx, &jmap.CalendarEvent{
		UID:    uid,
		Title:  "Gate Test",
		Start:  "2026-09-25T10:00:00Z",
		Status: "confirmed",
		Participants: map[string]*jmap.JSCalendarParticipant{
			organizer: {Email: organizer, Roles: map[string]bool{"owner": true}},
			attendee:  {Email: attendee, Roles: map[string]bool{"attendee": true}, ParticipationStatus: "needs-action"},
		},
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return ev.ID
}

func attendanceStatus(t *testing.T, calBackend *memory.MemoryCalendarsBackend, organizer string, id jmap.Id, attendee string) string {
	t.Helper()
	bobCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(organizer))
	evs, _, err := calBackend.GetCalendarEvents(bobCtx, []jmap.Id{id})
	if err != nil || len(evs) == 0 {
		t.Fatalf("get event: %v (len=%d)", err, len(evs))
	}
	if p, ok := evs[0].Participants[attendee]; ok {
		return p.ParticipationStatus
	}
	return ""
}

func mailboxCount(t *testing.T, mailBackend *memory.MemoryBackend, recipient string) int {
	t.Helper()
	ctx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(recipient))
	ids, _, err := mailBackend.QueryEmails(ctx, nil, nil, 0, nil)
	if err != nil {
		t.Fatalf("QueryEmails: %v", err)
	}
	return len(ids)
}

func TestRFC6047_SenderAuth_SPFPassAppliesREPLY(t *testing.T) {
	spectest.Require(t, "RFC7208", "2.6.3", spectest.MUST,
		"A 'pass' result is an explicit statement that the client is authorized to inject mail with the given identity.")
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Authenticated sender passes the gate and the iTIP REPLY is applied.")

	dns := &fakeDNS{txt: map[string][]string{
		"external.org": {"v=spf1 ip4:192.0.2.10 -all"},
	}}
	sess, calBackend, _ := startAuthSession(t, dns)
	const organizer = "bob@example.com"
	const attendee = "alice@external.org"
	id := seedEvent(t, calBackend, organizer, attendee, "gate-spf-pass@example.com")

	if err := sess.Mail(attendee, nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := sess.Rcpt(organizer, nil); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := sess.Data(bytes.NewReader(replyMsg(attendee, organizer, "gate-spf-pass@example.com", "ACCEPTED"))); err != nil {
		t.Fatalf("DATA: %v", err)
	}

	if got := attendanceStatus(t, calBackend, organizer, id, attendee); got != "accepted" {
		t.Fatalf("SPF-authenticated REPLY should apply, attendance = %q", got)
	}
}

func TestRFC6047_SenderAuth_SPFFailNoMutation(t *testing.T) {
	spectest.Require(t, "RFC7208", "2.6.4", spectest.MUST,
		"A 'fail' result is an explicit statement that the client is not authorized to use the domain in the given identity.")
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Unauthenticated messages may not be trusted: the message is delivered to the mailbox but never mutates calendar state.")

	dns := &fakeDNS{txt: map[string][]string{
		"external.org": {"v=spf1 ip4:198.51.100.9 -all"},
	}}
	sess, calBackend, mailBackend := startAuthSession(t, dns)
	const organizer = "bob@example.com"
	const attendee = "alice@external.org"
	id := seedEvent(t, calBackend, organizer, attendee, "gate-spf-fail@example.com")

	if err := sess.Mail(attendee, nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := sess.Rcpt(organizer, nil); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := sess.Data(bytes.NewReader(replyMsg(attendee, organizer, "gate-spf-fail@example.com", "ACCEPTED"))); err != nil {
		t.Fatalf("DATA: %v", err)
	}

	if got := attendanceStatus(t, calBackend, organizer, id, attendee); got != "needs-action" {
		t.Fatalf("SPF-failing REPLY must not mutate calendar state, attendance = %q", got)
	}
	if n := mailboxCount(t, mailBackend, organizer); n < 1 {
		t.Fatalf("SPF-failing message must still be delivered to the mailbox, got %d emails", n)
	}
}

func TestRFC6047_SenderAuth_DKIMPassImportsREQUEST(t *testing.T) {
	spectest.Require(t, "RFC6376", "6.1", spectest.MUST,
		"Every DKIM-Signature header field of the message is verified; a verified signature authenticates the message.")
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Authenticated sender passes the gate and the iTIP REQUEST is imported.")

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PublicKey(&priv.PublicKey))
	dns := &fakeDNS{txt: map[string][]string{
		"sec1._domainkey.external.org": {"v=DKIM1; k=rsa; p=" + pubB64},
	}}
	sess, calBackend, _ := startAuthSession(t, dns)
	const organizer = "alice@external.org"
	const invitee = "bob@example.com"
	const uid = "gate-dkim-pass@example.com"

	var signed bytes.Buffer
	if err := dkim.Sign(&signed, bytes.NewReader(requestMsg(organizer, invitee, uid)), &dkim.SignOptions{
		Domain:   "external.org",
		Selector: "sec1",
		Signer:   priv,
	}); err != nil {
		t.Fatalf("dkim.Sign: %v", err)
	}

	if err := sess.Mail(organizer, nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := sess.Rcpt(invitee, nil); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := sess.Data(bytes.NewReader(signed.Bytes())); err != nil {
		t.Fatalf("DATA: %v", err)
	}

	bobCtx := jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(invitee))
	ids, _, err := calBackend.QueryCalendarEvents(bobCtx, map[string]any{"uid": uid}, nil, 0, nil, false)
	if err != nil || len(ids) == 0 {
		t.Fatalf("DKIM-authenticated REQUEST should import the event (err=%v, count=%d)", err, len(ids))
	}
}

func TestRFC6047_SenderAuth_DMARCPolicyRejectBlocks(t *testing.T) {
	spectest.Require(t, "RFC7489", "6.6.2", spectest.MUST,
		"Step 5: if no Authenticated Identifier aligns with the RFC5322.From domain, the message fails the DMARC check.")
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Unauthenticated messages may not be trusted: a DMARC reject policy with no aligned identifier blocks the iTIP message.")

	// Envelope sender authenticates (SPF pass on external.org) but the From
	// header claims attacker.example, which carries p=reject and no aligned
	// identifier (SPF domain and From domain differ, no DKIM).
	dns := &fakeDNS{txt: map[string][]string{
		"external.org":            {"v=spf1 ip4:192.0.2.10 -all"},
		"_dmarc.attacker.example": {"v=DMARC1; p=reject"},
	}}
	sess, calBackend, mailBackend := startAuthSession(t, dns)
	const organizer = "bob@example.com"
	const attendee = "alice@external.org"
	id := seedEvent(t, calBackend, organizer, attendee, "gate-dmarc-reject@example.com")

	// Forged REPLY: envelope sender is a real (SPF-passing) domain, From header
	// is the attacker's domain.
	spoofed := strings.Replace(string(replyMsg(attendee, organizer, "gate-dmarc-reject@example.com", "ACCEPTED")),
		"From: Attendee <"+attendee+">", "From: Eve <eve@attacker.example>", 1)
	if err := sess.Mail(attendee, nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := sess.Rcpt(organizer, nil); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := sess.Data(bytes.NewReader([]byte(spoofed))); err != nil {
		t.Fatalf("DATA: %v", err)
	}

	if got := attendanceStatus(t, calBackend, organizer, id, attendee); got != "needs-action" {
		t.Fatalf("DMARC-reject message with unaligned identifiers must be blocked, attendance = %q", got)
	}
	if n := mailboxCount(t, mailBackend, organizer); n < 1 {
		t.Fatalf("blocked message must still be delivered to the mailbox, got %d emails", n)
	}
}

func TestRFC6047_SenderAuth_DMARCOrgDomainDiscoveryApplies(t *testing.T) {
	spectest.Require(t, "RFC7489", "6.6.3", spectest.MUST,
		"Policy Discovery: when no record exists at the RFC5322.From domain, the DMARC record at the Organizational Domain is used.")
	spectest.Require(t, "RFC7489", "3.1", spectest.MUST,
		"Identifier Alignment: relaxed alignment compares Organizational Domains.")

	// From domain is sub.external.org (no direct DMARC record); the policy is
	// found one level up at _dmarc.external.org (p=reject). SPF passes on the
	// envelope domain external.org, which relaxed-aligns with sub.external.org,
	// so the DMARC check passes and the REPLY is applied.
	dns := &fakeDNS{txt: map[string][]string{
		"sub.external.org":    {"v=spf1 ip4:192.0.2.10 -all"},
		"_dmarc.external.org": {"v=DMARC1; p=reject"},
	}}
	sess, calBackend, _ := startAuthSession(t, dns)
	const organizer = "bob@example.com"
	const attendee = "alice@sub.external.org"
	id := seedEvent(t, calBackend, organizer, attendee, "gate-dmarc-org@example.com")

	if err := sess.Mail(attendee, nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := sess.Rcpt(organizer, nil); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := sess.Data(bytes.NewReader(replyMsg(attendee, organizer, "gate-dmarc-org@example.com", "ACCEPTED"))); err != nil {
		t.Fatalf("DATA: %v", err)
	}

	if got := attendanceStatus(t, calBackend, organizer, id, attendee); got != "accepted" {
		t.Fatalf("DMARC pass (org-domain policy + relaxed alignment) should apply, attendance = %q", got)
	}
}

func TestRFC6047_SenderAuth_NoAuthInfoFailClosed(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Unauthenticated messages may not be trusted: absence of SPF, DKIM and DMARC information fails closed.")

	dns := &fakeDNS{}
	sess, calBackend, mailBackend := startAuthSession(t, dns)
	const organizer = "bob@example.com"
	const attendee = "alice@external.org"
	id := seedEvent(t, calBackend, organizer, attendee, "gate-noauth@example.com")

	if err := sess.Mail(attendee, nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := sess.Rcpt(organizer, nil); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := sess.Data(bytes.NewReader(replyMsg(attendee, organizer, "gate-noauth@example.com", "ACCEPTED"))); err != nil {
		t.Fatalf("DATA: %v", err)
	}

	if got := attendanceStatus(t, calBackend, organizer, id, attendee); got != "needs-action" {
		t.Fatalf("message without authentication info must fail closed, attendance = %q", got)
	}
	if n := mailboxCount(t, mailBackend, organizer); n < 1 {
		t.Fatalf("fail-closed message must still be delivered to the mailbox, got %d emails", n)
	}
}

func TestRFC6047_SenderAuth_DNSTimeoutFailClosed(t *testing.T) {
	spectest.Require(t, "RFC7208", "4.6.4", spectest.MUST,
		"Check Host() when a DNS error occurs: results other than 'domain does not exist' yield 'temperror'.")
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Unauthenticated messages may not be trusted: DNS errors fail closed.")

	dns := &fakeDNS{errs: map[string]error{
		"external.org": &net.DNSError{Err: "i/o timeout", Name: "external.org", IsTimeout: true},
	}}
	sess, calBackend, _ := startAuthSession(t, dns)
	const organizer = "bob@example.com"
	const attendee = "alice@external.org"
	id := seedEvent(t, calBackend, organizer, attendee, "gate-timeout@example.com")

	if err := sess.Mail(attendee, nil); err != nil {
		t.Fatalf("MAIL: %v", err)
	}
	if err := sess.Rcpt(organizer, nil); err != nil {
		t.Fatalf("RCPT: %v", err)
	}
	if err := sess.Data(bytes.NewReader(replyMsg(attendee, organizer, "gate-timeout@example.com", "ACCEPTED"))); err != nil {
		t.Fatalf("DATA: %v", err)
	}

	if got := attendanceStatus(t, calBackend, organizer, id, attendee); got != "needs-action" {
		t.Fatalf("DNS timeout must fail closed, attendance = %q", got)
	}
}

// TestRFC6047_SenderAuth_LocalDeliveryWorksWithoutValidation drives a message
// through the real SMTP server (TCP) from a loopback client and a local
// account: the trust exceptions must let local scheduling work even though
// the DNS has no authentication records at all.
func TestRFC6047_SenderAuth_LocalDeliveryWorksWithoutValidation(t *testing.T) {
	spectest.Require(t, "RFC6047", "2.2.2", spectest.MUST,
		"Trust exceptions: loopback clients and local-account senders need no DNS validation (local delivery works).")

	calBackend := memory.NewMemoryCalendarsBackend()
	mailBackend := memory.NewMemoryBackend()
	blobBackend := memory.NewMemoryBlobBackend()
	resolver := jmap.PrimaryDomainResolver{PrimaryDomain: "example.com"}
	backend := NewReceiverBackend(mailBackend, blobBackend, calBackend, resolver)
	backend.SenderVerifier = NewSPFDKIMDMARCVerifier(&fakeDNS{})

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()

	srv := NewServer(addr, mailBackend, blobBackend, calBackend,
		WithAccountResolver(resolver), WithSenderVerifier(NewSPFDKIMDMARCVerifier(&fakeDNS{})))
	go func() { _ = srv.ListenAndServe() }()
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	const organizer = "bob@example.com"
	const attendee = "alice@example.com"
	id := seedEvent(t, calBackend, organizer, attendee, "gate-local@example.com")

	_ = smtp.SendMail(addr, nil, attendee, []string{organizer},
		replyMsg(attendee, organizer, "gate-local@example.com", "ACCEPTED"))
	time.Sleep(100 * time.Millisecond)

	if got := attendanceStatus(t, calBackend, organizer, id, attendee); got != "accepted" {
		t.Fatalf("loopback local-account REPLY must be applied without validation, attendance = %q", got)
	}
}
