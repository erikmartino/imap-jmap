package jmaphandler

import (
	"context"

	"imap-jmap/jmap/jmapcore"
)

// NilIfEmpty returns nil when v is an empty map or slice, otherwise v.
// Used to omit empty notCreated/notUpdated/notDestroyed maps from set responses per RFC 8620 Section 5.3.
func NilIfEmpty(v any) any {
	return jmapcore.NilIfEmpty(v)
}

// NewSetCreationRefs extracts the current creation-ref map from context (populated by the
// batch executor for cross-method references) and returns a mutable copy for intra-method use.
func NewSetCreationRefs(ctx context.Context) map[string]jmapcore.Id {
	return jmapcore.NewSetCreationRefs(ctx)
}

// RecordCreationRefs writes a resolved creation-id → real-id mapping into both the local
// per-method refs map and the per-request CreationRefs carried in context.
func RecordCreationRefs(ctx context.Context, refs map[string]jmapcore.Id, cid string, realID jmapcore.Id) {
	jmapcore.RecordCreationRefs(ctx, refs, cid, realID)
}

// ResolvePatchCreationRefs replaces any #creationId placeholders within a patch map per
// RFC 8620 Section 5.3.
func ResolvePatchCreationRefs(patch map[string]any, refs map[string]jmapcore.Id) map[string]any {
	return jmapcore.ResolvePatchCreationRefs(patch, refs)
}

// ResolveCreationID resolves a single id string that may be a #creationId placeholder per
// RFC 8620 Section 5.3.
func ResolveCreationID(id string, resolved map[string]jmapcore.Id) string {
	return jmapcore.ResolveCreationID(id, resolved)
}

// ValidatePatch validates a PatchObject against target and serverSetProperties per RFC 8620 Section 5.3.
func ValidatePatch(target map[string]any, patch map[string]any, serverSetProperties []string) *jmapcore.SetError {
	return jmapcore.ValidatePatch(target, patch, serverSetProperties)
}

// RunCreateLoop processes a create map, resolving creation references and calling do for each
// item in dependency order per RFC 8620 Section 5.3. Items whose dependencies cannot yet be
// resolved are deferred until they can be, or reported as notCreated on a cycle.
func RunCreateLoop(createRaw map[string]any, refs map[string]jmapcore.Id, do func(creationID string, resolved map[string]any) (string, error)) map[string]any {
	return jmapcore.RunCreateLoop(createRaw, refs, do)
}

// NewCreationRefs creates a *CreationRefs from an initial string map.
func NewCreationRefs(initial map[string]string) *jmapcore.CreationRefs {
	return jmapcore.NewCreationRefs(initial)
}

// WithCreationRefs stores refs in ctx for cross-method resolution.
func WithCreationRefs(ctx context.Context, refs *jmapcore.CreationRefs) context.Context {
	return jmapcore.WithCreationRefs(ctx, refs)
}

// CreationRefsFrom retrieves *CreationRefs from ctx.
func CreationRefsFrom(ctx context.Context) *jmapcore.CreationRefs {
	return jmapcore.CreationRefsFrom(ctx)
}
