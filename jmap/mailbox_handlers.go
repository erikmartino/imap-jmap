package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

// Mailbox Handlers (RFC 8621 Section 2)

func handleMailboxGet(backend MailBackend) MethodHandler {
	return jmapmail.HandleMailboxGet(backend)
}

func handleMailboxChanges(backend MailBackend) MethodHandler {
	return jmapmail.HandleMailboxChanges(backend)
}

func handleMailboxSet(backend MailBackend) MethodHandler {
	return jmapmail.HandleMailboxSet(backend)
}

func handleMailboxQuery(backend MailBackend) MethodHandler {
	return jmapmail.HandleMailboxQuery(backend)
}

func handleMailboxQueryChanges(backend MailBackend) MethodHandler {
	return jmapmail.HandleMailboxQueryChanges(backend)
}

func handleMailboxCopy(backend MailBackend) MethodHandler {
	return jmapmail.HandleMailboxCopy(backend)
}
