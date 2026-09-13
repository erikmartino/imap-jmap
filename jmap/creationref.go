package jmap

import (
	"context"

	"imap-jmap/jmap/jmapcore"
)

// Creation references (RFC 8620 Section 5.3) helper wrappers mapping to jmapcore primitives.
// @spec RFC8620#5.3-p1-MUST

type CreationRefs = jmapcore.CreationRefs

func isIdProperty(k string) bool {
	return jmapcore.IsIdProperty(k)
}

func nilIfEmpty(v any) any {
	return jmapcore.NilIfEmpty(v)
}

func resolveCreationRef(v any, resolved map[string]Id, pending map[string]struct{}) (out any, deferred bool) {
	cResolved := make(map[string]jmapcore.Id, len(resolved))
	for k, val := range resolved {
		cResolved[k] = jmapcore.Id(val)
	}
	return jmapcore.ResolveCreationRef(v, cResolved, pending)
}

func resolveNodeCreationRefs(nodeMap map[string]any, resolved map[string]Id, pending map[string]struct{}) (map[string]any, bool) {
	cResolved := make(map[string]jmapcore.Id, len(resolved))
	for k, val := range resolved {
		cResolved[k] = jmapcore.Id(val)
	}
	return jmapcore.ResolveNodeCreationRefs(nodeMap, cResolved, pending)
}

func resolveIdBooleanMapRefs(m map[string]any, resolved map[string]Id, pending map[string]struct{}) (map[string]any, bool) {
	cResolved := make(map[string]jmapcore.Id, len(resolved))
	for k, val := range resolved {
		cResolved[k] = jmapcore.Id(val)
	}
	return jmapcore.ResolveIdBooleanMapRefs(m, cResolved, pending)
}

func resolveCreationID(id string, resolved map[string]Id) string {
	cResolved := make(map[string]jmapcore.Id, len(resolved))
	for k, val := range resolved {
		cResolved[k] = jmapcore.Id(val)
	}
	return jmapcore.ResolveCreationID(id, cResolved)
}

func runCreateLoop(createRaw map[string]any, refs map[string]Id, do func(creationID string, resolved map[string]any) (string, error)) map[string]any {
	cRefs := make(map[string]jmapcore.Id, len(refs))
	for k, val := range refs {
		cRefs[k] = jmapcore.Id(val)
	}
	res := jmapcore.RunCreateLoop(createRaw, cRefs, do)
	for k, val := range cRefs {
		refs[k] = Id(val)
	}
	return res
}

func resolvePatchCreationRefs(patch map[string]any, refs map[string]Id) map[string]any {
	cRefs := make(map[string]jmapcore.Id, len(refs))
	for k, val := range refs {
		cRefs[k] = jmapcore.Id(val)
	}
	return jmapcore.ResolvePatchCreationRefs(patch, cRefs)
}

func NewCreationRefs(initial map[string]string) *CreationRefs {
	return jmapcore.NewCreationRefs(initial)
}

func WithCreationRefs(ctx context.Context, refs *CreationRefs) context.Context {
	return jmapcore.WithCreationRefs(ctx, refs)
}

func CreationRefsFrom(ctx context.Context) *CreationRefs {
	return jmapcore.CreationRefsFrom(ctx)
}

func newSetCreationRefs(ctx context.Context) map[string]Id {
	cRefs := jmapcore.NewSetCreationRefs(ctx)
	out := make(map[string]Id, len(cRefs))
	for k, val := range cRefs {
		out[k] = Id(val)
	}
	return out
}

func recordCreationRefs(ctx context.Context, refs map[string]Id, cid string, realID Id) {
	cRefs := make(map[string]jmapcore.Id, len(refs))
	for k, val := range refs {
		cRefs[k] = jmapcore.Id(val)
	}
	jmapcore.RecordCreationRefs(ctx, cRefs, cid, jmapcore.Id(realID))
	if refs != nil {
		refs[cid] = realID
	}
}
