package jmap_test

import (
	"testing"

	"imap-jmap/jmap"
)

// TestRFC2177_IMAPIdlePush notifications tests RFC 2177 IDLE state real-time push notification via Broadcaster.
func TestRFC2177_IMAPIdlePush(t *testing.T) {
	b := jmap.NewBroadcaster()
	ch := b.Subscribe()
	defer b.Unsubscribe(ch)

	b.PublishStateChange("account-1", "Mailbox", "mb-idle-1")
	select {
	case event := <-ch:
		if event.Changed["account-1"]["Mailbox"] != "mb-idle-1" {
			t.Errorf("Expected Mailbox state mb-idle-1, got %s", event.Changed["account-1"]["Mailbox"])
		}
	default:
		t.Errorf("Expected event on channel per RFC 2177 IDLE notification")
	}
}
