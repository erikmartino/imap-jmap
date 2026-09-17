package jmap

import "imap-jmap/jmap/jmapmail"

// PushSubscriptionKeys holds client-provided encryption keys for push message encryption per RFC 8620 Section 7.2.
type PushSubscriptionKeys = jmapmail.PushSubscriptionKeys

// PushSubscription represents a JMAP Web Push subscription per RFC 8620 Section 7.2.
type PushSubscription = jmapmail.PushSubscription
