package smtp_test

import (
	"context"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"imap-jmap/smtp"
)

func startMockSMTPServer(t *testing.T, handler func(tp *textproto.Conn)) (net.Listener, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			go func(c net.Conn) {
				defer c.Close()
				tp := textproto.NewConn(c)
				handler(tp)
			}(conn)
		}
	}()
	cleanup := func() {
		close(done)
		ln.Close()
	}
	return ln, cleanup
}

func TestMXOutboundSender_SuccessDelivery(t *testing.T) {
	ln, cleanup := startMockSMTPServer(t, func(tp *textproto.Conn) {
		tp.PrintfLine("220 mock.remote.mx ESMTP Service Ready")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
				tp.PrintfLine("250-mock.remote.mx\n250-SIZE 1048576\n250 8BITMIME")
			case strings.HasPrefix(upper, "MAIL FROM"):
				tp.PrintfLine("250 2.1.0 Sender OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				tp.PrintfLine("250 2.1.5 Recipient OK")
			case strings.HasPrefix(upper, "DATA"):
				tp.PrintfLine("354 Start mail input; end with <CRLF>.<CRLF>")
				r := tp.DotReader()
				buf := make([]byte, 1024)
				for {
					_, err := r.Read(buf)
					if err != nil {
						break
					}
				}
				tp.PrintfLine("250 2.0.0 Message accepted for delivery")
			case strings.HasPrefix(upper, "QUIT"):
				tp.PrintfLine("221 2.0.0 Bye")
				return
			default:
				tp.PrintfLine("500 5.5.1 Command unrecognized")
			}
		}
	})
	defer cleanup()

	hostPort := ln.Addr().String()
	host, port, _ := net.SplitHostPort(hostPort)

	sender := smtp.NewMXOutboundSender()
	sender.LookupMX = func(domain string) ([]*net.MX, error) {
		return []*net.MX{{Host: host, Pref: 10}}, nil
	}
	sender.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return net.Dial(network, net.JoinHostPort(host, port))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := sender.SendMail(ctx, "sender@example.com", []string{"alice@remote.org"}, []byte("Subject: Hello\r\n\r\nTest message body\r\n"))
	res, ok := results["alice@remote.org"]
	if !ok {
		t.Fatalf("Expected result for alice@remote.org")
	}
	if !res.Delivered {
		t.Errorf("Expected delivered=true, got %v with reply %q", res.Delivered, res.SmtpReply)
	}
	if !strings.Contains(res.SmtpReply, "250") {
		t.Errorf("Expected 250 in reply, got %q", res.SmtpReply)
	}
}

func TestMXOutboundSender_PermanentRejection5xx(t *testing.T) {
	ln, cleanup := startMockSMTPServer(t, func(tp *textproto.Conn) {
		tp.PrintfLine("220 mock.remote.mx ESMTP Service Ready")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"):
				tp.PrintfLine("250 mock.remote.mx")
			case strings.HasPrefix(upper, "MAIL FROM"):
				tp.PrintfLine("250 2.1.0 Sender OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				tp.PrintfLine("550 5.1.1 User unknown")
			case strings.HasPrefix(upper, "QUIT"):
				tp.PrintfLine("221 Bye")
				return
			}
		}
	})
	defer cleanup()

	host, port, _ := net.SplitHostPort(ln.Addr().String())

	sender := smtp.NewMXOutboundSender()
	sender.LookupMX = func(domain string) ([]*net.MX, error) {
		return []*net.MX{{Host: host, Pref: 10}}, nil
	}
	sender.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return net.Dial(network, net.JoinHostPort(host, port))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results := sender.SendMail(ctx, "sender@example.com", []string{"nonexistent@remote.org"}, []byte("Subject: Hi\r\n\r\nHello\r\n"))
	res := results["nonexistent@remote.org"]
	if res.Delivered {
		t.Errorf("Expected delivered=false on 550, got true")
	}
	if !strings.Contains(res.SmtpReply, "550") {
		t.Errorf("Expected 550 in reply, got %q", res.SmtpReply)
	}
}

