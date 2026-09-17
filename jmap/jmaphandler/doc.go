// Package jmaphandler provides the core handler infrastructure for JMAP method dispatch:
// MethodHandler, MethodRegistry, property filtering, creation-reference helpers,
// and set/get limit validators. All handler packages import this instead of
// the top-level jmap package, breaking the circular dependency that would otherwise
// prevent domain packages from hosting their own handlers.
//
// Specification References:
//   - RFC 8620 §3.4:  Method Dispatch
//   - RFC 8620 §5.1:  /get — properties argument
//   - RFC 8620 §5.3:  /set — creation references (#creationId)
//   - RFC 8620 §6.1:  maxObjectsInGet capacity limit
package jmaphandler
