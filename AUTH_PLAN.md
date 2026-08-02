# JMAP Auth Plugin + Bulwark Integration Plan

## Status: PENDING IMPLEMENTATION

## Context

- Repository: `/home/martino/git/imap-jmap`
- Cluster manifests: `/home/martino/git/flux-clusters`
- Contabo node IP: `169.58.77.231`
- Domain: `profundo.dk`
- JMAP public URL: `https://imap-jmap.profundo.dk`

## Background

JMAP RFC 8620 Section 8.2 requires authentication on all endpoints but delegates the mechanism to the implementation. Currently `imap-jmap` has **no authentication** — all endpoints are open. This plan adds:

1. An `AuthBackend` plugin interface (swappable: test → production)
2. A test memory implementation where `username == password` means authenticated
3. Bearer token middleware protecting all JMAP endpoints
4. A `POST /jmap/login` endpoint for credential exchange
5. Documentation on what Bulwark (the web client) needs to change

---

## Part 1 — Files to Create / Modify

### [NEW] `jmap/auth.go`

Define the `AuthBackend` interface and context key:

```go
package jmap

import "context"

type contextKey int
const authAccountIDKey contextKey = iota

// AuthBackend defines the authentication plugin interface per RFC 8620 Section 8.2.
type AuthBackend interface {
    // Authenticate validates username/password and returns a Bearer token.
    Authenticate(ctx context.Context, username, password string) (token string, err error)
    // ValidateToken checks a Bearer token and returns the authenticated accountID.
    ValidateToken(ctx context.Context, token string) (accountID string, err error)
}

// AccountIDFromContext retrieves the authenticated accountID from the request context.
func AccountIDFromContext(ctx context.Context) (string, bool) {
    id, ok := ctx.Value(authAccountIDKey).(string)
    return id, ok
}

// contextWithAccountID injects an accountID into the context.
func contextWithAccountID(ctx context.Context, accountID string) context.Context {
    return context.WithValue(ctx, authAccountIDKey, accountID)
}
```

---

### [NEW] `jmap/memory/auth_store.go`

Test implementation — accepts any `username == password`:

```go
package memory

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "sync"
)

// MemoryAuthBackend is a test AuthBackend where username == password is accepted.
type MemoryAuthBackend struct {
    mu     sync.RWMutex
    tokens map[string]string // token → username/accountID
}

func NewMemoryAuthBackend() *MemoryAuthBackend {
    return &MemoryAuthBackend{tokens: make(map[string]string)}
}

func (a *MemoryAuthBackend) Authenticate(ctx context.Context, username, password string) (string, error) {
    if username == "" || username != password {
        return "", fmt.Errorf("invalid credentials")
    }
    token := generateToken()
    a.mu.Lock()
    a.tokens[token] = username
    a.mu.Unlock()
    return token, nil
}

func (a *MemoryAuthBackend) ValidateToken(ctx context.Context, token string) (string, error) {
    a.mu.RLock()
    defer a.mu.RUnlock()
    accountID, ok := a.tokens[token]
    if !ok {
        return "", fmt.Errorf("invalid token")
    }
    return accountID, nil
}

func generateToken() string {
    b := make([]byte, 32)
    _, _ = rand.Read(b)
    return hex.EncodeToString(b)
}
```

---

### [NEW] `jmap/auth_middleware.go`

HTTP middleware + login endpoint:

```go
package jmap

import (
    "encoding/json"
    "net/http"
    "strings"
)

// authMiddleware wraps an http.Handler, enforcing Bearer token authentication per RFC 8620 Section 8.2.
// It also handles POST /jmap/login for credential exchange.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Login endpoint: POST /jmap/login
        if r.URL.Path == "/jmap/login" && r.Method == http.MethodPost {
            s.handleLogin(w, r)
            return
        }

        // CORS preflight: pass through without auth.
        if r.Method == http.MethodOptions {
            next.ServeHTTP(w, r)
            return
        }

        // Extract Bearer token from Authorization header or ?access_token query param.
        token := ""
        if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
            token = strings.TrimPrefix(auth, "Bearer ")
        } else if qt := r.URL.Query().Get("access_token"); qt != "" {
            // RFC 6750 Section 2.3: token in query param (needed for SSE and WebSocket).
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

        // Inject authenticated accountID into request context.
        ctx := contextWithAccountID(r.Context(), accountID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
    var creds struct {
        Username string `json:"username"`
        Password string `json:"password"`
    }
    if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
        http.Error(w, "Bad Request", http.StatusBadRequest)
        return
    }

    token, err := s.AuthBackend.Authenticate(r.Context(), creds.Username, creds.Password)
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]string{
        "token":     token,
        "accountId": creds.Username,
    })
}
```

---

### [MODIFY] `jmap/server.go`

