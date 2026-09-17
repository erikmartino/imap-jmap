package jmapcopy

import (
	"context"
	"encoding/json"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
)

// SourceAccountContext returns a context scoped to the copy's fromAccountId so source objects
// are read from the correct account. An empty or "primary" fromAccountId means the caller's own
// account, i.e. the context is left unchanged.
func SourceAccountContext(ctx context.Context, args map[string]any) context.Context {
	if raw, _ := args["fromAccountId"].(string); raw != "" && raw != "primary" {
		return jmapauth.ContextWithAccountID(ctx, raw)
	}
	return ctx
}

// ResolveCopyAccountIDs extracts the target accountId and source fromAccountId per RFC 8620 Section 5.4.
// @spec RFC8620#5.4-p1-MUST
func ResolveCopyAccountIDs(args map[string]any) (accountID, fromAccountID string) {
	accountID, _ = args["accountId"].(string)
	fromAccountID, _ = args["fromAccountId"].(string)
	if fromAccountID == "" {
		fromAccountID = accountID
	}
	return accountID, fromAccountID
}

// ValidateCopyStates validates ifInState and destroyFromIfInState per RFC 8620 Section 5.4.
// @spec RFC8620#5.4-p2-MUST
func ValidateCopyStates(ctx, srcCtx context.Context, args map[string]any, getDstState, getSrcState func(context.Context) string) (oldState string, errInv *jmapcore.Invocation) {
	oldState = getDstState(ctx)
	if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
		return "", &jmapcore.Invocation{
			Name: "error",
			Args: jmapcore.MethodErrorArgs("stateMismatch", "ifInState does not match target account state"),
		}
	}
	if dfis, ok := args["destroyFromIfInState"].(string); ok && dfis != "" && dfis != getSrcState(srcCtx) {
		return "", &jmapcore.Invocation{
			Name: "error",
			Args: jmapcore.MethodErrorArgs("stateMismatch", "destroyFromIfInState does not match source account state"),
		}
	}
	return oldState, nil
}

// MergeCopyOverrides builds the property map for a Foo/copy creation per RFC 8620 Section 5.4.
// @spec RFC8620#5.4-p3-MUST
func MergeCopyOverrides(source any, overrides map[string]any) map[string]any {
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
