package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type AuthCode struct {
	Code                string
	Username            string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
	ExpiresAt           time.Time
}

type Session struct {
	SessionID string
	Username  string
	ExpiresAt time.Time
}

type TokenData struct {
	Token     string
	Username  string
	ClientID  string
	Scope     string
	ExpiresAt time.Time
}

type OIDCServer struct {
	Issuer              string
	Domain              string
	PrivateKey          *rsa.PrivateKey
	PublicKey           *rsa.PublicKey
	KeyID               string
	sessions            map[string]Session
	authCodes           map[string]AuthCode
	accessTokens        map[string]TokenData
	mu                  sync.RWMutex
	ValidateCredentials func(username, password string) bool
}

func NewOIDCServer(issuer string, domain string) (*OIDCServer, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	if domain == "" {
		domain = "profundo.dk"
	}

	srv := &OIDCServer{
		Issuer:       issuer,
		Domain:       domain,
		PrivateKey:   privKey,
		PublicKey:    &privKey.PublicKey,
		KeyID:        "mock-ldap-key-1",
		sessions:     make(map[string]Session),
		authCodes:    make(map[string]AuthCode),
		accessTokens: make(map[string]TokenData),
		ValidateCredentials: func(username, password string) bool {
			// Accept admin bind or standard username == password
			if (username == "admin" || username == "admin@"+domain) && (password == "admin" || password == "admin123") {
				return true
			}
			cleanUser := strings.TrimSuffix(username, "@"+domain)
			cleanPass := strings.TrimSuffix(password, "@"+domain)
			return username != "" && (password == username || cleanPass == cleanUser)
		},
	}

	return srv, nil
}

func (s *OIDCServer) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", s.handleOpenIDConfiguration)
	mux.HandleFunc("/oauth/keys", s.handleJWKS)
	mux.HandleFunc("/oauth/auth", s.handleAuth)
	mux.HandleFunc("/oauth/token", s.handleToken)
	mux.HandleFunc("/oauth/userinfo", s.handleUserInfo)
	mux.HandleFunc("/oauth/logout", s.handleLogout)
	mux.HandleFunc("/api/user", s.handleOpenPaaSUser)
	mux.HandleFunc("/api/configurations", s.handleOpenPaaSConfigurations)
	mux.HandleFunc("/ws/ticket", s.handleWebSocketTicket)
	mux.HandleFunc("/ws", s.handleWebSocket)

	return s.corsMiddleware(mux)
}

