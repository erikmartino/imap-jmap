package jmapmail

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// EmailSubmission Handlers (RFC 8621 Section 7)

// InboxMailboxID returns the id of the account's INBOX mailbox (role "inbox") as
// reported by the backend, or "" when it cannot be determined. Backends use
// different id schemes (the memory backend uses "mb-inbox", gateway backends
// derive ids from the folder name), so delivery code must never hardcode one.
func InboxMailboxID(ctx context.Context, backend MailBackend) jmapcore.Id {
	if backend == nil {
		return ""
	}
	mailboxes, err := backend.GetAllMailboxes(ctx)
	if err != nil {
		return ""
	}
	for _, mb := range mailboxes {
		if mb != nil && mb.Role != nil && *mb.Role == RoleInbox {
			return mb.ID
		}
	}
	return ""
}

// MailboxIDByName returns the ID of a mailbox matching name or role for the given context.
func MailboxIDByName(ctx context.Context, backend MailBackend, name string) jmapcore.Id {
	if backend == nil || name == "" {
		return ""
	}
	mailboxes, err := backend.GetAllMailboxes(ctx)
	if err != nil {
		return ""
	}
	for _, mb := range mailboxes {
		if mb != nil {
			if strings.EqualFold(mb.Name, name) {
				return mb.ID
			}
			if mb.Role != nil && strings.EqualFold(*mb.Role, name) {
				return mb.ID
			}
		}
	}
	return ""
}

// submissionSortableProperties is the set of EmailSubmission properties the server supports
// sorting on (RFC 8621 Section 7.2: emailId, threadId and sentAt MUST be supported; sentAt
// is accepted as an alias for the sendAt property; undoStatus is also supported).
var submissionSortableProperties = map[string]bool{
	"emailId": true, "threadId": true, "sendAt": true, "sentAt": true, "undoStatus": true,
}

func HandleEmailSubmissionGet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := jmaphandler.ParseProperties(args)

		var list []*EmailSubmission
		var notFound []jmapcore.Id
		var err error

		if hasIDs {
			ids := make([]jmapcore.Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, jmapcore.Id(idStr))
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
			notFound = []jmapcore.Id{}
		}

		return "EmailSubmission/get", map[string]any{
			"accountId": accountID,
			"state":     backend.SubmissionState(ctx),
			"list":      jmaphandler.FilterList(list, props),
			"notFound":  notFound,
		}
	}
}

