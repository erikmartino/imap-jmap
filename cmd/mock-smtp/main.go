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

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

type Backend struct {
	ldapHost string
}

func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &Session{backend: b, remoteAddr: c.Conn().RemoteAddr().String()}, nil
}

type Session struct {
	backend       *Backend
	remoteAddr    string
	from          string
	authenticated bool
	authUser      string
	recipients    []string
}

var _ smtp.AuthSession = (*Session)(nil)


func (s *Session) AuthMechanisms() []string {
	return []string{sasl.Plain}
}

func (s *Session) Auth(mech string) (sasl.Server, error) {
	switch mech {
	case sasl.Plain:
		return sasl.NewPlainServer(func(identity, username, password string) error {
			log.Printf("[MockSMTP] AUTH PLAIN request for user: %s", username)
			if err := authenticateLDAP(s.backend.ldapHost, username, password); err != nil {
				log.Printf("[MockSMTP] LDAP Auth FAILED for user: %s: %v", username, err)
				return fmt.Errorf("invalid LDAP credentials")
			}
			s.authenticated = true
			s.authUser = username
			log.Printf("[MockSMTP] LDAP Auth SUCCESS for user: %s", username)
			return nil
		}), nil
	default:
		return nil, smtp.ErrAuthUnsupported
	}
}


func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	s.from = from
	log.Printf("[MockSMTP] MAIL FROM: <%s>", from)
	return nil
}

func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	domain := getDomain(to)
	if domain != "profundo.dk" && domain != "example.org" && domain != "example.com" && !s.authenticated {
		return fmt.Errorf("authentication required for outbound mail delivery")
	}
	s.recipients = append(s.recipients, to)
	log.Printf("[MockSMTP] RCPT TO: <%s>", to)
	return nil
}

func (s *Session) Data(r io.Reader) error {
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	log.Printf("[MockSMTP] Received email (%d bytes) from <%s> to %v (auth: %v)", len(buf), s.from, s.recipients, s.authenticated)

	for _, rcpt := range s.recipients {
		log.Printf("[MockSMTP] Processing delivery for <%s> (domain: %s)", rcpt, getDomain(rcpt))
		go deliverLMTP(rcpt, s.from, buf)
	}

	return nil
}

func (s *Session) Reset() {
	s.from = ""
	s.recipients = nil
	s.authenticated = false
	s.authUser = ""
}

func (s *Session) Logout() error {
	return nil
}

func authenticateLDAP(ldapHost, username, password string) error {
	conn, err := net.Dial("tcp", ldapHost)
	if err != nil {
		if username != "" && password == username {
			return nil
		}
		return err
	}
	defer conn.Close()

	if username != "" && password == username {
		return nil
	}
	return fmt.Errorf("invalid credentials")
}

func deliverLMTP(recipient, from string, data []byte) {
	conn, err := net.Dial("tcp", "dovecot:24")
	if err != nil {
		conn, err = net.Dial("tcp", "imap-jmap:1025")
		if err != nil {
			log.Printf("[MockSMTP] Internal delivery skipped (no active LMTP/SMTP listener): %v", err)
			return
		}
	}
	defer conn.Close()

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

	ldapHost := os.Getenv("LDAP_HOST")
	if ldapHost == "" {
		ldapHost = "ldap:389"
	}

	s := smtp.NewServer(&Backend{ldapHost: ldapHost})
	s.Addr = "0.0.0.0:" + port
	s.Domain = "profundo.dk"
	s.AllowInsecureAuth = true

	log.Printf("Starting Mock SMTP server for profundo.dk on 0.0.0.0:%s (LDAP: %s) ...", port, ldapHost)

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
