package imapsmtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/emersion/go-imap/v2/imapclient"
)


// ClientPool manages active IMAP connections for user accounts.
type ClientPool struct {
	imapAddr string
}

func NewClientPool(imapAddr string) *ClientPool {
	return &ClientPool{imapAddr: imapAddr}
}

// GetClient establishes an authenticated IMAP client connection using the provided credentials.
func (p *ClientPool) GetClient(ctx context.Context, username, password string) (*imapclient.Client, error) {
	var client *imapclient.Client
	host, port, _ := net.SplitHostPort(p.imapAddr)
	if port == "993" {
		c, err := imapclient.DialTLS(p.imapAddr, &imapclient.Options{
			TLSConfig: &tls.Config{InsecureSkipVerify: true},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to dial TLS IMAP server %s: %w", p.imapAddr, err)
		}
		client = c
	} else {
		conn, err := net.Dial("tcp", p.imapAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to IMAP server %s: %w", p.imapAddr, err)
		}
		// Upgrade to TLS using STARTTLS if plaintext login is disallowed over plain TCP
		tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, ServerName: host})
		if err := tlsConn.Handshake(); err == nil {
			client = imapclient.New(tlsConn, nil)
		} else {
			conn.Close()
			return nil, fmt.Errorf("TLS handshake failed for %s: %w", p.imapAddr, err)
		}
	}


	if err := client.Login(username, password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("IMAP login failed for user %s: %w", username, err)
	}

	return client, nil
}
