package imapsmtp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"imap-jmap/imap"
	"imap-jmap/jmap/jmapauth"
)

type idleWatcherEntry struct {
	cancel context.CancelFunc
}

// startIdleWatcher starts a dedicated IMAP IDLE (RFC 2177) connection for the account.
func (b *IMAPSMTPBackend) startIdleWatcher(accountID string, creds jmapauth.AuthCredentials) {
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
func (b *IMAPSMTPBackend) runIdleLoop(idleCtx context.Context, accountID string, creds jmapauth.AuthCredentials) error {
	notifyCh := make(chan struct{}, 10)
	triggerNotify := func() {
		select {
		case notifyCh <- struct{}{}:
		default:
		}
	}

	client, err := imap.DialIdle(b.imapHost, creds.Username, creds.Password, imap.UnilateralHandlers{
		Mailbox: func() {
			slog.Debug("IMAP IDLE received Mailbox unilateral update", "accountID", accountID)
			triggerNotify()
		},
		Expunge: func(seqNum uint32) {
			slog.Debug("IMAP IDLE received Expunge unilateral update", "accountID", accountID, "seqNum", seqNum)
			triggerNotify()
		},
		Fetch: func() {
			slog.Debug("IMAP IDLE received Fetch unilateral update", "accountID", accountID)
			triggerNotify()
		},
	})
	if err != nil {
		return fmt.Errorf("IMAP IDLE connection/login failed for user %s: %w", creds.Username, err)
	}
	defer client.Close()

	slog.Info("IMAP IDLE watcher connected and authenticated", "accountID", accountID, "user", creds.Username)

	if err := client.Select("INBOX"); err != nil {
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
			ctx := jmapauth.ContextWithAccountID(context.Background(), accountID)
			ctx = jmapauth.ContextWithCredentials(ctx, creds.Username, creds.Password)
			ctx = jmapauth.ContextWithSubject(ctx, creds.Username)

			cs, err := b.GetCurrentCompositeState(ctx)
			if err == nil {
				token := cs.Encode()
				b.accountsMu.Lock()
				b.lastStates[accountID] = token
				b.accountsMu.Unlock()

				slog.Info("IMAP IDLE change event detected -> broadcasting JMAP StateChange", "accountID", accountID, "newState", token)
				if b.broadcaster != nil {
					b.broadcaster.PublishStateChanges(accountID, map[string]string{
						"Email":   token,
						"Mailbox": token,
						"Thread":  token,
					})
				}
			}

		case <-keepaliveTimer.C:
			_ = idleCmd.Close()
			_ = idleCmd.Wait()
		}
	}
}
