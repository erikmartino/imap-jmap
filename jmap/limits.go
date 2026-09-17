package jmap

import (
	"context"

	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmaphandler"
)

// WithCoreLimits returns a new context carrying the given CoreCapability limits.
func WithCoreLimits(ctx context.Context, limits CoreCapability) context.Context {
	return jmaphandler.WithCoreLimits(ctx, limits)
}

// CoreLimitsFromContext extracts CoreCapability limits from the context, if set.
func CoreLimitsFromContext(ctx context.Context) (CoreCapability, bool) {
	return jmaphandler.CoreLimitsFromContext(ctx)
}

// ValidateGetLimits checks whether the requested IDs or returned objects exceed MaxObjectsInGet.
func ValidateGetLimits(ctx context.Context, count int) (string, map[string]any, bool) {
	return jmaphandler.ValidateGetLimits(ctx, count)
}

// ValidateSetLimits checks whether create+update+destroy count exceeds MaxObjectsInSet.
func ValidateSetLimits(ctx context.Context, args map[string]any) (string, map[string]any, bool) {
	return jmaphandler.ValidateSetLimits(ctx, args)
}

const (
	// Default calendar capability limits — canonical definitions live in jmapcalendar.
	DefaultMinDateTime              = jmapcalendar.DefaultMinDateTime
	DefaultMaxDateTime              = jmapcalendar.DefaultMaxDateTime
	DefaultMaxExpandedQueryDuration = jmapcalendar.DefaultMaxExpandedQueryDuration
)

// WithCalendarsCapability returns a new context carrying the given CalendarsCapability limits.
// Forwarded to jmapcalendar for backward compatibility.
func WithCalendarsCapability(ctx context.Context, cap CalendarsCapability) context.Context {
	return jmapcalendar.WithCalendarsCapability(ctx, cap)
}

// CalendarsCapabilityFromContext extracts CalendarsCapability limits from context, or returns default limits.
// Forwarded to jmapcalendar for backward compatibility.
func CalendarsCapabilityFromContext(ctx context.Context) CalendarsCapability {
	return jmapcalendar.CalendarsCapabilityFromContext(ctx)
}
