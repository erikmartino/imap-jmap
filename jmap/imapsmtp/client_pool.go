package imapsmtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"sync"

	"github.com/emersion/go-imap/v2/imapclient"
	"imap-jmap/jmap"
)

// ClientPool manages active IMAP and SMTP connections for user accounts.
type ClientPool struct {
	imapAddr string
	smtpAddr string
	mu       sync.Mutex
	idle     map[string][]*imapclient.Client
}

func NewClientPool(imapAddr string) *ClientPool {
	return &ClientPool{
		imapAddr: imapAddr,
		idle:     make(map[string][]*imapclient.Client),
	}
}

func NewClientPoolWithSMTP(imapAddr, smtpAddr string) *ClientPool {
	return &ClientPool{
		imapAddr: imapAddr,
		smtpAddr: smtpAddr,
		idle:     make(map[string][]*imapclient.Client),
	}
}

// Close closes all pooled IMAP client connections.
func (p *ClientPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for k, list := range p.idle {
		for _, c := range list {
			_ = c.Close()
		}
		delete(p.idle, k)
	}
	return nil
}

// GetClientForContext extracts credentials from the request context and returns an authenticated IMAP client.
func (p *ClientPool) GetClientForContext(ctx context.Context) (*imapclient.Client, error) {
	creds, ok := jmap.CredentialsFromContext(ctx)
	if !ok {
		// Fall back to subject if available
		subject, hasSubject := jmap.SubjectFromContext(ctx)
		if hasSubject {
			creds = jmap.AuthCredentials{Username: subject, Password: subject}
		} else {
			accountID, hasAccount := jmap.AccountIDFromContext(ctx)
			if hasAccount {
				if sub, ok := jmap.SubjectForAccountID(accountID); ok {
					creds = jmap.AuthCredentials{Username: sub, Password: sub}
				}
			}
		}
	}

	if creds.Username == "" {
		return nil, errors.New("unauthorized: missing credentials in context")
	}

	return p.GetClient(ctx, creds.Username, creds.Password)
}

// ReleaseClient returns an active IMAP client back to the pool for reuse.
func (p *ClientPool) ReleaseClient(ctx context.Context, client *imapclient.Client) {
	if client == nil {
		return
	}

	creds, ok := jmap.CredentialsFromContext(ctx)
	if !ok {
		subject, hasSubject := jmap.SubjectFromContext(ctx)
		if hasSubject {
			creds = jmap.AuthCredentials{Username: subject, Password: subject}
		} else {
			accountID, hasAccount := jmap.AccountIDFromContext(ctx)
			if hasAccount {
				if sub, ok := jmap.SubjectForAccountID(accountID); ok {
					creds = jmap.AuthCredentials{Username: sub, Password: sub}
				}
			}
		}
	}

	if creds.Username == "" {
		_ = client.Close()
		return
	}

	p.ReleaseClientForUser(creds.Username, creds.Password, client)
}

// ReleaseClientForUser returns an active IMAP client for the given credentials.
func (p *ClientPool) ReleaseClientForUser(username, password string, client *imapclient.Client) {
	if client == nil {
		return
	}

	key := username + ":" + password

	p.mu.Lock()
	defer p.mu.Unlock()

	// Keep up to 8 idle connections per user
	if len(p.idle[key]) < 8 {
		p.idle[key] = append(p.idle[key], client)
	} else {
		_ = client.Close()
	}
}

// GetClient establishes or reuses an authenticated IMAP client connection using the provided credentials.
func (p *ClientPool) GetClient(ctx context.Context, username, password string) (*imapclient.Client, error) {
	key := username + ":" + password

	p.mu.Lock()
	if list := p.idle[key]; len(list) > 0 {
		c := list[len(list)-1]
		p.idle[key] = list[:len(list)-1]
		p.mu.Unlock()

		if err := c.Noop().Wait(); err == nil {
			return c, nil
		}
		_ = c.Close()
	} else {
		p.mu.Unlock()
	}

	var client *imapclient.Client
	host, port, err := net.SplitHostPort(p.imapAddr)
	if err != nil {
		host = p.imapAddr
		port = "993"
	}

	if port == "993" {
		c, err := imapclient.DialTLS(p.imapAddr, &imapclient.Options{
			TLSConfig: &tls.Config{InsecureSkipVerify: true, ServerName: host},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to dial TLS IMAP server %s: %w", p.imapAddr, err)
		}
		client = c
	} else {
		// Attempt STARTTLS first
		c, err := imapclient.DialStartTLS(p.imapAddr, &imapclient.Options{
			TLSConfig: &tls.Config{InsecureSkipVerify: true, ServerName: host},
		})
		if err != nil {
			// Fallback to insecure plain TCP
			c, err = imapclient.DialInsecure(p.imapAddr, &imapclient.Options{})
			if err != nil {
				return nil, fmt.Errorf("failed to connect to IMAP server %s: %w", p.imapAddr, err)
			}
		}
		client = c
	}

	if err := client.Login(username, password).Wait(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("IMAP login failed for user %s: %w", username, err)
	}

	return client, nil
}

// SendMail delivers a raw MIME message via upstream SMTP using context credentials.
func (p *ClientPool) SendMail(ctx context.Context, from string, recipients []string, rawMessage []byte) error {
	if p.smtpAddr == "" {
		return errors.New("smtp server address not configured")
	}

	c, err := smtp.Dial(p.smtpAddr)
	if err != nil {
		return fmt.Errorf("failed to dial SMTP server %s: %w", p.smtpAddr, err)
	}
	defer c.Close()

	host, _, err := net.SplitHostPort(p.smtpAddr)
	if err != nil {
		host = p.smtpAddr
	}

	if ok, _ := c.Extension("STARTTLS"); ok {
		config := &tls.Config{InsecureSkipVerify: true, ServerName: host}
		if err = c.StartTLS(config); err != nil {
			return fmt.Errorf("SMTP StartTLS failed: %w", err)
		}
	}

	creds, _ := jmap.CredentialsFromContext(ctx)
	if ok, _ := c.Extension("AUTH"); ok && creds.Username != "" && creds.Password != "" {
		auth := smtp.PlainAuth("", creds.Username, creds.Password, host)
		if err = c.Auth(auth); err != nil {
			return fmt.Errorf("SMTP Auth failed: %w", err)
		}
	}

	if err = c.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}
	for _, addr := range recipients {
		if err = c.Rcpt(addr); err != nil {
			return fmt.Errorf("SMTP RCPT TO failed for %s: %w", addr, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err = w.Write(rawMessage); err != nil {
		return fmt.Errorf("SMTP data write failed: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("SMTP data close failed: %w", err)
	}

	return c.Quit()
}
