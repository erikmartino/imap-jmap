package jmap

import (
	"encoding/json"
	"net/http"
)

// Server encapsulates the JMAP server handler, session object, blob backend, mail backend, and method registry.
type Server struct {
	Session        *Session
	BlobBackend    BlobBackend
	MailBackend    MailBackend
	MethodRegistry *MethodRegistry
}

// Option defines a functional configuration option for Server.
type Option func(*Server)

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

// NewServer initializes a new Server instance.
func NewServer(session *Session, opts ...Option) *Server {
	if session == nil {
		session = DefaultSession("")
	}
	s := &Server{
		Session:        session,
		MethodRegistry: NewMethodRegistry(),
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.BlobBackend == nil {
		s.BlobBackend = NewMemoryBlobBackend()
	}
	if s.MailBackend == nil {
		s.MailBackend = NewMemoryBackend()
	}

	RegisterMailHandlers(s.MethodRegistry, s.MailBackend)
	RegisterBlobHandlers(s.MethodRegistry, s.BlobBackend)

	return s
}

// Handler returns an http.Handler wrapped with CORS middleware that routes requests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jmap", s.handleWellKnownJMAP)
	mux.HandleFunc("/jmap", s.handleAPI)
	mux.HandleFunc("/upload/", s.HandleUpload)
	mux.HandleFunc("/download/", s.HandleDownload)
	mux.HandleFunc("/", s.handleNotFound)
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
			_ = json.NewEncoder(w).Encode(s.Session)
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
