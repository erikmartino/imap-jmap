package imapsmtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"imap-jmap/jmap"
)

type idleWatcherEntry struct {
	cancel context.CancelFunc
}

// startIdleWatcher starts a dedicated IMAP IDLE (RFC 2177) connection for the account.
func (b *IMAPSMTPBackend) startIdleWatcher(accountID string, creds jmap.AuthCredentials) {
	b.idleMu.Lock()
	if b.idleWatchers == nil {
		b.idleWatchers = make(map[string]*idleWatcherEntry)
	}
	if _, exists := b.idleWatchers[accountID]; exists {
		b.idleMu.Unlock()
		return
	}

	idleCtx, cancel := context.WithCancel(b.ctx)
	b.idleWatchers[accountID] = &idleWatcherEntry{cancel: cancel}
	b.idleMu.Unlock()

	slog.Info("Starting on-demand IMAP IDLE watcher for push subscriber", "accountID", accountID, "user", creds.Username)

	go func() {
		for {
			if idleCtx.Err() != nil {
				return
			}
			err := b.runIdleLoop(idleCtx, accountID, creds)
			if idleCtx.Err() != nil {
				return
			}
			if err != nil {
				slog.Debug("IMAP IDLE watcher connection disconnected, reconnecting in 3s", "accountID", accountID, "error", err)
			}
			select {
			case <-idleCtx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
}

// stopIdleWatcher terminates the active IMAP IDLE watcher when no push subscribers remain.
func (b *IMAPSMTPBackend) stopIdleWatcher(accountID string) {
	b.idleMu.Lock()
	entry, exists := b.idleWatchers[accountID]
	if exists {
		delete(b.idleWatchers, accountID)
	}
	b.idleMu.Unlock()

	if exists && entry != nil && entry.cancel != nil {
		slog.Info("Stopping IMAP IDLE watcher as all push subscribers disconnected", "accountID", accountID)
		entry.cancel()
	}
}

// runIdleLoop maintains an active IMAP IDLE session per RFC 2177.
func (b *IMAPSMTPBackend) runIdleLoop(idleCtx context.Context, accountID string, creds jmap.AuthCredentials) error {
	notifyCh := make(chan struct{}, 10)
	triggerNotify := func() {
		select {
		case notifyCh <- struct{}{}:
		default:
		}
	}

	unilateral := &imapclient.UnilateralDataHandler{
		Mailbox: func(data *imapclient.UnilateralDataMailbox) {
			slog.Debug("IMAP IDLE received Mailbox unilateral update", "accountID", accountID)
			triggerNotify()
		},
		Expunge: func(seqNum uint32) {
			slog.Debug("IMAP IDLE received Expunge unilateral update", "accountID", accountID, "seqNum", seqNum)
			triggerNotify()
		},
		Fetch: func(msg *imapclient.FetchMessageData) {
			slog.Debug("IMAP IDLE received Fetch unilateral update", "accountID", accountID)
			triggerNotify()
		},
	}

	host, port, err := net.SplitHostPort(b.imapHost)
	if err != nil {
		host = b.imapHost
		port = "993"
	}

	opts := &imapclient.Options{
		UnilateralDataHandler: unilateral,
	}

	var client *imapclient.Client
	if port == "993" {
		opts.TLSConfig = &tls.Config{InsecureSkipVerify: true, ServerName: host}
		c, err := imapclient.DialTLS(b.imapHost, opts)
		if err != nil {
			return fmt.Errorf("failed to dial TLS IMAP server %s: %w", b.imapHost, err)
		}
		client = c
	} else {
		opts.TLSConfig = &tls.Config{InsecureSkipVerify: true, ServerName: host}
		c, err := imapclient.DialStartTLS(b.imapHost, opts)
		if err != nil {
			c, err = imapclient.DialInsecure(b.imapHost, opts)
			if err != nil {
				return fmt.Errorf("failed to connect to IMAP server %s: %w", b.imapHost, err)
			}
		}
		client = c
	}
	defer client.Close()

	if err := client.Login(creds.Username, creds.Password).Wait(); err != nil {
		return fmt.Errorf("IMAP IDLE login failed for user %s: %w", creds.Username, err)
	}

	slog.Info("IMAP IDLE watcher connected and authenticated", "accountID", accountID, "user", creds.Username)

	if _, err := client.Select("INBOX", nil).Wait(); err != nil {
		return fmt.Errorf("failed to select INBOX for IDLE: %w", err)
	}

	for {
		idleCmd, err := client.Idle()
		if err != nil {
			return fmt.Errorf("IMAP IDLE command failed: %w", err)
		}

		// Keepalive: RFC 2177 Section 3 mandates client re-issues IDLE at least every 29 minutes.
		keepaliveTimer := time.NewTimer(15 * time.Minute)

		select {
		case <-idleCtx.Done():
			_ = idleCmd.Close()
			_ = idleCmd.Wait()
			keepaliveTimer.Stop()
			return nil

		case <-notifyCh:
			// Drain any coalesced notification events
			for len(notifyCh) > 0 {
				<-notifyCh
			}
			_ = idleCmd.Close()
			_ = idleCmd.Wait()
			keepaliveTimer.Stop()

			// Fetch latest composite state and publish immediately via broadcaster
			ctx := jmap.ContextWithAccountID(context.Background(), accountID)
			ctx = jmap.ContextWithCredentials(ctx, creds.Username, creds.Password)
			ctx = jmap.ContextWithSubject(ctx, creds.Username)

			cs, err := b.GetCurrentCompositeState(ctx)
			if err == nil {
				token := cs.Encode()
				b.accountsMu.Lock()
				b.lastStates[accountID] = token
				b.accountsMu.Unlock()

				slog.Info("IMAP IDLE change event detected -> broadcasting JMAP StateChange", "accountID", accountID, "newState", token)
				if b.broadcaster != nil {
					b.broadcaster.PublishStateChange(accountID, "Email", token)
					b.broadcaster.PublishStateChange(accountID, "Mailbox", token)
					b.broadcaster.PublishStateChange(accountID, "Thread", token)
				}
			}

		case <-keepaliveTimer.C:
			_ = idleCmd.Close()
			_ = idleCmd.Wait()
		}
	}
}
