package jmap

import "strings"

// Creation references (RFC 8620 Section 5.3) let a single /set call refer to objects it is
// creating in the same request via a "#creationId" placeholder in any field that takes an Id.
// The server assigns the real id and substitutes it everywhere the placeholder appears, so a
// client can create a parent and its children (a composite update) in one round trip.

// resolveCreationRef resolves a single value that may be a "#creationId" placeholder.
//   - resolved: creationId -> assigned real Id, for creations already completed in this call.
//   - pending:  creationIds still awaiting creation in this call.
//
// It returns the concrete value to use, and defer=true when the value references a creation
// that has not been made yet but is still pending (the caller should retry it later).
func resolveCreationRef(v any, resolved map[string]Id, pending map[string]struct{}) (out any, deferred bool) {
	s, ok := v.(string)
	if !ok || !strings.HasPrefix(s, "#") {
		return v, false
	}
	cid := s[1:]
	if realID, done := resolved[cid]; done {
		return string(realID), false
	}
	if _, isPending := pending[cid]; isPending {
		return nil, true
	}
	// References a creation id that does not exist in this call; leave it so the backend rejects it.
	return v, false
}

// resolveNodeCreationRefs substitutes every "#creationId" placeholder in a create payload.
// It returns deferred=true if any referenced creation is still pending, so the caller can
// process the payload after its dependencies are created.
func resolveNodeCreationRefs(nodeMap map[string]any, resolved map[string]Id, pending map[string]struct{}) (map[string]any, bool) {
	out := make(map[string]any, len(nodeMap))
	for k, v := range nodeMap {
		rv, deferred := resolveCreationRef(v, resolved, pending)
		if deferred {
			return nil, true
		}
		out[k] = rv
	}
	return out, false
}

// resolveCreationID resolves a "#creationId" reference used as an update key or destroy id
// against creations completed earlier in the same /set call. Non-placeholder ids pass through.
func resolveCreationID(id string, resolved map[string]Id) string {
	if !strings.HasPrefix(id, "#") {
		return id
	}
	if realID, ok := resolved[id[1:]]; ok {
		return string(realID)
	}
	return id
}
