package jmap

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"
)

// RegisterBlobHandlers registers RFC 9404 Blob methods into MethodRegistry.
func RegisterBlobHandlers(r *MethodRegistry, backend BlobBackend, refs BlobReferenceBackend) {
	r.Register("Blob/get", handleBlobGet(backend))
	r.Register("Blob/upload", handleBlobUpload(backend))
	r.Register("Blob/lookup", handleBlobLookup(backend, refs))
	r.Register("Blob/copy", handleBlobCopy(backend))
}

// handleBlobGet implements Blob/get per RFC 9404 Section 4.2.
func handleBlobGet(backend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		props := parseProperties(args)

		var offset int
		if offVal, ok := args["offset"].(float64); ok {
			offset = int(offVal)
		}
		var length *int
		if lenVal, ok := args["length"].(float64); ok {
			l := int(lenVal)
			length = &l
		}

		list := make([]map[string]any, 0)
		var blobs []*Blob
		var notFound []Id

		if idsRaw, ok := args["ids"].([]any); ok && idsRaw != nil {
			for _, item := range idsRaw {
				idStr, ok := item.(string)
				if !ok {
					continue
				}
				blob, found, err := backend.GetBlob(ctx, accountID, idStr)
				if err != nil || !found || blob == nil {
					notFound = append(notFound, Id(idStr))
					continue
				}
				blobs = append(blobs, blob)
			}
		} else {
			blobs, _ = backend.GetAllBlobs(ctx, accountID)
		}

		for _, blob := range blobs {
			idStr := blob.ID

			data := blob.Data
			totalSize := len(data)
			start := offset
			if start > totalSize {
				start = totalSize
			}
			end := totalSize
			isTruncated := false
			if length != nil {
				if start+*length < end {
					end = start + *length
					isTruncated = true
				}
			}

			rangeBytes := data[start:end]
			isEncodingProblem := false

			res := map[string]any{
				"id": idStr,
			}

			hash := sha256.Sum256(rangeBytes)
			digestBase64 := base64.StdEncoding.EncodeToString(hash[:])

			wantProp := func(p string) bool {
				if len(props) == 0 {
					return true
				}
				for _, name := range props {
					if name == p {
						return true
					}
				}
				return false
			}

			if wantProp("size") {
				res["size"] = totalSize
			}
			if wantProp("isTruncated") {
				res["isTruncated"] = isTruncated
			}
			if wantProp("data:asBase64") {
				res["data:asBase64"] = base64.StdEncoding.EncodeToString(rangeBytes)
			}
			if wantProp("data:asText") || wantProp("data") {
				if utf8.Valid(rangeBytes) {
					res["data:asText"] = string(rangeBytes)
					if wantProp("data") {
						res["data"] = string(rangeBytes)
					}
				} else {
					res["data:asText"] = nil
					isEncodingProblem = true
				}
			}
			if wantProp("isEncodingProblem") || isEncodingProblem {
				res["isEncodingProblem"] = isEncodingProblem
			}
			if wantProp("digest:sha-256") {
				res["digest:sha-256"] = digestBase64
			}
			for _, p := range props {
				if strings.HasPrefix(p, "digest:") {
					res[p] = digestBase64
				}
			}

			list = append(list, res)
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

// handleBlobUpload implements Blob/upload per RFC 9404 Section 4.1.
func handleBlobUpload(backend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		createMap, _ := args["create"].(map[string]any)
		creationRefs := newSetCreationRefs(ctx)

		created := make(map[string]*Blob)
		notCreated := make(map[string]any)

		for clientKey, raw := range createMap {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}

			contentType, _ := item["type"].(string)
			var totalData []byte
			var uploadErr *SetError

			// Parse data sources array (RFC 9404 Section 4.1 DataSourceObject[])
			var sources []map[string]any
			if rawSources, ok := item["data"].([]any); ok {
				for _, srcRaw := range rawSources {
					if srcMap, ok := srcRaw.(map[string]any); ok {
						sources = append(sources, srcMap)
					}
				}
			} else if srcMap, ok := item["data"].(map[string]any); ok {
				sources = append(sources, srcMap)
			} else if txt, ok := item["data:asText"].(string); ok {
				sources = append(sources, map[string]any{"data:asText": txt})
			} else if txt, ok := item["data"].(string); ok {
				if decoded, err := base64.StdEncoding.DecodeString(txt); err == nil && len(decoded) > 0 {
					sources = append(sources, map[string]any{"data:asBase64": txt})
				} else {
					sources = append(sources, map[string]any{"data:asText": txt})
				}
			}

			for _, src := range sources {
				if txt, ok := src["data:asText"].(string); ok {
					if !utf8.ValidString(txt) {
						uploadErr = &SetError{Type: "invalidProperties", Description: "invalid UTF-8 in data:asText"}
						break
					}
					totalData = append(totalData, []byte(txt)...)
				} else if b64, ok := src["data:asBase64"].(string); ok {
					decoded, err := base64.StdEncoding.DecodeString(b64)
					if err != nil {
						uploadErr = &SetError{Type: "invalidProperties", Description: "invalid base64 in data:asBase64"}
						break
					}
					totalData = append(totalData, decoded...)
				} else if refBlobID, ok := src["blobId"].(string); ok {
					refBlob, found, err := backend.GetBlob(ctx, accountID, refBlobID)
					if err != nil || !found || refBlob == nil {
						uploadErr = &SetError{Type: "notFound", Description: "blobId not found: " + refBlobID}
						break
					}
					refData := refBlob.Data
					offset := 0
					if offVal, ok := src["offset"].(float64); ok {
						offset = int(offVal)
					}
					if offset > len(refData) {
						uploadErr = &SetError{Type: "invalidProperties", Description: "offset out of bounds"}
						break
					}
					length := len(refData) - offset
					if lenVal, ok := src["length"].(float64); ok {
						length = int(lenVal)
					}
					if offset+length > len(refData) {
						uploadErr = &SetError{Type: "invalidProperties", Description: "offset + length out of bounds"}
						break
					}
					totalData = append(totalData, refData[offset:offset+length]...)
				} else {
					uploadErr = &SetError{Type: "invalidProperties", Description: "invalid data source object"}
					break
				}
			}

			if uploadErr != nil {
				notCreated[clientKey] = *uploadErr
				continue
			}

			blob, err := backend.PutBlob(ctx, accountID, contentType, totalData)
			if err != nil {
				notCreated[clientKey] = SetError{Type: "serverError", Description: err.Error()}
			} else {
				created[clientKey] = blob
				recordCreationRefs(ctx, creationRefs, clientKey, Id(blob.ID))
			}
		}

		return "Blob/upload", map[string]any{
			"accountId":  accountID,
			"created":    created,
			"notCreated": notCreated,
		}
	}
}

