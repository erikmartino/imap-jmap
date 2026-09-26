package jmapcore

import (
	"context"
	"reflect"
	"strings"
)

// Creation references (RFC 8620 Section 5.3) let a single /set call refer to objects it is
// creating in the same request via a "#creationId" placeholder in any field that takes an Id.
// @spec RFC8620#5.3-p1-MUST

func IsIdProperty(k string) bool {
	lk := strings.ToLower(k)
	return lk == "id" || strings.HasSuffix(lk, "id") || strings.HasSuffix(lk, "ids")
}

// NilIfEmpty returns nil when v is an empty map or slice, so that RFC 8620
// Section 5.3 set-method arguments typed "Id[...]|null" serialize as JSON null.
// @spec RFC8620#5.3-p2-MUST
func NilIfEmpty(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		if rv.Len() == 0 {
			return nil
		}
	}
	return v
}

// ResolveCreationRef resolves a single value that may be a "#creationId" placeholder.
// @spec RFC8620#5.3-p3-MUST
func ResolveCreationRef(v any, resolved map[string]Id, pending map[string]struct{}) (out any, deferred bool) {
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
	return v, false
}

// ResolveNodeCreationRefs substitutes every "#creationId" placeholder in a create payload.
// @spec RFC8620#5.3-p4-MUST
func ResolveNodeCreationRefs(nodeMap map[string]any, resolved map[string]Id, pending map[string]struct{}) (map[string]any, bool) {
	out := make(map[string]any, len(nodeMap))
	for k, v := range nodeMap {
		if !IsIdProperty(k) {
			out[k] = v
			continue
		}
		rv, deferred := ResolveCreationRef(v, resolved, pending)
		if deferred {
			return nil, true
		}
		if sub, isMap := v.(map[string]any); isMap {
			subResolved, subDeferred := ResolveIdBooleanMapRefs(sub, resolved, pending)
			if subDeferred {
				return nil, true
			}
			rv = subResolved
		}
		out[k] = rv
	}
	return out, false
}

// ResolveIdBooleanMapRefs resolves "#creationId" keys in an Id[Boolean] set-map.
// @spec RFC8620#5.3-p5-MUST
func ResolveIdBooleanMapRefs(m map[string]any, resolved map[string]Id, pending map[string]struct{}) (map[string]any, bool) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		rv, deferred := ResolveCreationRef(k, resolved, pending)
		if deferred {
			return nil, true
		}
		key, _ := rv.(string)
		out[key] = v
	}
	return out, false
}

// ResolveCreationID resolves a "#creationId" reference used as an update key or destroy id.
// @spec RFC8620#5.3-p6-MUST
func ResolveCreationID(id string, resolved map[string]Id) string {
	if !strings.HasPrefix(id, "#") {
		return id
	}
	if realID, ok := resolved[id[1:]]; ok {
		return string(realID)
	}
	return id
}

// SetError defines an error object for /set methods per RFC 8620 Section 5.3.
// @spec RFC8620#5.3-p7-MUST
type SetError struct {
	Type              string   `json:"type"`
	Description       string   `json:"description,omitempty"`
	Properties        []string `json:"properties,omitempty"`
	NotFound          []string `json:"notFound,omitempty"`
	ExistingID        Id       `json:"existingId,omitempty"`
	MaxSize           *uint64  `json:"maxSize,omitempty"`
	MaxRecipients     *uint64  `json:"maxRecipients,omitempty"`
	InvalidRecipients []string `json:"invalidRecipients,omitempty"`
}

func (e SetError) Error() string {
	if e.Description != "" {
		return e.Type + ": " + e.Description
	}
	return e.Type
}

// RunCreateLoop executes the creates of a /set call in dependency order.
// @spec RFC8620#5.3-p8-MUST
func RunCreateLoop(createRaw map[string]any, refs map[string]Id, do func(creationID string, resolved map[string]any) (string, error)) map[string]any {
	notCreated := make(map[string]any)
	pending := make(map[string]struct{}, len(createRaw))
	for cid := range createRaw {
		pending[cid] = struct{}{}
	}
	for len(pending) > 0 {
		progressed := false
		for cid := range pending {
			objMap, _ := createRaw[cid].(map[string]any)
			resolved, deferred := ResolveNodeCreationRefs(objMap, refs, pending)
			if deferred {
				continue
			}
			realID, err := do(cid, resolved)
			if err != nil {
				if setErr, ok := err.(SetError); ok {
					notCreated[cid] = setErr
				} else if setErrPtr, ok := err.(*SetError); ok && setErrPtr != nil {
					notCreated[cid] = *setErrPtr
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

// ResolvePatchCreationRefs substitutes "#creationId" references in an update PatchObject.
// @spec RFC8620#5.3-p9-MUST
func ResolvePatchCreationRefs(patch map[string]any, refs map[string]Id) map[string]any {
	out := make(map[string]any, len(patch))
	for k, v := range patch {
		key := ResolveCreationID(k, refs)
		switch tv := v.(type) {
		case string:
			rv, deferred := ResolveCreationRef(tv, refs, nil)
			if !deferred {
				v = rv
			}
		case map[string]any:
			resolved, _ := ResolveIdBooleanMapRefs(tv, refs, nil)
			v = resolved
		}
		out[key] = v
	}
	return out
}

type creationRefsKey struct{}

// CreationRefs is the request-scoped map of creation id to assigned real id.
// @spec RFC8620#5.3-p10-MUST
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

// NewSetCreationRefs builds the per-/set-call resolution map.
func NewSetCreationRefs(ctx context.Context) map[string]Id {
	refs := make(map[string]Id)
	if r := CreationRefsFrom(ctx); r != nil {
		for cid, real := range r.m {
			refs[cid] = Id(real)
		}
	}
	return refs
}

// RecordCreationRefs adds a newly assigned id to both the per-call map and request-scoped map.
func RecordCreationRefs(ctx context.Context, refs map[string]Id, cid string, realID Id) {
	if refs != nil {
		refs[cid] = realID
	}
	if r := CreationRefsFrom(ctx); r != nil {
		r.m[cid] = string(realID)
	}
}
