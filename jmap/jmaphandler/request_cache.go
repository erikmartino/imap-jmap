package jmaphandler

import (
	"context"

	"imap-jmap/jmap/jmapcore"
)

// RequestCache aliases jmapcore.RequestCache.
type RequestCache = jmapcore.RequestCache

// NewRequestCache creates a new RequestCache.
func NewRequestCache() *RequestCache {
	return jmapcore.NewRequestCache()
}

// NewDummyRequestCache creates a dummy RequestCache that always returns empty.
func NewDummyRequestCache() *RequestCache {
	return jmapcore.NewDummyRequestCache()
}

// WithRequestCache attaches a RequestCache to the context.
func WithRequestCache(ctx context.Context, cache *RequestCache) context.Context {
	return jmapcore.WithRequestCache(ctx, cache)
}

// RequestCacheFrom retrieves the RequestCache from context, or nil if not present.
func RequestCacheFrom(ctx context.Context) *RequestCache {
	return jmapcore.RequestCacheFrom(ctx)
}
