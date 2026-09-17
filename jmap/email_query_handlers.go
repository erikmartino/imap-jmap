package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

func handleEmailQuery(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailQuery(backend)
}

func handleEmailQueryChanges(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailQueryChanges(backend)
}
