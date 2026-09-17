// Package jmapauth provides authentication types, context helpers, and the AccountResolver
// interface shared by all JMAP domain handler packages. Domain packages import jmapauth
// instead of the top-level jmap package so they can access auth context without introducing
// a circular dependency.
//
// Specification References:
//   - RFC 8620 §2:   Session Resource (accountId in session object)
//   - RFC 8620 §3.4: Method Dispatch (accountId in every method call)
//   - RFC 8620 §8:   Security Considerations
//   - RFC 6750:      Bearer Token Usage
package jmapauth
