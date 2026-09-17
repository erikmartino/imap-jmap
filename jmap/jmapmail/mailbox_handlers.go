package jmapmail

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"imap-jmap/jmap/jmapcopy"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// RegisterMailboxHandlers registers RFC 8621 Section 2 Mailbox method handlers into MethodRegistry.
func RegisterMailboxHandlers(r *jmaphandler.MethodRegistry, backend MailBackend) {
	r.Register("Mailbox/get", HandleMailboxGet(backend))
	r.Register("Mailbox/changes", HandleMailboxChanges(backend))
	r.Register("Mailbox/set", HandleMailboxSet(backend))
	r.Register("Mailbox/copy", HandleMailboxCopy(backend))
	r.Register("Mailbox/query", HandleMailboxQuery(backend))
	r.Register("Mailbox/queryChanges", HandleMailboxQueryChanges(backend))
}

// HandleMailboxGet implements Mailbox/get per RFC 8621 Section 2.1.
func HandleMailboxGet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := jmaphandler.ParseProperties(args)

		var list []*Mailbox
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			ids := make([]jmapcore.Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, jmapcore.Id(idStr))
				}
			}
			list, notFound, err = backend.GetMailboxes(ctx, ids)
		} else {
			list, err = backend.GetAllMailboxes(ctx)
			if errName, errArgs, ok := jmaphandler.ValidateGetLimits(ctx, len(list)); !ok {
				return errName, errArgs
			}
		}

		if err != nil || list == nil {
			list = []*Mailbox{}
		}
		if notFound == nil {
			notFound = []jmapcore.Id{}
		}

		return "Mailbox/get", map[string]any{
			"accountId": accountID,
			"state":     backend.MailboxState(ctx),
			"list":      jmaphandler.FilterList(list, props),
			"notFound":  notFound,
		}
	}
}

// HandleMailboxChanges implements Mailbox/changes per RFC 8621 Section 2.2.
func HandleMailboxChanges(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		var maxChanges *uint64
		if mc, ok := args["maxChanges"].(float64); ok {
			if mc < 0 {
				return "error", jmapcore.InvalidArgumentsErrorArgs([]string{"maxChanges"}, "maxChanges must be non-negative")
			}
			m := uint64(mc)
			maxChanges = &m
		}

		created, updated, destroyed, updatedProperties, newState, hasMore := backend.MailboxChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
		}

		resp := map[string]any{
			"accountId":      accountID,
			"oldState":       sinceState,
			"newState":       newState,
			"hasMoreChanges": hasMore,
			"created":        created,
			"updated":        updated,
			"destroyed":      destroyed,
		}
		if len(updatedProperties) > 0 && len(created) == 0 && len(destroyed) == 0 {
			resp["updatedProperties"] = updatedProperties
		}
		return "Mailbox/changes", resp
	}
}

