package jmap

import (
	"context"
	"strings"
)

// Creation references (RFC 8620 Section 5.3) let a single /set call refer to objects it is
// creating in the same request via a "#creationId" placeholder in any field that takes an Id.
// The server assigns the real id and substitutes it everywhere the placeholder appears, so a
// client can create a parent and its children (a composite update) in one round trip.
//
// Creation ids are not scoped by type: the server MUST keep a single map of creation id to
// real id for the duration of the request (RFC 8620 Section 5.3), so a later method call may
// reference an object created by an earlier one. The map is seeded from the Request
// "createdIds" property and echoed back in the Response when the client supplied it.

// resolveCreationRef resolves a single value that may be a "#creationId" placeholder.
//   - resolved: creationId -> assigned real Id, for creations already completed in this call.
//   - pending:  creationIds still awaiting creation in this call (nil outside a create loop,
//     i.e. for update patches, where an unresolvable placeholder is left for the backend).
//
// It returns the concrete value to use, and defer=true when the value references a creation
// that has not been made yet but is still pending (the caller should retry it later).
// In a create payload a placeholder naming a creation id that is not being made in this call
// can never resolve, so it is reported as deferred to force the caller to reject the
// creation (RFC 8620 Section 5.3 requires rejecting dangling references).
func resolveCreationRef(v any, resolved map[string]Id, pending map[string]struct{}) (out any, deferred bool) {
	s, ok := v.(string)
	if !ok || !strings.HasPrefix(s, "#") {
		return v, false
	}
	cid := s[1:]
	if realID, done := resolved[cid]; done {
		return string(realID), false
	}
	if pending != nil {
		return v, true
	}
	// Update patch: leave it so the backend rejects it.
	return v, false
}

// resolveNodeCreationRefs substitutes every "#creationId" placeholder in a create payload:
// top-level Id-typed string values (e.g. Mailbox "parentId") and the keys of Id[Boolean]
// set-maps (e.g. Email "mailboxIds", Card "addressBookIds", CalendarEvent "calendarIds").
// It returns deferred=true if any referenced creation is still pending, so the caller can
// process the payload after its dependencies are created.
func resolveNodeCreationRefs(nodeMap map[string]any, resolved map[string]Id, pending map[string]struct{}) (map[string]any, bool) {
	out := make(map[string]any, len(nodeMap))
	for k, v := range nodeMap {
		rv, deferred := resolveCreationRef(v, resolved, pending)
		if deferred {
			return nil, true
		}
		if sub, isMap := v.(map[string]any); isMap {
			subResolved, subDeferred := resolveIdBooleanMapRefs(sub, resolved, pending)
			if subDeferred {
				return nil, true
			}
			rv = subResolved
		}
		out[k] = rv
	}
	return out, false
}

// resolveIdBooleanMapRefs resolves "#creationId" keys in an Id[Boolean] set-map.
func resolveIdBooleanMapRefs(m map[string]any, resolved map[string]Id, pending map[string]struct{}) (map[string]any, bool) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		rv, deferred := resolveCreationRef(k, resolved, pending)
		if deferred {
			return nil, true
		}
		key, _ := rv.(string)
		out[key] = v
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

