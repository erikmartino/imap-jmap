package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

func handleThreadGet(backend MailBackend) MethodHandler {
	return jmapmail.HandleThreadGet(backend)
}

func handleThreadChanges(backend MailBackend) MethodHandler {
	return jmapmail.HandleThreadChanges(backend)
}
