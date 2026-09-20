package jmapfilenode

import (
	"context"
	"encoding/json"
	"errors"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// RegisterFileNodeHandlers registers FileNode/* method handlers into MethodRegistry.
func RegisterFileNodeHandlers(r *jmaphandler.MethodRegistry, backend FileNodeBackend) {
	r.Register("FileNode/get", handleFileNodeGet(backend))
	r.Register("FileNode/query", handleFileNodeQuery(backend))
	r.Register("FileNode/set", handleFileNodeSet(backend))
	r.Register("FileNode/changes", handleFileNodeChanges(backend))
	r.Register("FileNode/queryChanges", handleFileNodeQueryChanges(backend))
}

func handleFileNodeGet(backend FileNodeBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*FileNode
		var notFound []jmapcore.Id
		var err error
		state := "0"

		if backend != nil {
			state = backend.FileNodeState(ctx)
			if hasIDs {
				ids := make([]jmapcore.Id, 0, len(idsRaw))
				for _, item := range idsRaw {
					if idStr, ok := item.(string); ok {
						ids = append(ids, jmapcore.Id(idStr))
					}
				}
				list, notFound, err = backend.GetFileNodes(ctx, ids)
			} else {
				list, err = backend.GetAllFileNodes(ctx)
			}
		}

		if err != nil || list == nil {
			list = []*FileNode{}
		}
		if notFound == nil {
			notFound = []jmapcore.Id{}
		}

		return "FileNode/get", map[string]any{
			"accountId": accountID,
			"state":     state,
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleFileNodeQuery(backend FileNodeBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		filter, _ := args["filter"].(map[string]any)
		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := parseQueryAnchor(args)
		if anchorErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, anchorErr)
		}

		var limit *uint64
		if limitFloat, ok := args["limit"].(float64); ok {
			l := uint64(limitFloat)
			limit = &l
		}

		var ids []jmapcore.Id
		var total int
		var err error
		queryState := "0"

		if backend != nil {
			if anchor != "" {
				var allIDs []jmapcore.Id
				allIDs, total, err = backend.QueryFileNodes(ctx, filter, 0, nil)
				if err == nil {
					var found bool
					position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
					if !found {
						return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
					}
				}
			} else {
				ids, total, err = backend.QueryFileNodes(ctx, filter, position, limit)
			}
			queryState = backend.FileNodeState(ctx)
		}
		if err != nil || ids == nil {
			ids = []jmapcore.Id{}
			total = 0
		}
		position = NormalizePosition(position, total)

		return "FileNode/query", map[string]any{
			"accountId":           accountID,
			"queryState":          queryState,
			"canCalculateChanges": true,
			"position":            position,
			"ids":                 ids,
			"total":               total,
		}
	}
}

func handleFileNodeSet(backend FileNodeBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := "0"
		newState := "0"
		if backend != nil {
			oldState = backend.FileNodeState(ctx)
			newState = oldState
		}

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}

		created := make(map[string]*FileNode)
		updated := make(map[string]any)
		destroyed := make([]jmapcore.Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		creationRefs := newSetCreationRefs(ctx)

		if backend != nil {
			if createRaw, ok := args["create"].(map[string]any); ok {
				notCreated = runCreateLoop(createRaw, creationRefs, func(creationID string, resolvedMap map[string]any) (string, error) {
					nodeBytes, _ := json.Marshal(resolvedMap)
					var node FileNode
					_ = json.Unmarshal(nodeBytes, &node)

					createdNode, err := backend.CreateFileNode(ctx, &node)
					if err != nil {
						return "", err
					}
					created[creationID] = createdNode
					recordCreationRefs(ctx, creationRefs, creationID, createdNode.ID)
					return string(createdNode.ID), nil
				})
			}

			if updateRaw, ok := args["update"].(map[string]any); ok {
				for idStr, patchRaw := range updateRaw {
					resolvedID := resolveCreationID(idStr, creationRefs)
					patch, _ := patchRaw.(map[string]any)
					_, err := backend.UpdateFileNode(ctx, jmapcore.Id(resolvedID), patch)
					if err != nil {
						if errors.Is(err, ErrNotFound) {
							notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: err.Error()}
						} else {
							notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
						}
					} else {
						updated[string(resolvedID)] = nil
					}
				}
			}

			if destroyRaw, ok := args["destroy"].([]any); ok {
				for _, item := range destroyRaw {
					if idStr, ok := item.(string); ok {
						resolvedID := resolveCreationID(idStr, creationRefs)
						okDel, err := backend.DeleteFileNode(ctx, jmapcore.Id(resolvedID))
						if err != nil {
							notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "serverFail", Description: err.Error()}
						} else if !okDel {
							notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: "filenode not found"}
						} else {
							destroyed = append(destroyed, jmapcore.Id(resolvedID))
						}
					}
				}
			}
			newState = backend.FileNodeState(ctx)
		}

		return "FileNode/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     newState,
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

func handleFileNodeChanges(backend FileNodeBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		var created, updated, destroyed []jmapcore.Id
		newState := "0"
		hasMore := false

		if backend != nil {
			created, updated, destroyed, newState, hasMore = backend.FileNodeChanges(ctx, sinceState)
		}

		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
		}

		return "FileNode/changes", map[string]any{
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

func handleFileNodeQueryChanges(backend FileNodeBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceQueryState, _ := args["sinceQueryState"].(string)
		upToID, _ := args["upToId"].(string)
		filter, _ := args["filter"].(map[string]any)

		added := make([]map[string]any, 0)
		removed := make([]jmapcore.Id, 0)
		newQueryState := "0"

		if backend != nil {
			newQueryState = backend.FileNodeState(ctx)

			created, updated, destroyed, _, hasMore := backend.FileNodeChanges(ctx, sinceQueryState)
			if hasMore {
				return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
			}

			currentIDs, _, _ := backend.QueryFileNodes(ctx, filter, 0, nil)
			added, removed = computeQueryChanges(created, updated, destroyed, currentIDs, upToID)
		}

		res := map[string]any{
			"accountId":     accountID,
			"oldQueryState": sinceQueryState,
			"newQueryState": newQueryState,
			"added":         added,
			"removed":       removed,
		}
		if upToID != "" {
			res["upToId"] = upToID
		}
		return "FileNode/queryChanges", res
	}
}
