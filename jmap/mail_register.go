package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

// RegisterMailHandlers registers all RFC 8621 JMAP Mail and RFC 9219 S/MIME methods into MethodRegistry.
// blobBackend is used by Email/import and Email/parse to read raw RFC 5322 message blobs, and by
// EmailSubmission/set to fetch the raw message bytes for outbound relay. outbound delivers
// submissions to external (allow-listed) recipients via their domain's MX servers.
func RegisterMailHandlers(r *MethodRegistry, backend MailBackend, blobBackend BlobBackend, resolver AccountResolver, allowedRecipients map[string]bool, outbound OutboundMailSender) {
	jmapmail.RegisterMailHandlers(r, backend, blobBackend, resolver, allowedRecipients, outbound)
}
