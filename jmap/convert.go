package jmap

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"imap-jmap/jmap/vcardconv"
)

// handleConvert serves POST /convert for bidirectional conversion between
// JSContact (RFC 9553, application/jscontact+json) and vCard 4.0 (RFC 6350, text/vcard).
// Returns HTTP 422 Unprocessable Entity for invalid cards per RFC 9553 and jscontact-tests.
func (s *Server) handleConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20)) // 10MB limit
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read body: %v", err), http.StatusBadRequest)
		return
	}

	rawCT := r.Header.Get("Content-Type")
	var mediaType string
	if rawCT != "" {
		mediaType, _, _ = mime.ParseMediaType(rawCT)
		mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	}

	trimmedBody := strings.TrimSpace(string(body))
	if mediaType == "" {
		if strings.HasPrefix(trimmedBody, "{") || strings.HasPrefix(trimmedBody, "[") {
			mediaType = "application/jscontact+json"
		} else if strings.HasPrefix(trimmedBody, "BEGIN:") {
			mediaType = "text/vcard"
		}
	}

	switch mediaType {
	case "application/jscontact+json", "application/json":
		var card map[string]any
		if err := json.Unmarshal(body, &card); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusUnprocessableEntity)
			return
		}
		if card == nil {
			http.Error(w, "empty card object", http.StatusUnprocessableEntity)
			return
		}

		vcf, err := vcardconv.ToVCard(card)
		if err != nil {
			http.Error(w, fmt.Sprintf("conversion error: %v", err), http.StatusUnprocessableEntity)
			return
		}

		w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(vcf))

	case "text/vcard", "text/x-vcard":
		if trimmedBody == "" {
			http.Error(w, "empty vcard body", http.StatusUnprocessableEntity)
			return
		}

		card, err := vcardconv.FromVCard(trimmedBody)
		if err != nil {
			http.Error(w, fmt.Sprintf("conversion error: %v", err), http.StatusUnprocessableEntity)
			return
		}

		respBytes, err := json.Marshal(card)
		if err != nil {
			http.Error(w, fmt.Sprintf("JSON marshal error: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/jscontact+json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBytes)

	default:
		http.Error(w, fmt.Sprintf("unsupported media type: %s", rawCT), http.StatusUnsupportedMediaType)
	}
}
