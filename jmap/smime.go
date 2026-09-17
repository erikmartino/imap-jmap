package jmap

import (
	"crypto/x509"

	"imap-jmap/jmap/jmapmail"
)

// SMIMEVerification encapsulates the result of verifying an S/MIME signed email per RFC 9219.
type SMIMEVerification = jmapmail.SMIMEVerification

// VerifySMIMEMessage inspects a raw RFC 822 email and performs S/MIME signature verification.
func VerifySMIMEMessage(raw []byte, trustedRoots *x509.CertPool) *SMIMEVerification {
	return jmapmail.VerifySMIMEMessage(raw, trustedRoots)
}
