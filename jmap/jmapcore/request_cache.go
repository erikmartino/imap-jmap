package jmapcore

import (
	"context"
	"sync"
)

// RequestCache provides thread-safe in-memory caching scoped to a single JMAP request lifecycle.
type RequestCache struct {
	dummy bool
	data  sync.Map
}

// NewRequestCache creates a new functional RequestCache.
func NewRequestCache() *RequestCache {
	return &RequestCache{}
}

// NewDummyRequestCache creates a dummy RequestCache that always returns empty and drops writes.
func NewDummyRequestCache() *RequestCache {
	return &RequestCache{dummy: true}
}

// Load retrieves an entry from the request cache. If dummy or nil, it always returns (nil, false).
func (c *RequestCache) Load(key any) (any, bool) {
	if c == nil || c.dummy {
		return nil, false
	}
	return c.data.Load(key)
}

// Store places an entry into the request cache. If dummy or nil, it is a no-op.
func (c *RequestCache) Store(key, val any) {
	if c == nil || c.dummy {
		return
	}
	c.data.Store(key, val)
}

// Delete removes an entry from the request cache. If dummy or nil, it is a no-op.
func (c *RequestCache) Delete(key any) {
	if c == nil || c.dummy {
		return
	}
	c.data.Delete(key)
}

type requestCacheKey struct{}

// WithRequestCache attaches a RequestCache to the context.
func WithRequestCache(ctx context.Context, cache *RequestCache) context.Context {
	return context.WithValue(ctx, requestCacheKey{}, cache)
}

// RequestCacheFrom retrieves the RequestCache from context, or nil if not present.
func RequestCacheFrom(ctx context.Context) *RequestCache {
	c, _ := ctx.Value(requestCacheKey{}).(*RequestCache)
	return c
}
