package jmap

import (
	"context"
	"encoding/json"
	"errors"
)

// handleMDNSend processes MDN/send method calls per RFC 9007 Section 3.1.
func handleMDNSend(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		sent := make(map[string]*MDN)
		notSent := make(map[string]any)

		if sendMap, ok := args["send"].(map[string]any); ok {
			for clientKey, rawMDN := range sendMap {
				mdnBytes, err := json.Marshal(rawMDN)
				if err != nil {
					notSent[clientKey] = map[string]any{
						"type":        "invalidProperties",
						"description": "Failed to parse MDN payload",
					}
					continue
				}

				var mdn MDN
				if err := json.Unmarshal(mdnBytes, &mdn); err != nil {
					notSent[clientKey] = map[string]any{
						"type":        "invalidProperties",
						"description": err.Error(),
					}
					continue
				}

				sentMDN, err := backend.SendMDN(ctx, &mdn)
				if err != nil {
					notSent[clientKey] = map[string]any{
						"type":        "notFound",
						"description": err.Error(),
					}
				} else {
					sent[clientKey] = sentMDN
				}
			}
		}

		return "MDN/send", map[string]any{
			"accountId": accountID,
			"sent":      sent,
			"notSent":   notSent,
		}
	}
}

// handleMDNParse processes MDN/parse method calls per RFC 9007 Section 3.2.
func handleMDNParse(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		blobIDsRaw, _ := args["blobIds"].([]any)

		parsed := make(map[string]*MDN)
		var notParsable []Id
		var notFound []Id

		for _, item := range blobIDsRaw {
			blobIDStr, ok := item.(string)
			if !ok {
				continue
			}

			blobID := Id(blobIDStr)
			mdn, err := backend.ParseMDN(ctx, blobID)
			if errors.Is(err, ErrBlobNotFound) {
				notFound = append(notFound, blobID)
			} else if err != nil || mdn == nil {
				notParsable = append(notParsable, blobID)
			} else {
				parsed[blobIDStr] = mdn
			}
		}

		return "MDN/parse", map[string]any{
			"accountId":   accountID,
			"parsed":      parsed,
			"notParsable": notParsable,
			"notFound":    notFound,
		}
	}
}
