package smtp_test

import (
	"net"
	"testing"
	"time"

	"imap-jmap/jmap/memory"
	jmapsmtp "imap-jmap/smtp"
)

// TestRFC4954_SMTPAuthentication tests RFC 4954 SMTP Service Extension for Authentication (AUTH PLAIN).
func TestRFC4954_SMTPAuthentication(t *testing.T) {
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	srv := jmapsmtp.NewServer(addr, memBackend, memBlobBackend, nil)
	go func() {
		_ = srv.ListenAndServe()
	}()
	defer srv.Close()

	time.Sleep(50 * time.Millisecond)

	backend := jmapsmtp.NewReceiverBackend(memBackend, memBlobBackend, nil)
	sess, err := backend.NewSession(nil)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	sessImpl, ok := sess.(*jmapsmtp.Session)
	if !ok {
		t.Fatalf("Expected *jmapsmtp.Session, got %T", sess)
	}

	err = sessImpl.AuthPlain("user@example.com", "secret")
	if err != nil {
		t.Errorf("AuthPlain failed per RFC 4954: %v", err)
	}
}
