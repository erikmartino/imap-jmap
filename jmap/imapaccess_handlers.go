package jmap

import (
	"context"
)

// RegisterIMAPAccessHandlers registers all RFC 9698 JMAPACCESS extension methods into MethodRegistry.
func RegisterIMAPAccessHandlers(r *MethodRegistry, backend IMAPAccessBackend) {
	if backend == nil {
		return
	}
	r.Register("IMAPAccount/get", handleIMAPAccountGet(backend))
	r.Register("IMAPAccount/changes", handleIMAPAccountChanges(backend))
	r.Register("IMAPAccount/set", handleIMAPAccountSet(backend))
}

// handleIMAPAccountGet handles IMAPAccount/get per RFC 9698 Section 3.1.
func handleIMAPAccountGet(backend IMAPAccessBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		var ids []Id
		if idsRaw, ok := args["ids"].([]any); ok && len(idsRaw) > 0 {
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
		}

		list, notFound, err := backend.GetIMAPAccounts(ctx, ids)
		if err != nil || list == nil {
			list = []*IMAPAccount{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "IMAPAccount/get", map[string]any{
			"accountId": accountID,
			"state":     backend.State(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

// handleIMAPAccountChanges handles IMAPAccount/changes per RFC 9698.
func handleIMAPAccountChanges(backend IMAPAccessBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)
		currentState := backend.State(ctx)

		hasMore := false
		var created, updated, destroyed []Id

		if sinceState != currentState {
			accounts, _ := backend.GetAllIMAPAccounts(ctx)
			for _, acc := range accounts {
				updated = append(updated, acc.ID)
			}
		}

		return "IMAPAccount/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       sinceState,
			"newState":       currentState,
			"hasMoreChanges": hasMore,
			"created":        created,
			"updated":        updated,
			"destroyed":      destroyed,
		}
	}
}

// handleIMAPAccountSet handles IMAPAccount/set per RFC 9698 Section 3.2.
func handleIMAPAccountSet(backend IMAPAccessBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.State(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		created := make(map[string]*IMAPAccount)
		notCreated := make(map[string]SetError)
		updated := make(map[string]any)
		notUpdated := make(map[string]SetError)
		var destroyed []Id
		notDestroyed := make(map[string]SetError)

		if createRaw, ok := args["create"].(map[string]any); ok {
			for key, val := range createRaw {
				if accData, ok := val.(map[string]any); ok {
					host, _ := accData["host"].(string)
					portVal, _ := accData["port"].(float64)
					tlsOpt, _ := accData["tls"].(string)
					user, _ := accData["username"].(string)

					acc := &IMAPAccount{
						Host:     host,
						Port:     uint32(portVal),
						TLS:      tlsOpt,
						Username: user,
						State:    "connected",
					}

					newAcc, err := backend.CreateIMAPAccount(ctx, acc)
					if err != nil {
						notCreated[key] = SetError{Type: "invalidProperties", Description: err.Error()}
					} else {
						created[key] = newAcc
					}
				}
			}
		}

		if updateRaw, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateRaw {
				if patch, ok := patchRaw.(map[string]any); ok {
					upAcc, err := backend.UpdateIMAPAccount(ctx, Id(idStr), patch)
					if err != nil {
						notUpdated[idStr] = SetError{Type: "notFound", Description: err.Error()}
					} else {
						updated[idStr] = upAcc
					}
				}
			}
		}

		if destroyRaw, ok := args["destroy"].([]any); ok {
			for _, item := range destroyRaw {
				if idStr, ok := item.(string); ok {
					okDel, err := backend.DeleteIMAPAccount(ctx, Id(idStr))
					if err != nil || !okDel {
						notDestroyed[idStr] = SetError{Type: "notFound", Description: "IMAPAccount not found"}
					} else {
						destroyed = append(destroyed, Id(idStr))
					}
				}
			}
		}

		return "IMAPAccount/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.State(ctx),
			"created":      created,
			"notCreated":   notCreated,
			"updated":      updated,
			"notUpdated":   notUpdated,
			"destroyed":    destroyed,
			"notDestroyed": notDestroyed,
		}
	}
}