// runCreateLoop executes the creates of a /set call in dependency order, deferring any
// create whose "#creationId" references are not yet satisfied so that forward references
// (a child listed before its parent) resolve; RFC 8620 Section 5.3 requires the server to
// order creates so dependencies come first. Creates that cannot ever be satisfied (cycles
// or references to creation ids never created in this call) are reported in notCreated.
// The do callback builds the concrete object from the resolved payload and calls the
// backend, returning the server-assigned id on success.
func runCreateLoop(createRaw map[string]any, refs map[string]Id, do func(creationID string, resolved map[string]any) (string, error)) map[string]any {
	notCreated := make(map[string]any)
	pending := make(map[string]struct{}, len(createRaw))
	for cid := range createRaw {
		pending[cid] = struct{}{}
	}
	for len(pending) > 0 {
		progressed := false
		for cid := range pending {
			objMap, _ := createRaw[cid].(map[string]any)
			resolved, deferred := resolveNodeCreationRefs(objMap, refs, pending)
			if deferred {
				continue
			}
			realID, err := do(cid, resolved)
			if err != nil {
				if setErr, ok := err.(SetError); ok {
					notCreated[cid] = setErr
				} else if strings.HasPrefix(err.Error(), "forbidden:") {
					notCreated[cid] = SetError{Type: "forbidden", Description: strings.TrimSpace(strings.TrimPrefix(err.Error(), "forbidden:"))}
				} else {
					notCreated[cid] = SetError{Type: "invalidProperties", Description: err.Error()}
				}
			} else {
				refs[cid] = Id(realID)
			}
			delete(pending, cid)
			progressed = true
		}
		if !progressed {
			for cid := range pending {
				notCreated[cid] = SetError{Type: "invalidProperties", Description: "unresolved creation reference"}
				delete(pending, cid)
			}
		}
	}
	return notCreated
}

// resolvePatchCreationRefs substitutes "#creationId" references in an update PatchObject:
// string values of Id-typed properties (e.g. Mailbox "parentId"), the keys of Id[Boolean]
// set-maps (e.g. Email "mailboxIds"), and "#cid" update keys.
func resolvePatchCreationRefs(patch map[string]any, refs map[string]Id) map[string]any {
	out := make(map[string]any, len(patch))
	for k, v := range patch {
		key := resolveCreationID(k, refs)
		switch tv := v.(type) {
		case string:
			rv, deferred := resolveCreationRef(tv, refs, nil)
			if !deferred {
				v = rv
			}
		case map[string]any:
			resolved, _ := resolveIdBooleanMapRefs(tv, refs, nil)
			v = resolved
		}
		out[key] = v
	}
	return out
}

// creationRefsKey is the context key carrying the request-scoped creation id map.
type creationRefsKey struct{}

// CreationRefs is the request-scoped map of creation id to assigned real id that the server
// MUST keep for the duration of a request (RFC 8620 Sections 3.3 & 5.3). It is shared by
// every /set method call in the request, seeded from the Request "createdIds" property, and
// echoed back in the Response when the client supplied it.
type CreationRefs struct {
	m map[string]string
}

// NewCreationRefs returns a CreationRefs seeded with the initial map (may be nil).
func NewCreationRefs(initial map[string]string) *CreationRefs {
	m := make(map[string]string, len(initial))
	for k, v := range initial {
		m[k] = v
	}
	return &CreationRefs{m: m}
}

// WithCreationRefs returns a context carrying the request-scoped creation id map.
func WithCreationRefs(ctx context.Context, refs *CreationRefs) context.Context {
	return context.WithValue(ctx, creationRefsKey{}, refs)
}

// CreationRefsFrom returns the request-scoped creation id map, or nil if not present.
func CreationRefsFrom(ctx context.Context) *CreationRefs {
	refs, _ := ctx.Value(creationRefsKey{}).(*CreationRefs)
	return refs
}

// Snapshot returns a copy of the current map, for the Response "createdIds" property.
func (r *CreationRefs) Snapshot() map[string]string {
	out := make(map[string]string, len(r.m))
	for k, v := range r.m {
		out[k] = v
	}
	return out
}

// newSetCreationRefs builds the per-/set-call resolution map, seeded from the request-scoped
// createdIds map so references can span method calls within a request.
func newSetCreationRefs(ctx context.Context) map[string]Id {
	refs := make(map[string]Id)
	if r := CreationRefsFrom(ctx); r != nil {
		for cid, real := range r.m {
			refs[cid] = Id(real)
		}
	}
	return refs
}

// recordCreationRefs adds a newly assigned id to both the per-call map and the
// request-scoped map so later method calls in the request can reference it.
func recordCreationRefs(ctx context.Context, refs map[string]Id, cid string, realID Id) {
	refs[cid] = realID
	if r := CreationRefsFrom(ctx); r != nil {
		r.m[cid] = string(realID)
	}
}
