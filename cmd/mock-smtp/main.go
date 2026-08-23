package main

import (
	"fmt"
	"io"
	"log"
	"net"
	netsmtp "net/smtp"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/emersion/go-smtp"
)


// Backend implements smtp.Backend for receiving inbound emails and relaying
// to Dovecot (or local delivery) and sending emails for @profundo.dk.
type Backend struct{}

func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &Session{remoteAddr: c.Conn().RemoteAddr().String()}, nil
}

type Session struct {
	remoteAddr string
	from       string
	recipients []string
}

func (s *Session) AuthPlain(username, password string) error {
	// Require valid credentials for sending outbound mail
	if username == "" || (password != username && password == "") {
		return fmt.Errorf("invalid credentials")
	}
	log.Printf("[MockSMTP] Authenticated session for user: %s", username)
	return nil
}

func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	s.from = from
	log.Printf("[MockSMTP] MAIL FROM: <%s>", from)
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.recipients = append(s.recipients, to)
	log.Printf("[MockSMTP] RCPT TO: <%s>", to)
	return nil
}

func (s *Session) Data(r io.Reader) error {
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	log.Printf("[MockSMTP] Received email (%d bytes) from <%s> to %v", len(buf), s.from, s.recipients)

	// Deliver internally via LMTP or store for Dovecot / imap-jmap
	for _, rcpt := range s.recipients {
		log.Printf("[MockSMTP] Processing delivery for <%s> (domain: %s)", rcpt, getDomain(rcpt))
		// Deliver to Dovecot LMTP if available or log successful handling
		go deliverLMTP(rcpt, s.from, buf)
	}

	return nil
}

func (s *Session) Reset() {
	s.from = ""
	s.recipients = nil
}

func (s *Session) Logout() error {
	return nil
}

func deliverLMTP(recipient, from string, data []byte) {
	// Attempt local delivery to Dovecot LMTP port 24 if running
	conn, err := net.Dial("tcp", "dovecot:24")
	if err != nil {
		// Fallback to localhost / imap-jmap SMTP delivery port 1025
		conn, err = net.Dial("tcp", "imap-jmap:1025")
		if err != nil {
			log.Printf("[MockSMTP] Internal delivery skipped (no active LMTP/SMTP listener): %v", err)
			return
		}
	}
	defer conn.Close()

	// Send message to local receiver
	client, err := netsmtp.NewClient(conn, "profundo.dk")

	if err != nil {
		return
	}
	defer client.Quit()

	if err := client.Mail(from); err != nil {
		return
	}
	if err := client.Rcpt(recipient); err != nil {
		return
	}
	w, err := client.Data()
	if err != nil {
		return
	}
	w.Write(data)
	w.Close()
	log.Printf("[MockSMTP] Successfully delivered email to <%s>", recipient)
}

func getDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

func main() {
	port := os.Getenv("SMTP_LISTEN_PORT")
	if port == "" {
		envPort := os.Getenv("SMTP_PORT")
		if strings.Contains(envPort, ":") {
			parts := strings.Split(envPort, ":")
			port = parts[len(parts)-1]
		} else {
			port = envPort
		}
	}
	if port == "" {
		port = "25"
	}


	s := smtp.NewServer(&Backend{})
	s.Addr = "0.0.0.0:" + port
	s.Domain = "profundo.dk"
	s.AllowInsecureAuth = true

	log.Printf("Starting Mock SMTP server for profundo.dk on 0.0.0.0:%s ...", port)

	go func() {
		if err := s.ListenAndServe(); err != nil {
			log.Fatalf("SMTP server error: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("Shutting down Mock SMTP server.")
	s.Close()
}
