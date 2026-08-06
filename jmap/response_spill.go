package jmap

import (
	"context"
)

// responseSpillKey carries a pointer to a slice of additional method responses
// that a handler may append to (RFC 8621 Section 7.5: EmailSubmission/set
// triggers an implicit Email/set whose response must follow its own).
type responseSpillKey struct{}

type responseSpill struct {
	list []Invocation
}

// withResponseSpill attaches the spill collector to the request context.
func withResponseSpill(ctx context.Context) context.Context {
	return context.WithValue(ctx, responseSpillKey{}, &responseSpill{})
}

// appendSpillResponse records an additional method response emitted by a handler.
func appendSpillResponse(ctx context.Context, inv Invocation) {
	if sp, ok := ctx.Value(responseSpillKey{}).(*responseSpill); ok {
		sp.list = append(sp.list, inv)
	}
}

// drainResponseSpill returns any extra responses recorded by the handler and
// clears the spill so later method calls in the same request start empty.
func drainResponseSpill(ctx context.Context) []Invocation {
	sp, ok := ctx.Value(responseSpillKey{}).(*responseSpill)
	if !ok {
		return nil
	}
	out := sp.list
	sp.list = nil
	return out
}
