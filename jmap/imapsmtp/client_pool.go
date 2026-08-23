package imapsmtp

import (
	"context"
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
	conn, err := net.Dial("tcp", p.imapAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to IMAP server %s: %w", p.imapAddr, err)
	}

	client := imapclient.New(conn, nil)
	if err := client.Login(username, password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("IMAP login failed for user %s: %w", username, err)
	}

	return client, nil
}
