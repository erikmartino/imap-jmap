package jmap

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Server encapsulates the JMAP server handler, session object, blob backend, mail backend, and method registry.
type Server struct {
	Session        *Session
	BlobBackend    BlobBackend
	MailBackend    MailBackend
	AuthBackend    AuthBackend
	MethodRegistry *MethodRegistry
	Broadcaster    *Broadcaster
}

// Option defines a functional configuration option for Server.
type Option func(*Server)

// WithBroadcaster sets a custom Broadcaster instance.
func WithBroadcaster(b *Broadcaster) Option {
	return func(s *Server) {
		s.Broadcaster = b
	}
}

// WithMailBackend sets a custom MailBackend implementation.
func WithMailBackend(mb MailBackend) Option {
	return func(s *Server) {
		s.MailBackend = mb
	}
}

// WithBlobBackend sets a custom BlobBackend implementation.
func WithBlobBackend(bb BlobBackend) Option {
	return func(s *Server) {
		s.BlobBackend = bb
	}
}

// WithAuthBackend sets a custom AuthBackend implementation for Bearer token authentication per RFC 8620 Section 8.2.
func WithAuthBackend(ab AuthBackend) Option {
	return func(s *Server) {
		s.AuthBackend = ab
	}
}

// NewServer initializes a new Server instance.
func NewServer(session *Session, opts ...Option) *Server {
	if session == nil {
		session = DefaultSession("")
	}
	s := &Server{
		Session:        session,
		MethodRegistry: NewMethodRegistry(),
		Broadcaster:    NewBroadcaster(),
	}

	for _, opt := range opts {
		opt(s)
	}

	RegisterMailHandlers(s.MethodRegistry, s.MailBackend)
	RegisterBlobHandlers(s.MethodRegistry, s.BlobBackend)
	RegisterQuotaHandlers(s.MethodRegistry, s.MailBackend)

	return s
}

// Handler returns an http.Handler wrapped with CORS middleware that routes requests.
// If an AuthBackend is configured, all endpoints except OPTIONS are protected by Bearer token auth.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jmap", s.handleWellKnownJMAP)
	mux.HandleFunc("/jmap", s.handleAPI)
	mux.HandleFunc("/jmap/ws", s.HandleWebSocket)
	mux.HandleFunc("/upload/", s.HandleUpload)
	mux.HandleFunc("/download/", s.HandleDownload)
	mux.HandleFunc("/eventsource", s.HandleEventSource)
	mux.HandleFunc("/", s.handleNotFound)

	if s.AuthBackend != nil {
		// Register login endpoint and wrap all routes with auth middleware.
		mux.HandleFunc("/jmap/login", s.handleLogin)
		return s.corsMiddleware(s.authMiddleware(mux))
	}
	return s.corsMiddleware(mux)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-Requested-With, Link")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, PATCH, PUT, DELETE, HEAD, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	return scheme + "://" + host
}

func (s *Server) sessionForRequest(r *http.Request) *Session {
	if s.Session == nil {
		return DefaultSession(requestBaseURL(r))
	}

	baseURL := requestBaseURL(r)
	sess := *s.Session

	// Clone capabilities map to avoid mutating template session concurrently
	caps := make(map[string]any, len(s.Session.Capabilities))
	for k, v := range s.Session.Capabilities {
		if k == WebSocketCapabilityURI {
			wsScheme := "ws"
			if strings.HasPrefix(baseURL, "https://") {
				wsScheme = "wss"
			}
			wsHost := strings.TrimPrefix(strings.TrimPrefix(baseURL, "http://"), "https://")
			wsCap := WebSocketCapability{
				URL:          wsScheme + "://" + wsHost + "/jmap/ws",
				SupportsPush: true,
			}
			if origWs, ok := v.(WebSocketCapability); ok {
				wsCap.SupportsPush = origWs.SupportsPush
			}
			caps[k] = wsCap
		} else {
			caps[k] = v
		}
	}
	sess.Capabilities = caps

	sess.APIURL = baseURL + "/jmap"
	sess.DownloadURL = baseURL + "/download/{accountId}/{blobId}/{name}?type={type}"
	sess.UploadURL = baseURL + "/upload/{accountId}/"
	sess.EventSourceURL = baseURL + "/eventsource?types={types}&closeafter={closeafter}&ping={ping}"

	return &sess
}

func (s *Server) handleWellKnownJMAP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/.well-known/jmap" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(s.sessionForRequest(r))
		}
	default:
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/jmap" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeRequestError(w, http.StatusBadRequest, ErrorInvalidJSON, "The request body could not be parsed as valid JSON.")
		return
	}

	// Validate 'using' capabilities (RFC 8620 Section 3.1 & 3.6.1)
	for _, capURI := range req.Using {
		if _, ok := s.Session.Capabilities[capURI]; !ok {
			s.writeRequestError(w, http.StatusBadRequest, ErrorUnknownCapability, "Unknown capability: "+capURI)
			return
		}
	}

	var responses []Invocation
	executedMap := make(map[string]Invocation) // clientCallID -> response invocation

	for _, call := range req.MethodCalls {
		// Resolve Result References in arguments (RFC 8620 Section 3.3)
		resolvedArgs, refErr := s.resolveResultReferences(call.Args, executedMap)
		if refErr != "" {
			respInv := Invocation{
				Name:         "error",
				Args:         MethodErrorArgs(MethodErrorInvalidResultReference, refErr),
				ClientCallID: call.ClientCallID,
			}
			responses = append(responses, respInv)
			executedMap[call.ClientCallID] = respInv
			continue
		}

		handler, ok := s.MethodRegistry.Get(call.Name)
		if !ok {
			respInv := Invocation{
				Name:         "error",
				Args:         MethodErrorArgs(MethodErrorUnknownMethod, "Unknown method: "+call.Name),
				ClientCallID: call.ClientCallID,
			}
			responses = append(responses, respInv)
			executedMap[call.ClientCallID] = respInv
			continue
		}

		respName, respArgs := handler(r.Context(), resolvedArgs, call.ClientCallID)
		respInv := Invocation{
			Name:         respName,
			Args:         respArgs,
			ClientCallID: call.ClientCallID,
		}
		responses = append(responses, respInv)
		executedMap[call.ClientCallID] = respInv
	}

	resp := Response{
		MethodResponses: responses,
		SessionState:    s.Session.State,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) resolveResultReferences(args map[string]any, executed map[string]Invocation) (map[string]any, string) {
	if args == nil {
		return make(map[string]any), ""
	}

	resolved := make(map[string]any, len(args))
	for k, v := range args {
		m, ok := v.(map[string]any)
		if ok && IsResultReference(m) {
			resultOf, _ := m["#resultOf"].(string)
			reqName, _ := m["#name"].(string)
			path, _ := m["#path"].(string)

			prevInv, ok := executed[resultOf]
			if !ok {
				return nil, "Referenced call ID " + resultOf + " not found"
			}

			if reqName != "" && prevInv.Name != reqName {
				return nil, "Referenced method name mismatch: expected " + reqName + ", got " + prevInv.Name
			}

			val, err := EvaluateJSONPointer(prevInv.Args, path)
			if err != nil {
				return nil, "Failed to evaluate JSON pointer: " + err.Error()
			}

			resolved[k] = val
		} else {
			resolved[k] = v
		}
	}
	return resolved, ""
}

func (s *Server) writeRequestError(w http.ResponseWriter, status int, errType string, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(RequestError{
		Type:   errType,
		Status: status,
		Detail: detail,
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}
