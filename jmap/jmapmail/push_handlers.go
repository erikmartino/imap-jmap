package jmapmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmaphandler"
)

// RegisterPushSubscriptionHandlers registers RFC 8620 Section 7.2 PushSubscription handlers into MethodRegistry.
func RegisterPushSubscriptionHandlers(r *jmaphandler.MethodRegistry, backend MailBackend) {
	r.Register("PushSubscription/get", HandlePushSubscriptionGet(backend))
	r.Register("PushSubscription/set", HandlePushSubscriptionSet(backend))
}

// HandlePushSubscriptionGet processes PushSubscription/get per RFC 8620 Section 7.2.1.
func HandlePushSubscriptionGet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		// Reject if url or keys explicitly requested (RFC 8620 Section 7.2.1)
		var reqProps []string
		if pList, ok := args["properties"].([]any); ok {
			for _, p := range pList {
				if ps, ok := p.(string); ok {
					if ps == "url" || ps == "keys" {
						return "error", jmapcore.MethodErrorArgs("forbidden", fmt.Sprintf("property %q cannot be requested on PushSubscription/get", ps))
					}
					reqProps = append(reqProps, ps)
				}
			}
		}

		var list []*PushSubscription
		var notFound []jmapcore.Id

		if idsRaw, ok := args["ids"]; ok && idsRaw != nil {
			idsAny, _ := idsRaw.([]any)
			ids := make([]jmapcore.Id, 0, len(idsAny))
			for _, id := range idsAny {
				if s, ok := id.(string); ok {
					ids = append(ids, jmapcore.Id(s))
				}
			}
			var err error
			list, notFound, err = backend.GetPushSubscriptions(ctx, ids)
			if err != nil {
				list = []*PushSubscription{}
			}
		} else {
			var err error
			list, err = backend.GetAllPushSubscriptions(ctx)
			if err != nil {
				list = []*PushSubscription{}
			}
		}

		if list == nil {
			list = []*PushSubscription{}
		}
		if notFound == nil {
			notFound = []jmapcore.Id{}
		}

		// Sanitize returned objects: url and keys MUST NOT be returned per RFC 8620 §7.2.1
		sanitizedList := make([]map[string]any, 0, len(list))
		for _, sub := range list {
			if sub == nil {
				continue
			}
			item := map[string]any{
				"id":             string(sub.ID),
				"deviceClientId": sub.DeviceClientID,
			}
			if sub.Expires != nil {
				item["expires"] = *sub.Expires
			}
			if sub.Types != nil {
				item["types"] = sub.Types
			}
			if sub.VerificationCode != nil {
				item["verificationCode"] = *sub.VerificationCode
			}

			if len(reqProps) > 0 {
				filtered := map[string]any{"id": item["id"]}
				for _, p := range reqProps {
					if val, ok := item[p]; ok {
						filtered[p] = val
					}
				}
				sanitizedList = append(sanitizedList, filtered)
			} else {
				sanitizedList = append(sanitizedList, item)
			}
		}

		res := map[string]any{
			"list":     sanitizedList,
			"notFound": notFound,
		}
		if accountID != "" {
			res["accountId"] = accountID
		}
		return "PushSubscription/get", res
	}
}