// HandleMailboxSet implements Mailbox/set per RFC 8621 Section 2.3.
func HandleMailboxSet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.MailboxState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", jmapcore.MethodErrorArgs("stateMismatch", "state mismatch")
		}
		created := make(map[string]*Mailbox)
		updated := make(map[string]any)
		destroyed := []jmapcore.Id{}
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		creationRefs := jmaphandler.NewSetCreationRefs(ctx)

		if createMap, ok := args["create"].(map[string]any); ok {
			notCreated = jmaphandler.RunCreateLoop(createMap, creationRefs, func(clientKey string, m map[string]any) (string, error) {
				var invalidProps []string
				if _, has := m["id"]; has {
					invalidProps = append(invalidProps, "id")
				}
				if val, has := m["totalEmails"]; has {
					if n, ok := val.(float64); !ok || n != 0 {
						invalidProps = append(invalidProps, "totalEmails")
					}
				}
				if val, has := m["unreadEmails"]; has {
					if n, ok := val.(float64); !ok || n != 0 {
						invalidProps = append(invalidProps, "unreadEmails")
					}
				}
				if val, has := m["totalThreads"]; has {
					if n, ok := val.(float64); !ok || n != 0 {
						invalidProps = append(invalidProps, "totalThreads")
					}
				}
				if val, has := m["unreadThreads"]; has {
					if n, ok := val.(float64); !ok || n != 0 {
						invalidProps = append(invalidProps, "unreadThreads")
					}
				}
				if mr, has := m["myRights"]; has {
					if mrMap, ok := mr.(map[string]any); ok {
						for k, v := range mrMap {
							b, ok := v.(bool)
							if !ok || !b {
								invalidProps = append(invalidProps, "myRights/"+k)
							}
						}
					} else {
						invalidProps = append(invalidProps, "myRights")
					}
				}
				if len(invalidProps) > 0 {
					return "", jmapcore.SetError{
						Type:       "invalidProperties",
						Properties: invalidProps,
					}
				}

				name, _ := m["name"].(string)
				if name == "" {
					return "", fmt.Errorf("name is required")
				}
				mb := &Mailbox{
					Name:         name,
					SortOrder:    10,
					IsSubscribed: false,
				}
				if pid, ok := m["parentId"].(string); ok && pid != "" {
					p := jmapcore.Id(pid)
					mb.ParentID = &p
				}
				if role, ok := m["role"].(string); ok && role != "" {
					mb.Role = &role
				}
				if so, ok := m["sortOrder"].(float64); ok {
					mb.SortOrder = uint64(so)
				}
				if sub, ok := m["isSubscribed"].(bool); ok {
					mb.IsSubscribed = sub
				}
				createdMB, err := backend.CreateMailbox(ctx, mb)
				if err != nil {
					return "", err
				}
				created[clientKey] = createdMB
				jmaphandler.RecordCreationRefs(ctx, creationRefs, clientKey, createdMB.ID)
				return string(createdMB.ID), nil
			})
		}

		if updateMap, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateMap {
				rawPatch, _ := patchRaw.(map[string]any)
				patch := jmaphandler.ResolvePatchCreationRefs(rawPatch, creationRefs)
				resolvedID := jmaphandler.ResolveCreationID(idStr, creationRefs)
				_, err := backend.UpdateMailbox(ctx, jmapcore.Id(resolvedID), patch)
				if err != nil {
					if errors.Is(err, jmapcore.ErrNotFound) {
						notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: err.Error()}
					} else if setErr, ok := err.(jmapcore.SetError); ok {
						notUpdated[string(resolvedID)] = setErr
					} else {
						notUpdated[string(resolvedID)] = jmapcore.SetError{Type: "invalidProperties", Description: err.Error()}
					}
				} else {
					updated[string(resolvedID)] = nil
				}
			}
		}

		onDestroyRemoveMessages, _ := args["onDestroyRemoveMessages"].(bool)
		onDestroyRemoveEmails, _ := args["onDestroyRemoveEmails"].(bool)
		removeEmails := onDestroyRemoveMessages || onDestroyRemoveEmails
		if destroyList, ok := args["destroy"].([]any); ok {
			for _, rawID := range destroyList {
				if idStr, ok := rawID.(string); ok {
					resolvedID := jmaphandler.ResolveCreationID(idStr, creationRefs)
					okDel, err := backend.DeleteMailbox(ctx, jmapcore.Id(resolvedID), removeEmails)
					if err != nil {
						if setErr, ok := err.(jmapcore.SetError); ok {
							notDestroyed[string(resolvedID)] = setErr
						} else {
							notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "serverFail", Description: err.Error()}
						}
					} else if !okDel {
						notDestroyed[string(resolvedID)] = jmapcore.SetError{Type: "notFound", Description: "mailbox not found"}
					} else {
						destroyed = append(destroyed, jmapcore.Id(resolvedID))
					}
				}
			}
		}

		return "Mailbox/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.MailboxState(ctx),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		}
	}
}

// MatchesMailboxFilter checks whether a mailbox satisfies the given filter map (FilterCondition or FilterOperator).
func MatchesMailboxFilter(mb *Mailbox, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}
	if match, isOp := jmapcore.EvalFilterOperator(filter, func(cond map[string]any) bool {
		return MatchesMailboxFilter(mb, cond)
	}); isOp {
		return match
	}

	if parentVal, exists := filter["parentId"]; exists {
		if parentVal == nil {
			if mb.ParentID != nil {
				return false
			}
		} else if parentIDStr, ok := parentVal.(string); ok {
			if mb.ParentID == nil || string(*mb.ParentID) != parentIDStr {
				return false
			}
		}
	}
	if nameReq, ok := filter["name"].(string); ok {
		if mb.Name != nameReq {
			return false
		}
	}
	if roleReq, ok := filter["role"].(string); ok {
		if mb.Role == nil || *mb.Role != roleReq {
			return false
		}
	}
	if hasAnyRole, ok := filter["hasAnyRole"].(bool); ok {
		hasRole := mb.Role != nil && *mb.Role != ""
		if hasRole != hasAnyRole {
			return false
		}
	}
	if isSubscribed, ok := filter["isSubscribed"].(bool); ok {
		if mb.IsSubscribed != isSubscribed {
			return false
		}
	}
	return true
}