func TestMXOutboundSender_SizeExceeded(t *testing.T) {
	ln, cleanup := startMockSMTPServer(t, func(tp *textproto.Conn) {
		tp.PrintfLine("220 mock.remote.mx ESMTP")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"):
				tp.PrintfLine("250-mock.remote.mx\n250 SIZE 50")
			case strings.HasPrefix(upper, "QUIT"):
				tp.PrintfLine("221 Bye")
				return
			}
		}
	})
	defer cleanup()

	host, port, _ := net.SplitHostPort(ln.Addr().String())

	sender := smtp.NewMXOutboundSender()
	sender.LookupMX = func(domain string) ([]*net.MX, error) {
		return []*net.MX{{Host: host, Pref: 10}}, nil
	}
	sender.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return net.Dial(network, net.JoinHostPort(host, port))
	}

	largeMsg := []byte("Subject: Large Message Exceeding Limit\r\n\r\n" + strings.Repeat("ABCDEFGHIJ", 20))
	results := sender.SendMail(context.Background(), "sender@example.com", []string{"large@remote.org"}, largeMsg)
	res := results["large@remote.org"]
	if res.Delivered {
		t.Errorf("Expected delivered=false for oversized message")
	}
	if !strings.Contains(res.SmtpReply, "552") {
		t.Errorf("Expected 552 SIZE error, got %q", res.SmtpReply)
	}
}

func TestMXOutboundSender_FallbackToNextMX(t *testing.T) {
	// MX 1 rejects RCPT with 451
	ln1, cleanup1 := startMockSMTPServer(t, func(tp *textproto.Conn) {
		tp.PrintfLine("220 mx1.remote.mx ESMTP")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"):
				tp.PrintfLine("250 mx1.remote.mx")
			case strings.HasPrefix(upper, "MAIL FROM"):
				tp.PrintfLine("250 OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				tp.PrintfLine("451 4.3.0 Mailbox busy, try later")
			case strings.HasPrefix(upper, "QUIT"):
				tp.PrintfLine("221 Bye")
				return
			}
		}
	})
	defer cleanup1()

	// MX 2 accepts the message
	ln2, cleanup2 := startMockSMTPServer(t, func(tp *textproto.Conn) {
		tp.PrintfLine("220 mx2.remote.mx ESMTP")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "EHLO"):
				tp.PrintfLine("250 mx2.remote.mx")
			case strings.HasPrefix(upper, "MAIL FROM"):
				tp.PrintfLine("250 OK")
			case strings.HasPrefix(upper, "RCPT TO"):
				tp.PrintfLine("250 OK")
			case strings.HasPrefix(upper, "DATA"):
				tp.PrintfLine("354 Go ahead")
				r := tp.DotReader()
				buf := make([]byte, 1024)
				for {
					_, err := r.Read(buf)
					if err != nil {
						break
					}
				}
				tp.PrintfLine("250 2.0.0 Accepted on backup MX")
			case strings.HasPrefix(upper, "QUIT"):
				tp.PrintfLine("221 Bye")
				return
			}
		}
	})
	defer cleanup2()

	host1, port1, _ := net.SplitHostPort(ln1.Addr().String())
	host2, port2, _ := net.SplitHostPort(ln2.Addr().String())

	sender := smtp.NewMXOutboundSender()
	sender.LookupMX = func(domain string) ([]*net.MX, error) {
		return []*net.MX{
			{Host: "mx1.remote.mx", Pref: 10},
			{Host: "mx2.remote.mx", Pref: 20},
		}, nil
	}
	sender.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if strings.HasPrefix(addr, "mx1.remote.mx:") {
			return net.Dial(network, net.JoinHostPort(host1, port1))
		}
		if strings.HasPrefix(addr, "mx2.remote.mx:") {
			return net.Dial(network, net.JoinHostPort(host2, port2))
		}
		return nil, fmt.Errorf("unknown host: %s", addr)
	}

	results := sender.SendMail(context.Background(), "sender@example.com", []string{"fallback@remote.org"}, []byte("Subject: Hi\r\n\r\nHello\r\n"))
	res := results["fallback@remote.org"]
	if !res.Delivered {
		t.Errorf("Expected delivery to succeed on backup MX, got delivered=false: %q", res.SmtpReply)
	}
	if !strings.Contains(res.SmtpReply, "Accepted on backup MX") {
		t.Errorf("Expected reply from backup MX, got %q", res.SmtpReply)
	}
}

func TestMXOutboundSender_InvalidRecipientAddress(t *testing.T) {
	sender := smtp.NewMXOutboundSender()
	results := sender.SendMail(context.Background(), "sender@example.com", []string{"badaddress"}, []byte("Subject: Hi\r\n\r\nHello\r\n"))
	res := results["badaddress"]
	if res.Delivered {
		t.Errorf("Expected delivered=false for address without domain")
	}
	if !strings.Contains(res.SmtpReply, "554") {
		t.Errorf("Expected 554 for invalid recipient address, got %q", res.SmtpReply)
	}
}
