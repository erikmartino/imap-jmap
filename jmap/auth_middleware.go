package jmap

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// authMiddleware wraps an http.Handler, enforcing webmail authentication per RFC 8620 Section 8.2.
// It accepts credentials either via:
//   - Authorization: Bearer <token> header (standard, RFC 6750 Section 2.1)
//   - Authorization: Basic base64(username:password) header (HTTP Basic auth, as used by JMAP clients)
//   - ?access_token=<token> query parameter (RFC 6750 Section 2.3, needed for SSE and WebSocket)
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS preflight: pass through without auth.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Unauthenticated endpoints.
		if r.URL.Path == "/jmap/login" || r.URL.Path == "/version" || r.URL.Path == "/" {
			next.ServeHTTP(w, r)
			return
		}

		var accountID string
		var subject string
		var authErr error
		authed := false

		auth := r.Header.Get("Authorization")
		queryToken := r.URL.Query().Get("access_token")
		if strings.HasPrefix(auth, "Basic ") {
			// HTTP Basic: validate username/password directly (used by JMAP webmail clients).
			username, password, ok := r.BasicAuth()
			if ok {
				accountID, authErr = s.AuthBackend.ValidateCredentials(r.Context(), username, password)
				subject = username
				authed = authErr == nil
			} else {
				authErr = errors.New("malformed Basic authorization header")
			}
		} else if strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimPrefix(auth, "Bearer ")
			accountID, subject, authErr = s.AuthBackend.ValidateToken(r.Context(), token)
			authed = authErr == nil
		} else if queryToken != "" {
			// RFC 6750 Section 2.3: token in URI query (required for browser SSE and WebSocket).
			accountID, subject, authErr = s.AuthBackend.ValidateToken(r.Context(), queryToken)
			authed = authErr == nil
		} else if auth != "" {
			authErr = fmt.Errorf("unsupported authorization scheme: %q", auth)
		} else {
			authErr = errors.New("missing authorization header or access_token query param")
		}

		if !authed {
			log.Printf("AUTH FAILURE: path=%s remote=%s auth_header_present=%t err=%v", r.URL.Path, r.RemoteAddr, auth != "", authErr)
			w.Header().Set("WWW-Authenticate", `Basic realm="jmap", Bearer realm="jmap"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Inject authenticated accountID and subject email into request context for downstream handlers.
		ctx := ContextWithAccountID(r.Context(), accountID)
		if subject != "" {
			ctx = ContextWithSubject(ctx, subject)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// handleLogin processes POST /jmap/login — exchanges username/password for a Bearer token.
// This endpoint is not part of RFC 8620 (which defers to OAuth 2.0), but provides a simple
// credential exchange mechanism for clients that don't use an external identity provider.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Bad Request: invalid JSON", http.StatusBadRequest)
		return
	}

	token, err := s.AuthBackend.Authenticate(r.Context(), creds.Username, creds.Password)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="jmap"`)
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	accountID, _ := s.AuthBackend.ValidateCredentials(r.Context(), creds.Username, creds.Password)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token":     token,
		"accountId": accountID,
	})
}