- Add `AuthBackend AuthBackend` field to `Server` struct
- Add `WithAuthBackend(ab AuthBackend) Option`
- Modify `Handler()` to wrap mux with `authMiddleware` when `AuthBackend != nil`
- Add `/jmap/login` route to mux

```go
// In Server struct:
AuthBackend AuthBackend

// New option:
func WithAuthBackend(ab AuthBackend) Option {
    return func(s *Server) { s.AuthBackend = ab }
}

// In Handler():
if s.AuthBackend != nil {
    mux.HandleFunc("/jmap/login", s.handleLogin)
    return s.corsMiddleware(s.authMiddleware(mux))
}
return s.corsMiddleware(mux)
```

---

### [MODIFY] `main.go`

```go
import "imap-jmap/jmap/memory"

authBackend := memory.NewMemoryAuthBackend()
server := jmap.NewServer(
    session,
    jmap.WithMailBackend(memBackend),
    jmap.WithBlobBackend(memBlobBackend),
    jmap.WithAuthBackend(authBackend),
)
```

---

### [NEW] `jmap/auth_test.go`

Tests to write:
- `TestAuth_Login_Success` — POST `/jmap/login` with matching username/password → 200 + token
- `TestAuth_Login_WrongPassword` — mismatched → 401
- `TestAuth_ProtectedEndpoint_NoToken` — GET `/.well-known/jmap` without token → 401
- `TestAuth_ProtectedEndpoint_ValidToken` — valid Bearer token → 200
- `TestAuth_ProtectedEndpoint_InvalidToken` — bogus token → 401
- `TestAuth_AccessToken_QueryParam` — `/eventsource?access_token=<token>` → 200 (SSE)
- `TestAuth_WebSocket_AccessToken` — `/jmap/ws?access_token=<token>` → WS upgrade succeeds

---

## Part 2 — Bulwark Integration

### What Bulwark Must Do

| Step | Action | Details |
|:---|:---|:---|
| **1. Login** | `POST /jmap/login` | Body: `{"username":"x","password":"x"}` → `{"token":"…","accountId":"…"}` |
| **2. Store token** | `localStorage.setItem('jmapToken', token)` | Persist across page loads |
| **3. Session discovery** | `GET /.well-known/jmap` | Header: `Authorization: Bearer <token>` |
| **4. All JMAP calls** | `POST /jmap` | Header: `Authorization: Bearer <token>` |
| **5. EventSource (SSE)** | `GET /eventsource?types=…&access_token=<token>` | Browser `EventSource` API can't set headers → use query param (RFC 6750 §2.3) |
| **6. WebSocket** | `GET /jmap/ws?access_token=<token>` | Browser `WebSocket` API can't set headers → use query param |

### Login Request Example

```http
POST /jmap/login
Content-Type: application/json

{"username": "alice", "password": "alice"}

→ 200 OK
{"token": "a1b2c3deadbeef…", "accountId": "alice"}
```

### Subsequent Requests

```http
GET /.well-known/jmap
Authorization: Bearer a1b2c3deadbeef…

→ 200 OK  (JMAP Session object)
```

```http
POST /jmap
Authorization: Bearer a1b2c3deadbeef…
Content-Type: application/json

{"using": ["urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"],
 "methodCalls": [["Mailbox/get", {"accountId": "alice"}, "c0"]]}
```

### SSE (EventSource)

```javascript
// Cannot use EventSource with custom headers — use access_token param instead
const token = localStorage.getItem('jmapToken');
const es = new EventSource(`/eventsource?types=Email,Mailbox&access_token=${token}`);
```

### WebSocket

```javascript
const token = localStorage.getItem('jmapToken');
const ws = new WebSocket(`wss://imap-jmap.profundo.dk/jmap/ws?access_token=${token}`, ['jmap']);
```

---

## Remaining JMAP RFCs (after auth)

| RFC | Specification | Status |
|:---|:---|:---|
| RFC 9661 | JMAP for Sieve Scripts | ⬜ Not started |
| RFC 9670 | JMAP Sharing / Principals | ⬜ Not started |
| RFC 9610 | JMAP for Contacts (JSContact) | ⬜ Not started |
| RFC 9698 | JMAPACCESS Extension for IMAP | ⬜ Not started |

---

## Verification Commands

```bash
# Run auth tests
timeout 10s go test -count=1 -v -run TestAuth ./jmap

# Test login manually
curl -X POST http://localhost:8080/jmap/login \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"alice"}'

# Test protected endpoint without token (should return 401)
curl -i http://localhost:8080/.well-known/jmap

# Test with valid token
curl -H "Authorization: Bearer <token>" http://localhost:8080/.well-known/jmap

# Test SSE with access_token param
curl "http://localhost:8080/eventsource?types=Email&access_token=<token>"
```
