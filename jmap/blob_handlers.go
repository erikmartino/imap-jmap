package jmap

import (
	"context"
	"encoding/base64"
)

// RegisterBlobHandlers registers RFC 9404 Blob methods into MethodRegistry.
func RegisterBlobHandlers(r *MethodRegistry, backend BlobBackend) {
	r.Register("Blob/get", handleBlobGet(backend))
	r.Register("Blob/upload", handleBlobUpload(backend))
	r.Register("Blob/lookup", handleBlobLookup(backend))
}

// handleBlobGet implements Blob/get per RFC 9404 Section 4.
func handleBlobGet(backend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, _ := args["ids"].([]any)

		var list []*Blob
		var notFound []Id

		for _, item := range idsRaw {
			if idStr, ok := item.(string); ok {
				blob, ok, _ := backend.GetBlob(ctx, accountID, idStr)
				if ok {
					list = append(list, blob)
				} else {
					notFound = append(notFound, Id(idStr))
				}
			}
		}

		if list == nil {
			list = []*Blob{}
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Blob/get", map[string]any{
			"accountId": accountID,
			"list":      list,
			"notFound":  notFound,
		}
	}
}

// handleBlobUpload implements Blob/upload per RFC 9404 Section 5.
func handleBlobUpload(backend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)

		created := make(map[string]*Blob)
		notCreated := make(map[string]any)

		for clientKey, raw := range createMap {
			if item, ok := raw.(map[string]any); ok {
				dataAsBase64, _ := item["data:asText"].(string)
				if dataAsBase64 == "" {
					dataAsBase64, _ = item["data"].(string)
				}
				contentType, _ := item["type"].(string)

				var data []byte
				var err error
				if dataAsBase64 != "" {
					data, err = base64.StdEncoding.DecodeString(dataAsBase64)
					if err != nil {
						// Try raw string bytes if not base64
						data = []byte(dataAsBase64)
					}
				}

				blob, err := backend.PutBlob(ctx, accountID, contentType, data)
				if err != nil {
					notCreated[clientKey] = map[string]any{
						"type":        "serverError",
						"description": err.Error(),
					}
				} else {
					created[clientKey] = blob
				}
			}
		}

		return "Blob/upload", map[string]any{
			"accountId":  accountID,
			"created":    created,
			"notCreated": notCreated,
		}
	}
}

// handleBlobLookup implements Blob/lookup per RFC 9404 Section 6.
func handleBlobLookup(backend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		blobIDsRaw, _ := args["blobIds"].([]any)

		list := make(map[string]any)
		for _, item := range blobIDsRaw {
			if blobIDStr, ok := item.(string); ok {
				blob, found, _ := backend.GetBlob(ctx, accountID, blobIDStr)
				if found {
					list[blobIDStr] = map[string]any{
						"id":           blob.ID,
						"size":         blob.Size,
						"digest:sha-256": blob.DigestSHA256,
						"type":         blob.Type,
					}
				}
			}
		}

		return "Blob/lookup", map[string]any{
			"accountId": accountID,
			"list":      list,
			"notFound":  []Id{},
		}
	}
}