func filterMailboxes(all []*Mailbox, filter map[string]any) []*Mailbox {
	var filtered []*Mailbox
	for _, mb := range all {
		if MatchesMailboxFilter(mb, filter) {
			filtered = append(filtered, mb)
		}
	}
	return filtered
}

// MailboxSortableProperties is the set of Mailbox properties the server supports sorting on.
var MailboxSortableProperties = map[string]bool{"sortOrder": true, "name": true}

// SortMailboxes orders the given mailboxes per RFC 8621 Section 2.4 sort comparators.
func SortMailboxes(all []*Mailbox, comparators []jmapcore.Comparator) {
	usable := false
	for _, c := range comparators {
		if c.Property == "sortOrder" || c.Property == "name" {
			usable = true
		}
	}
	if !usable {
		comparators = []jmapcore.Comparator{{Property: "sortOrder", IsAscending: true}, {Property: "name", IsAscending: true}}
	}
	sort.SliceStable(all, func(i, j int) bool {
		for _, c := range comparators {
			var less, equal bool
			switch c.Property {
			case "sortOrder":
				less, equal = all[i].SortOrder < all[j].SortOrder, all[i].SortOrder == all[j].SortOrder
			case "name":
				less, equal = all[i].Name < all[j].Name, all[i].Name == all[j].Name
			default:
				continue
			}
			if equal {
				continue
			}
			return less == c.IsAscending
		}
		return false
	})
}

// HandleMailboxQuery implements Mailbox/query per RFC 8621 Section 2.4.
func HandleMailboxQuery(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		all, _ := backend.GetAllMailboxes(ctx)

		filter, _ := args["filter"].(map[string]any)
		filtered := filterMailboxes(all, filter)

		comparators := jmapcore.ParseComparators(args)
		if errType, errMsg := jmapcore.ValidateComparators(comparators, MailboxSortableProperties); errType != "" {
			return "error", jmapcore.MethodErrorArgs(errType, errMsg)
		}
		SortMailboxes(filtered, comparators)

		position, posErr := jmapcore.ParseQueryPosition(args)
		if posErr != "" {
			return "error", jmapcore.InvalidArgumentsErrorArgs([]string{"position"}, posErr)
		}

		anchor, anchorOffset, anchorErr := jmapcore.ParseQueryAnchor(args)
		if anchorErr != "" {
			return "error", jmapcore.InvalidArgumentsErrorArgs([]string{"anchor"}, anchorErr)
		}

		if limVal, ok := args["limit"].(float64); ok {
			if limVal < 0 {
				return "error", jmapcore.InvalidArgumentsErrorArgs([]string{"limit"}, "limit must be non-negative")
			}
		}

		total := len(filtered)
		position = jmapcore.NormalizePosition(position, total)
		var pagedIDs []jmapcore.Id
		if anchor != "" {
			allIDs := make([]jmapcore.Id, 0, len(filtered))
			for _, mb := range filtered {
				allIDs = append(allIDs, mb.ID)
			}
			var limit *uint64
			if limVal, ok := args["limit"].(float64); ok {
				l := uint64(limVal)
				limit = &l
			}
			var pos int
			var ok bool
			pos, pagedIDs, ok = jmapcore.ApplyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !ok {
				return "error", jmapcore.MethodErrorArgs("anchorNotFound", "anchor is not in query results")
			}
			position = pos
		} else {
			end := total
			if limVal, ok := args["limit"].(float64); ok {
				l := int(limVal)
				if position+l < end {
					end = position + l
				}
			}
			for i := position; i < end; i++ {
				pagedIDs = append(pagedIDs, filtered[i].ID)
			}
		}
		if pagedIDs == nil {
			pagedIDs = []jmapcore.Id{}
		}
		if len(pagedIDs) == 0 {
			position = 0
		}

		res := map[string]any{
			"accountId":           accountID,
			"queryState":          backend.MailboxState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"ids":                 pagedIDs,
			"total":               total,
		}
		if calcTotal, _ := args["calculateTotal"].(bool); calcTotal {
			res["calculateTotal"] = true
		}
		return "Mailbox/query", res
	}
}

