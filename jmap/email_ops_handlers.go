package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

func handleEmailCopy(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailCopy(backend)
}

func handleEmailImport(backend MailBackend, blobBackend BlobBackend) MethodHandler {
	return jmapmail.HandleEmailImport(backend, blobBackend)
}

func handleEmailParse(backend MailBackend, blobBackend BlobBackend) MethodHandler {
	return jmapmail.HandleEmailParse(backend, blobBackend)
}

func handleEmailVerifySmime(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailVerifySmime(backend)
}

func handleSearchSnippetGet(backend MailBackend) MethodHandler {
	return jmapmail.HandleSearchSnippetGet(backend)
}
