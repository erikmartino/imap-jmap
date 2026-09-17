package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

// handlePushSubscriptionGet processes PushSubscription/get per RFC 8620 Section 7.2.1.
func handlePushSubscriptionGet(backend MailBackend) MethodHandler {
	return jmapmail.HandlePushSubscriptionGet(backend)
}

// handlePushSubscriptionSet processes PushSubscription/set per RFC 8620 Section 7.2.2.
func handlePushSubscriptionSet(backend MailBackend) MethodHandler {
	return jmapmail.HandlePushSubscriptionSet(backend)
}
