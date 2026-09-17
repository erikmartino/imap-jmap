package jmap

import (
	"context"

	"imap-jmap/jmap/jmaphandler"
)

// withResponseSpill attaches the spill collector to the request context.
func withResponseSpill(ctx context.Context) context.Context {
	return jmaphandler.WithResponseSpill(ctx)
}

// appendSpillResponse records an additional method response emitted by a handler.
func appendSpillResponse(ctx context.Context, inv Invocation) {
	jmaphandler.AppendSpillResponse(ctx, inv)
}

// drainResponseSpill returns any extra responses recorded by the handler and
// clears the spill so later method calls in the same request start empty.
func drainResponseSpill(ctx context.Context) []Invocation {
	return jmaphandler.DrainResponseSpill(ctx)
}
