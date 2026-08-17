package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/textproto"
	"sort"
	"strings"
	"time"

	"imap-jmap/jmap"
)

// MXOutboundSender relays raw RFC 5322 messages to external recipients by connecting
// to the recipient domain's MX servers in preference order (RFC 5321 Section 5.1),
// upgrading to TLS via STARTTLS when offered, and logging every command and reply
// of the SMTP conversation verbatim so delivery is fully observable.
type MXOutboundSender struct {
	LocalName      string
	DialTimeout    time.Duration
	CommandTimeout time.Duration
	LookupMX       func(domain string) ([]*net.MX, error)
	Dial           func(ctx context.Context, network, addr string) (net.Conn, error)
}

var _ jmap.OutboundMailSender = (*MXOutboundSender)(nil)

// NewMXOutboundSender returns a sender with production defaults (public DNS MX
// lookup, TCP dial with a 15s timeout). Dial and LookupMX are overridable for tests.
func NewMXOutboundSender() *MXOutboundSender {
	return &MXOutboundSender{
		LocalName:      "localhost",
		DialTimeout:    15 * time.Second,
		CommandTimeout: 30 * time.Second,
		LookupMX:       net.LookupMX,
		Dial:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
	}
}

// SendMail delivers rawMessage to each recipient by routing it to the recipient
// domain's MX hosts. The returned map holds one OutboundDeliveryResult per
// recipient: Delivered reports acceptance by the remote server (250 on DATA),
// and SmtpReply is the verbatim reply (e.g. "550 5.1.1 <x>: User unknown").
func (s *MXOutboundSender) SendMail(ctx context.Context, from string, recipients []string, rawMessage []byte) map[string]jmap.OutboundDeliveryResult {
	results := make(map[string]jmap.OutboundDeliveryResult, len(recipients))

	byDomain := make(map[string][]string)
	for _, rcpt := range recipients {
		rcpt = strings.TrimSpace(rcpt)
		at := strings.LastIndex(rcpt, "@")
		if at < 0 || at == len(rcpt)-1 {
			log.Printf("SMTP outbound: invalid recipient address %q", rcpt)
			results[rcpt] = jmap.OutboundDeliveryResult{Delivered: false, SmtpReply: "554 5.1.3 Invalid recipient address"}
			continue
		}
		domain := strings.ToLower(rcpt[at+1:])
		byDomain[domain] = append(byDomain[domain], rcpt)
	}

	for domain, domainRecipients := range byDomain {
		for rcpt, res := range s.deliverToDomain(ctx, domain, domainRecipients, from, rawMessage) {
			results[rcpt] = res
		}
	}
	return results
}

func (s *MXOutboundSender) deliverToDomain(ctx context.Context, domain string, recipients []string, from string, rawMessage []byte) map[string]jmap.OutboundDeliveryResult {
	results := make(map[string]jmap.OutboundDeliveryResult)

	hosts := s.mxHosts(domain)
	pending := append([]string(nil), recipients...)
	for _, host := range hosts {
		if len(pending) == 0 || ctx.Err() != nil {
			break
		}
		final, retry, delivered := s.tryHost(ctx, host, from, pending, rawMessage)
		for rcpt, res := range final {
			results[rcpt] = res
		}
		if delivered {
			// Host accepted the message; any still-pending recipients were
			// rejected by this host with a 4xx, so try the next host.
		}
		pending = retry
		log.Printf("SMTP outbound [%s]: %d delivered, %d still pending for %s", host, len(final), len(retry), domain)
	}

	for _, rcpt := range pending {
		results[rcpt] = jmap.OutboundDeliveryResult{
			Delivered: false,
			SmtpReply: fmt.Sprintf("421 4.4.4 All MX hosts for %s failed", domain),
		}
	}
	return results
}

