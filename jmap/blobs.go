package jmap

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// Blob represents a stored binary blob per RFC 8620 Section 6 and RFC 9404 Section 4.
type Blob struct {
	ID           string `json:"id"`
	BlobID       string `json:"blobId,omitempty"`
	AccountID    string `json:"accountId,omitempty"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`
	DigestSHA256 string `json:"digest:sha-256,omitempty"`
	Data         []byte `json:"-"`
}

func writeProblemDetails(w http.ResponseWriter, status int, problemType, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   problemType,
		"status": status,
		"title":  title,
		"detail": detail,
	})
}

// HandleUpload handles POST requests to /upload/{accountId}/ per RFC 8620 Section 6.1.
func (s *Server) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeProblemDetails(w, http.StatusMethodNotAllowed, "urn:ietf:params:jmap:error:methodNotAllowed", "Method Not Allowed", "POST required")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/upload/")
	accountID := strings.TrimSuffix(path, "/")
	if accountID == "" {
		writeProblemDetails(w, http.StatusBadRequest, "urn:ietf:params:jmap:error:invalidArguments", "Bad Request", "Account ID required")
		return
	}

	// Enforce maxSizeUpload (default 50MB)
	maxSizeUpload := int64(50000000)
	if sess := s.sessionForRequest(r); sess != nil {
		if capRaw, ok := sess.Capabilities[CoreCapabilityURI].(CoreCapability); ok && capRaw.MaxSizeUpload > 0 {
			maxSizeUpload = int64(capRaw.MaxSizeUpload)
		}
	}

	if r.ContentLength > maxSizeUpload {
		writeProblemDetails(w, http.StatusRequestEntityTooLarge, "urn:ietf:params:jmap:error:maxSizeUpload", "Payload Too Large", "Upload exceeds maxSizeUpload")
		return
	}

	lr := io.LimitReader(r.Body, maxSizeUpload+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		writeProblemDetails(w, http.StatusInternalServerError, "urn:ietf:params:jmap:error:serverError", "Server Error", "Failed to read request body")
		return
	}

	if int64(len(data)) > maxSizeUpload {
		writeProblemDetails(w, http.StatusRequestEntityTooLarge, "urn:ietf:params:jmap:error:maxSizeUpload", "Payload Too Large", "Upload exceeds maxSizeUpload")
		return
	}

	contentType := r.Header.Get("Content-Type")
	blob, err := s.BlobBackend.PutBlob(r.Context(), accountID, contentType, data)
	if err != nil {
		writeProblemDetails(w, http.StatusInternalServerError, "urn:ietf:params:jmap:error:serverError", "Server Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"accountId": accountID,
		"blobId":    blob.ID,
		"id":        blob.ID,
		"type":      blob.Type,
		"size":      blob.Size,
	})
}

// HandleDownload handles GET requests to /download/{accountId}/{blobId}/{name} per RFC 8620 Section 6.2.
func (s *Server) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblemDetails(w, http.StatusMethodNotAllowed, "urn:ietf:params:jmap:error:methodNotAllowed", "Method Not Allowed", "GET or HEAD required")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/download/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		writeProblemDetails(w, http.StatusNotFound, "urn:ietf:params:jmap:error:notFound", "Not Found", "Blob URL incomplete")
		return
	}

	accountID := parts[0]
	blobID := parts[1]
	name := ""
	if len(parts) == 3 {
		name = parts[2]
	}

	blob, ok, err := s.BlobBackend.GetBlob(r.Context(), accountID, blobID)
	if err != nil || !ok {
		writeProblemDetails(w, http.StatusNotFound, "urn:ietf:params:jmap:error:notFound", "Not Found", "Blob not found")
		return
	}

	mediaType := r.URL.Query().Get("type")
	if mediaType == "" {
		mediaType = blob.Type
	}

	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "immutable, max-age=31536000")
	if name != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	} else {
		w.Header().Set("Content-Disposition", `attachment`)
	}

	// http.ServeContent handles HTTP Range: bytes=... headers (returning 206 Partial Content),
	// Content-Length, Accept-Ranges, and HEAD requests per RFC 8620 Section 6.2 MAY provisions.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(blob.Data))
}
