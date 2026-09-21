package jmapcore

import (
	"context"
	"sync"
)

// RequestScope is an always-on key/value store scoped to a single JMAP request. It is
// intentionally independent of the global cache flag: it only deduplicates work within
// one request and is discarded when the request ends, so it holds no state across
// requests and does not make the service stateful.
type RequestScope struct {
	m sync.Map
}

// NewRequestScope creates an empty request scope.
func NewRequestScope() *RequestScope {
	return &RequestScope{}
}

// Load retrieves an entry from the scope. A nil scope always reports not found.
func (s *RequestScope) Load(key any) (any, bool) {
	if s == nil {
		return nil, false
	}
	return s.m.Load(key)
}

// Store places an entry into the scope. A nil scope is a no-op.
func (s *RequestScope) Store(key, val any) {
	if s == nil {
		return
	}
	s.m.Store(key, val)
}

// LoadOrStore returns the existing value for the key if present, otherwise stores and
// returns the given value.
func (s *RequestScope) LoadOrStore(key, val any) (any, bool) {
	if s == nil {
		return val, false
	}
	return s.m.LoadOrStore(key, val)
}

// Delete removes an entry from the scope. A nil scope is a no-op.
func (s *RequestScope) Delete(key any) {
	if s == nil {
		return
	}
	s.m.Delete(key)
}

type requestScopeKey struct{}

// WithRequestScope attaches a fresh RequestScope to the context.
func WithRequestScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestScopeKey{}, NewRequestScope())
}

// RequestScopeFrom retrieves the RequestScope from the context, or nil if absent.
func RequestScopeFrom(ctx context.Context) *RequestScope {
	s, _ := ctx.Value(requestScopeKey{}).(*RequestScope)
	return s
}
