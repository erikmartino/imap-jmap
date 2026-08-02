package jmap

import (
	"context"
)

// Mailbox Handlers (RFC 8621 Section 2)

func handleMailboxGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)

		var list []*Mailbox
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetMailboxes(ctx, ids)
		} else {
			list, err = backend.GetAllMailboxes(ctx)
		}

		if err != nil || list == nil {
			list = []*Mailbox{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Mailbox/get", map[string]any{
			"accountId": accountID,
			"state":     backend.State(ctx),
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleMailboxChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Mailbox/changes", map[string]any{
			"accountId":         accountID,
			"oldState":          args["sinceState"],
			"newState":          backend.State(ctx),
			"hasMoreChanges":    false,
			"created":           []Id{},
			"updated":           []Id{},
			"destroyed":         []Id{},
			"updatedProperties": []string{},
		}
	}
}

func handleMailboxSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		created := make(map[string]*Mailbox)
		destroyed := []Id{}

		if createMap, ok := args["create"].(map[string]any); ok {
			for clientKey, raw := range createMap {
				if mbData, ok := raw.(map[string]any); ok {
					name, _ := mbData["name"].(string)
					mb, err := backend.CreateMailbox(ctx, &Mailbox{Name: name})
					if err == nil {
						created[clientKey] = mb
					}
				}
			}
		}

		if destroyList, ok := args["destroy"].([]any); ok {
			for _, rawID := range destroyList {
				if idStr, ok := rawID.(string); ok {
					if ok, _ := backend.DeleteMailbox(ctx, Id(idStr)); ok {
						destroyed = append(destroyed, Id(idStr))
					}
				}
			}
		}

		return "Mailbox/set", map[string]any{
			"accountId": accountID,
			"oldState":  backend.State(ctx),
			"newState":  backend.State(ctx),
			"created":   created,
			"destroyed": destroyed,
		}
	}
}

func handleMailboxQuery(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		all, _ := backend.GetAllMailboxes(ctx)
		ids := make([]Id, len(all))
		for i, mb := range all {
			ids[i] = mb.ID
		}
		return "Mailbox/query", map[string]any{
			"accountId":           accountID,
			"queryState":          backend.State(ctx),
			"canCalculateChanges": true,
			"position":            0,
			"ids":                 ids,
			"total":               len(ids),
		}
	}
}

func handleMailboxQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		return "Mailbox/queryChanges", map[string]any{
			"accountId":     accountID,
			"oldQueryState": args["sinceQueryState"],
			"newQueryState": backend.State(ctx),
			"added":         []any{},
			"removed":       []Id{},
		}
	}
}
