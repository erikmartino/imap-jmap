package jmap

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Mailbox Handlers (RFC 8621 Section 2)

func handleMailboxGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

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
			"state":     backend.MailboxState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleMailboxChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		created, updated, destroyed, newState, hasMore := backend.MailboxChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "Mailbox/changes", map[string]any{
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

func handleMailboxSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.MailboxState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", "state mismatch")
		}
		created := make(map[string]*Mailbox)
		updated := make(map[string]any)
		destroyed := []Id{}
		notCreated := make(map[string]any)
		notUpdated := make(map[string]any)
		notDestroyed := make(map[string]any)

		// creationRefs maps a creation id to the real id the server assigned (seeded from
		// the request-scoped createdIds map), so #creationId references in this call and
		// in later method calls of the same request resolve (RFC 8620 Section 5.3).
		creationRefs := newSetCreationRefs(ctx)

		if createMap, ok := args["create"].(map[string]any); ok {
			notCreated = runCreateLoop(createMap, creationRefs, func(clientKey string, m map[string]any) (string, error) {
				name, _ := m["name"].(string)
				if name == "" {
					return "", fmt.Errorf("name is required")
				}
				mb := &Mailbox{Name: name}
				if pid, ok := m["parentId"].(string); ok && pid != "" {
					p := Id(pid)
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
				recordCreationRefs(ctx, creationRefs, clientKey, createdMB.ID)
				return string(createdMB.ID), nil
			})
		}

		if updateMap, ok := args["update"].(map[string]any); ok {
			for idStr, patchRaw := range updateMap {
				rawPatch, _ := patchRaw.(map[string]any)
				patch := resolvePatchCreationRefs(rawPatch, creationRefs)
				resolvedID := resolveCreationID(idStr, creationRefs)
				_, err := backend.UpdateMailbox(ctx, Id(resolvedID), patch)
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						notUpdated[string(resolvedID)] = SetError{Type: "notFound", Description: err.Error()}
					} else {
						notUpdated[string(resolvedID)] = SetError{Type: "invalidProperties", Description: err.Error()}
					}
				} else {
					updated[string(resolvedID)] = nil
				}
			}
		}

		if destroyList, ok := args["destroy"].([]any); ok {
			for _, rawID := range destroyList {
				if idStr, ok := rawID.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					okDel, err := backend.DeleteMailbox(ctx, Id(resolvedID))
					if err != nil {
						notDestroyed[string(resolvedID)] = SetError{Type: "serverFail", Description: err.Error()}
					} else if !okDel {
						notDestroyed[string(resolvedID)] = SetError{Type: "notFound", Description: "mailbox not found"}
					} else {
						destroyed = append(destroyed, Id(resolvedID))
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

// filterMailboxes applies a Mailbox/query FilterCondition (RFC 8621 Section 2.4) to the
// given mailboxes, keeping those matching every provided condition.
func filterMailboxes(all []*Mailbox, filter map[string]any) []*Mailbox {
	var filtered []*Mailbox
	for _, mb := range all {
		match := true
		if filter != nil {
			if roleReq, ok := filter["role"].(string); ok {
				if mb.Role == nil || *mb.Role != roleReq {
					match = false
				}
			}
			if parentReq, ok := filter["parentId"].(string); ok {
				if mb.ParentID == nil || string(*mb.ParentID) != parentReq {
					match = false
				}
			}
			if nameReq, ok := filter["name"].(string); ok {
				if mb.Name != nameReq {
					match = false
				}
			}
			if hasAnyRole, ok := filter["hasAnyRole"].(bool); ok {
				if hasAnyRole != (mb.Role != nil && *mb.Role != "") {
					match = false
				}
			}
			if isSubscribed, ok := filter["isSubscribed"].(bool); ok {
				if mb.IsSubscribed != isSubscribed {
					match = false
				}
			}
		}
		if match {
			filtered = append(filtered, mb)
		}
	}
	return filtered
}

// mailboxSortableProperties is the set of Mailbox properties the server supports sorting on
// (RFC 8621 Section 2.4: sortOrder and name MUST be supported).
var mailboxSortableProperties = map[string]bool{"sortOrder": true, "name": true}

// sortMailboxes orders the given mailboxes per the RFC 8621 Section 2.4 sort comparators
// ("sortOrder" and "name"). When no usable comparator is given, the default order is
// sortOrder ascending, then name ascending; RFC 8620 Section 5.5 requires the default
// order to be consistent across calls so queryChanges indices remain stable. Equal
// sortOrder values are ordered alphabetically by name, as recommended by RFC 8621
// Section 2 (Mailbox "sortOrder" description).
func sortMailboxes(all []*Mailbox, comparators []Comparator) {
	usable := false
	for _, c := range comparators {
		if c.Property == "sortOrder" || c.Property == "name" {
			usable = true
		}
	}
	if !usable {
		comparators = []Comparator{{Property: "sortOrder", IsAscending: true}, {Property: "name", IsAscending: true}}
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

func handleMailboxQuery(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		all, _ := backend.GetAllMailboxes(ctx)

		filter, _ := args["filter"].(map[string]any)
		filtered := filterMailboxes(all, filter)

		// RFC 8621 Section 2.4 requires support for sorting by "sortOrder" and "name";
		// the default order (sortOrder then name, both ascending) MUST be applied
		// consistently so /queryChanges indices stay stable between calls (RFC 8620
		// Section 5.5). Without this the results follow map iteration order.
		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, mailboxSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}
		sortMailboxes(filtered, comparators)

		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := parseQueryAnchor(args)
		if anchorErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, anchorErr)
		}

		total := len(filtered)
		position = NormalizePosition(position, total)
		var pagedIDs []Id
		if anchor != "" {
			allIDs := make([]Id, 0, len(filtered))
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
			pos, pagedIDs, ok = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !ok {
				return "error", MethodErrorArgs("anchorNotFound", "anchor is not in query results")
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
			pagedIDs = []Id{}
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

func handleMailboxQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)
		filter, _ := args["filter"].(map[string]any)
		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, mailboxSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.MailboxChanges(ctx, sinceState)
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		all, _ := backend.GetAllMailboxes(ctx)
		current := filterMailboxes(all, filter)
		sortMailboxes(current, comparators)
		currentIDs := make([]Id, 0, len(current))
		for _, mb := range current {
			currentIDs = append(currentIDs, mb.ID)
		}

		added, removed := computeQueryChanges(createdIDs, updatedIDs, destroyedIDs, currentIDs, upToID)

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

func handleMailboxCopy(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		fromAccountID, _ := args["fromAccountId"].(string)
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)
		onDestroy, _ := args["onSuccessDestroyOriginal"].(bool)

		oldState := backend.MailboxState(ctx)
		created := make(map[string]*Mailbox)
		notCreated := make(map[string]SetError)

		for clientKey, raw := range createMap {
			if mbData, ok := raw.(map[string]any); ok {
				if idStr, ok := mbData["id"].(string); ok {
					list, _, _ := backend.GetMailboxes(ctx, []Id{Id(idStr)})
					if len(list) > 0 {
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
								pid := Id(parentIDOverride)
								cp.ParentID = &pid
							}
						}

						createdMB, err := backend.CreateMailbox(ctx, &cp)
						if err == nil {
							created[clientKey] = createdMB
							if onDestroy {
								_, _ = backend.DeleteMailbox(ctx, Id(idStr))
							}
						} else {
							notCreated[clientKey] = SetError{Type: "serverFail", Description: err.Error()}
						}
					} else {
						notCreated[clientKey] = SetError{Type: "notFound", Description: "mailbox not found"}
					}
				}
			}
		}

		return "Mailbox/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"oldState":      oldState,
			"newState":      backend.MailboxState(ctx),
			"created":       created,
			"notCreated":    notCreated,
		}
	}
}
