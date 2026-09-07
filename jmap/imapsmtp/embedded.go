package imapsmtp

import (
	"net"
	"sync"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

type dynamicSession struct {
	imapserver.Session
	server     *imapmemserver.Server
	userMu     *sync.Mutex
	knownUsers map[string]bool
}

var (
	_ imapserver.Session          = (*dynamicSession)(nil)
	_ imapserver.SessionIMAP4rev2 = (*dynamicSession)(nil)
	_ imapserver.SessionMove      = (*dynamicSession)(nil)
	_ imapserver.SessionNamespace = (*dynamicSession)(nil)
)

func (s *dynamicSession) Move(w *imapserver.MoveWriter, numSet imap.NumSet, destName string) error {
	return s.Session.(imapserver.SessionMove).Move(w, numSet, destName)
}

func (s *dynamicSession) Namespace() (*imap.NamespaceData, error) {
	return s.Session.(imapserver.SessionNamespace).Namespace()
}

func (s *dynamicSession) Login(username, password string) error {
	s.userMu.Lock()
	if !s.knownUsers[username] {
		u := imapmemserver.NewUser(username, password)
		_ = u.Create("INBOX", nil)
		_ = u.Create("Drafts", nil)
		_ = u.Create("Sent", nil)
		_ = u.Create("Trash", nil)
		_ = u.Create("Junk", nil)
		_ = u.Create("Archive", nil)
		s.server.AddUser(u)
		s.knownUsers[username] = true
	}
	s.userMu.Unlock()
	return s.Session.Login(username, password)
}

// NewEmbeddedBackend creates a standalone, in-process IMAP server and returns an
// IMAPSMTPBackend connected to it, along with a cleanup function.
// Dynamic user provisioning is enabled: any user logging in for the first time
// is automatically initialized with standard mailboxes.
func NewEmbeddedBackend(usernames ...string) (*IMAPSMTPBackend, func()) {
	if len(usernames) == 0 {
		usernames = []string{"user@example.com"}
	}

	memServer := imapmemserver.New()
	userMu := &sync.Mutex{}
	knownUsers := make(map[string]bool)

	for _, u := range usernames {
		user := imapmemserver.NewUser(u, u)
		_ = user.Create("INBOX", nil)
		_ = user.Create("Drafts", nil)
		_ = user.Create("Sent", nil)
		_ = user.Create("Trash", nil)
		_ = user.Create("Junk", nil)
		_ = user.Create("Archive", nil)
		memServer.AddUser(user)
		knownUsers[u] = true
	}

	caps := imap.CapSet{
		imap.CapIMAP4rev1: {},
		imap.CapIMAP4rev2: {},
		imap.CapNamespace: {},
		imap.CapUIDPlus:   {},
		imap.CapMove:      {},
	}

	opts := &imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &dynamicSession{
				Session:    memServer.NewSession(),
				server:     memServer,
				userMu:     userMu,
				knownUsers: knownUsers,
			}, nil, nil
		},
		Caps:         caps,
		InsecureAuth: true,
	}
	server := imapserver.New(opts)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	go func() {
		_ = server.Serve(listener)
	}()

	backend := New(listener.Addr().String(), "")

	cleanup := func() {
		_ = backend.Close()
		_ = server.Close()
		_ = listener.Close()
	}
	return backend, cleanup
}