func HandleEmailSubmissionChanges(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		maxChanges, errArgs := jmaphandler.ParseMaxChanges(args)
		if errArgs != nil {
			return "error", errArgs
		}

		created, updated, destroyed, newState, hasMore := backend.SubmissionChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []jmapcore.Id{}
		}
		if updated == nil {
			updated = []jmapcore.Id{}
		}
		if destroyed == nil {
			destroyed = []jmapcore.Id{}
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

func HandleEmailSubmissionSet(backend MailBackend, blobBackend jmapblob.BlobBackend, resolver jmapauth.AccountResolver, allowedRecipients map[string]bool, outbound OutboundMailSender) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		oldState := backend.SubmissionState(ctx)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" && ifInState != oldState {
			return "error", jmapcore.MethodErrorArgs("stateMismatch", fmt.Sprintf("state token %q does not match current state %q", ifInState, oldState))
		}

		created := make(map[string]*EmailSubmission)
		notCreated := make(map[string]any)
		updated := make(map[string]*EmailSubmission)
		notUpdated := make(map[string]any)
		var destroyed []jmapcore.Id
		notDestroyed := make(map[string]any)

		// creationRefs maps a creation id to the real id the server assigned (seeded from
		// the request-scoped createdIds map), so #creationId references in this call and
		// in later method calls of the same request resolve (RFC 8620 Section 5.3).
		creationRefs := jmaphandler.NewSetCreationRefs(ctx)

		if createMap, ok := args["create"].(map[string]any); ok {
			notCreated = jmaphandler.RunCreateLoop(createMap, creationRefs, func(clientKey string, subData map[string]any) (string, error) {
				identityID, _ := subData["identityId"].(string)
				emailID, _ := subData["emailId"].(string)
				sendAt, _ := subData["sendAt"].(string)

				// Resolve creation ref if emailID or identityID uses #creationId
				emailID = jmaphandler.ResolveCreationID(emailID, creationRefs)
				identityID = jmaphandler.ResolveCreationID(identityID, creationRefs)

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

				identities, _ := backend.GetIdentities(ctx)
				if identityID == "" {
					if env != nil && env.MailFrom.Email != "" {
						for _, ident := range identities {
							if strings.EqualFold(ident.Email, env.MailFrom.Email) {
								identityID = string(ident.ID)
								break
							}
						}
						if identityID == "" && len(identities) > 0 {
							identityID = string(identities[0].ID)
						}
					}
				}

				if identityID == "" {
					return "", jmapcore.SetError{Type: "invalidProperties", Description: "identityId is required"}
				} else if len(identities) > 0 {
					foundIdent := false
					for _, ident := range identities {
						if ident.ID == jmapcore.Id(identityID) {
							foundIdent = true
							break
						}
					}
					if !foundIdent && identityID != "id-default" {
						return "", jmapcore.SetError{Type: "invalidProperties", Description: "identityId not found"}
					}
				}

				if emailID == "" {
					return "", jmapcore.SetError{Type: "invalidProperties", Description: "emailId is required"}
				}

				// RFC 8621 Section 7.5: validate sendAt format if provided
				if sendAt != "" {
					if _, err := time.Parse(time.RFC3339, sendAt); err != nil {
						return "", jmapcore.SetError{Type: "invalidProperties", Description: "invalid sendAt date format"}
					}
				}

				log.Printf("EmailSubmission/set: creating submission for account %s (email %s, identity %s, sendAt %q)",
					accountID, emailID, identityID, sendAt)

				// Load referenced email to read headers if envelope rcptTo is missing
				var targetEmail *Email
				emails, _, _ := backend.GetEmails(ctx, []jmapcore.Id{jmapcore.Id(emailID)})
				if len(emails) == 0 {
					return "", jmapcore.SetError{Type: "invalidProperties", Description: "referenced email not found"}
				}
				targetEmail = emails[0]

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

				if len(recipients) == 0 {
					return "", jmapcore.SetError{Type: "noRecipients", Description: "email and envelope have no recipients"}
				}

				deliveryStatus := make(map[string]DeliveryStatus)
				deliverableCount := 0
				var externalRecipients []string

				activeResolver := resolver
				if activeResolver == nil {
					activeResolver = jmapauth.PrimaryDomainResolver{PrimaryDomain: "example.com"}
				}

				accountEmail, _ := jmapauth.SubjectForAccountID(accountID)
				hasSMTPServer := false
				if smtpBe, ok := backend.(SMTPAvailableBackend); ok && smtpBe.HasSMTPServer() {
					hasSMTPServer = true
				}

				if hasSMTPServer {
					// Route all deliveries through the outer SMTP server (e.g. Postfix).
					// The outer SMTP server delivers local recipients to Dovecot via LMTP
					// (creating clean, unread, non-draft messages and running Sieve scripts),
					// and relays external recipients. Avoids in-process loopback in imap-jmap.
					for _, rcpt := range recipients {
						rcptClean := strings.TrimSpace(rcpt)
						if rcptClean == "" {
							continue
						}
						targetAccountID, local := activeResolver.ResolveAccountID(ctx, rcptClean)
						if !local && accountEmail != "" && strings.Contains(accountEmail, "@") && strings.Contains(rcptClean, "@") {
							senderParts := strings.Split(accountEmail, "@")
							rcptParts := strings.Split(rcptClean, "@")
							if len(senderParts) == 2 && len(rcptParts) == 2 && strings.EqualFold(senderParts[1], rcptParts[1]) {
								targetAccountID = jmapauth.AccountIDForSubject(rcptClean)
								local = true
							}
						}
						if !local {
							isAllowed := false
							if allowedRecipients == nil || len(allowedRecipients) == 0 || allowedRecipients["*"] {
								isAllowed = true
							} else if allowedRecipients[strings.ToLower(rcptClean)] {
								isAllowed = true
							}

							if !isAllowed {
								log.Printf("EmailSubmission/set: recipient %q is external and NOT allow-listed; refused", rcptClean)
								deliveryStatus[rcptClean] = DeliveryStatus{
									Delivered: "failed",
									SmtpReply: "550 5.7.1 Recipient not in allow-list",
								}
								continue
							}
						}
						_ = targetAccountID
						deliverableCount++
					}
				} else {
					for _, rcpt := range recipients {
						rcptClean := strings.TrimSpace(rcpt)
						if rcptClean == "" {
							continue
						}
						targetAccountID, local := activeResolver.ResolveAccountID(ctx, rcptClean)
						if !local && accountEmail != "" && strings.Contains(accountEmail, "@") && strings.Contains(rcptClean, "@") {
							senderParts := strings.Split(accountEmail, "@")
							rcptParts := strings.Split(rcptClean, "@")
							if len(senderParts) == 2 && len(rcptParts) == 2 && strings.EqualFold(senderParts[1], rcptParts[1]) {
								targetAccountID = jmapauth.AccountIDForSubject(rcptClean)
								local = true
							}
						}
						log.Printf("EmailSubmission/set: recipient %q resolved local=%v account=%q", rcptClean, local, targetAccountID)
						if local {
							// Do NOT inherit the submitting account's context: it carries the
							// sender's credentials (and accountID), which gateway backends use
							// to pick the Dovecot login — the delivered copy would land in the
							// sender's mailbox. A fresh context keyed only by the recipient's
							// accountID makes GetClientForContext resolve the recipient.
							rcptCtx := jmapauth.ContextWithAccountID(context.Background(), targetAccountID)
							if targetEmail != nil {
								copyEmail := *targetEmail
								copyEmail.ID = ""
								// Resolve the recipient's INBOX mailbox id by role rather than
								// assuming a backend-specific id: the memory backend uses
								// "mb-inbox", gateway backends (IMAP/SMTP) derive it from the
								// folder name. A hardcoded id would append into a nonexistent
								// folder and fail local delivery.
								inboxID := InboxMailboxID(rcptCtx, backend)
								if inboxID == "" {
									log.Printf("EmailSubmission/set: local delivery to %q failed: no INBOX mailbox for account", rcptClean)
									deliveryStatus[rcptClean] = DeliveryStatus{
										Delivered: "failed",
										SmtpReply: "451 4.3.0 local delivery failed: no INBOX mailbox",
									}
									continue
								}
								copyEmail.MailboxIDs = map[jmapcore.Id]bool{inboxID: true}
								// Delivered copy in recipient inbox must not inherit sender's draft keywords
								copyEmail.Keywords = make(map[string]bool)
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
							isAllowed := false
							if allowedRecipients == nil || len(allowedRecipients) == 0 || allowedRecipients["*"] {
								isAllowed = true
							} else if allowedRecipients[strings.ToLower(rcptClean)] {
								isAllowed = true
							}

							if isAllowed {
								log.Printf("EmailSubmission/set: recipient %q is external and allowed; relaying via MX", rcptClean)
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
								principalAccountID, _ := jmapauth.AccountIDFromContext(ctx)
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
							rawBytes = EnsureValidMessageID(rawBytes, mailFrom)
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
				}

				mailFrom := accountEmail
				if env != nil && env.MailFrom.Email != "" {
					mailFrom = env.MailFrom.Email
				} else if targetEmail != nil && len(targetEmail.From) > 0 {
					mailFrom = targetEmail.From[0].Email
				}
				subj := ""
				if targetEmail != nil {
					subj = targetEmail.Subject
				}

				if len(recipients) > 0 && deliverableCount == 0 {
					log.Printf("[MAIL OUTBOUND FORBIDDEN] Account: %s From: <%s> To: %v Subject: %q (EmailId: %s, Reason: no recipient is deliverable, DeliveryStatus: %v)",
						accountID, mailFrom, recipients, subj, emailID, deliveryStatus)
					return "", fmt.Errorf("forbidden: no recipient is deliverable")
				}

				threadID := jmapcore.Id(emailID)
				if targetEmail != nil && targetEmail.ThreadID != "" {
					threadID = targetEmail.ThreadID
				}
				if sendAt == "" {
					sendAt = time.Now().UTC().Format(time.RFC3339)
				}

				undoStatus := "final"
				if sendAt != "" {
					if t, err := time.Parse(time.RFC3339, sendAt); err == nil && t.After(time.Now()) {
						undoStatus = "pending"
					}
				}

				sub, err := backend.CreateSubmission(ctx, &EmailSubmission{
					IdentityID:     jmapcore.Id(identityID),
					EmailID:        jmapcore.Id(emailID),
					ThreadID:       threadID,
					Envelope:       env,
					SendAt:         sendAt,
					UndoStatus:     undoStatus,
					DeliveryStatus: deliveryStatus,
				})
				if err != nil {
					log.Printf("[MAIL OUTBOUND ERROR] Account: %s EmailId: %s: failed to create submission %q: %v", accountID, emailID, clientKey, err)
					return "", err
				}
				if sub != nil && sub.DeliveryStatus != nil {
					for rcpt, st := range sub.DeliveryStatus {
						deliveryStatus[rcpt] = st
					}
				}
				for rcpt, st := range deliveryStatus {
					log.Printf("[MAIL OUTBOUND] Account: %s From: <%s> To: <%s> Subject: %q -> SubmissionId: %s EmailId: %s (Status: %s, SmtpReply: %q)",
						accountID, mailFrom, rcpt, subj, sub.ID, emailID, st.Delivered, st.SmtpReply)
				}
				created[clientKey] = sub
				jmaphandler.RecordCreationRefs(ctx, creationRefs, clientKey, sub.ID)

				return string(sub.ID), nil
			})
		}

		// RFC 8621 Section 7.5: EmailSubmission update
		// Submissions are immutable except for updating undoStatus to "canceled" when pending.
		if updateMap, ok := args["update"].(map[string]any); ok {
			for clientKey, patchRaw := range updateMap {
				resolvedID := jmaphandler.ResolveCreationID(clientKey, creationRefs)
				patch, ok := patchRaw.(map[string]any)
				if !ok {
					notUpdated[resolvedID] = jmapcore.SetError{
						Type:        "invalidProperties",
						Description: "patch must be an object",
					}
					continue
				}
				resolvedPatch := jmaphandler.ResolvePatchCreationRefs(patch, creationRefs)
				updatedSub, err := backend.UpdateSubmission(ctx, jmapcore.Id(resolvedID), resolvedPatch)
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
					notUpdated[resolvedID] = jmapcore.SetError{
						Type:        errType,
						Description: errMsg,
					}
				} else {
					updated[resolvedID] = updatedSub
				}
			}
		}

		// RFC 8621 Section 7.3: destroy cancels / deletes submissions
		destroyedEmailIDs := make(map[string]jmapcore.Id)
		if destroySlice, ok := args["destroy"].([]any); ok {
			destroyed = make([]jmapcore.Id, 0, len(destroySlice))
			for _, item := range destroySlice {
				if idStr, ok := item.(string); ok {
					resolvedID := jmaphandler.ResolveCreationID(idStr, creationRefs)
					var emailID jmapcore.Id
					if subs, _, err := backend.GetSubmissions(ctx, []jmapcore.Id{jmapcore.Id(resolvedID)}); err == nil && len(subs) > 0 {
						emailID = subs[0].EmailID
					}
					ok, err := backend.DeleteSubmission(ctx, jmapcore.Id(resolvedID))
					if err != nil || !ok {
						notDestroyed[resolvedID] = jmapcore.SetError{
							Type:        "notFound",
							Description: "EmailSubmission not found or cannot be destroyed",
						}
					} else {
						destroyed = append(destroyed, jmapcore.Id(resolvedID))
						if emailID != "" {
							destroyedEmailIDs[resolvedID] = emailID
						}
					}
				}
			}
		}

		if destroyed == nil {
			destroyed = []jmapcore.Id{}
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
		succeededEmailID := func(idStr string) (jmapcore.Id, bool) {
			if strings.HasPrefix(idStr, "#") {
				// Submission created in this same call: resolve via the created map (keyed by
				// the client's creation id) and read its Email id from the created record.
				sub, ok := created[idStr[1:]]
				if ok && sub != nil {
					return sub.EmailID, true
				}
			}
			if sub, ok := created[idStr]; ok && sub != nil {
				return sub.EmailID, true
			}
			for _, sub := range created {
				if sub != nil && (sub.ID == jmapcore.Id(idStr) || sub.EmailID == jmapcore.Id(idStr)) {
					return sub.EmailID, true
				}
			}
			resolvedID := jmaphandler.ResolveCreationID(idStr, creationRefs)
			// Pre-existing submission destroyed in this call: capture its Email id before it
			// was removed.
			if emailID, ok := destroyedEmailIDs[resolvedID]; ok {
				return emailID, true
			}
			// Pre-existing submission (e.g. destroyed in a different account access): fetch it.
			subs, _, err := backend.GetSubmissions(ctx, []jmapcore.Id{jmapcore.Id(resolvedID)})
			if err == nil && len(subs) > 0 && subs[0] != nil {
				return subs[0].EmailID, true
			}
			// If idStr names the Email directly:
			if emails, _, err := backend.GetEmails(ctx, []jmapcore.Id{jmapcore.Id(idStr)}); err == nil && len(emails) > 0 {
				return jmapcore.Id(idStr), true
			}
			return "", false
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
					emailNotUpdated[string(emailID)] = jmapcore.SetError{
						Type:        "invalidProperties",
						Description: "onSuccessUpdateEmail patch must be an object",
					}
					continue
				}
				resolvedPatch := jmaphandler.ResolvePatchCreationRefs(p, creationRefs)
				if _, err := backend.UpdateEmail(ctx, emailID, resolvedPatch); err != nil {
					emailNotUpdated[string(emailID)] = jmapcore.SetError{
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
					emailNotDestroyed[string(emailID)] = jmapcore.SetError{
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
			jmaphandler.AppendSpillResponse(ctx, jmapcore.Invocation{
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

func HandleEmailSubmissionQuery(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		position, posErr := jmapcore.ParseQueryPosition(args)
		if posErr != "" {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, posErr)
		}

		anchor, anchorOffset, anchorErr := jmapcore.ParseQueryAnchor(args)
		if anchorErr != "" {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, anchorErr)
		}

		var limit *uint64
		if limVal, ok := args["limit"].(float64); ok {
			l := uint64(limVal)
			limit = &l
		}

		filter, _ := args["filter"].(map[string]any)
		comparators := jmapcore.ParseComparators(args)
		if errType, errMsg := jmapcore.ValidateComparators(comparators, submissionSortableProperties); errType != "" {
			return "error", jmapcore.MethodErrorArgs(errType, errMsg)
		}
		var ids []jmapcore.Id
		var total int
		if anchor != "" {
			allIDs, allTotal, _ := backend.QuerySubmissions(ctx, filter, comparators, 0, nil)
			total = allTotal
			var found bool
			position, ids, found = jmapcore.ApplyQueryAnchor(anchor, anchorOffset, allIDs, limit)
			if !found {
				return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorAnchorNotFound, "anchor not found in results: "+anchor)
			}
		} else {
			ids, total, _ = backend.QuerySubmissions(ctx, filter, comparators, position, limit)
			position = jmapcore.NormalizePosition(position, total)
		}
		if ids == nil {
			ids = []jmapcore.Id{}
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

func HandleEmailSubmissionQueryChanges(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		upToID, _ := args["upToId"].(string)
		sinceState, _ := args["sinceQueryState"].(string)
		filter, _ := args["filter"].(map[string]any)

		createdIDs, updatedIDs, destroyedIDs, newState, hasMore := backend.SubmissionChanges(ctx, sinceState, nil)
		if hasMore {
			return "error", jmapcore.MethodErrorArgs("cannotCalculateChanges", "sinceQueryState is too old")
		}

		comparators := jmapcore.ParseComparators(args)
		if errType, errMsg := jmapcore.ValidateComparators(comparators, submissionSortableProperties); errType != "" {
			return "error", jmapcore.MethodErrorArgs(errType, errMsg)
		}
		currentIDs, _, _ := backend.QuerySubmissions(ctx, filter, comparators, 0, nil)
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
		return "EmailSubmission/queryChanges", res
	}
}
