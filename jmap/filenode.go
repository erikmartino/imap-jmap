package jmap

import (
	"context"
	"encoding/json"
	"errors"
)

// FileNode represents a FileNode object in the JMAP FileNode extension.
type FileNode struct {
	ID        Id     `json:"id"`
	Name      string `json:"name"`
	ParentID  *Id    `json:"parentId,omitempty"`
	BlobID    *Id    `json:"blobId,omitempty"`
	Size      uint64 `json:"size,omitempty"`
	Type      string `json:"type,omitempty"`
	IsFolder  bool   `json:"isFolder"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// RegisterFileNodeHandlers registers FileNode/* method handlers into MethodRegistry.
func RegisterFileNodeHandlers(r *MethodRegistry, backend FileNodeBackend) {
	r.Register("FileNode/get", handleFileNodeGet(backend))
	r.Register("FileNode/query", handleFileNodeQuery(backend))
	r.Register("FileNode/set", handleFileNodeSet(backend))
	r.Register("FileNode/changes", handleFileNodeChanges(backend))
	r.Register("FileNode/queryChanges", handleFileNodeQueryChanges(backend))
}

func handleFileNodeGet(backend FileNodeBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*FileNode
		var notFound []Id
		var err error
		state := "0"

		if backend != nil {
			state = backend.FileNodeState(ctx)
			if hasIDs {
				ids := make([]Id, 0, len(idsRaw))
				for _, item := range idsRaw {
					if idStr, ok := item.(string); ok {
						ids = append(ids, Id(idStr))
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
			notFound = []Id{}
		}

		return "FileNode/get", map[string]any{
			"accountId": accountID,
			"state":     state,
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleFileNodeQuery(backend FileNodeBackend) MethodHandler {
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

		var ids []Id
		var total int
		var err error
		queryState := "0"

		if backend != nil {
			if anchor != "" {
				var allIDs []Id
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
			ids = []Id{}
			total = 0
		}

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

func handleFileNodeSet(backend FileNodeBackend) MethodHandler {
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
		destroyed := make([]Id, 0)
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		// creationRefs maps a creation id to the real id the server assigned, so that
		// #creationId references elsewhere in this same /set resolve (RFC 8620 Section 5.3).
		creationRefs := make(map[string]Id)

		if backend != nil {
			if createRaw, ok := args["create"].(map[string]any); ok {
				// Track which creations are still pending so forward references can be deferred.
				pending := make(map[string]struct{}, len(createRaw))
				for creationID := range createRaw {
					pending[creationID] = struct{}{}
				}

				// Process creations, deferring any whose #creationId references are not yet
				// satisfied, until no further progress can be made (missing or cyclic refs).
				for len(pending) > 0 {
					progressed := false
					for creationID := range pending {
						nodeMap, _ := createRaw[creationID].(map[string]any)
						resolvedMap, deferred := resolveNodeCreationRefs(nodeMap, creationRefs, pending)
						if deferred {
							continue
						}

						nodeBytes, _ := json.Marshal(resolvedMap)
						var node FileNode
						_ = json.Unmarshal(nodeBytes, &node)

						createdNode, err := backend.CreateFileNode(ctx, &node)
						if err != nil {
							notCreated[creationID] = SetError{Type: "invalidProperties", Description: err.Error()}
						} else {
							created[creationID] = createdNode
							creationRefs[creationID] = createdNode.ID
						}
						delete(pending, creationID)
						progressed = true
					}
					if !progressed {
						for creationID := range pending {
							notCreated[creationID] = SetError{Type: "invalidProperties", Description: "unresolved creation reference"}
							delete(pending, creationID)
						}
					}
				}
			}

			if updateRaw, ok := args["update"].(map[string]any); ok {
				for idStr, patchRaw := range updateRaw {
					resolvedID := resolveCreationID(idStr, creationRefs)
					patch, _ := patchRaw.(map[string]any)
					_, err := backend.UpdateFileNode(ctx, Id(resolvedID), patch)
					if err != nil {
						if errors.Is(err, ErrNotFound) {
							notUpdated[idStr] = SetError{Type: "notFound", Description: err.Error()}
						} else {
							notUpdated[idStr] = SetError{Type: "invalidProperties", Description: err.Error()}
						}
					} else {
						updated[idStr] = nil
					}
				}
			}

			if destroyRaw, ok := args["destroy"].([]any); ok {
				for _, item := range destroyRaw {
					if idStr, ok := item.(string); ok {
						resolvedID := resolveCreationID(idStr, creationRefs)
						okDel, err := backend.DeleteFileNode(ctx, Id(resolvedID))
						if err != nil {
							notDestroyed[idStr] = SetError{Type: "serverFail", Description: err.Error()}
						} else if !okDel {
							notDestroyed[idStr] = SetError{Type: "notFound", Description: "filenode not found"}
						} else {
							destroyed = append(destroyed, Id(resolvedID))
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

func handleFileNodeChanges(backend FileNodeBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		var created, updated, destroyed []Id
		newState := "0"
		hasMore := false

		if backend != nil {
			created, updated, destroyed, newState, hasMore = backend.FileNodeChanges(ctx, sinceState)
		}

		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
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

func handleFileNodeQueryChanges(backend FileNodeBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceQueryState, _ := args["sinceQueryState"].(string)
		upToID, _ := args["upToId"].(string)
		filter, _ := args["filter"].(map[string]any)

		added := make([]map[string]any, 0)
		removed := make([]Id, 0)
		newQueryState := "0"

		if backend != nil {
			newQueryState = backend.FileNodeState(ctx)

			// Resolve which objects changed since the client's query state.
			created, updated, destroyed, _, _ := backend.FileNodeChanges(ctx, sinceQueryState)

			// Any changed-or-gone object is first removed from the client's view; those still
			// matching the filter are then re-added at their current position, so moves and
			// membership changes are both reflected (RFC 8620 Section 5.6).
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
