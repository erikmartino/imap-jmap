package jmaphandler

import (
	"context"
	"fmt"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapsession"
)

// MethodErrorArgs returns an argument map for a standard JMAP method error per RFC 8620 Section 3.6.2.
var MethodErrorArgs = jmapcore.MethodErrorArgs

// CoreCapabilityKey is the context key for CoreCapability limits, for use by handlers that
// need to enforce maxObjectsInGet / maxObjectsInSet without importing the top-level jmap package.
type CoreCapabilityKey struct{}

// WithCoreLimits returns a new context carrying the given CoreCapability limits.
func WithCoreLimits(ctx context.Context, limits jmapsession.CoreCapability) context.Context {
	return context.WithValue(ctx, CoreCapabilityKey{}, limits)
}

// CoreLimitsFromContext extracts CoreCapability limits from the context, if set.
func CoreLimitsFromContext(ctx context.Context) (jmapsession.CoreCapability, bool) {
	if ctx == nil {
		return jmapsession.CoreCapability{}, false
	}
	limits, ok := ctx.Value(CoreCapabilityKey{}).(jmapsession.CoreCapability)
	return limits, ok
}

// ValidateGetLimits checks whether the requested ID count exceeds MaxObjectsInGet per RFC 8620.
// Returns an error response name and args if exceeded, otherwise ("", nil, true).
func ValidateGetLimits(ctx context.Context, count int) (string, map[string]any, bool) {
	limits, ok := CoreLimitsFromContext(ctx)
	if !ok || limits.MaxObjectsInGet == 0 {
		return "", nil, true
	}
	if uint64(count) > limits.MaxObjectsInGet {
		return "error", MethodErrorArgs("requestTooLarge", fmt.Sprintf(
			"Number of objects (%d) exceeds maxObjectsInGet (%d)", count, limits.MaxObjectsInGet)), false
	}
	return "", nil, true
}

// ValidateSetLimits checks whether create+update+destroy total exceeds MaxObjectsInSet per RFC 8620.
// Returns an error response name and args if exceeded, otherwise ("", nil, true).
func ValidateSetLimits(ctx context.Context, args map[string]any) (string, map[string]any, bool) {
	limits, ok := CoreLimitsFromContext(ctx)
	if !ok || limits.MaxObjectsInSet == 0 {
		return "", nil, true
	}
	var count uint64
	if c, ok := args["create"].(map[string]any); ok {
		count += uint64(len(c))
	}
	if u, ok := args["update"].(map[string]any); ok {
		count += uint64(len(u))
	}
	if d, ok := args["destroy"].([]any); ok {
		count += uint64(len(d))
	}
	if count > limits.MaxObjectsInSet {
		return "error", MethodErrorArgs("requestTooLarge", fmt.Sprintf(
			"Total objects in set (%d) exceeds maxObjectsInSet (%d)", count, limits.MaxObjectsInSet)), false
	}
	return "", nil, true
}
