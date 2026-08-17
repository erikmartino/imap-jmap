package jmap

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// EmailSubmission Handlers (RFC 8621 Section 7)

// submissionSortableProperties is the set of EmailSubmission properties the server supports
// sorting on (RFC 8621 Section 7.2: emailId, threadId and sentAt MUST be supported; sentAt
// is accepted as an alias for the sendAt property; undoStatus is also supported).
var submissionSortableProperties = map[string]bool{
	"emailId": true, "threadId": true, "sendAt": true, "sentAt": true, "undoStatus": true,
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

func handleEmailSubmissionSet(backend MailBackend, blobBackend BlobBackend, resolver AccountResolver, allowedRecipients map[string]bool, outbound OutboundMailSender) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.SubmissionState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", MethodErrorArgs("stateMismatch", fmt.Sprintf("state token %q does not match current state %q", ifInState, oldState))
		}

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

				if identityID == "" {
					return "", SetError{Type: "invalidProperties", Description: "identityId is required"}
				}
				if emailID == "" {
					return "", SetError{Type: "invalidProperties", Description: "emailId is required"}
				}

				// RFC 8621 Section 7.5: validate identityId corresponds to an Identity the account has access to
				identities, err := backend.GetIdentities(ctx)
				if err == nil && len(identities) > 0 {
					foundIdent := false
					for _, ident := range identities {
						if ident.ID == Id(identityID) {
							foundIdent = true
							break
						}
					}
					if !foundIdent {
						return "", SetError{Type: "invalidProperties", Description: "identityId not found"}
					}
				}

				// RFC 8621 Section 7.5: validate sendAt format if provided
				if sendAt != "" {
					if _, err := time.Parse(time.RFC3339, sendAt); err != nil {
						return "", SetError{Type: "invalidProperties", Description: "invalid sendAt date format"}
					}
				}

				log.Printf("EmailSubmission/set: creating submission for account %s (email %s, identity %s, sendAt %q)",
					accountID, emailID, identityID, sendAt)

				// Load referenced email to read headers if envelope rcptTo is missing
				var targetEmail *Email
				emails, _, _ := backend.GetEmails(ctx, []Id{Id(emailID)})
				if len(emails) == 0 {
					return "", SetError{Type: "invalidProperties", Description: "referenced email not found"}
				}
				targetEmail = emails[0]

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

				// Collect recipient email addresses
				var recipients []string
				if env != nil && len(env.RcptTo) > 0 {
					for _, sa := range env.RcptTo {
						if sa.Email != "" {
							recipients = append(recipients, sa.Email)
						}
					}
				} else if targetEmail != nil {
					for _, addr := range targetEmail.To {
						if addr.Email != "" {
							recipients = append(recipients, addr.Email)
						}
					}
					for _, addr := range targetEmail.CC {
						if addr.Email != "" {
							recipients = append(recipients, addr.Email)
						}
					}
					for _, addr := range targetEmail.BCC {
						if addr.Email != "" {
							recipients = append(recipients, addr.Email)
						}
					}
				}

				deliveryStatus := make(map[string]DeliveryStatus)
				deliverableCount := 0
				var externalRecipients []string

				activeResolver := resolver
				if activeResolver == nil {
					activeResolver = PrimaryDomainResolver{PrimaryDomain: "example.com"}
				}

				for _, rcpt := range recipients {
					rcptClean := strings.TrimSpace(rcpt)
					if rcptClean == "" {
						continue
					}
					targetAccountID, local := activeResolver.ResolveAccountID(ctx, rcptClean)
					log.Printf("EmailSubmission/set: recipient %q resolved local=%v account=%q", rcptClean, local, targetAccountID)
					if local {
						rcptCtx := ContextWithAccountID(ctx, targetAccountID)
						if targetEmail != nil {
							copyEmail := *targetEmail
							copyEmail.ID = ""
							copyEmail.MailboxIDs = map[Id]bool{"mb-inbox": true}
							deliveredCopy, err := backend.CreateEmail(rcptCtx, &copyEmail)
							if err != nil {
								log.Printf("EmailSubmission/set: local delivery to %q failed: %v", rcptClean, err)
								deliveryStatus[rcptClean] = DeliveryStatus{
									Delivered: "failed",
									SmtpReply: "451 4.3.0 local delivery failed: " + err.Error(),
								}
							} else {
								log.Printf("EmailSubmission/set: delivered copy of email %s to %q (account %s, new email %s)",
									emailID, rcptClean, targetAccountID, deliveredCopy.ID)
								deliveryStatus[rcptClean] = DeliveryStatus{
									Delivered: "yes",
									SmtpReply: "250 2.0.0 OK local delivery",
								}
								deliverableCount++
							}
						} else {
							deliveryStatus[rcptClean] = DeliveryStatus{
								Delivered: "yes",
								SmtpReply: "250 2.0.0 OK local delivery",
							}
							deliverableCount++
						}
					} else {
						if allowedRecipients != nil && allowedRecipients[strings.ToLower(rcptClean)] {
							log.Printf("EmailSubmission/set: recipient %q is external and allow-listed; relaying via MX", rcptClean)
							externalRecipients = append(externalRecipients, rcptClean)
						} else {
							log.Printf("EmailSubmission/set: recipient %q is external and NOT allow-listed; refused", rcptClean)
							deliveryStatus[rcptClean] = DeliveryStatus{
								Delivered: "failed",
								SmtpReply: "550 5.7.1 Recipient not in allow-list",
							}
						}
					}
				}

				// Relay allow-listed external recipients to their domain's MX servers
				// (RFC 5321 Section 5.1).
				if len(externalRecipients) > 0 {
					var rawBytes []byte
					if targetEmail != nil {
						if targetEmail.BlobID != "" && blobBackend != nil {
							principalAccountID, _ := AccountIDFromContext(ctx)
							if blob, found, err := blobBackend.GetBlob(ctx, principalAccountID, string(targetEmail.BlobID)); err == nil && found && blob != nil {
								rawBytes = blob.Data
							}
						}
						if len(rawBytes) == 0 {
							rawBytes = FormatEmailRFC822(targetEmail)
						}
					}

					if outbound != nil && len(rawBytes) > 0 {
						mailFrom := ""
						if env != nil && env.MailFrom.Email != "" {
							mailFrom = env.MailFrom.Email
						} else if len(targetEmail.From) > 0 {
							mailFrom = targetEmail.From[0].Email
						}
						results := outbound.SendMail(ctx, mailFrom, externalRecipients, rawBytes)
						for _, rcpt := range externalRecipients {
							res, ok := results[rcpt]
							status := "failed"
							if !ok {
								res = OutboundDeliveryResult{Delivered: false, SmtpReply: "451 4.3.0 no delivery result from outbound relay"}
							}
							if res.Delivered {
								status = "yes"
								deliverableCount++
							}
							deliveryStatus[rcpt] = DeliveryStatus{Delivered: status, SmtpReply: res.SmtpReply}
						}
						log.Printf("EmailSubmission/set: external delivery results: %v", deliveryStatus)
					} else if outbound == nil {
						// In environments without a configured outbound sender (e.g. basic in-memory test server),
						// allow-listed external recipients are accepted and queued.
						for _, rcpt := range externalRecipients {
							deliveryStatus[rcpt] = DeliveryStatus{
								Delivered: "yes",
								SmtpReply: "250 2.0.0 OK queued external",
							}
							deliverableCount++
						}
					} else {
						for _, rcpt := range externalRecipients {
							deliveryStatus[rcpt] = DeliveryStatus{
								Delivered: "failed",
								SmtpReply: "554 5.3.4 referenced message unavailable",
							}
						}
					}
				}

				if len(recipients) > 0 && deliverableCount == 0 {
					log.Printf("EmailSubmission/set: submission %q forbidden: no recipient is deliverable", clientKey)
					return "", fmt.Errorf("forbidden: no recipient is deliverable")
				}

				sub, err := backend.CreateSubmission(ctx, &EmailSubmission{
					IdentityID:     Id(identityID),
					EmailID:        Id(emailID),
					Envelope:       env,
					SendAt:         sendAt,
					DeliveryStatus: deliveryStatus,
				})
				if err != nil {
					log.Printf("EmailSubmission/set: failed to create submission %q: %v", clientKey, err)
					return "", err
				}
				log.Printf("EmailSubmission/set: created submission %s for email %s (recipients %v, deliveryStatus %v)",
					sub.ID, emailID, recipients, deliveryStatus)
				created[clientKey] = sub
				recordCreationRefs(ctx, creationRefs, clientKey, sub.ID)

				return string(sub.ID), nil
			})
		}

		// RFC 8621 Section 7.5: EmailSubmission update
		// Submissions are immutable except for updating undoStatus to "canceled" when pending.
		if updateMap, ok := args["update"].(map[string]any); ok {
			for clientKey, patchRaw := range updateMap {
				resolvedID := resolveCreationID(clientKey, creationRefs)
				patch, ok := patchRaw.(map[string]any)
				if !ok {
					notUpdated[resolvedID] = SetError{
						Type:        "invalidProperties",
						Description: "patch must be an object",
					}
					continue
				}
				resolvedPatch := resolvePatchCreationRefs(patch, creationRefs)
				updatedSub, err := backend.UpdateSubmission(ctx, Id(resolvedID), resolvedPatch)
				if err != nil {
					errStr := err.Error()
					errType := "invalidProperties"
					errMsg := errStr
					if strings.HasPrefix(errStr, "notFound:") {
						errType = "notFound"
						errMsg = strings.TrimSpace(strings.TrimPrefix(errStr, "notFound:"))
					} else if strings.HasPrefix(errStr, "cannotCancel:") {
						errType = "cannotCancel"
						errMsg = strings.TrimSpace(strings.TrimPrefix(errStr, "cannotCancel:"))
					} else if strings.HasPrefix(errStr, "alreadyCanceled:") {
						errType = "alreadyCanceled"
						errMsg = strings.TrimSpace(strings.TrimPrefix(errStr, "alreadyCanceled:"))
					} else if strings.HasPrefix(errStr, "invalidProperties:") {
						errType = "invalidProperties"
						errMsg = strings.TrimSpace(strings.TrimPrefix(errStr, "invalidProperties:"))
					}
					notUpdated[resolvedID] = SetError{
						Type:        errType,
						Description: errMsg,
					}
				} else {
					updated[resolvedID] = updatedSub
				}
			}
		}

		// RFC 8621 Section 7.3: destroy cancels / deletes submissions
		destroyedEmailIDs := make(map[string]Id)
		if destroySlice, ok := args["destroy"].([]any); ok {
			destroyed = make([]Id, 0, len(destroySlice))
			for _, item := range destroySlice {
				if idStr, ok := item.(string); ok {
					resolvedID := resolveCreationID(idStr, creationRefs)
					var emailID Id
					if subs, _, err := backend.GetSubmissions(ctx, []Id{Id(resolvedID)}); err == nil && len(subs) > 0 {
						emailID = subs[0].EmailID
					}
					ok, err := backend.DeleteSubmission(ctx, Id(resolvedID))
					if err != nil || !ok {
						notDestroyed[resolvedID] = SetError{
							Type:        "notFound",
							Description: "EmailSubmission not found or cannot be destroyed",
						}
					} else {
						destroyed = append(destroyed, Id(resolvedID))
						if emailID != "" {
							destroyedEmailIDs[resolvedID] = emailID
						}
					}
				}
			}
		}

		if destroyed == nil {
			destroyed = []Id{}
		}

		// RFC 8621 Section 7.5: onSuccessUpdateEmail / onSuccessDestroyEmail are top-level
		// arguments mapping EmailSubmission ids (possibly "#creationId" refs to submissions
		// created in this same call) to a patch/destroy request applied to the Email the
		// submission references, after the submission itself succeeds. A failed email may
		// still have left the server, so the submission is NOT rolled back (RFC 8621
		// Section 7.5: "If the referenced Email is destroyed at any point after the
		// EmailSubmission object is created, this MUST NOT change the behaviour of the
		// submission"). Instead the failures are reported via the implicit Email/set
		// response that MUST follow the EmailSubmission/set response.
		oldEmailState := backend.EmailState(ctx)
		var emailUpdated, emailNotUpdated map[string]any
		var emailDestroyed []string
		var emailNotDestroyed map[string]any

		// On success-reference threading (RFC 8620 Section 5.3) the EmailSubmission created
		// id is resolved from its "#creationId"; a plain id names a pre-existing submission.
		// "succeeded" means the submission's create/update/destroy in this call succeeded.
		succeededEmailID := func(idStr string) (Id, bool) {
			var resolvedID string
			if strings.HasPrefix(idStr, "#") {
				// Submission created in this same call: resolve via the created map (keyed by
				// the client's creation id) and read its Email id from the created record.
				sub, ok := created[idStr[1:]]
				if !ok {
					return "", false
				}
				return sub.EmailID, true
			}
			resolvedID = resolveCreationID(idStr, creationRefs)
			// Pre-existing submission destroyed in this call: capture its Email id before it
			// was removed.
			if emailID, ok := destroyedEmailIDs[resolvedID]; ok {
				return emailID, true
			}
			// Pre-existing submission (e.g. destroyed in a different account access): fetch it.
			subs, _, err := backend.GetSubmissions(ctx, []Id{Id(resolvedID)})
			if err != nil || len(subs) == 0 {
				return "", false
			}
			return subs[0].EmailID, true
		}

		if patchMap, ok := args["onSuccessUpdateEmail"].(map[string]any); ok {
			emailUpdated = map[string]any{}
			emailNotUpdated = map[string]any{}
			for idStr, patch := range patchMap {
				emailID, ok := succeededEmailID(idStr)
				if !ok {
					continue
				}
				p, _ := patch.(map[string]any)
				if p == nil {
					emailNotUpdated[string(emailID)] = SetError{
						Type:        "invalidProperties",
						Description: "onSuccessUpdateEmail patch must be an object",
					}
					continue
				}
				resolvedPatch := resolvePatchCreationRefs(p, creationRefs)
				if _, err := backend.UpdateEmail(ctx, emailID, resolvedPatch); err != nil {
					emailNotUpdated[string(emailID)] = SetError{
						Type:        "invalidProperties",
						Description: err.Error(),
					}
				} else {
					emailUpdated[string(emailID)] = nil
				}
			}
		}

		if destroyIDs, ok := args["onSuccessDestroyEmail"].([]any); ok {
			emailDestroyed = []string{}
			emailNotDestroyed = map[string]any{}
			for _, item := range destroyIDs {
				idStr, _ := item.(string)
				if idStr == "" {
					continue
				}
				emailID, ok := succeededEmailID(idStr)
				if !ok {
					continue
				}
				delOK, delErr := backend.DeleteEmail(ctx, emailID)
				if delErr != nil || !delOK {
					emailNotDestroyed[string(emailID)] = SetError{
						Type:        "notFound",
						Description: "Email referenced by onSuccessDestroyEmail not found",
					}
				} else {
					emailDestroyed = append(emailDestroyed, string(emailID))
				}
			}
		}

		// Emit the implicit Email/set response AFTER this response (RFC 8621 Section 7.5),
		// reusing the same client call id, when any email was touched by the two arguments.
		if len(emailUpdated) > 0 || len(emailNotUpdated) > 0 || len(emailDestroyed) > 0 || len(emailNotDestroyed) > 0 {
			appendSpillResponse(ctx, Invocation{
				Name: "Email/set",
				Args: map[string]any{
					"accountId":    accountID,
					"oldState":     oldEmailState,
					"newState":     backend.EmailState(ctx),
					"created":      nil,
					"updated":      emailUpdated,
					"destroyed":    emailDestroyed,
					"notCreated":   nil,
					"notUpdated":   emailNotUpdated,
					"notDestroyed": emailNotDestroyed,
				},
				ClientCallID: clientCallID,
			})
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
