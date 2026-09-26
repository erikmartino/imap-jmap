package jmap_test

import (
	"context"
	"net"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
	"imap-jmap/smtp"
)

// startMockRelay runs a minimal SMTP server that answers RCPT TO with rcptReply
// and DATA with dataReply, and returns the listener plus a cleanup func.
func startMockRelay(t *testing.T, rcptReply, dataReply string) (net.Listener, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				tp := textproto.NewConn(c)
				_ = tp.PrintfLine("220 mock.remote.mx ESMTP")
				for {
					line, err := tp.ReadLine()
					if err != nil {
						return
					}
					up := strings.ToUpper(line)
					switch {
					case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
						_ = tp.PrintfLine("250 mock.remote.mx")
					case strings.HasPrefix(up, "MAIL FROM"):
						_ = tp.PrintfLine("250 2.1.0 Sender OK")
					case strings.HasPrefix(up, "RCPT TO"):
						_ = tp.PrintfLine("%s", rcptReply)
					case strings.HasPrefix(up, "DATA"):
						_ = tp.PrintfLine("354 Start mail input")
						r := tp.DotReader()
						buf := make([]byte, 1024)
						for {
							if _, err := r.Read(buf); err != nil {
								break
							}
						}
						_ = tp.PrintfLine("%s", dataReply)
					case strings.HasPrefix(up, "QUIT"):
						_ = tp.PrintfLine("221 Bye")
						return
					}
				}
			}(c)
		}
	}()
	return ln, func() { ln.Close() }
}

// submitExternal configures a server whose OutboundMailSender is a real
// MXOutboundSender pointed at the mock relay, submits an allow-listed external
// message, and returns the recipient's deliveryStatus map.
func submitExternal(t *testing.T, ln net.Listener) map[string]any {
	t.Helper()
	mockHost, mockPort, _ := net.SplitHostPort(ln.Addr().String())
	sender := smtp.NewMXOutboundSender()
	sender.LookupMX = func(domain string) ([]*net.MX, error) {
		return []*net.MX{{Host: "mx.mock", Pref: 10}}, nil
	}
	sender.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return net.Dial(network, net.JoinHostPort(mockHost, mockPort))
	}

	srv := newTestServer(
		jmap.WithAllowedRecipients([]string{"external@allowed.org"}),
		jmap.WithOutboundSender(sender),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	em, _ := srv.MailBackend.CreateEmail(seedCtx(), &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{"mb-drafts": true},
		Subject:    "submission reply",
		From:       []jmap.EmailAddress{{Email: "sender@example.com"}},
		To:         []jmap.EmailAddress{{Email: "external@allowed.org"}},
		BodyValues: map[string]jmap.EmailBodyValue{"1": {Value: "hello"}},
	})

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SubmissionCapabilityURI}
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"EmailSubmission/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"sub1": map[string]any{"identityId": "id-primary", "emailId": string(em.ID)},
			},
		}, "c1"},
	})
	created, _ := res.MethodResponses[0].Args["created"].(map[string]any)
	subObj, ok := created["sub1"].(map[string]any)
	if !ok {
		t.Fatalf("submission create failed: %v", res.MethodResponses[0].Args)
	}
	ds, _ := subObj["deliveryStatus"].(map[string]any)
	status, _ := ds["external@allowed.org"].(map[string]any)
	return status
}

// TestRFC8621_EmailSubmissionSMTPReplyIsRCPTStage verifies that the smtpReply of
// a successful external submission is the (flattened) RCPT TO stage reply
// rather than the DATA stage reply (RFC 8621 Section 7).
func TestRFC8621_EmailSubmissionSMTPReplyIsRCPTStage(t *testing.T) {
	spectest.Require(t, "RFC8621", "7", spectest.SHOULD, "This SHOULD be the response to the RCPT TO stage,")
	spectest.Require(t, "RFC8621", "7", spectest.SHOULD, "Multi-line SMTP responses should be concatenated to a single")

	ln, cleanup := startMockRelay(t, "250-2.1.5 first line\n250 2.1.5 second line", "250 2.0.0 Message accepted")
	defer cleanup()

	status := submitExternal(t, ln)
	reply, _ := status["smtpReply"].(string)
	if strings.Contains(reply, "\n") {
		t.Errorf("smtpReply must be a single line, got %q", reply)
	}
	if reply != "250 2.1.5 first line 2.1.5 second line" {
		t.Errorf("smtpReply should be the flattened RCPT stage reply, got %q", reply)
	}
	if strings.Contains(reply, "Message accepted") {
		t.Errorf("smtpReply must not be the DATA stage reply, got %q", reply)
	}
}
