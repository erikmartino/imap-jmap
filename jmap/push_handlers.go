package jmap

import (
	"context"
	"encoding/json"
)

// handlePushSubscriptionGet processes PushSubscription/get per RFC 8620 Section 7.2.1.
func handlePushSubscriptionGet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		var list []*PushSubscription
		var notFound []Id

		if idsRaw, ok := args["ids"]; ok && idsRaw != nil {
			idsAny, _ := idsRaw.([]any)
			ids := make([]Id, 0, len(idsAny))
			for _, id := range idsAny {
				if s, ok := id.(string); ok {
					ids = append(ids, Id(s))
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
			notFound = []Id{}
		}

		return "PushSubscription/get", map[string]any{
			"accountId": accountID,
			"list":      list,
			"notFound":  notFound,
		}
	}
}

// handlePushSubscriptionSet processes PushSubscription/set per RFC 8620 Section 7.2.2.
func handlePushSubscriptionSet(backend MailBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		created := make(map[string]*PushSubscription)
		notCreated := make(map[string]any)
		updated := make(map[string]*PushSubscription)
		notUpdated := make(map[string]any)
		destroyed := []Id{}
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
					notCreated[clientKey] = map[string]any{"type": "invalidProperties", "description": "url is required"}
					continue
				}
				if sub.DeviceClientID == "" {
					notCreated[clientKey] = map[string]any{"type": "invalidProperties", "description": "deviceClientId is required"}
					continue
				}
				created_, err := backend.CreatePushSubscription(ctx, &sub)
				if err != nil {
					notCreated[clientKey] = map[string]any{"type": "serverFail", "description": err.Error()}
				} else {
					created[clientKey] = created_
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
				upd, err := backend.UpdatePushSubscription(ctx, Id(idStr), patch)
				if err != nil {
					notUpdated[idStr] = map[string]any{"type": "notFound", "description": err.Error()}
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
				ok, err := backend.DeletePushSubscription(ctx, Id(idStr))
				if err != nil || !ok {
					msg := "not found"
					if err != nil {
						msg = err.Error()
					}
					notDestroyed[idStr] = map[string]any{"type": "notFound", "description": msg}
				} else {
					destroyed = append(destroyed, Id(idStr))
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
