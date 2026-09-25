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

var allowedThreadProperties = map[string]bool{
	"id":       true,
	"emailIds": true,
}

// HandleThreadGet implements Thread/get per RFC 8621 Section 3.1.
func HandleThreadGet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		ids, hasIDs := jmaphandler.ParseIDs(args)
		props := jmaphandler.ParseProperties(args)

		if ok, bad := jmaphandler.ValidateProperties(props, allowedThreadProperties, nil); !ok {
			return "error", jmapcore.InvalidArgumentsErrorArgs([]string{"properties"}, "invalid property: "+bad)
		}

		var list []*Thread
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
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

		maxChanges, errArgs := jmaphandler.ParseMaxChanges(args)
		if errArgs != nil {
			return "error", errArgs
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
