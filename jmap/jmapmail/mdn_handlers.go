package jmapmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// RegisterMDNHandlers registers RFC 9007 MDN method handlers into MethodRegistry.
func RegisterMDNHandlers(r *jmaphandler.MethodRegistry, backend MailBackend) {
	r.Register("MDN/send", HandleMDNSend(backend))
	r.Register("MDN/parse", HandleMDNParse(backend))
}

// HandleMDNSend processes MDN/send method calls per RFC 9007 Section 2.1.
func HandleMDNSend(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		if accountID == "" {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, "accountId is required")
		}

		identityID, _ := args["identityId"].(string)
		if identityID == "" {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, "identityId is required")
		}

		identities, err := backend.GetIdentities(ctx)
		if err != nil {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, err.Error())
		}
		var foundIdentity *Identity
		for _, ident := range identities {
			if string(ident.ID) == identityID {
				foundIdentity = ident
				break
			}
		}
		if foundIdentity == nil {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, "identityId not found")
		}

		sendMap, _ := args["send"].(map[string]any)
		onSuccessUpdateEmail, _ := args["onSuccessUpdateEmail"].(map[string]any)
		creationRefs := jmaphandler.NewSetCreationRefs(ctx)

		sent := make(map[string]*MDN)
		notSent := make(map[string]any)
		emailUpdated := make(map[string]any)
		oldEmailState := backend.EmailState(ctx)

		for clientKey, rawMDN := range sendMap {
			mdnBytes, err := json.Marshal(rawMDN)
			if err != nil {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "invalidProperties",
					Description: "Failed to parse MDN payload",
				}
				continue
			}

			var mdn MDN
			if err := json.Unmarshal(mdnBytes, &mdn); err != nil {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "invalidProperties",
					Description: err.Error(),
				}
				continue
			}

			// RFC 9007 §2: forEmailId MUST NOT be null for MDN/send
			if mdn.ForEmailID == "" {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "invalidProperties",
					Description: "forEmailId MUST NOT be null for MDN/send",
				}
				continue
			}

			// RFC 9007 §2: validate disposition fields
			actionMode := strings.ToLower(strings.TrimSpace(mdn.Disposition.ActionMode))
			sendingMode := strings.ToLower(strings.TrimSpace(mdn.Disposition.SendingMode))
			dispType := strings.ToLower(strings.TrimSpace(mdn.Disposition.Type))

			if actionMode != "manual-action" && actionMode != "automatic-action" {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "invalidProperties",
					Description: "disposition.actionMode MUST be manual-action or automatic-action",
				}
				continue
			}
			if sendingMode != "mdn-sent-manually" && sendingMode != "mdn-sent-automatically" {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "invalidProperties",
					Description: "disposition.sendingMode MUST be mdn-sent-manually or mdn-sent-automatically",
				}
				continue
			}
			validTypes := map[string]bool{
				"deleted":    true,
				"dispatched": true,
				"displayed":  true,
				"processed":   true,
			}
			if !validTypes[dispType] {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "invalidProperties",
					Description: "disposition.type MUST be deleted, dispatched, displayed, or processed",
				}
				continue
			}
			mdn.Disposition.ActionMode = actionMode
			mdn.Disposition.SendingMode = sendingMode
			mdn.Disposition.Type = dispType

			// RFC 9007 §2.1: The server MUST reject an MDN/send that does not result in setting
			// the keyword "$mdnsent". Thus, the server MUST check that the "onSuccessUpdateEmail"
			// property of the method is correctly set to update this keyword.
			var patchMap map[string]any
			if onSuccessUpdateEmail != nil {
				if p, ok := onSuccessUpdateEmail["#"+clientKey].(map[string]any); ok {
					patchMap = p
				} else if p, ok := onSuccessUpdateEmail[clientKey].(map[string]any); ok {
					patchMap = p
				} else if p, ok := onSuccessUpdateEmail[string(mdn.ForEmailID)].(map[string]any); ok {
					patchMap = p
				}
			}

			hasMDNSentKeyword := false
			if patchMap != nil {
				// RFC 9007 §1.2: the "$mdnsent" keyword MUST always be used in lowercase
				if val, ok := patchMap["keywords/$mdnsent"]; ok {
					if b, isBool := val.(bool); isBool && b {
						hasMDNSentKeyword = true
					}
				}
				if kwMap, ok := patchMap["keywords"].(map[string]any); ok {
					if b, isBool := kwMap["$mdnsent"].(bool); isBool && b {
						hasMDNSentKeyword = true
					}
				}
			}

			if !hasMDNSentKeyword {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "invalidProperties",
					Description: "onSuccessUpdateEmail MUST set keyword $mdnsent",
				}
				continue
			}

			// Fetch target email
			emails, notFoundList, err := backend.GetEmails(ctx, []jmapcore.Id{mdn.ForEmailID})
			if err != nil || len(notFoundList) > 0 || len(emails) == 0 {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "notFound",
					Description: "referenced email not found",
				}
				continue
			}
			targetEmail := emails[0]

			// RFC 9007 §2.1: The client MUST NOT issue an MDN/send request if the message has
			// the "$mdnsent" keyword set.
			if targetEmail.Keywords != nil && targetEmail.Keywords["$mdnsent"] {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "mdnAlreadySent",
					Description: "$mdnsent keyword is already present",
				}
				continue
			}

			// RFC 9007 §2.1: notFound: The reference "forEmailId" cannot be found or has no valid
			// "Disposition-Notification-To" header field.
			var dispToHdr string
			for _, h := range targetEmail.Headers {
				if strings.EqualFold(h.Name, "Disposition-Notification-To") {
					dispToHdr = strings.TrimSpace(h.Value)
					break
				}
			}
			if dispToHdr == "" {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "notFound",
					Description: "target email has no Disposition-Notification-To header field",
				}
				continue
			}
			// Validate Disposition-Notification-To address with net/mail
			if _, addrErr := mail.ParseAddress(dispToHdr); addrErr != nil {
				if addrs, listErr := mail.ParseAddressList(dispToHdr); listErr != nil || len(addrs) == 0 {
					notSent[clientKey] = jmapcore.SetError{
						Type:        "notFound",
						Description: "invalid Disposition-Notification-To header address",
					}
					continue
				}
			}

			// RFC 9007 §5: Validate in conformance to the provided Identity that the user is permitted
			// to use the "finalRecipient" value and return a "forbiddenFrom" error if not.
			if mdn.FinalRecipient != "" {
				cleanFinal := strings.TrimSpace(mdn.FinalRecipient)
				if strings.HasPrefix(strings.ToLower(cleanFinal), "rfc822;") {
					cleanFinal = strings.TrimSpace(cleanFinal[7:])
				}
				parsedFinal, err := mail.ParseAddress(cleanFinal)
				finalEmail := cleanFinal
				if err == nil && parsedFinal != nil {
					finalEmail = parsedFinal.Address
				}
				if !strings.EqualFold(finalEmail, foundIdentity.Email) {
					notSent[clientKey] = jmapcore.SetError{
						Type:        "forbiddenFrom",
						Description: "user is not permitted to use this finalRecipient",
					}
					continue
				}
			} else {
				mdn.FinalRecipient = "rfc822; " + foundIdentity.Email
			}

			// Server-set defaults per RFC 9007 §2.1 & RFC 8098
			if mdn.OriginalMessageID == "" {
				if len(targetEmail.MessageID) > 0 {
					mdn.OriginalMessageID = targetEmail.MessageID[0]
				} else {
					for _, h := range targetEmail.Headers {
						if strings.EqualFold(h.Name, "Message-ID") {
							mdn.OriginalMessageID = h.Value
							break
						}
					}
				}
			}
			if mdn.OriginalRecipient == "" {
				for _, h := range targetEmail.Headers {
					if strings.EqualFold(h.Name, "Original-Recipient") {
						mdn.OriginalRecipient = h.Value
						break
					}
				}
				if mdn.OriginalRecipient == "" && len(targetEmail.To) > 0 {
					mdn.OriginalRecipient = "rfc822; " + targetEmail.To[0].Email
				}
			}
			if mdn.Subject == "" {
				mdn.Subject = "Disposition Notification: " + targetEmail.Subject
			}
			if mdn.ReportingUA == "" {
				mdn.ReportingUA = "imap-jmap-server/1.0"
			}

			sentMDN, err := backend.SendMDN(ctx, &mdn)
			if err != nil {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "notFound",
					Description: err.Error(),
				}
				continue
			}

			// Apply onSuccessUpdateEmail patch to target email
			resolvedPatch := jmaphandler.ResolvePatchCreationRefs(patchMap, creationRefs)
			if _, err := backend.UpdateEmail(ctx, targetEmail.ID, resolvedPatch); err != nil {
				notSent[clientKey] = jmapcore.SetError{
					Type:        "invalidProperties",
					Description: "failed to update email keywords: " + err.Error(),
				}
				continue
			}
			emailUpdated[string(targetEmail.ID)] = nil
			sent[clientKey] = sentMDN
		}

		if len(emailUpdated) > 0 {
			jmaphandler.AppendSpillResponse(ctx, jmapcore.Invocation{
				Name: "Email/set",
				Args: map[string]any{
					"accountId": accountID,
					"oldState":  oldEmailState,
					"newState":  backend.EmailState(ctx),
					"updated":   emailUpdated,
				},
				ClientCallID: clientCallID,
			})
		}

		var sentResult any = sent
		if len(sent) == 0 {
			sentResult = nil
		}
		var notSentResult any = notSent
		if len(notSent) == 0 {
			notSentResult = nil
		}

		return "MDN/send", map[string]any{
			"accountId": accountID,
			"sent":      sentResult,
			"notSent":   notSentResult,
		}
	}
}