// mxHosts resolves the domain's MX records in preference order (RFC 5321 Section
// 5.1), falling back to the domain's address records when no MX exists.
func (s *MXOutboundSender) mxHosts(domain string) []string {
	var hosts []string
	if mxs, err := s.LookupMX(domain); err == nil && len(mxs) > 0 {
		sort.Slice(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
		for _, mx := range mxs {
			hosts = append(hosts, net.JoinHostPort(strings.TrimSuffix(mx.Host, "."), "25"))
		}
	} else if ips, err := net.LookupHost(domain); err == nil && len(ips) > 0 {
		log.Printf("SMTP outbound: no MX for %s, falling back to address records", domain)
		hosts = append(hosts, net.JoinHostPort(domain, "25"))
	} else {
		log.Printf("SMTP outbound: no MX or address records for %s", domain)
	}
	log.Printf("SMTP outbound: MX hosts for %s: %v", domain, hosts)
	return hosts
}

// tryHost performs one complete SMTP transaction (MAIL, RCPT, DATA) against a
// single MX host. It returns the definitive results for recipients that received
// a permanent (5xx) rejection, the recipients to retry on another host (transient
// 4xx or session-level failure), and whether DATA was accepted.
func (s *MXOutboundSender) tryHost(ctx context.Context, host, from string, recipients []string, rawMessage []byte) (final map[string]jmap.OutboundDeliveryResult, retry []string, delivered bool) {
	final = make(map[string]jmap.OutboundDeliveryResult)

	conn, err := s.Dial(ctx, "tcp", host)
	if err != nil {
		log.Printf("SMTP outbound [%s] E: dial: %v", host, err)
		return final, append([]string(nil), recipients...), false
	}
	defer conn.Close()
	if t, ok := conn.(*net.TCPConn); ok {
		t.SetKeepAlive(true)
	}

	sess := &mxSession{s: s, host: host, conn: conn, tp: textproto.NewConn(conn)}
	sess.setTimeout()

	code, msg, err := sess.readReply(220) // greeting
	if err != nil {
		log.Printf("SMTP outbound [%s] E: greeting: %v", host, err)
		return final, append([]string(nil), recipients...), false
	}
	if code == 421 {
		return final, append([]string(nil), recipients...), false
	}

	code, msg, err = sess.cmd(250, "EHLO %s", s.LocalName)
	if err != nil {
		log.Printf("SMTP outbound [%s] E: EHLO: %v", host, err)
		return final, append([]string(nil), recipients...), false
	}
	extensions := parseExtensions(msg)

	if _, ok := extensions["STARTTLS"]; ok {
		code, msg, err = sess.cmd(220, "STARTTLS")
		if err == nil {
			tlsConn := tls.Client(conn, &tls.Config{
				ServerName: host[:strings.LastIndex(host, ":")],
				MinVersion: tls.VersionTLS12,
			})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				log.Printf("SMTP outbound [%s] E: TLS handshake: %v", host, err)
				return final, append([]string(nil), recipients...), false
			}
			sess.tp = textproto.NewConn(tlsConn)
			sess.conn = tlsConn
			sess.setTimeout()
			log.Printf("SMTP outbound [%s] S: TLS established (tls%d.%d)", host,
				tlsConn.ConnectionState().Version>>8, tlsConn.ConnectionState().Version&0xff)
			code, msg, err = sess.cmd(250, "EHLO %s", s.LocalName)
			if err != nil {
				log.Printf("SMTP outbound [%s] E: EHLO after STARTTLS: %v", host, err)
				return final, append([]string(nil), recipients...), false
			}
			extensions = parseExtensions(msg)
		}
	}

	rawMessage = normalizeCRLF(rawMessage)
	if sizeExt, ok := extensions["SIZE"]; ok {
		maxSize := int64(0)
		fmt.Sscanf(sizeExt, "%d", &maxSize)
		if maxSize > 0 && int64(len(rawMessage)) > maxSize {
			log.Printf("SMTP outbound [%s] E: message of %d bytes exceeds advertised SIZE %d", host, len(rawMessage), maxSize)
			for _, rcpt := range recipients {
				final[rcpt] = jmap.OutboundDeliveryResult{Delivered: false, SmtpReply: "552 5.3.4 Message size exceeds fixed maximum message size"}
			}
			return final, nil, false
		}
	}

	from = sanitizeEnvelope(from, "<>")
	code, msg, err = sess.cmd(250, "MAIL FROM:%s", from)
	if err != nil {
		// MAIL rejected: permanent failure for everyone, transient for retry.
		reply := fmt.Sprintf("%d %s", code, msg)
		if code >= 500 && code < 600 {
			for _, rcpt := range recipients {
				final[rcpt] = jmap.OutboundDeliveryResult{Delivered: false, SmtpReply: reply}
			}
			return final, nil, false
		}
		return final, append([]string(nil), recipients...), false
	}

	var accepted []string
	for _, rcpt := range recipients {
		sess.setTimeout()
		code, msg, err = sess.cmd(250, "RCPT TO:<%s>", sanitizeEnvelope(rcpt, "invalid"))
		if err == nil {
			accepted = append(accepted, rcpt)
			continue
		}
		reply := fmt.Sprintf("%d %s", code, msg)
		if code >= 500 && code < 600 {
			log.Printf("SMTP outbound [%s] S: %s (permanent rejection)", host, reply)
			final[rcpt] = jmap.OutboundDeliveryResult{Delivered: false, SmtpReply: reply}
		} else {
			log.Printf("SMTP outbound [%s] S: %s (transient, will retry)", host, reply)
			retry = append(retry, rcpt)
		}
	}

	if len(accepted) == 0 {
		return final, retry, false
	}

	sess.setTimeout()
	code, msg, err = sess.cmd(354, "DATA (%d bytes)", len(rawMessage))
	if err != nil {
		return final, append(accepted, retry...), false
	}

	w := sess.tp.DotWriter()
	if _, err := w.Write(rawMessage); err != nil {
		log.Printf("SMTP outbound [%s] E: writing message: %v", host, err)
		return final, append(accepted, retry...), false
	}
	if err := w.Close(); err != nil {
		log.Printf("SMTP outbound [%s] E: closing DATA: %v", host, err)
		return final, append(accepted, retry...), false
	}
	sess.setTimeout()
	code, msg, err = sess.readReply(250)
	reply := fmt.Sprintf("%d %s", code, msg)
	if err == nil {
		for _, rcpt := range accepted {
			final[rcpt] = jmap.OutboundDeliveryResult{Delivered: true, SmtpReply: reply}
		}
		return final, retry, true
	}
	if code >= 500 && code < 600 {
		for _, rcpt := range accepted {
			final[rcpt] = jmap.OutboundDeliveryResult{Delivered: false, SmtpReply: reply}
		}
		return final, retry, false
	}
	return final, append(accepted, retry...), false
}

