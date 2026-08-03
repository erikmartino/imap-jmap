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

// HandleUpload handles POST requests to /upload/{accountId}/ per RFC 8620 Section 6.1.
func (s *Server) HandleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/upload/")
	accountID := strings.TrimSuffix(path, "/")
	if accountID == "" {
		http.Error(w, "Account ID required", http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	contentType := r.Header.Get("Content-Type")
	blob, err := s.BlobBackend.PutBlob(r.Context(), accountID, contentType, data)
	if err != nil {
		http.Error(w, "Failed to store blob: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(blob)
}

// HandleDownload handles GET requests to /download/{accountId}/{blobId}/{name} per RFC 8620 Section 6.2.
func (s *Server) HandleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/download/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		http.NotFound(w, r)
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
		http.NotFound(w, r)
		return
	}

	mediaType := r.URL.Query().Get("type")
	if mediaType == "" {
		mediaType = blob.Type
	}

	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Accept-Ranges", "bytes")
	if name != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}

	// http.ServeContent handles HTTP Range: bytes=... headers (returning 206 Partial Content),
	// Content-Length, Accept-Ranges, and HEAD requests per RFC 8620 Section 6.2 MAY provisions.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(blob.Data))
}
