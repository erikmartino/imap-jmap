package jmap

import (
	"context"
	"encoding/base64"
	"fmt"
)

// RegisterBlobHandlers registers RFC 9404 Blob methods into MethodRegistry.
func RegisterBlobHandlers(r *MethodRegistry, backend BlobBackend, refs BlobReferenceBackend) {
	r.Register("Blob/get", handleBlobGet(backend))
	r.Register("Blob/upload", handleBlobUpload(backend))
	r.Register("Blob/lookup", handleBlobLookup(backend, refs))
	r.Register("Blob/copy", handleBlobCopy(backend))
}

// handleBlobGet implements Blob/get per RFC 9404 Section 4.
func handleBlobGet(backend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		idsRaw, hasIDs := args["ids"].([]any)

		var list []*Blob
		var notFound []Id

		if hasIDs {
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
		} else {
			list, _ = backend.GetAllBlobs(ctx, accountID)
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

// handleBlobLookup implements Blob/lookup per RFC 9404 Section 4.3: a reverse lookup of which
// objects of the requested data types reference each blob id.
func handleBlobLookup(backend BlobBackend, refs BlobReferenceBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		typeNamesRaw, _ := args["typeNames"].([]any)
		idsRaw, _ := args["ids"].([]any)

		// RFC 9404 Section 4.3: only data types for which "Can reference blobs" is true may
		// be specified; anything else is an unknownDataType error.
		validTypes := map[string]bool{"Mailbox": true, "Thread": true, "Email": true}
		var typeNames []string
		for _, raw := range typeNamesRaw {
			tn, _ := raw.(string)
			if !validTypes[tn] {
				return "error", MethodErrorArgs(MethodErrorUnknownDataType,
					fmt.Sprintf("type %q cannot reference blobs or is not known", tn))
			}
			typeNames = append(typeNames, tn)
		}

		list := make([]map[string]any, 0, len(idsRaw))
		var notFound []Id
		for _, raw := range idsRaw {
			blobID, ok := raw.(string)
			if !ok {
				continue
			}
			if _, found, _ := backend.GetBlob(ctx, accountID, blobID); !found {
				notFound = append(notFound, Id(blobID))
				continue
			}
			matched := make(map[string][]Id, len(typeNames))
			for _, tn := range typeNames {
				matched[tn] = []Id{}
			}
			if refs != nil {
				refMatched, _ := refs.LookupBlobReferences(ctx, typeNames, Id(blobID))
				for tn, ids := range refMatched {
					matched[tn] = ids
				}
			}
			list = append(list, map[string]any{
				"id":         blobID,
				"matchedIds": matched,
			})
		}
		if notFound == nil {
			notFound = []Id{}
		}

		return "Blob/lookup", map[string]any{
			"accountId": accountID,
			"list":      list,
			"notFound":  notFound,
		}
	}
}

// handleBlobCopy implements Blob/copy per RFC 9404 Section 4.
func handleBlobCopy(backend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		fromAccountID, _ := args["fromAccountId"].(string)
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)

		copied := make(map[string]*Blob)
		notCopied := make(map[string]any)

		for clientKey, raw := range createMap {
			if item, ok := raw.(map[string]any); ok {
				blobID, _ := item["blobId"].(string)
				copiedBlob, err := backend.CopyBlob(ctx, fromAccountID, accountID, blobID)
				if err != nil {
					notCopied[clientKey] = map[string]any{
						"type":        "notFound",
						"description": "blob not found",
					}
				} else {
					copied[clientKey] = copiedBlob
				}
			}
		}

		return "Blob/copy", map[string]any{
			"fromAccountId": fromAccountID,
			"accountId":     accountID,
			"copied":        copied,
			"notCopied":     notCopied,
		}
	}
}
