package imapsmtp

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-smtp"

	"imap-jmap/jmap"
)

type mockIMAPUserSession struct{}

func (s *mockIMAPUserSession) Close() error                                                 { return nil }
func (s *mockIMAPUserSession) Login(username, password string) error                        { return nil }
func (s *mockIMAPUserSession) Select(mailbox string, options *imap.SelectOptions) (*imap.SelectData, error) {
	return &imap.SelectData{}, nil
}
func (s *mockIMAPUserSession) Create(mailbox string, options *imap.CreateOptions) error     { return nil }
func (s *mockIMAPUserSession) Delete(mailbox string) error                                   { return nil }
func (s *mockIMAPUserSession) Rename(mailbox, newName string, options *imap.RenameOptions) error {
	return nil
}
func (s *mockIMAPUserSession) Subscribe(mailbox string) error                               { return nil }
func (s *mockIMAPUserSession) Unsubscribe(mailbox string) error                             { return nil }
func (s *mockIMAPUserSession) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	return nil
}
func (s *mockIMAPUserSession) Status(mailbox string, options *imap.StatusOptions) (*imap.StatusData, error) {
	return &imap.StatusData{}, nil
}
func (s *mockIMAPUserSession) Append(mailbox string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	return &imap.AppendData{}, nil
}
func (s *mockIMAPUserSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	return nil
}
func (s *mockIMAPUserSession) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	return nil
}

func (s *mockIMAPUserSession) Unselect() error { return nil }
func (s *mockIMAPUserSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	return nil
}
func (s *mockIMAPUserSession) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	return &imap.SearchData{}, nil
}
func (s *mockIMAPUserSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	return nil
}
func (s *mockIMAPUserSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	return nil
}
func (s *mockIMAPUserSession) Copy(numSet imap.NumSet, dest string) (*imap.CopyData, error) {
	return &imap.CopyData{}, nil
}

type mockSMTPBackend struct{}

func (b *mockSMTPBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &mockSMTPSession{}, nil
}

type mockSMTPSession struct{}

func (s *mockSMTPSession) Reset()                                           {}
func (s *mockSMTPSession) Logout() error                                    { return nil }
func (s *mockSMTPSession) Mail(from string, opts *smtp.MailOptions) error { return nil }
func (s *mockSMTPSession) Rcpt(to string, opts *smtp.RcptOptions) error   { return nil }
func (s *mockSMTPSession) Data(r io.Reader) error                           { return nil }

func startTestIMAPServer(t *testing.T) (string, func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for test IMAP server: %v", err)
	}

	options := &imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &mockIMAPUserSession{}, nil, nil
		},
		InsecureAuth: true,
	}

	srv := imapserver.New(options)
	go func() {
		_ = srv.Serve(ln)
	}()

	addr := ln.Addr().String()
	cleanup := func() {
		_ = srv.Close()
		_ = ln.Close()
	}

	return addr, cleanup
}

func startTestSMTPServer(t *testing.T) (string, func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for test SMTP server: %v", err)
	}

	srv := smtp.NewServer(&mockSMTPBackend{})
	srv.AllowInsecureAuth = true
	go func() {
		_ = srv.Serve(ln)
	}()

	addr := ln.Addr().String()
	cleanup := func() {
		_ = srv.Close()
		_ = ln.Close()
	}

	return addr, cleanup
}

func TestClientPool_LocalRealIMAPConnection(t *testing.T) {
	imapAddr, cleanupIMAP := startTestIMAPServer(t)
	defer cleanupIMAP()

	pool := NewClientPool(imapAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client, err := pool.GetClient(ctx, "user", "pass")
	if err != nil {
		t.Fatalf("failed to get client from pool: %v", err)
	}
	client.Close()
}

func TestIMAPSMTPBackend_LocalRealServersIntegration(t *testing.T) {
	imapAddr, cleanupIMAP := startTestIMAPServer(t)
	defer cleanupIMAP()

	smtpAddr, cleanupSMTP := startTestSMTPServer(t)
	defer cleanupSMTP()

	be := New(imapAddr, smtpAddr)
	var _ jmap.MailBackend = be
	var _ jmap.BlobBackend = be

	ctx := context.Background()

	if state := be.State(ctx); state == "" {
		t.Errorf("expected non-empty state")
	}

	mailboxes, err := be.GetAllMailboxes(ctx)
	if err != nil {
		t.Fatalf("unexpected error getting mailboxes: %v", err)
	}
	if len(mailboxes) != 0 {
		t.Errorf("expected 0 mailboxes initially, got %d", len(mailboxes))
	}
}
