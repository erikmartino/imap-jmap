package jmap

import (
	"context"
	"strings"
	"unicode/utf8"
)

func handleEmailGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)
		props := parseProperties(args)

		// Validate any header properties in props
		var parsedHeaderProps []*ParsedHeaderProperty
		if props != nil {
			for _, p := range props {
				if strings.HasPrefix(p, "header:") {
					hp, err := ParseHeaderProperty(p)
					if err != nil {
						return "error", MethodErrorArgs(MethodErrorInvalidArguments, err.Error())
					}
					parsedHeaderProps = append(parsedHeaderProps, hp)
				}
			}
		}

		bodyProps := parsePropertiesBody(args)
		fetchTextBodyValues, _ := args["fetchTextBodyValues"].(bool)
		fetchHTMLBodyValues, _ := args["fetchHTMLBodyValues"].(bool)
		fetchAllBodyValues, _ := args["fetchAllBodyValues"].(bool)
		var maxBodyValueBytes uint64
		if rawMBV, present := args["maxBodyValueBytes"]; present {
			if mbv, ok := rawMBV.(float64); ok && mbv > 0 && mbv == float64(uint64(mbv)) {
				maxBodyValueBytes = uint64(mbv)
			} else {
				return "error", InvalidArgumentsErrorArgs([]string{"maxBodyValueBytes"}, "")
			}
		}

		var list []*Email
		var notFound []Id
		var err error

		if hasIDs {
			ids := make([]Id, 0, len(idsRaw))
			for _, item := range idsRaw {
				if idStr, ok := item.(string); ok {
					ids = append(ids, Id(idStr))
				}
			}
			list, notFound, err = backend.GetEmails(ctx, ids)
		} else {
			list, err = backend.GetAllEmails(ctx)
		}

		if err != nil || list == nil {
			list = []*Email{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		// Filter and format each email
		formattedList := make([]any, 0, len(list))
		for _, em := range list {
			formattedList = append(formattedList, formatEmailGet(em, props, parsedHeaderProps, bodyProps, fetchTextBodyValues, fetchHTMLBodyValues, fetchAllBodyValues, maxBodyValueBytes))
		}

		return "Email/get", map[string]any{
			"accountId": accountID,
			"state":     backend.EmailState(ctx),
			"list":      formattedList,
			"notFound":  notFound,
		}
	}
}

func parsePropertiesBody(args map[string]any) []string {
	rawVal, ok := args["bodyProperties"]
	if !ok || rawVal == nil {
		return nil
	}
	raw, ok := rawVal.([]any)
	if !ok {
		return nil
	}
	props := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			props = append(props, s)
		}
	}
	return props
}

