package jmap

import (
	"context"

	"imap-jmap/jmap/jmapcopy"
)

// SourceAccountContext returns a context scoped to the copy's fromAccountId so source objects
// are read from the correct account. An empty or "primary" fromAccountId means the caller's own
// account, i.e. the context is left unchanged.
func SourceAccountContext(ctx context.Context, args map[string]any) context.Context {
	return jmapcopy.SourceAccountContext(ctx, args)
}

func sourceAccountContext(ctx context.Context, args map[string]any) context.Context {
	return jmapcopy.SourceAccountContext(ctx, args)
}

// ResolveCopyAccountIDs extracts the target accountId and source fromAccountId per RFC 8620 Section 5.4.
func ResolveCopyAccountIDs(args map[string]any) (accountID, fromAccountID string) {
	return jmapcopy.ResolveCopyAccountIDs(args)
}

// ValidateCopyStates validates ifInState and destroyFromIfInState per RFC 8620 Section 5.4.
func ValidateCopyStates(ctx, srcCtx context.Context, args map[string]any, getDstState, getSrcState func(context.Context) string) (oldState string, errInv *Invocation) {
	oldState, cErrInv := jmapcopy.ValidateCopyStates(ctx, srcCtx, args, getDstState, getSrcState)
	if cErrInv != nil {
		return "", &Invocation{
			Name:         cErrInv.Name,
			Args:         cErrInv.Args,
			ClientCallID: cErrInv.ClientCallID,
		}
	}
	return oldState, nil
}

// mergeCopyOverrides builds the property map for a Foo/copy creation (RFC 8620 Section 5.4).
func mergeCopyOverrides(source any, overrides map[string]any) map[string]any {
	return jmapcopy.MergeCopyOverrides(source, overrides)
}