func (s *OIDCServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-Requested-With")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *OIDCServer) getIssuer(r *http.Request) string {
	if s.Issuer != "" {
		return s.Issuer
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if fHost := r.Header.Get("X-Forwarded-Host"); fHost != "" {
		host = fHost
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func (s *OIDCServer) handleOpenIDConfiguration(w http.ResponseWriter, r *http.Request) {
	issuer := s.getIssuer(r)

	config := map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth/auth",
		"token_endpoint":                        issuer + "/oauth/token",
		"userinfo_endpoint":                     issuer + "/oauth/userinfo",
		"jwks_uri":                              issuer + "/oauth/keys",
		"end_session_endpoint":                  issuer + "/oauth/logout",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "offline_access"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
		"claims_supported":                      []string{"sub", "aud", "iss", "exp", "iat", "email", "name", "preferred_username"},
		"code_challenge_methods_supported":      []string{"S256", "plain"},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(config)
}

func (s *OIDCServer) handleJWKS(w http.ResponseWriter, r *http.Request) {
	nBytes := s.PublicKey.N.Bytes()
	eBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(eBytes, uint32(s.PublicKey.E))
	// Trim leading zeros in exponent
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}

	jwks := map[string]any{
		"keys": []map[string]string{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": s.KeyID,
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jwks)
}

func (s *OIDCServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")
	scope := r.FormValue("scope")

	if r.Method == http.MethodGet {
		// Check for active session cookie
		if cookie, err := r.Cookie("oidc_session"); err == nil && cookie.Value != "" {
			s.mu.RLock()
			session, ok := s.sessions[cookie.Value]
			s.mu.RUnlock()

			if ok && session.ExpiresAt.After(time.Now()) {
				s.issueAuthCodeAndRedirect(w, r, session.Username, clientID, redirectURI, state, codeChallenge, codeChallengeMethod, scope)
				return
			}
		}

		s.renderLoginForm(w, "", clientID, redirectURI, state, codeChallenge, codeChallengeMethod, scope)
		return
	}

	if r.Method == http.MethodPost {
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		rememberMe := r.FormValue("remember_me") == "on" || r.FormValue("remember_me") == "true"

		if !s.ValidateCredentials(username, password) {
			s.renderLoginForm(w, "Invalid username or password", clientID, redirectURI, state, codeChallenge, codeChallengeMethod, scope)
			return
		}

		// Ensure username has full email domain if not present
		fullUser := username
		if !strings.Contains(fullUser, "@") {
			fullUser = fullUser + "@" + s.Domain
		}

		if rememberMe {
			sessionID := generateRandomToken(32)
			s.mu.Lock()
			s.sessions[sessionID] = Session{
				SessionID: sessionID,
				Username:  fullUser,
				ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
			}
			s.mu.Unlock()

			http.SetCookie(w, &http.Cookie{
				Name:     "oidc_session",
				Value:    sessionID,
				Path:     "/",
				MaxAge:   30 * 24 * 3600,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}

		s.issueAuthCodeAndRedirect(w, r, fullUser, clientID, redirectURI, state, codeChallenge, codeChallengeMethod, scope)
	}
}

func (s *OIDCServer) issueAuthCodeAndRedirect(w http.ResponseWriter, r *http.Request, username, clientID, redirectURI, state, codeChallenge, codeChallengeMethod, scope string) {
	code := generateRandomToken(32)

	s.mu.Lock()
	s.authCodes[code] = AuthCode{
		Code:                code,
		Username:            username,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		Scope:               scope,
		ExpiresAt:           time.Now().Add(5 * time.Minute),
	}
	s.mu.Unlock()

	parsedURI, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "Invalid redirect URI", http.StatusBadRequest)
		return
	}

	q := parsedURI.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	parsedURI.RawQuery = q.Encode()

	http.Redirect(w, r, parsedURI.String(), http.StatusFound)
}

func (s *OIDCServer) handleToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	grantType := r.FormValue("grant_type")
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	codeVerifier := r.FormValue("code_verifier")
	clientID := r.FormValue("client_id")

	if grantType != "authorization_code" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "unsupported_grant_type",
			"error_description": "Only authorization_code grant type is supported",
		})
		return
	}

	s.mu.Lock()
	authCode, ok := s.authCodes[code]
	if ok {
		delete(s.authCodes, code) // Codes are one-time use
	}
	s.mu.Unlock()

	if !ok || authCode.ExpiresAt.Before(time.Now()) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "Authorization code is invalid or expired",
		})
		return
	}

	if redirectURI != "" && authCode.RedirectURI != "" && redirectURI != authCode.RedirectURI {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "Redirect URI mismatch",
		})
		return
	}

	// Validate PKCE
	if authCode.CodeChallenge != "" {
		if codeVerifier == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_request",
				"error_description": "code_verifier required for PKCE",
			})
			return
		}

		if authCode.CodeChallengeMethod == "S256" || authCode.CodeChallengeMethod == "" {
			h := sha256.Sum256([]byte(codeVerifier))
			calculated := base64.RawURLEncoding.EncodeToString(h[:])
			if calculated != authCode.CodeChallenge {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "invalid_grant",
					"error_description": "PKCE verification failed",
				})
				return
			}
		} else if authCode.CodeChallengeMethod == "plain" {
			if codeVerifier != authCode.CodeChallenge {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "invalid_grant",
					"error_description": "PKCE plain verification failed",
				})
				return
			}
		}
	}

	if clientID == "" {
		clientID = authCode.ClientID
	}

	issuer := s.getIssuer(r)
	now := time.Now()
	exp := now.Add(24 * time.Hour)

	username := authCode.Username
	cleanName := strings.Split(username, "@")[0]

	// Create ID Token claims
	idClaims := map[string]any{
		"iss":                issuer,
		"sub":                username,
		"aud":                clientID,
		"exp":                exp.Unix(),
		"iat":                now.Unix(),
		"auth_time":          now.Unix(),
		"email":              username,
		"email_verified":     true,
		"name":               cleanName,
		"preferred_username": cleanName,
	}

	idToken, err := s.signJWT(idClaims)
	if err != nil {
		log.Printf("[OIDC] Failed to sign ID Token: %v", err)
		http.Error(w, "Failed to issue token", http.StatusInternalServerError)
		return
	}

	accessToken := generateRandomToken(32)
	s.mu.Lock()
	s.accessTokens[accessToken] = TokenData{
		Token:     accessToken,
		Username:  username,
		ClientID:  clientID,
		Scope:     authCode.Scope,
		ExpiresAt: exp,
	}
	s.mu.Unlock()

	resp := map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   86400,
		"id_token":     idToken,
		"scope":        authCode.Scope,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *OIDCServer) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("access_token")
	}

	if token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer error=\"invalid_token\"")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	s.mu.RLock()
	data, ok := s.accessTokens[token]
	s.mu.RUnlock()

	username := data.Username
	if !ok || username == "" {
		// Fallback for demo/mock tokens if not found
		username = "user@" + s.Domain
	}

	cleanName := strings.Split(username, "@")[0]

	info := map[string]any{
		"sub":                username,
		"name":               cleanName,
		"preferred_username": cleanName,
		"email":              username,
		"email_verified":     true,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

func (s *OIDCServer) handleOpenPaaSUser(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")

	s.mu.RLock()
	data, ok := s.accessTokens[token]
	s.mu.RUnlock()

	username := data.Username
	if !ok || username == "" {
		username = "user@" + s.Domain
	}
	cleanName := strings.Split(username, "@")[0]

	user := map[string]any{
		"_id":            username,
		"id":             username,
		"username":       username,
		"firstname":      cleanName,
		"lastname":       "",
		"preferredEmail": username,
		"emails":         []string{username},
		"accounts": []map[string]any{
			{
				"type": "email",
				"emails": []string{username},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(user)
}

func (s *OIDCServer) handleOpenPaaSConfigurations(w http.ResponseWriter, r *http.Request) {
	// Return empty configurations list for Twake OpenPaaS settings
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`[]`))
}

func (s *OIDCServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("oidc_session"); err == nil && cookie.Value != "" {
		s.mu.Lock()
		delete(s.sessions, cookie.Value)
		s.mu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oidc_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	postLogout := r.URL.Query().Get("post_logout_redirect_uri")
	if postLogout != "" {
		http.Redirect(w, r, postLogout, http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html><html><head><title>Logged Out</title><style>body{font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f5f5;}</style></head><body><div style="background:#fff;padding:32px;border-radius:12px;box-shadow:0 2px 8px rgba(0,0,0,0.1);text-align:center;"><h2>You have been logged out</h2><p>You may close this window or return to the application.</p></div></body></html>`))
}

func (s *OIDCServer) handleWebSocketTicket(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("access_token")
	}

	s.mu.RLock()
	data, ok := s.accessTokens[token]
	s.mu.RUnlock()

	username := data.Username
	if !ok || username == "" {
		username = "user@" + s.Domain
	}

	ticket := map[string]any{
		"clientAddress": r.RemoteAddr,
		"value":         generateRandomToken(32),
		"generatedOn":   time.Now().UTC().Format(time.RFC3339),
		"validUntil":    time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		"username":      username,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ticket)
}

func (s *OIDCServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		OriginPatterns:     []string{"*"},
	})
	if err != nil {
		log.Printf("[OIDC/WS] WebSocket accept failed: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	ctx := r.Context()
	for {
		typ, msg, err := conn.Read(ctx)
		if err != nil {
			break
		}
		if typ == websocket.MessageText {
			if strings.Contains(string(msg), "ping") {
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"pong"}`))
			}
		}
	}
}

