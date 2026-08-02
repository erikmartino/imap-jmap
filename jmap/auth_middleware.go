package jmap

import (
	"encoding/json"
	"net/http"
	"strings"
)

// authMiddleware wraps an http.Handler, enforcing Bearer token authentication per RFC 8620 Section 8.2.
// It accepts the token either via:
//   - Authorization: Bearer <token> header (standard, RFC 6750 Section 2.1)
//   - ?access_token=<token> query parameter (RFC 6750 Section 2.3, needed for SSE and WebSocket)
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS preflight: pass through without auth.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Login endpoint handled separately (no token required).
		if r.URL.Path == "/jmap/login" {
			next.ServeHTTP(w, r)
			return
		}

		// Extract Bearer token — Authorization header takes precedence over query param.
		token := ""
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		} else if qt := r.URL.Query().Get("access_token"); qt != "" {
			// RFC 6750 Section 2.3: token in URI query (required for browser SSE and WebSocket).
			token = qt
		}

		if token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="jmap"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		accountID, err := s.AuthBackend.ValidateToken(r.Context(), token)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="jmap", error="invalid_token"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Inject authenticated accountID into request context for downstream handlers.
		ctx := contextWithAccountID(r.Context(), accountID)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"token":     token,
		"accountId": creds.Username,
	})
}
