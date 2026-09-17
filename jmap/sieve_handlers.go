package jmap

import (
	"imap-jmap/jmap/jmapsieve"
)

// RegisterSieveHandlers registers RFC 9661 JMAP for Sieve Scripts method handlers into MethodRegistry.
func RegisterSieveHandlers(r *MethodRegistry, backend SieveBackend) {
	jmapsieve.RegisterSieveHandlers(r, backend)
}