// mxSession wraps one SMTP conversation with a remote MX host, logging every
// client command ("C:") and server reply ("S:") line verbatim.
type mxSession struct {
	s    *MXOutboundSender
	host string
	conn net.Conn
	tp   *textproto.Conn
}

func (m *mxSession) setTimeout() {
	m.conn.SetDeadline(time.Now().Add(m.s.CommandTimeout))
}

func (m *mxSession) cmd(expect int, format string, args ...any) (int, string, error) {
	line := fmt.Sprintf(format, args...)
	m.s.logC(m.host, line)
	if err := m.tp.PrintfLine("%s", line); err != nil {
		return 0, "", err
	}
	m.setTimeout()
	return m.readReply(expect)
}

func (m *mxSession) readReply(expect int) (int, string, error) {
	code, msg, err := m.tp.ReadResponse(expect)
	m.s.logS(m.host, fmt.Sprintf("%d %s", code, msg))
	return code, msg, err
}

func (s *MXOutboundSender) logC(host, line string) {
	log.Printf("SMTP outbound [%s] C: %s", host, line)
}

func (s *MXOutboundSender) logS(host, reply string) {
	for _, line := range strings.Split(strings.TrimRight(reply, "\n"), "\n") {
		log.Printf("SMTP outbound [%s] S: %s", host, line)
	}
}

// parseExtensions parses the EHLO multi-line reply into a map of extension name
// (upper-cased) to its optional parameter text (RFC 5321 Section 4.1.1.1).
func parseExtensions(reply string) map[string]string {
	exts := make(map[string]string)
	for _, line := range strings.Split(reply, "\n") {
		line = strings.TrimSpace(line)
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.ToUpper(fields[0])
		if len(fields) > 1 {
			exts[name] = strings.Join(fields[1:], " ")
		} else {
			exts[name] = ""
		}
	}
	return exts
}

// normalizeCRLF ensures the message uses CRLF line endings as required for the
// DATA transfer (RFC 5321 Section 4.1.1.4), since JMAP blobs are stored as-is.
func normalizeCRLF(raw []byte) []byte {
	body := strings.ReplaceAll(string(raw), "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	if !strings.HasSuffix(body, "\r\n") {
		body += "\r\n"
	}
	return []byte(body)
}

// sanitizeEnvelope guards against CR/LF injection in envelope addresses
// (RFC 5321 Section 7.1) by refusing addresses containing bare line breaks.
func sanitizeEnvelope(addr, fallback string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" || strings.ContainsAny(addr, "\r\n") || strings.Contains(addr, " ") {
		return fallback
	}
	return addr
}