func formatEmailGet(em *Email, props []string, parsedHeaderProps []*ParsedHeaderProperty, bodyProps []string, fetchText, fetchHTML, fetchAll bool, maxBytes uint64) any {
	effectiveTextBody := em.TextBody
	effectiveHTMLBody := em.HTMLBody
	if len(effectiveHTMLBody) == 0 && len(effectiveTextBody) > 0 {
		effectiveHTMLBody = effectiveTextBody
	} else if len(effectiveTextBody) == 0 && len(effectiveHTMLBody) > 0 {
		effectiveTextBody = effectiveHTMLBody
	}

	bodyValues := make(map[string]EmailBodyValue)
	if fetchAll {
		for k, v := range em.BodyValues {
			bodyValues[k] = applyMaxBodyValueBytes(v, maxBytes)
		}
	} else {
		if fetchText {
			for _, part := range effectiveTextBody {
				if part.PartID != nil {
					if bv, ok := em.BodyValues[*part.PartID]; ok {
						bodyValues[*part.PartID] = applyMaxBodyValueBytes(bv, maxBytes)
					}
				}
			}
		}
		if fetchHTML {
			for _, part := range effectiveHTMLBody {
				if part.PartID != nil {
					if bv, ok := em.BodyValues[*part.PartID]; ok {
						bodyValues[*part.PartID] = applyMaxBodyValueBytes(bv, maxBytes)
					}
				}
			}
		}
	}

	filteredBodyStructure := FilterEmailBodyPart(em.BodyStructure, bodyProps)
	filteredTextBody := make([]any, 0, len(effectiveTextBody))
	for _, p := range effectiveTextBody {
		filteredTextBody = append(filteredTextBody, FilterEmailBodyPart(p, bodyProps))
	}
	filteredHTMLBody := make([]any, 0, len(effectiveHTMLBody))
	for _, p := range effectiveHTMLBody {
		filteredHTMLBody = append(filteredHTMLBody, FilterEmailBodyPart(p, bodyProps))
	}
	filteredAttachments := make([]any, 0, len(em.Attachments))
	for _, p := range em.Attachments {
		filteredAttachments = append(filteredAttachments, FilterEmailBodyPart(p, bodyProps))
	}

	if props == nil {
		out := map[string]any{
			"id":            em.ID,
			"blobId":        em.BlobID,
			"threadId":      em.ThreadID,
			"mailboxIds":    em.MailboxIDs,
			"keywords":      em.Keywords,
			"size":          em.Size,
			"receivedAt":    em.ReceivedAt,
			"messageId":     em.MessageID,
			"inReplyTo":     em.InReplyTo,
			"references":    em.References,
			"sender":        em.Sender,
			"from":          em.From,
			"to":            em.To,
			"cc":            em.CC,
			"bcc":           em.BCC,
			"replyTo":       em.ReplyTo,
			"subject":       em.Subject,
			"sentAt":        em.SentAt,
			"textBody":      filteredTextBody,
			"htmlBody":      filteredHTMLBody,
			"attachments":   filteredAttachments,
			"hasAttachment": em.HasAttachment,
			"preview":       em.Preview,
		}
		out["bodyValues"] = bodyValues
		if em.SMIMEStatus != nil {
			out["smimeStatus"] = *em.SMIMEStatus
		}
		if em.SMIMEStatusAt != nil {
			out["smimeStatusAt"] = *em.SMIMEStatusAt
		}
		if em.SMIMEVerifiedWith != nil {
			out["smimeVerifiedWith"] = *em.SMIMEVerifiedWith
		}
		if len(em.SMIMEErrors) > 0 {
			out["smimeErrors"] = em.SMIMEErrors
		}
		return out
	}

	out := make(map[string]any, len(props)+1)
	out["id"] = em.ID

	hpMap := make(map[string]*ParsedHeaderProperty, len(parsedHeaderProps))
	for _, hp := range parsedHeaderProps {
		hpMap[hp.RawProp] = hp
	}

	for _, p := range props {
		if hp, ok := hpMap[p]; ok {
			out[p] = EvaluateHeaderProperty(em, hp)
			continue
		}
		switch p {
		case "id":
			out["id"] = em.ID
		case "blobId":
			out["blobId"] = em.BlobID
		case "threadId":
			out["threadId"] = em.ThreadID
		case "mailboxIds":
			out["mailboxIds"] = em.MailboxIDs
		case "keywords":
			out["keywords"] = em.Keywords
		case "size":
			out["size"] = em.Size
		case "receivedAt":
			out["receivedAt"] = em.ReceivedAt
		case "messageId":
			out["messageId"] = em.MessageID
		case "inReplyTo":
			out["inReplyTo"] = em.InReplyTo
		case "references":
			out["references"] = em.References
		case "sender":
			out["sender"] = em.Sender
		case "from":
			out["from"] = em.From
		case "to":
			out["to"] = em.To
		case "cc":
			out["cc"] = em.CC
		case "bcc":
			out["bcc"] = em.BCC
		case "replyTo":
			out["replyTo"] = em.ReplyTo
		case "subject":
			out["subject"] = em.Subject
		case "sentAt":
			out["sentAt"] = em.SentAt
		case "headers":
			out["headers"] = em.Headers
		case "bodyStructure":
			out["bodyStructure"] = filteredBodyStructure
		case "bodyValues":
			out["bodyValues"] = bodyValues
		case "textBody":
			out["textBody"] = filteredTextBody
		case "htmlBody":
			out["htmlBody"] = filteredHTMLBody
		case "attachments":
			out["attachments"] = filteredAttachments
		case "hasAttachment":
			out["hasAttachment"] = em.HasAttachment
		case "preview":
			out["preview"] = em.Preview
		case "smimeStatus":
			if em.SMIMEStatus != nil {
				out["smimeStatus"] = *em.SMIMEStatus
			} else {
				out["smimeStatus"] = nil
			}
		case "smimeStatusAt":
			if em.SMIMEStatusAt != nil {
				out["smimeStatusAt"] = *em.SMIMEStatusAt
			} else {
				out["smimeStatusAt"] = nil
			}
		case "smimeVerifiedWith":
			if em.SMIMEVerifiedWith != nil {
				out["smimeVerifiedWith"] = *em.SMIMEVerifiedWith
			} else {
				out["smimeVerifiedWith"] = nil
			}
		case "smimeErrors":
			if em.SMIMEErrors != nil {
				out["smimeErrors"] = em.SMIMEErrors
			} else {
				out["smimeErrors"] = []string{}
			}
		}
	}

	return out
}

func applyMaxBodyValueBytes(bv EmailBodyValue, maxBytes uint64) EmailBodyValue {
	if maxBytes == 0 || uint64(len(bv.Value)) <= maxBytes {
		return bv
	}
	b := []byte(bv.Value)[:maxBytes]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return EmailBodyValue{
		Value:             string(b),
		IsTruncated:       true,
		IsEncodingProblem: bv.IsEncodingProblem,
	}
}

func handleEmailChanges(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		sinceState, _ := args["sinceState"].(string)

		var maxChanges *uint64
		if mc, ok := args["maxChanges"].(float64); ok {
			if mc < 0 {
				return "error", MethodErrorArgs(MethodErrorInvalidArguments, "maxChanges must be non-negative")
			}
			m := uint64(mc)
			maxChanges = &m
		}

		created, updated, destroyed, newState, hasMore := backend.EmailChanges(ctx, sinceState, maxChanges)
		if created == nil {
			created = []Id{}
		}
		if updated == nil {
			updated = []Id{}
		}
		if destroyed == nil {
			destroyed = []Id{}
		}

		return "Email/changes", map[string]any{
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