// handleBlobLookup implements Blob/lookup per RFC 9404 Section 4.3.
func handleBlobLookup(backend BlobBackend, refs BlobReferenceBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)
		typeNamesRaw, _ := args["typeNames"].([]any)
		idsRaw, _ := args["ids"].([]any)

		// RFC 9404 Section 4.3: data types mapped to their defining capability URIs
		validTypes := map[string]string{
			"Mailbox":       MailCapabilityURI,
			"Thread":        MailCapabilityURI,
			"Email":         MailCapabilityURI,
			"Calendar":      CalendarsCapabilityURI,
			"CalendarEvent": CalendarsCapabilityURI,
			"AddressBook":   ContactsCapabilityURI,
			"ContactCard":   ContactsCapabilityURI,
			"Card":          ContactsCapabilityURI,
			"FileNode":      FileNodeCapabilityURI,
			"SieveScript":   SieveCapabilityURI,
		}

		var typeNames []string
		for _, raw := range typeNamesRaw {
			tn, _ := raw.(string)
			capURI, ok := validTypes[tn]
			if !ok || !IsUsingCapability(ctx, capURI) {
				return "error", MethodErrorArgs(MethodErrorUnknownDataType,
					fmt.Sprintf("type %q cannot reference blobs or capability not in using set", tn))
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

// handleBlobCopy implements Blob/copy per RFC 8620 Section 6.3.
func handleBlobCopy(backend BlobBackend) MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		fromAccountID, _ := args["fromAccountId"].(string)
		accountID, _ := args["accountId"].(string)

		copied := make(map[string]string)
		notCopied := make(map[string]any)

		var blobIDs []string
		if rawIDs, ok := args["blobIds"].([]any); ok {
			for _, item := range rawIDs {
				if idStr, ok := item.(string); ok {
					blobIDs = append(blobIDs, idStr)
				}
			}
		} else if createMap, ok := args["create"].(map[string]any); ok {
			// Back-compat for legacy create map payload
			for clientKey, raw := range createMap {
				if item, ok := raw.(map[string]any); ok {
					if blobID, ok := item["blobId"].(string); ok {
						copiedBlob, err := backend.CopyBlob(ctx, fromAccountID, accountID, blobID)
						if err != nil {
							notCopied[clientKey] = SetError{Type: "notFound", Description: "blob not found"}
						} else {
							copied[clientKey] = copiedBlob.ID
						}
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

		for _, blobID := range blobIDs {
			copiedBlob, err := backend.CopyBlob(ctx, fromAccountID, accountID, blobID)
			if err != nil {
				notCopied[blobID] = SetError{Type: "notFound", Description: "blob not found"}
			} else {
				copied[blobID] = copiedBlob.ID
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
