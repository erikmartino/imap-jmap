// Package jmapmail provides the domain types, backend interface, and outbound mail sender
// interface for JMAP Mail (RFC 8621), S/MIME verification (RFC 9219), Quotas (RFC 9425),
// and related resources. Domain handler packages import jmapmail instead of the top-level
// jmap package so they can reference mail types without a circular dependency.
//
// Specification References:
//   - RFC 8621: JMAP for Mail
//   - RFC 9219: S/MIME Signature Verification Extension
//   - RFC 9425: JMAP for Quotas
//   - RFC 9007: MDN in JMAP
package jmapmail
