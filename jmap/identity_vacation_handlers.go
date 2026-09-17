package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

func handleIdentityGet(backend MailBackend) MethodHandler {
	return jmapmail.HandleIdentityGet(backend)
}

func handleIdentityChanges(backend MailBackend) MethodHandler {
	return jmapmail.HandleIdentityChanges(backend)
}

func handleIdentitySet(backend MailBackend) MethodHandler {
	return jmapmail.HandleIdentitySet(backend)
}

func handleVacationResponseGet(backend MailBackend) MethodHandler {
	return jmapmail.HandleVacationResponseGet(backend)
}

func handleVacationResponseSet(backend MailBackend) MethodHandler {
	return jmapmail.HandleVacationResponseSet(backend)
}
