package jmap

import (
	"context"
)

// EmailSubmission Handlers (RFC 8621 Section 7)

// submissionSortableProperties is the set of EmailSubmission properties the server supports
// sorting on (RFC 8621 Section 7.2: emailId, threadId and sentAt MUST be supported; sentAt
// is accepted as an alias for the sendAt property).
var submissionSortableProperties = map[string]bool{
	"emailId": true, "threadId": true, "sendAt": true, "sentAt": true,
}

func handleEmailSubmissionGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		var list []*EmailSubmission
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetSubmissions(ctx, ids)
		} else {
			list, err = backend.GetAllSubmissions(ctx)
		}

		if err != nil || list == nil {
			list = []*EmailSubmission{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "EmailSubmission/get", map[string]any{
			"accountId": accountID,
			"state":     backend.SubmissionState(ctx),
			"list":      filterList(list, props),
			"notFound":  notFound,
		}
	}
}

func handleEmailSubmissionChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		created, updated, destroyed, newState, hasMore := backend.SubmissionChanges(ctx, sinceState)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "EmailSubmission/changes", map[string]any{
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

func handleEmailSubmissionSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.SubmissionState(ctx)
		created := make(map[string]*EmailSubmission)
		notCreated := make(map[string]any)
		updated := make(map[string]*EmailSubmission)
		notUpdated := make(map[string]any)
		var destroyed []Id
		notDestroyed := make(map[string]any)

		// creationRefs maps a creation id to the real id the server assigned (seeded from
		// the request-scoped createdIds map), so #creationId references in this call and
		// in later method calls of the same request resolve (RFC 8620 Section 5.3).
		creationRefs := newSetCreationRefs(ctx)

		if createMap, ok := args["create"].(map[string]any); ok {
			notCreated = runCreateLoop(createMap, creationRefs, func(clientKey string, subData map[string]any) (string, error) {
				identityID, _ := subData["identityId"].(string)
				emailID, _ := subData["emailId"].(string)
				sendAt, _ := subData["sendAt"].(string)

				// Resolve creation ref if emailID or identityID uses #creationId
				emailID = resolveCreationID(emailID, creationRefs)
				identityID = resolveCreationID(identityID, creationRefs)

				var env *SubmissionEnvelope
				if envMap, ok := subData["envelope"].(map[string]any); ok {
					env = &SubmissionEnvelope{}
					if mfMap, ok := envMap["mailFrom"].(map[string]any); ok {
						email, _ := mfMap["email"].(string)
						params, _ := mfMap["parameters"].(map[string]any)
						env.MailFrom = SubmissionAddress{Email: email, Parameters: params}
					}
					if rcptSlice, ok := envMap["rcptTo"].([]any); ok {
						for _, item := range rcptSlice {
							if rcptMap, ok := item.(map[string]any); ok {
								email, _ := rcptMap["email"].(string)
								params, _ := rcptMap["parameters"].(map[string]any)
								env.RcptTo = append(env.RcptTo, SubmissionAddress{Email: email, Parameters: params})
							}
						}
					}
				}

				sub, err := backend.CreateSubmission(ctx, &EmailSubmission{
					IdentityID: Id(identityID),
					EmailID:    Id(emailID),
					Envelope:   env,
					SendAt:     sendAt,
				})
				if err != nil {
					return "", err
				}
				created[clientKey] = sub
				recordCreationRefs(ctx, creationRefs, clientKey, sub.ID)

				// RFC 8621 Section 7.5: onSuccessUpdateEmail
				if patch, ok := subData["onSuccessUpdateEmail"].(map[string]any); ok && emailID != "" {
					if _, err := backend.UpdateEmail(ctx, Id(emailID), patch); err != nil {
						// Email update failed
						delete(created, clientKey)
						_, _ = backend.DeleteSubmission(ctx, sub.ID)
						return "", err
					}
				}
				// RFC 8621 Section 7.5: onSuccessDestroyEmail
				if destroy, ok := subData["onSuccessDestroyEmail"].(bool); ok && destroy && emailID != "" {
					if _, err := backend.DeleteEmail(ctx, Id(emailID)); err != nil {
						delete(created, clientKey)
						_, _ = backend.DeleteSubmission(ctx, sub.ID)
						return "", err
					}
				}
				return string(sub.ID), nil
			})
		}

		// RFC 8621 Section 7.3: EmailSubmissions cannot be updated after creation
		if updateMap, ok := args["update"].(map[string]any); ok {
			for clientKey := range updateMap {
				resolvedID := resolveCreationID(clientKey, creationRefs)
				notUpdated[resolvedID] = SetError{
					Type: "invalidProperties",
					Description: "EmailSubmission objects cannot be updated",
				}
			}
		}

		// RFC 8621 Section 7.3: destroy cancels / deletes submissions
		if destroySlice, ok := args["destroy"].([]any); ok {
			destroyed = make([]Id, 0, len(destroySlice))
			for _, item := range destroySlice {
				if idStr, ok := item.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					ok, err := backend.DeleteSubmission(ctx, Id(resolvedID))
					if err != nil || !ok {
						notDestroyed[resolvedID] = SetError{
							Type: "notFound",
							Description: "EmailSubmission not found or cannot be destroyed",
						}
					} else {
						destroyed = append(destroyed, Id(resolvedID))
					}
				}
			}
		}

		if destroyed == nil {
			destroyed = []Id{}
		}

		return "EmailSubmission/set", map[string]any{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     backend.SubmissionState(ctx),
			"created":      created,
			"notCreated":   notCreated,
			"updated":      updated,
			"notUpdated":   notUpdated,
			"destroyed":    destroyed,
			"notDestroyed": notDestroyed,
		}
	}
}

func handleEmailSubmissionQuery(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		position, posErr := parseQueryPosition(args)
		if posErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := parseQueryAnchor(args)
		if anchorErr != "" {
			return "error", MethodErrorArgs(MethodErrorInvalidArguments, anchorErr)
		}

		var limit *uint64
		if limVal, ok := args["limit"].(float64); ok {
			l := uint64(limVal)
			limit = &l
		}

		filter, _ := args["filter"].(map[string]any)
		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, submissionSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}
		var ids []Id
		var total int
		if anchor != "" {
			allIDs, allTotal, _ := backend.QuerySubmissions(ctx, filter, comparators, 0, nil)
			total = allTotal
			var found bool
			position, ids, found = applyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", MethodErrorArgs(MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else {
			ids, total, _ = backend.QuerySubmissions(ctx, filter, comparators, position, limit)
			position = NormalizePosition(position, total)
		}
		if ids == nil {
			ids = []Id{}
		}

		res := map[string]any{
			"accountId":           accountID,
			"queryState":          backend.SubmissionState(ctx),
			"canCalculateChanges": true,
			"position":            position,
			"ids":                 ids,
			"total":               total,
		}
		if calcTotal, _ := args["calculateTotal"].(bool); calcTotal {
			res["calculateTotal"] = true
		}
		return "EmailSubmission/query", res
	}
}

func handleEmailSubmissionQueryChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)
		filter, _ := args["filter"].(map[string]any)

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.SubmissionChanges(ctx, sinceState)
		if hasMore {
			return "error", MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		comparators := parseComparators(args)
		if errType, errMsg := validateComparators(comparators, submissionSortableProperties); errType != "" {
			return "error", MethodErrorArgs(errType, errMsg)
		}
		currentIDs, _, _ := backend.QuerySubmissions(ctx, filter, comparators, 0, nil)
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
		return "EmailSubmission/queryChanges", res
	}
}
