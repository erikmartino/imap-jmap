package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

// handleMDNSend processes MDN/send method calls per RFC 9007 Section 3.1.
func handleMDNSend(backend MailBackend) MethodHandler {
	return jmapmail.HandleMDNSend(backend)
}

// handleMDNParse processes MDN/parse method calls per RFC 9007 Section 3.2.
func handleMDNParse(backend MailBackend) MethodHandler {
	return jmapmail.HandleMDNParse(backend)
}