func (s *OIDCServer) signJWT(claims map[string]any) (string, error) {
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": s.KeyID,
	}

	hdrBytes, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	encodedHdr := base64.RawURLEncoding.EncodeToString(hdrBytes)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsBytes)
	signingInput := encodedHdr + "." + encodedClaims

	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.PrivateKey, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}

	encodedSig := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + encodedSig, nil
}

func generateRandomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

const loginTemplateHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Sign in — Profundo Workplace</title>
  <style>
    :root {
      --primary: #1976d2;
      --primary-hover: #1565c0;
      --bg: #f8fafc;
      --surface: #ffffff;
      --text: #1e293b;
      --text-muted: #64748b;
      --border: #e2e8f0;
      --error-bg: #fee2e2;
      --error-text: #b91c1c;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      background: var(--bg);
      color: var(--text);
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 16px;
    }
    .card {
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 16px;
      box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.05), 0 8px 10px -6px rgba(0, 0, 0, 0.01);
      width: 100%;
      max-width: 400px;
      padding: 36px 32px;
    }
    .logo {
      text-align: center;
      margin-bottom: 24px;
    }
    .logo svg {
      width: 48px;
      height: 48px;
      fill: var(--primary);
    }
    h1 {
      font-size: 1.5rem;
      font-weight: 700;
      text-align: center;
      margin-bottom: 8px;
    }
    p.subtitle {
      font-size: 0.875rem;
      color: var(--text-muted);
      text-align: center;
      margin-bottom: 28px;
    }
    .error-banner {
      background: var(--error-bg);
      color: var(--error-text);
      padding: 12px 14px;
      border-radius: 8px;
      font-size: 0.875rem;
      margin-bottom: 20px;
      text-align: center;
    }
    .form-group {
      margin-bottom: 20px;
    }
    label {
      display: block;
      font-size: 0.875rem;
      font-weight: 500;
      margin-bottom: 6px;
    }
    input[type="text"], input[type="password"] {
      width: 100%;
      padding: 11px 14px;
      border: 1px solid var(--border);
      border-radius: 8px;
      font-size: 0.95rem;
      outline: none;
      transition: border-color 0.15s;
    }
    input[type="text"]:focus, input[type="password"]:focus {
      border-color: var(--primary);
      box-shadow: 0 0 0 3px rgba(25, 118, 210, 0.15);
    }
    .checkbox-group {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 24px;
    }
    .checkbox-group input {
      width: 16px;
      height: 16px;
      accent-color: var(--primary);
      cursor: pointer;
    }
    .checkbox-group label {
      margin-bottom: 0;
      font-size: 0.875rem;
      color: var(--text);
      cursor: pointer;
    }
    button.btn-submit {
      width: 100%;
      padding: 12px;
      background: var(--primary);
      color: #fff;
      border: none;
      border-radius: 8px;
      font-size: 0.95rem;
      font-weight: 600;
      cursor: pointer;
      transition: background 0.15s;
    }
    button.btn-submit:hover {
      background: var(--primary-hover);
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">
      <svg viewBox="0 0 24 24">
        <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 3c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm0 14.2c-2.5 0-4.71-1.28-6-3.22.03-1.99 4-3.08 6-3.08 1.99 0 5.97 1.09 6 3.08-1.29 1.94-3.5 3.22-6 3.22z"/>
      </svg>
    </div>
    <h1>Sign In</h1>
    <p class="subtitle">Enter your credentials to continue</p>

    {{if .Error}}
    <div class="error-banner">{{.Error}}</div>
    {{end}}

    <form method="POST" action="/oauth/auth">
      <input type="hidden" name="client_id" value="{{.ClientID}}">
      <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
      <input type="hidden" name="state" value="{{.State}}">
      <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
      <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
      <input type="hidden" name="scope" value="{{.Scope}}">

      <div class="form-group">
        <label for="username">Email or Username</label>
        <input type="text" id="username" name="username" placeholder="user@profundo.dk" required autofocus>
      </div>

      <div class="form-group">
        <label for="password">Password</label>
        <input type="password" id="password" name="password" placeholder="••••••••" required>
      </div>

      <div class="checkbox-group">
        <input type="checkbox" id="remember_me" name="remember_me" checked>
        <label for="remember_me">Remember me</label>
      </div>

      <button type="submit" class="btn-submit">Sign In</button>
    </form>
  </div>
</body>
</html>`

var tmpl = template.Must(template.New("login").Parse(loginTemplateHTML))

func (s *OIDCServer) renderLoginForm(w http.ResponseWriter, errMsg, clientID, redirectURI, state, codeChallenge, codeChallengeMethod, scope string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, map[string]string{
		"Error":               errMsg,
		"ClientID":            clientID,
		"RedirectURI":         redirectURI,
		"State":               state,
		"CodeChallenge":       codeChallenge,
		"CodeChallengeMethod": codeChallengeMethod,
		"Scope":               scope,
	})
}
