package jmaphandler

import "context"

// MethodHandler defines the function signature for a JMAP method handler per RFC 8620 Section 3.4.
type MethodHandler func(ctx context.Context, args map[string]any, clientCallID string) (responseName string, responseArgs map[string]any)

// MethodRegistry manages registered JMAP methods per RFC 8620 Section 3.4.
type MethodRegistry struct {
	handlers map[string]MethodHandler
}

// NewMethodRegistry creates a new MethodRegistry with the standard Core/echo method registered.
func NewMethodRegistry() *MethodRegistry {
	r := &MethodRegistry{
		handlers: make(map[string]MethodHandler),
	}
	r.Register("Core/echo", handleCoreEcho)
	return r
}

// Register adds a new method handler for the given method name.
func (r *MethodRegistry) Register(name string, handler MethodHandler) {
	r.handlers[name] = handler
}

// Get returns the handler for the given method name if registered.
func (r *MethodRegistry) Get(name string) (MethodHandler, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

// AliasMethod wraps a handler so it can be registered under an additional method name,
// overriding the response name to match the alias. Used to expose a method under both an
// RFC-canonical name and a legacy name (e.g. ContactCard/* and Card/*) from one implementation.
func AliasMethod(name string, h MethodHandler) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		_, respArgs := h(ctx, args, clientCallID)
		return name, respArgs
	}
}

// handleCoreEcho implements RFC 8620 Section 3.8.1 Core/echo method.
func handleCoreEcho(_ context.Context, args map[string]any, _ string) (string, map[string]any) {
	if args == nil {
		args = make(map[string]any)
	}
	return "Core/echo", args
}
