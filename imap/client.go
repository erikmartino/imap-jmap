package imap

import (
	"crypto/tls"
	"fmt"
	"net"

	"github.com/emersion/go-imap/v2/imapclient"
)

// Client is a thin wrapper around go-imap's imapclient.Client that manages connection and authentication to an IMAP server.
type Client struct {
	*imapclient.Client
}

// Dial connects and authenticates to an IMAP server at the specified address using the given credentials.
// It automatically handles TLS (port 993), STARTTLS, or plain TCP fallbacks as appropriate.
func Dial(addr string, username, password string) (*Client, error) {
	var c *imapclient.Client
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = "993"
	}

	if port == "993" {
		client, err := imapclient.DialTLS(addr, &imapclient.Options{
			TLSConfig: &tls.Config{InsecureSkipVerify: true, ServerName: host},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to dial TLS IMAP server %s: %w", addr, err)
		}
		c = client
	} else {
		client, err := imapclient.DialStartTLS(addr, &imapclient.Options{
			TLSConfig: &tls.Config{InsecureSkipVerify: true, ServerName: host},
		})
		if err != nil {
			client, err = imapclient.DialInsecure(addr, &imapclient.Options{})
			if err != nil {
				return nil, fmt.Errorf("failed to connect to IMAP server %s: %w", addr, err)
			}
		}
		c = client
	}

	if err := c.Login(username, password).Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("IMAP login failed for user %s: %w", username, err)
	}

	return &Client{Client: c}, nil
}

// Close closes the underlying IMAP client connection.
func (c *Client) Close() error {
	if c.Client != nil {
		return c.Client.Close()
	}
	return nil
}
