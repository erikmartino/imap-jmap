package jmapmail

import (
	"context"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// RegisterThreadHandlers registers RFC 8621 Section 3 Thread method handlers into MethodRegistry.
func RegisterThreadHandlers(r *jmaphandler.MethodRegistry, backend MailBackend) {
	r.Register("Thread/get", HandleThreadGet(backend))
	r.Register("Thread/changes", HandleThreadChanges(backend))
}

// HandleThreadGet implements Thread/get per RFC 8621 Section 3.1.
func HandleThreadGet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := jmaphandler.ParseProperties(args)

		var list []*Thread
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			ids := make([]jmapcore.Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, jmapcore.Id(idStr))
				}
			}
			list, notFound, err = backend.GetThreads(ctx, ids)
		} else {
			list, err = backend.GetAllThreads(ctx)
		}

		if err != nil || list == nil {
			list = []*Thread{}
		}
		if notFound == nil {
			notFound = []jmapcore.Id{}
		}

		return "Thread/get", map[string]any{
			"accountId": accountID,
			"state":     backend.ThreadState(ctx),
			"list":      jmaphandler.FilterList(list, props),
			"notFound":  notFound,
		}
	}
}

// HandleThreadChanges implements Thread/changes per RFC 8621 Section 3.2.
func HandleThreadChanges(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		var maxChanges *uint64
		if mc, ok := args["maxChanges"].(float64); ok {
			if mc < 0 {
				return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, "maxChanges must be non-negative")
			}
			m := uint64(mc)
			maxChanges = &m
		}

		created, updated, destroyed, newState, hasMore := backend.ThreadChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
		}

		return "Thread/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       sinceState,
			"newState":       newState,
			"hasMoreChanges": hasMore,
			"created":        created,
			"updated":        updated,
			"destroyed":      destroyed,
		}
	}
}