// HandleMailboxQueryChanges implements Mailbox/queryChanges per RFC 8621 Section 2.4.
func HandleMailboxQueryChanges(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)
		filter, _ := args["filter"].(map[string]any)
		comparators := jmapcore.ParseComparators(args)
		if errType, errMsg := jmapcore.ValidateComparators(comparators, MailboxSortableProperties); errType != "" {
			return "error", jmapcore.MethodErrorArgs(errType, errMsg)
		}

		createdIDs, updatedIDs, destroyedIDs, _, newState, hasMore := backend.MailboxChanges(ctx, sinceState, nil)
		if hasMore {
			return "error", jmapcore.MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		all, _ := backend.GetAllMailboxes(ctx)
		current := filterMailboxes(all, filter)
		SortMailboxes(current, comparators)
		currentIDs := make([]jmapcore.Id, 0, len(current))
		for _, mb := range current {
			currentIDs = append(currentIDs, mb.ID)
		}

		added, removed := jmapcore.ComputeQueryChanges(createdIDs, updatedIDs, destroyedIDs, currentIDs, upToID)

		res := map[string]any{
			"accountId":     accountID,
			"oldQueryState": sinceState,
			"newQueryState": newState,
			"added":         added,
			"removed":       removed,
		}
		if upToID != "" {
			res["upToId"] = upToID
		}
		return "Mailbox/queryChanges", res
	}
}

// HandleMailboxCopy implements Mailbox/copy per RFC 8621 Section 2.5.
func HandleMailboxCopy(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, fromAccountID := jmapcopy.ResolveCopyAccountIDs(args)
		srcCtx := jmapcopy.SourceAccountContext(ctx, args)

		oldState, errInv := jmapcopy.ValidateCopyStates(ctx, srcCtx, args, backend.MailboxState, backend.MailboxState)
		if errInv != nil {
			return errInv.Name, errInv.Args
		}

		onDestroy, _ := args["onSuccessDestroyOriginal"].(bool)
		created := make(map[string]*Mailbox)
		notCreated := make(map[string]any)
		destroyOriginals := make([]jmapcore.Id, 0)
		creationRefs := jmaphandler.NewSetCreationRefs(ctx)

		if createMap, ok := args["create"].(map[string]any); ok {
			for clientKey, raw := range createMap {
				mbData, ok := raw.(map[string]any)
				if !ok {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Description: "invalid create entry"}
					continue
				}
				idStr, _ := mbData["id"].(string)
				if idStr == "" {
					notCreated[clientKey] = jmapcore.SetError{Type: "invalidProperties", Description: "missing id"}
					continue
				}
				resolvedID := jmaphandler.ResolveCreationID(idStr, creationRefs)
				list, notFound, _ := backend.GetMailboxes(srcCtx, []jmapcore.Id{jmapcore.Id(resolvedID)})
				if len(list) == 0 || len(notFound) > 0 {
					notCreated[clientKey] = jmapcore.SetError{Type: "notFound", Description: "mailbox not found"}
					continue
				}

				cp := *list[0]
				cp.ID = ""

				// Apply overrides (RFC 8621 Section 2.5)
				if nameOverride, ok := mbData["name"].(string); ok && nameOverride != "" {
					cp.Name = nameOverride
				}
				if parentIDOverride, ok := mbData["parentId"].(string); ok {
					if parentIDOverride == "" {
						cp.ParentID = nil
					} else {
						pid := jmapcore.Id(jmaphandler.ResolveCreationID(parentIDOverride, creationRefs))
						cp.ParentID = &pid
					}
				}

				createdMB, err := backend.CreateMailbox(ctx, &cp)
				if err != nil {
					notCreated[clientKey] = jmapcore.SetError{Type: "serverFail", Description: err.Error()}
				} else {
					created[clientKey] = createdMB
					jmaphandler.RecordCreationRefs(ctx, creationRefs, clientKey, createdMB.ID)
					destroyOriginals = append(destroyOriginals, jmapcore.Id(resolvedID))
				}
			}
		}

		if onDestroy {
			for _, srcID := range destroyOriginals {
				_, _ = backend.DeleteMailbox(srcCtx, srcID, false)
			}
		}

		return "Mailbox/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.MailboxState(ctx),
			"created":       jmaphandler.NilIfEmpty(created),
			"notCreated":    jmaphandler.NilIfEmpty(notCreated),
		}
	}
}
