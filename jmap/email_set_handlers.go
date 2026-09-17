package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

func handleEmailSet(backend MailBackend, blobBackend BlobBackend) MethodHandler {
	return jmapmail.HandleEmailSet(backend, blobBackend)
}

func isValidKeyword(k string) bool {
	return jmapmail.IsValidKeyword(k)
}
