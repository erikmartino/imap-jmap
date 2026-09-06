package jmap

import (
	"context"
	"fmt"
)

type coreLimitsKey struct{}

// WithCoreLimits returns a new context carrying the given CoreCapability limits.
func WithCoreLimits(ctx context.Context, limits CoreCapability) context.Context {
	return context.WithValue(ctx, coreLimitsKey{}, limits)
}

// CoreLimitsFromContext extracts CoreCapability limits from the context, if set.
func CoreLimitsFromContext(ctx context.Context) (CoreCapability, bool) {
	if ctx == nil {
		return CoreCapability{}, false
	}
	limits, ok := ctx.Value(coreLimitsKey{}).(CoreCapability)
	return limits, ok
}

// ValidateGetLimits checks whether the requested IDs or returned objects exceed MaxObjectsInGet.
func ValidateGetLimits(ctx context.Context, count int) (string, map[string]any, bool) {
	limits, ok := CoreLimitsFromContext(ctx)
	if !ok || limits.MaxObjectsInGet == 0 {
		return "", nil, true
	}
	if uint64(count) > limits.MaxObjectsInGet {
		return "error", MethodErrorArgs(MethodErrorRequestTooLarge, fmt.Sprintf("Number of objects (%d) exceeds maxObjectsInGet (%d)", count, limits.MaxObjectsInGet)), false
	}
	return "", nil, true
}

// ValidateSetLimits checks whether create+update+destroy count exceeds MaxObjectsInSet.
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
		return "error", MethodErrorArgs(MethodErrorRequestTooLarge, fmt.Sprintf("Total objects in set (%d) exceeds maxObjectsInSet (%d)", count, limits.MaxObjectsInSet)), false
	}
	return "", nil, true
}

const (
	// Default calendar capability limits per draft-ietf-jmap-calendars-27 Section 1.5.1 and 5.11.
	DefaultMinDateTime              = "1900-01-01T00:00:00"
	DefaultMaxDateTime              = "9999-12-31T23:59:59"
	DefaultMaxExpandedQueryDuration = "P730D"
)

type calendarsLimitsKey struct{}

// WithCalendarsCapability returns a new context carrying the given CalendarsCapability limits.
func WithCalendarsCapability(ctx context.Context, cap CalendarsCapability) context.Context {
	return context.WithValue(ctx, calendarsLimitsKey{}, cap)
}

// CalendarsCapabilityFromContext extracts CalendarsCapability limits from context, or returns default limits.
func CalendarsCapabilityFromContext(ctx context.Context) CalendarsCapability {
	if ctx != nil {
		if cap, ok := ctx.Value(calendarsLimitsKey{}).(CalendarsCapability); ok {
			if cap.MinDateTime == "" {
				cap.MinDateTime = DefaultMinDateTime
			}
			if cap.MaxDateTime == "" {
				cap.MaxDateTime = DefaultMaxDateTime
			}
			if cap.MaxExpandedQueryDuration == "" {
				cap.MaxExpandedQueryDuration = DefaultMaxExpandedQueryDuration
			}
			return cap
		}
	}
	return CalendarsCapability{
		MinDateTime:              DefaultMinDateTime,
		MaxDateTime:              DefaultMaxDateTime,
		MaxExpandedQueryDuration: DefaultMaxExpandedQueryDuration,
	}
}

