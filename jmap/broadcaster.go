package jmap

import (
	"imap-jmap/jmap/jmappush"
)

// Re-exports from jmappush for backward compatibility.
type StateChange = jmappush.StateChange
type StateChangeListener = jmappush.StateChangeListener
type SubscriptionListener = jmappush.SubscriptionListener
type Broadcaster = jmappush.Broadcaster

var NewBroadcaster = jmappush.NewBroadcaster
