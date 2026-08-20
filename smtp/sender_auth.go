package smtp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"

	"github.com/emersion/go-msgauth/dkim"
	"github.com/emersion/go-msgauth/dmarc"
	"github.com/redsift/spf/v2"
)

// DNSResolver abstracts the DNS lookups used by sender authentication so that
// SPF (RFC 7208), DKIM (RFC 6376), and DMARC (RFC 7489) verification can be
// unit-tested without a live resolver.
type DNSResolver interface {
	// LookupTXT returns the DNS TXT records for the given domain name.
	LookupTXT(ctx context.Context, name string) ([]string, error)
	// LookupHost returns the A/AAAA address records for the given host name.
	LookupHost(ctx context.Context, host string) ([]net.IP, error)
	// LookupMX returns the MX records for the given domain name.
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	// LookupAddr returns the PTR (reverse DNS) names for the given IP address.
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

// SystemDNSResolver resolves via the host's default resolver (net.DefaultResolver).
type SystemDNSResolver struct{}

func (SystemDNSResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

func (SystemDNSResolver) LookupHost(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func (SystemDNSResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return net.DefaultResolver.LookupMX(ctx, name)
}

func (SystemDNSResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	return net.DefaultResolver.LookupAddr(ctx, reverseAddr(addr))
}

// reverseAddr converts an IP address to its in-addr.arpa / ip6.arpa reverse
// DNS form used for PTR lookups (RFC 1035 Section 3.5).
func reverseAddr(addr string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", ip4[3], ip4[2], ip4[1], ip4[0])
	}
	nibbles := make([]string, 0, 32)
	for i := len(ip) - 1; i >= 0; i-- {
		nibbles = append(nibbles, strconv.FormatInt(int64(ip[i]&0x0f), 16), strconv.FormatInt(int64(ip[i]>>4), 16))
	}
	return strings.Join(nibbles, ".") + ".ip6.arpa."
}

// SenderVerifier authenticates the sender of a received message (SPF, DKIM,
// DMARC) before the message is allowed to mutate calendar state via iTIP.
// Verification failures are decisive: a sender that cannot be authenticated
// MUST NOT have its iTIP applied (fail closed, SEC-1).
type SenderVerifier interface {
	// Verify evaluates the message sender's authenticity. The returned
	// SenderAuthResult.AuthAuthenticated reports whether the sender is
	// authenticated; Verify never fails open.
	Verify(ctx context.Context, msg *MessageToVerify) (*SenderAuthResult, error)
}

// MessageToVerify carries the parts of an SMTP transaction needed to evaluate
// the sender's authenticity: the message exactly as received (before the
// server's own Received: trace header is prepended), the envelope MAIL FROM,
// the connecting client IP, and the HELO/EHLO name.
type MessageToVerify struct {
	RawMessage   []byte
	EnvelopeFrom string
	ClientIP     net.IP
	HeloName     string
}

// SenderAuthResult summarizes the SPF / DKIM / DMARC outcome of a verification
// for logging. AuthAuthenticated is the fail-closed decision: it is true only
// when the sender is positively authenticated.
type SenderAuthResult struct {
	AuthAuthenticated bool
	SPF               string
	DKIM              string
	DMARC             string
	Reason            string
}

// SPFDKIMDMARCVerifier implements SenderVerifier by combining SPF (RFC 7208),
// DKIM (RFC 6376), and DMARC (RFC 7489) sender authentication:
//
//   - SPF check_host() is evaluated against the MAIL FROM identity (the HELO
//     identity when MAIL FROM is null, RFC 7208 Section 2.4).
//   - Every DKIM-Signature header is verified (RFC 6376 Section 6.1).
//   - When the RFC5322.From domain (or its Organizational Domain, RFC 7489
//     Section 6.6.3) publishes a DMARC policy, the message passes the DMARC
//     mechanism check only if an aligned SPF or DKIM identifier passed
//     (RFC 7489 Section 6.6.2 step 5).
//
// The sender is authenticated when the DMARC check passes, or — when no DMARC
// policy is published — when SPF or DKIM passes. DNS errors, malformed From
// headers, and any inconclusive outcome fail closed.
type SPFDKIMDMARCVerifier struct {
	resolver DNSResolver
}

// NewSPFDKIMDMARCVerifier returns a verifier using the system DNS resolver, or
// the supplied resolver (for tests).
func NewSPFDKIMDMARCVerifier(resolver ...DNSResolver) *SPFDKIMDMARCVerifier {
	var r DNSResolver = SystemDNSResolver{}
	if len(resolver) > 0 && resolver[0] != nil {
		r = resolver[0]
	}
	return &SPFDKIMDMARCVerifier{resolver: r}
}

// Verify implements SenderVerifier. It never fails open: any error or
// inconclusive outcome yields AuthAuthenticated=false.
func (v *SPFDKIMDMARCVerifier) Verify(ctx context.Context, msg *MessageToVerify) (*SenderAuthResult, error) {
	if msg == nil || len(msg.RawMessage) == 0 {
		return nil, errors.New("sender verification requires a raw message")
	}
	res := &SenderAuthResult{}

	// RFC 7489 Section 6.6.1: the Author Domain is extracted from the
	// RFC5322.From header. Without a single, parseable From address there is no
	// way to determine the DMARC policy or alignment, so the sender cannot be
	// authenticated (fail closed).
	fromDomain, err := extractFromDomain(msg.RawMessage)
	if err != nil {
		res.Reason = fmt.Sprintf("message has no usable RFC5322.From domain (%v); sender not authenticated", err)
		return res, nil
	}

	spfDomain, spfResult := v.evaluateSPF(ctx, msg)
	res.SPF = spfResult
	dkimVerifs := v.evaluateDKIM(ctx, msg.RawMessage)
	res.DKIM = summarizeDKIM(dkimVerifs)

	dmarcFound, dmarcPass, dmarcPolicy := v.evaluateDMARC(ctx, fromDomain, spfDomain, spfResult, dkimVerifs)
	res.DMARC = summarizeDMARC(dmarcFound, dmarcPass, dmarcPolicy)

	spfPass := spfResult == "pass"
	dkimPass := anyDKIMPass(dkimVerifs)

	switch {
	case dmarcFound && dmarcPass:
		res.AuthAuthenticated = true
		res.Reason = fmt.Sprintf("DMARC pass for %s (policy %s)", fromDomain, dmarcPolicy)
	case dmarcFound:
		res.AuthAuthenticated = false
		res.Reason = fmt.Sprintf("DMARC fail for %s: no aligned SPF/DKIM pass (policy %s)", fromDomain, dmarcPolicy)
	case spfPass || dkimPass:
		res.AuthAuthenticated = true
		res.Reason = fmt.Sprintf("SPF=%s and/or DKIM=pass; no DMARC policy published for %s", spfResult, fromDomain)
	default:
		res.AuthAuthenticated = false
		res.Reason = fmt.Sprintf("no authentication: SPF=%s, DKIM=%s, no DMARC policy for %s", spfResult, res.DKIM, fromDomain)
	}
	return res, nil
}

// evaluateSPF runs the RFC 7208 check_host() evaluation against the MAIL FROM
// identity, or the HELO identity when MAIL FROM is null (RFC 7208 Section
// 2.4). It returns the domain the check was evaluated against (used for DMARC
// alignment, RFC 7489 Section 3.1.2) and the RFC 7208 result string.
func (v *SPFDKIMDMARCVerifier) evaluateSPF(ctx context.Context, msg *MessageToVerify) (domain, result string) {
	mailFromDomain := addressDomain(msg.EnvelopeFrom)
	domain, sender := mailFromDomain, msg.EnvelopeFrom
	if domain == "" {
		domain = addressDomain(msg.HeloName)
		sender = msg.HeloName
	}
	if domain == "" || msg.ClientIP == nil {
		return domain, "none"
	}
	opts := []spf.Option{spf.WithResolver(&spfDNSAdapter{resolver: v.resolver})}
	if msg.HeloName != "" {
		opts = append(opts, spf.HeloDomain(addressDomain(msg.HeloName)))
	}
	spfResult, _, _, err := spf.CheckHost(msg.ClientIP, domain, sender, opts...)
	if err != nil && !errors.Is(err, spf.ErrDNSPermerror) {
		return domain, "temperror"
	}
	return domain, spfResult.String()
}

// evaluateDKIM verifies every DKIM-Signature header of the message (RFC 6376
// Section 6.1). A verification result with a nil Err is a passing signature.
func (v *SPFDKIMDMARCVerifier) evaluateDKIM(ctx context.Context, raw []byte) []*dkim.Verification {
	verifs, err := dkim.VerifyWithOptions(bytes.NewReader(raw), &dkim.VerifyOptions{
		LookupTXT: func(name string) ([]string, error) {
			return v.resolver.LookupTXT(ctx, name)
		},
	})
	if err != nil {
		return nil
	}
	return verifs
}

// evaluateDMARC determines whether the message passes the DMARC mechanism
// check for the Author Domain (RFC 7489 Section 6.6.2 step 5): at least one
// aligned SPF or DKIM identifier must pass. It also reports whether a policy
// was found (including via Organizational Domain discovery, RFC 7489 Section
// 6.6.3) and its disposition. A transient DNS error is treated as a DMARC
// failure so the caller fails closed.
func (v *SPFDKIMDMARCVerifier) evaluateDMARC(ctx context.Context, fromDomain, spfDomain, spfResult string, dkims []*dkim.Verification) (found, pass bool, policy string) {
	lookup := func(domain string) (*dmarc.Record, error) {
		return dmarc.LookupWithOptions(domain, &dmarc.LookupOptions{
			LookupTXT: func(name string) ([]string, error) {
				return v.resolver.LookupTXT(ctx, name)
			},
		})
	}
	rec, err := lookup(fromDomain)
	if errors.Is(err, dmarc.ErrNoPolicy) {
		// RFC 7489 Section 6.6.3 step 3: retry at the Organizational Domain
		// level when the exact From domain publishes no policy.
		if org := organizationalDomain(fromDomain); org != "" && org != fromDomain {
			rec, err = lookup(org)
		}
	}
	if errors.Is(err, dmarc.ErrNoPolicy) {
		return false, false, ""
	}
	if err != nil || rec == nil {
		// DNS failure: DMARC state is indeterminate; fail closed.
		return true, false, ""
	}
	policy = string(rec.Policy)
	spfAligned := spfResult == "pass" && aligned(fromDomain, spfDomain, rec.SPFAlignment)
	dkimAligned := false
	for _, verif := range dkims {
		if verif != nil && verif.Err == nil && aligned(fromDomain, verif.Domain, rec.DKIMAlignment) {
			dkimAligned = true
			break
		}
	}
	return true, spfAligned || dkimAligned, policy
}

// extractFromDomain returns the domain of the RFC5322.From header (RFC 7489
// Section 6.6.1). A missing, multiple, or unparseable From header yields an
// error so callers fail closed.
func extractFromDomain(raw []byte) (string, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	from := msg.Header.Get("From")
	if from == "" {
		return "", errors.New("missing From header")
	}
	addr, err := mail.ParseAddress(from)
	if err != nil || addr == nil {
		return "", errors.New("unparseable From header")
	}
	domain := addressDomain(addr.Address)
	if domain == "" {
		return "", errors.New("From address has no domain")
	}
	return strings.ToLower(domain), nil
}

// addressDomain extracts the (lowercased) domain part of an email address,
// returning "" when the address has none (e.g. a null reverse-path).
func addressDomain(addr string) string {
	addr = strings.TrimSpace(strings.ToLower(addr))
	addr = strings.TrimPrefix(addr, "mailto:")
	at := strings.LastIndex(addr, "@")
	if at < 0 || at == len(addr)-1 {
		return ""
	}
	return addr[at+1:]
}

// organizationalDomain returns the last two DNS labels of the domain — the
// RFC 7489 Section 3.2 Organizational Domain heuristic used when no public
// suffix list is available. A PSL-backed implementation may be substituted.
func organizationalDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	labels := strings.Split(domain, ".")
	if len(labels) <= 2 {
		return domain
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

// aligned reports RFC 7489 Section 3.1 identifier alignment between the
// Author Domain and an authenticated SPF/DKIM domain. Strict mode requires an
// exact FQDN match; relaxed mode (the default) compares Organizational
// Domains.
func aligned(fromDomain, authDomain string, mode dmarc.AlignmentMode) bool {
	f := strings.ToLower(strings.TrimSuffix(fromDomain, "."))
	a := strings.ToLower(strings.TrimSuffix(authDomain, "."))
	if f == "" || a == "" {
		return false
	}
	if f == a {
		return true
	}
	if mode == dmarc.AlignmentStrict {
		return false
	}
	return organizationalDomain(f) == organizationalDomain(a)
}

// anyDKIMPass reports whether at least one DKIM signature verified.
func anyDKIMPass(verifs []*dkim.Verification) bool {
	for _, verif := range verifs {
		if verif != nil && verif.Err == nil {
			return true
		}
	}
	return false
}

func summarizeDKIM(verifs []*dkim.Verification) string {
	if len(verifs) == 0 {
		return "none"
	}
	if anyDKIMPass(verifs) {
		return "pass"
	}
	return "fail"
}

func summarizeDMARC(found, pass bool, policy string) string {
	if !found {
		return "none"
	}
	if pass {
		return "pass (policy " + policy + ")"
	}
	return "fail (policy " + policy + ")"
}

// spfDNSAdapter adapts the DNSResolver to the redsift/spf Resolver interface,
// translating DNS errors per RFC 7208 Section 4.6.4: an NXDOMAIN answer
// behaves as an empty answer (evaluation continues), any other DNS failure is
// a temporary error.
type spfDNSAdapter struct {
	resolver DNSResolver
}

// trimFQDN strips a single trailing dot from an absolute domain name, so
// resolver implementations that key records by the plain name (as the test
// fake and our DNSResolver abstraction do) match redsift's normalized queries.
func trimFQDN(name string) string {
	return strings.TrimSuffix(name, ".")
}

func (a *spfDNSAdapter) LookupTXT(name string) ([]string, *spf.ResponseExtras, error) {
	txts, err := a.resolver.LookupTXT(context.Background(), trimFQDN(name))
	if isDNSNotFound(err) {
		return nil, nil, nil
	}
	return txts, nil, spfDNSError(err)
}

func (a *spfDNSAdapter) LookupTXTStrict(name string) ([]string, *spf.ResponseExtras, error) {
	txts, err := a.resolver.LookupTXT(context.Background(), trimFQDN(name))
	if isDNSNotFound(err) {
		return nil, nil, spf.ErrDNSPermerror
	}
	return txts, nil, spfDNSError(err)
}

func (a *spfDNSAdapter) Exists(name string) (bool, *spf.ResponseExtras, error) {
	ips, err := a.resolver.LookupHost(context.Background(), trimFQDN(name))
	if isDNSNotFound(err) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, spfDNSError(err)
	}
	return len(ips) > 0, nil, nil
}

func (a *spfDNSAdapter) MatchIP(name string, fn spf.IPMatcherFunc) (bool, *spf.ResponseExtras, error) {
	ips, err := a.resolver.LookupHost(context.Background(), trimFQDN(name))
	if isDNSNotFound(err) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, spfDNSError(err)
	}
	for _, ip := range ips {
		if ok, err := fn(ip, name); err != nil {
			return false, nil, err
		} else if ok {
			return true, nil, nil
		}
	}
	return false, nil, nil
}

func (a *spfDNSAdapter) MatchMX(name string, fn spf.IPMatcherFunc) (bool, *spf.ResponseExtras, error) {
	mxs, err := a.resolver.LookupMX(context.Background(), trimFQDN(name))
	if isDNSNotFound(err) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, spfDNSError(err)
	}
	for _, mx := range mxs {
		ips, err := a.resolver.LookupHost(context.Background(), trimFQDN(mx.Host))
		if isDNSNotFound(err) {
			continue
		}
		if err != nil {
			return false, nil, spfDNSError(err)
		}
		for _, ip := range ips {
			if ok, err := fn(ip, name); err != nil {
				return false, nil, err
			} else if ok {
				return true, nil, nil
			}
		}
	}
	return false, nil, nil
}

func (a *spfDNSAdapter) LookupPTR(addr string) ([]string, *spf.ResponseExtras, error) {
	names, err := a.resolver.LookupAddr(context.Background(), addr)
	if isDNSNotFound(err) {
		return nil, nil, nil
	}
	return names, nil, spfDNSError(err)
}

// isDNSNotFound reports whether err is an NXDOMAIN answer.
func isDNSNotFound(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// spfDNSError maps any non-NXDOMAIN DNS failure to a temporary error as
// RFC 7208 Section 4.6.4 requires (an RCODE 3 answer is handled as empty).
func spfDNSError(err error) error {
	if err == nil || isDNSNotFound(err) {
		return nil
	}
	return spf.ErrDNSTemperror
}
