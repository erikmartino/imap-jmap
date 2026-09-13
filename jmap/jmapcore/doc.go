// Package jmapcore implements the core JSON Meta Application Protocol (JMAP) data types,
// protocol envelope, batch execution, creation reference resolution, patch merging, and push dispatch
// as defined in IETF RFC 8620.
//
// Specification References:
//   - RFC 8620 §1: Data Types (Id, Date, UnsignedInt, PatchObject)
//   - RFC 8620 §2: Session Resource & Capabilities
//   - RFC 8620 §3: Protocol Envelope & Invocation Framing
//   - RFC 8620 §3.3: Result References (#resultRef)
//   - RFC 8620 §5.3: Creation Id Resolution (#creationId) & Set Operations
//   - RFC 8620 §5.5: Query Pagination (Position, Limit, Anchor)
//   - RFC 8620 §7: Push Event Dispatching (StateChange)
package jmapcore
