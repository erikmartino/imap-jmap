package imapsmtp

import (
	"imap-jmap/imap"
)

// NewEmbeddedBackend creates a standalone, in-process IMAP server and returns an
// IMAPSMTPBackend connected to it, along with a cleanup function.
func NewEmbeddedBackend(usernames ...string) (*IMAPSMTPBackend, func()) {
	ts, cleanup := imap.NewTestServer(usernames...)
	backend := New(ts.Addr, "")
	return backend, func() {
		_ = backend.Close()
		cleanup()
	}
}
