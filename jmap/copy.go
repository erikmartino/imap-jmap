package jmap

import (
	"context"
	"encoding/json"
)

// SourceAccountContext returns a context scoped to the copy's fromAccountId so source objects
// are read from the correct account. An empty or "primary" fromAccountId means the caller's own
// account, i.e. the context is left unchanged.
func SourceAccountContext(ctx context.Context, args map[string]any) context.Context {
	if raw, _ := args["fromAccountId"].(string); raw != "" && raw != "primary" {
		return ContextWithAccountID(ctx, raw)
	}
	return ctx
}

func sourceAccountContext(ctx context.Context, args map[string]any) context.Context {
	return SourceAccountContext(ctx, args)
}

// ResolveCopyAccountIDs extracts the target accountId and source fromAccountId per RFC 8620
// Section 5.4. If fromAccountId is omitted, it defaults to accountId.
func ResolveCopyAccountIDs(args map[string]any) (accountID, fromAccountID string) {
	accountID, _ = args["accountId"].(string)
	fromAccountID, _ = args["fromAccountId"].(string)
	if fromAccountID == "" {
		fromAccountID = accountID
	}
	return accountID, fromAccountID
}

// ValidateCopyStates validates ifInState (against target account state) and
// destroyFromIfInState (against source account state) per RFC 8620 Section 5.4.
func ValidateCopyStates(ctx, srcCtx context.Context, args map[string]any, getDstState, getSrcState func(context.Context) string) (oldState string, errInv *Invocation) {
	oldState = getDstState(ctx)
	if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
		return "", &Invocation{
			Name: "error",
			Args: MethodErrorArgs("stateMismatch", "ifInState does not match target account state"),
		}
	}
	if dfis, ok := args["destroyFromIfInState"].(string); ok && dfis != "" && dfis != getSrcState(srcCtx) {
		return "", &Invocation{
			Name: "error",
			Args: MethodErrorArgs("stateMismatch", "destroyFromIfInState does not match source account state"),
		}
	}
	return oldState, nil
}

// mergeCopyOverrides builds the property map for a Foo/copy creation (RFC 8620 Section 5.4).
// It starts from the source object's properties and applies the client-supplied overrides,
// dropping the "id" so the target account assigns a fresh one. Server-set fields the backend
// re-derives (timestamps, ids) are left for the backend to populate on create.
func mergeCopyOverrides(source any, overrides map[string]any) map[string]any {
	merged := make(map[string]any)
	if b, err := json.Marshal(source); err == nil {
		_ = json.Unmarshal(b, &merged)
	}
	for k, v := range overrides {
		if k == "id" {
			continue
		}
		merged[k] = v
	}
	delete(merged, "id")
	return merged
}