// HandleMDNParse processes MDN/parse method calls per RFC 9007 Section 2.2.
func HandleMDNParse(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		if accountID == "" {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorInvalidArguments, "accountId is required")
		}

		blobIDsRaw, _ := args["blobIds"].([]any)
		limits, ok := jmaphandler.CoreLimitsFromContext(ctx)
		if ok && limits.MaxObjectsInGet > 0 && uint64(len(blobIDsRaw)) > limits.MaxObjectsInGet {
			return "error", jmapcore.MethodErrorArgs(jmapcore.MethodErrorRequestTooLarge, fmt.Sprintf("Number of requested blobIds (%d) exceeds maxObjectsInGet (%d)", len(blobIDsRaw), limits.MaxObjectsInGet))
		}

		parsed := make(map[string]*MDN)
		var notParsable []jmapcore.Id
		var notFound []jmapcore.Id

		for _, item := range blobIDsRaw {
			blobIDStr, ok := item.(string)
			if !ok {
				continue
			}

			blobID := jmapcore.Id(blobIDStr)
			mdn, err := backend.ParseMDN(ctx, blobID)
			if errors.Is(err, jmapblob.ErrBlobNotFound) {
				notFound = append(notFound, blobID)
			} else if err != nil || mdn == nil {
				notParsable = append(notParsable, blobID)
			} else {
				parsed[blobIDStr] = mdn
			}
		}

		var parsedResult any = parsed
		if len(parsed) == 0 {
			parsedResult = nil
		}
		var notParsableResult any = notParsable
		if len(notParsable) == 0 {
			notParsableResult = nil
		}
		var notFoundResult any = notFound
		if len(notFound) == 0 {
			notFoundResult = nil
		}

		return "MDN/parse", map[string]any{
			"accountId":   accountID,
			"parsed":      parsedResult,
			"notParsable": notParsableResult,
			"notFound":    notFoundResult,
		}
	}
}
