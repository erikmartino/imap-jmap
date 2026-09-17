package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

// RegisterQuotaHandlers registers RFC 9425 Quota method handlers into MethodRegistry.
func RegisterQuotaHandlers(r *MethodRegistry, backend MailBackend) {
	jmapmail.RegisterQuotaHandlers(r, backend)
}