// HandlePushSubscriptionSet processes PushSubscription/set per RFC 8620 Section 7.2.2.
func HandlePushSubscriptionSet(backend MailBackend) jmaphandler.MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		if ifInState, ok := args["ifInState"].(string); ok && ifInState != "" {
			oldState := backend.State(ctx)
			if ifInState != oldState {
				return "error", jmapcore.MethodErrorArgs("stateMismatch", fmt.Sprintf("state token %q does not match current state %q", ifInState, oldState))
			}
		}

		created := make(map[string]*PushSubscription)
		notCreated := make(map[string]any)
		updated := make(map[string]*PushSubscription)
		notUpdated := make(map[string]any)
		destroyed := []jmapcore.Id{}
		notDestroyed := make(map[string]any)

		// Process create
		if createMap, ok := args["create"].(map[string]any); ok {
			for clientKey, rawSub := range createMap {
				subBytes, err := json.Marshal(rawSub)
				if err != nil {
					notCreated[clientKey] = map[string]any{"type": "invalidProperties", "description": err.Error()}
					continue
				}
				var sub PushSubscription
				if err := json.Unmarshal(subBytes, &sub); err != nil {
					notCreated[clientKey] = map[string]any{"type": "invalidProperties", "description": err.Error()}
					continue
				}
				if sub.URL == "" {
					notCreated[clientKey] = map[string]any{"type": "invalidProperties", "description": "url is required", "properties": []string{"url"}}
					continue
				}
				urlLower := strings.ToLower(sub.URL)
				isLocal := strings.HasPrefix(urlLower, "http://localhost") || strings.HasPrefix(urlLower, "http://127.0.0.1") || strings.HasPrefix(urlLower, "http://[::1]")
				if !strings.HasPrefix(urlLower, "https://") && !isLocal {
					notCreated[clientKey] = map[string]any{"type": "invalidProperties", "description": "url must be https", "properties": []string{"url"}}
					continue
				}
				if sub.DeviceClientID == "" {
					notCreated[clientKey] = map[string]any{"type": "invalidProperties", "description": "deviceClientId is required", "properties": []string{"deviceClientId"}}
					continue
				}
				created_, err := backend.CreatePushSubscription(ctx, &sub)
				if err != nil {
					notCreated[clientKey] = map[string]any{"type": "serverFail", "description": err.Error()}
				} else {
					created[clientKey] = created_

					// Asynchronously post PushVerification payload to client URL per RFC 8620 §7.2.2
					if created_.URL != "" && created_.VerificationCode != nil {
						go func(targetURL, pushSubID, verifyCode string) {
							verificationPayload, _ := json.Marshal(map[string]any{
								"@type":              "PushVerification",
								"pushSubscriptionId": pushSubID,
								"verificationCode":   verifyCode,
							})
							req, err := http.NewRequest("POST", targetURL, bytes.NewReader(verificationPayload))
							if err == nil {
								req.Header.Set("Content-Type", "application/json")
								client := &http.Client{Timeout: 5 * time.Second}
								resp, err := client.Do(req)
								if err == nil {
									resp.Body.Close()
								}
							}
						}(created_.URL, string(created_.ID), *created_.VerificationCode)
					}
				}
			}
		}

		// Process update
		if updateMap, ok := args["update"].(map[string]any); ok {
			for idStr, rawPatch := range updateMap {
				patch, ok := rawPatch.(map[string]any)
				if !ok {
					notUpdated[idStr] = map[string]any{"type": "invalidProperties", "description": "patch must be an object"}
					continue
				}
				if _, hasURL := patch["url"]; hasURL {
					notUpdated[idStr] = map[string]any{"type": "invalidProperties", "description": "url is immutable", "properties": []string{"url"}}
					continue
				}
				if _, hasKeys := patch["keys"]; hasKeys {
					notUpdated[idStr] = map[string]any{"type": "invalidProperties", "description": "keys is immutable", "properties": []string{"keys"}}
					continue
				}
				upd, err := backend.UpdatePushSubscription(ctx, jmapcore.Id(idStr), patch)
				if err != nil {
					errType := "invalidProperties"
					if errors.Is(err, jmapcore.ErrNotFound) {
						errType = "notFound"
					}
					notUpdated[idStr] = map[string]any{"type": errType, "description": err.Error()}
				} else {
					updated[idStr] = upd
				}
			}
		}

		// Process destroy
		if destroyArr, ok := args["destroy"].([]any); ok {
			for _, idRaw := range destroyArr {
				idStr, ok := idRaw.(string)
				if !ok {
					continue
				}
				ok, err := backend.DeletePushSubscription(ctx, jmapcore.Id(idStr))
				if err != nil || !ok {
					msg := "not found"
					if err != nil {
						msg = err.Error()
					}
					notDestroyed[idStr] = map[string]any{"type": "notFound", "description": msg}
				} else {
					destroyed = append(destroyed, jmapcore.Id(idStr))
				}
			}
		}

		return "PushSubscription/set", map[string]any{
			"accountId":    accountID,
			"created":      created,
			"notCreated":   notCreated,
			"updated":      updated,
			"notUpdated":   notUpdated,
			"destroyed":    destroyed,
			"notDestroyed": notDestroyed,
		}
	}
}
