package imapsmtp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/emersion/go-message/mail"

	"imap-jmap/jmap/jmapextstore"
)

// IMAPExtensionStore implements jmapextstore.Store by persisting JSON state as RFC 822 messages in IMAP Drafts.
type IMAPExtensionStore struct {
	backend *IMAPSMTPBackend
	folder  string
}

// NewIMAPExtensionStore creates an extension store connected to the IMAP backend.
func NewIMAPExtensionStore(backend *IMAPSMTPBackend) *IMAPExtensionStore {
	return &IMAPExtensionStore{
		backend: backend,
		folder:  ".jmap",
	}
}

var _ jmapextstore.Store = (*IMAPExtensionStore)(nil)

func (s *IMAPExtensionStore) Get(ctx context.Context, accountID, key string) ([]byte, error) {
	ctx = s.backend.ensureContextCredentials(ctx, accountID)
	client, err := s.backend.pool.GetClientForContext(ctx)
	if err != nil {
		return nil, err
	}
	defer s.backend.pool.ReleaseClient(ctx, client)

	subjectQuery := fmt.Sprintf("[JMAP-STORE: %s]", key)
	uids, err := client.SearchSubject(s.folder, subjectQuery)
	if err != nil || len(uids) == 0 {
		return nil, nil
	}

	// Fetch the most recent message
	latestUID := uids[len(uids)-1]
	raw, err := client.FetchRawMessageByUID(s.folder, latestUID)
	if err != nil {
		return nil, err
	}

	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		return io.ReadAll(part.Body)
	}

	return nil, nil
}

func (s *IMAPExtensionStore) Put(ctx context.Context, accountID, key string, data []byte) error {
	ctx = s.backend.ensureContextCredentials(ctx, accountID)
	client, err := s.backend.pool.GetClientForContext(ctx)
	if err != nil {
		return err
	}
	defer s.backend.pool.ReleaseClient(ctx, client)

	var h mail.Header
	subject := fmt.Sprintf("[JMAP-STORE: %s]", key)
	h.SetSubject(subject)
	h.SetDate(time.Now())
	h.SetContentType("application/json", nil)
	h.Set("X-JMAP-Store-Key", key)

	var buf bytes.Buffer
	w, err := mail.CreateSingleInlineWriter(&buf, h)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	if errApp := client.Append(s.folder, buf.Bytes(), []string{"\\Seen"}, time.Now()); errApp != nil {
		if errCreate := client.Create(s.folder); errCreate == nil {
			if errApp2 := client.Append(s.folder, buf.Bytes(), []string{"\\Seen"}, time.Now()); errApp2 != nil {
				return errApp2
			}
		} else {
			return errApp
		}
	}

	// Clean up older messages for this key
	uids, err := client.SearchSubject(s.folder, subject)
	if err == nil && len(uids) > 1 {
		// Keep only the latest UID
		toDelete := uids[:len(uids)-1]
		_ = client.MarkDeletedAndExpunge(s.folder, toDelete)
	}

	return nil
}

func (s *IMAPExtensionStore) Delete(ctx context.Context, accountID, key string) error {
	ctx = s.backend.ensureContextCredentials(ctx, accountID)
	client, err := s.backend.pool.GetClientForContext(ctx)
	if err != nil {
		return err
	}
	defer s.backend.pool.ReleaseClient(ctx, client)

	subject := fmt.Sprintf("[JMAP-STORE: %s]", key)
	uids, err := client.SearchSubject(s.folder, subject)
	if err != nil || len(uids) == 0 {
		return nil
	}
	return client.MarkDeletedAndExpunge(s.folder, uids)
}
