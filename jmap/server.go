package jmap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
)

// Server encapsulates the JMAP server handler, session object, blob backend, mail backend, and method registry.
type Server struct {
	Session           *Session
	BlobBackend       BlobBackend
	MailBackend       MailBackend
	ContactsBackend   ContactsBackend
	CalendarsBackend  CalendarsBackend
	SieveBackend      SieveBackend
	IMAPAccessBackend IMAPAccessBackend
	FileNodeBackend   FileNodeBackend
	PrincipalsBackend PrincipalsBackend
	AuthBackend       AuthBackend
	PermissionGuard   PermissionGuard
	AccountResolver   AccountResolver
	AllowedRecipients map[string]bool
	MethodRegistry    *MethodRegistry
	Broadcaster       *Broadcaster
	// PublicBaseURL, when set (e.g. from PUBLIC_URL), is the canonical externally-reachable
	// base (scheme+host) used to build the session's apiUrl/downloadUrl/uploadUrl/
	// eventSourceUrl. It takes precedence over request-derived URLs so a TLS-terminating
	// proxy that does not forward X-Forwarded-Proto cannot cause a cleartext http:// apiUrl
	// (which Android clients such as Ltt.rs refuse to use).
	PublicBaseURL string
}

// Option defines a functional configuration option for Server.
type Option func(*Server)

// WithBroadcaster sets a custom Broadcaster instance.
func WithBroadcaster(b *Broadcaster) Option {
	return func(s *Server) {
		s.Broadcaster = b
	}
}

// WithPublicBaseURL sets the canonical externally-reachable base URL (scheme+host, e.g.
// "https://jmap.example.com") used for the session's apiUrl/downloadUrl/uploadUrl/
// eventSourceUrl, overriding request-derived URLs. Set this to PUBLIC_URL when behind a
// TLS-terminating proxy so the session never advertises a cleartext http:// endpoint.
func WithPublicBaseURL(u string) Option {
	return func(s *Server) {
		s.PublicBaseURL = strings.TrimRight(u, "/")
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

// WithContactsBackend sets a custom ContactsBackend implementation per RFC 9610.
func WithContactsBackend(cb ContactsBackend) Option {
	return func(s *Server) {
		s.ContactsBackend = cb
	}
}

// WithCalendarsBackend sets a custom CalendarsBackend implementation for JMAP Calendars & JSCalendar (RFC 8984).
func WithCalendarsBackend(cb CalendarsBackend) Option {
	return func(s *Server) {
		s.CalendarsBackend = cb
	}
}

// WithSieveBackend sets a custom SieveBackend implementation per RFC 9661.
func WithSieveBackend(sb SieveBackend) Option {
	return func(s *Server) {
		s.SieveBackend = sb
	}
}

// WithIMAPAccessBackend sets a custom IMAPAccessBackend implementation per RFC 9698.
func WithIMAPAccessBackend(ib IMAPAccessBackend) Option {
	return func(s *Server) {
		s.IMAPAccessBackend = ib
	}
}

// WithFileNodeBackend sets a custom FileNodeBackend implementation for the JMAP FileNode file storage extension.
func WithFileNodeBackend(fb FileNodeBackend) Option {
	return func(s *Server) {
		s.FileNodeBackend = fb
	}
}

// WithAuthBackend sets a custom AuthBackend implementation for Bearer token authentication per RFC 8620 Section 8.2.
func WithAuthBackend(ab AuthBackend) Option {
	return func(s *Server) {
		s.AuthBackend = ab
	}
}

// WithPermissionGuard sets a custom PermissionGuard implementation for account authorization.
func WithPermissionGuard(g PermissionGuard) Option {
	return func(s *Server) {
		s.PermissionGuard = g
	}
}

// WithPrincipalsBackend sets a custom PrincipalsBackend implementation for JMAP Principals & Availability.
func WithPrincipalsBackend(pb PrincipalsBackend) Option {
	return func(s *Server) {
		s.PrincipalsBackend = pb
	}
}

// WithAccountResolver sets a custom AccountResolver implementation for email-to-account resolution.
func WithAccountResolver(r AccountResolver) Option {
	return func(s *Server) {
		s.AccountResolver = r
	}
}

// WithAllowedRecipients sets the allow-list of external email addresses permitted for outbound mail delivery.
func WithAllowedRecipients(allowed []string) Option {
	return func(s *Server) {
		m := make(map[string]bool, len(allowed))
		for _, a := range allowed {
			a = strings.TrimSpace(a)
			if a != "" {
				m[strings.ToLower(a)] = true
			}
		}
		s.AllowedRecipients = m
	}
}

// NewServer initializes a new Server instance.
func NewServer(session *Session, opts ...Option) *Server {
	if session == nil {
		session = DefaultSession("", "user@example.com")
	}
	s := &Server{
		Session:        session,
		MethodRegistry: NewMethodRegistry(),
		Broadcaster:    NewBroadcaster(),
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.PermissionGuard == nil {
		s.PermissionGuard = SelfAccessGuard{}
	}
	if s.AccountResolver == nil {
		s.AccountResolver = PrimaryDomainResolver{PrimaryDomain: "example.com"}
	}
	if s.AuthBackend == nil {
		s.AuthBackend = defaultAuthBackend{}
	}

	RegisterMailHandlers(s.MethodRegistry, s.MailBackend, s.BlobBackend, s.AccountResolver, s.AllowedRecipients)
	refs, _ := s.MailBackend.(BlobReferenceBackend)
	RegisterBlobHandlers(s.MethodRegistry, s.BlobBackend, refs)
	RegisterQuotaHandlers(s.MethodRegistry, s.MailBackend)
	RegisterContactsHandlers(s.MethodRegistry, s.ContactsBackend)
	RegisterCalendarHandlers(s.MethodRegistry, s.CalendarsBackend, s.MailBackend, s.BlobBackend, s.AccountResolver)
	RegisterSieveHandlers(s.MethodRegistry, s.SieveBackend)
	if s.IMAPAccessBackend != nil {
		RegisterIMAPAccessHandlers(s.MethodRegistry, s.IMAPAccessBackend)
	}
	RegisterFileNodeHandlers(s.MethodRegistry, s.FileNodeBackend)
	RegisterPrincipalsHandlers(s.MethodRegistry, s.PrincipalsBackend)

	return s
}

// Handler returns an http.Handler wrapped with CORS middleware that routes requests.
// All endpoints except OPTIONS and /jmap/login are protected by authentication per RFC 8620.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jmap", s.handleWellKnownJMAP)
	mux.HandleFunc("/jmap/session", s.handleWellKnownJMAP)
	mux.HandleFunc("/jmap", s.handleAPI)
	mux.HandleFunc("/jmap/ws", s.HandleWebSocket)
	mux.HandleFunc("/upload/", s.HandleUpload)
	mux.HandleFunc("/download/", s.HandleDownload)
	mux.HandleFunc("/eventsource", s.HandleEventSource)
	mux.HandleFunc("/jmap/login", s.handleLogin)
	mux.HandleFunc("/version", s.handleVersion)
	mux.HandleFunc("/", s.handleNotFound)

	return s.corsMiddleware(loggingMiddleware(s.authMiddleware(mux)))
}

type statusLoggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusLoggingResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusLoggingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusLoggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("http.Hijacker not supported")
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusLoggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(sw, r)
		hasAuth := r.Header.Get("Authorization") != ""
		log.Printf("HTTP %s %s -> %d | Origin: %q | HasAuth: %t", r.Method, r.URL.Path, sw.statusCode, r.Header.Get("Origin"), hasAuth)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
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
	baseURL := requestBaseURL(r)
	// A configured public base URL is authoritative: it prevents a TLS-terminating proxy
	// that drops X-Forwarded-Proto from producing a cleartext http:// apiUrl.
	if s.PublicBaseURL != "" {
		baseURL = s.PublicBaseURL
	}
	accountID, authed := AccountIDFromContext(r.Context())
	subject := accountID
	if !authed || subject == "" {
		subject = "user@example.com"
		accountID = AccountIDForSubject(subject)
	}
	if subj, ok := SubjectFromContext(r.Context()); ok {
		subject = subj
	}

	sess := *SessionForAccountID(baseURL, subject, accountID)

	// Clone capabilities map to avoid mutating template session concurrently
	caps := make(map[string]any, len(sess.Capabilities))
	for k, v := range sess.Capabilities {
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
	if s.IMAPAccessBackend != nil {
		caps[ImapAccessCapabilityURI] = ImapAccessCapability{}
	}
	sess.Capabilities = caps

	sess.APIURL = "/jmap"
	sess.DownloadURL = "/download/{accountId}/{blobId}/{name}?accept={type}"
	sess.UploadURL = "/upload/{accountId}/"
	sess.EventSourceURL = "/eventsource?types={types}&closeafter={closeafter}&ping={ping}"

	return &sess
}

func (s *Server) handleWellKnownJMAP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/.well-known/jmap" && r.URL.Path != "/jmap/session" {
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

// capabilitySupported reports whether a capability URI from a request "using" array is
// supported by this server. Besides capabilities advertised directly in the Session
// "capabilities" object (RFC 8620 Section 2), this accepts sub-capabilities whose support
// is implied by an advertised capability, such as "urn:ietf:params:jmap:principals:owner"
// which is implied by "urn:ietf:params:jmap:principals" (RFC 9670 Section 1.5.2).
func (s *Server) capabilitySupported(capURI string) bool {
	if capURI == ImapAccessCapabilityURI {
		return s.IMAPAccessBackend != nil
	}
	if capURI == PrincipalsOwnerCapabilityURI {
		if _, ok := s.Session.Capabilities[PrincipalsCapabilityURI]; ok {
			return true
		}
	}
	_, ok := s.Session.Capabilities[capURI]
	return ok
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
		if !s.capabilitySupported(capURI) {
			s.writeRequestError(w, http.StatusBadRequest, ErrorUnknownCapability, "Unknown capability: "+capURI)
			return
		}
	}

	var responses []Invocation
	executedMap := make(map[string]Invocation) // clientCallID -> response invocation

	// Request-scoped creation id map: created in one method call may be referenced by a
	// "#creationId" in a later call (RFC 8620 Sections 3.3 & 5.3).
	refs := NewCreationRefs(req.CreatedIds)
	reqCtx := WithCreationRefs(r.Context(), refs)
	reqCtx = WithUsingCapabilities(reqCtx, req.Using)
	reqCtx = withResponseSpill(reqCtx)

	for _, call := range req.MethodCalls {
		// Resolve Result References in arguments (RFC 8620 Section 3.7)
		resolvedArgs, refErrType, refErr := s.resolveResultReferences(call.Args, executedMap)
		if refErr != "" {
			respInv := Invocation{
				Name:         "error",
				Args:         MethodErrorArgs(refErrType, refErr),
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

		targetAccountID, _ := resolvedArgs["accountId"].(string)
		principalAccountID, _ := AccountIDFromContext(reqCtx)
		if targetAccountID == "" || targetAccountID == "primary" || targetAccountID == principalAccountID || AccountIDForSubject(targetAccountID) == principalAccountID {
			targetAccountID = principalAccountID
			resolvedArgs["accountId"] = principalAccountID
		} else {
			if s.PermissionGuard != nil && !s.PermissionGuard.CanAccessAccount(reqCtx, principalAccountID, targetAccountID) {
				respInv := Invocation{
					Name:         "error",
					Args:         MethodErrorArgs(MethodErrorAccountNotFound, fmt.Sprintf("Account %q not found", targetAccountID)),
					ClientCallID: call.ClientCallID,
				}
				responses = append(responses, respInv)
				executedMap[call.ClientCallID] = respInv
				continue
			}
		}

		respName, respArgs := handler(reqCtx, resolvedArgs, call.ClientCallID)
		normalizeSetResult(respName, respArgs)
		respInv := Invocation{
			Name:         respName,
			Args:         respArgs,
			ClientCallID: call.ClientCallID,
		}
		responses = append(responses, respInv)
		executedMap[call.ClientCallID] = respInv

		// RFC 8621 Section 7.5: a single EmailSubmission/set may trigger an implicit
		// Email/set (onSuccessUpdateEmail / onSuccessDestroyEmail), whose response MUST
		// follow the EmailSubmission/set response with the same client call id.
		for _, extra := range drainResponseSpill(reqCtx) {
			responses = append(responses, extra)
		}
	}

	resp := Response{
		MethodResponses: responses,
		SessionState:    s.Session.State,
	}
	// RFC 8620 Section 3.4: echo the createdIds map back if the client supplied it.
	if req.CreatedIds != nil {
		resp.CreatedIds = refs.Snapshot()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// normalizeSetResult applies RFC 8620 Section 5.3 to every set-family method response:
// the six set result properties (created, updated, destroyed, notCreated, notUpdated,
// notDestroyed) are typed "Id[...]|null" (or "Id[]|null" for destroyed) and MUST
// serialize as JSON null when empty, never as "{}"/"[]". Clients such as Bulwark
// webmail treat a truthy empty object as an error marker.
func normalizeSetResult(respName string, args map[string]any) {
	inSet := strings.HasSuffix(respName, "/set") ||
		strings.HasSuffix(respName, "/copy") ||
		strings.HasSuffix(respName, "/import")
	if !inSet {
		return
	}
	for _, k := range []string{"created", "updated", "destroyed", "notCreated", "notUpdated", "notDestroyed"} {
		if v, ok := args[k]; ok {
			args[k] = nilIfEmpty(v)
		}
	}
}

// resolveResultReferences replaces every result-reference argument (an argument whose name is
// prefixed with "#", per RFC 8620 Section 3.7) with the value obtained from the response of an
// earlier method call in the same request. On failure it returns the JMAP method error type and
// a description; an empty description means success.
func (s *Server) resolveResultReferences(args map[string]any, executed map[string]Invocation) (map[string]any, string, string) {
	if args == nil {
		return make(map[string]any), "", ""
	}

	resolved := make(map[string]any, len(args))
	for k, v := range args {
		if !strings.HasPrefix(k, "#") {
			resolved[k] = v
			continue
		}

		name := strings.TrimPrefix(k, "#")
		if _, dup := args[name]; dup {
			return nil, MethodErrorInvalidArguments,
				fmt.Sprintf("Argument %q is given in both normal and referenced form", name)
		}

		m, ok := v.(map[string]any)
		if !ok || !IsResultReference(m) {
			return nil, MethodErrorInvalidResultReference,
				fmt.Sprintf("Argument %q is not a valid ResultReference object", k)
		}

		val, refErr := s.resolveResultReference(m, executed)
		if refErr != "" {
			return nil, MethodErrorInvalidResultReference, refErr
		}

		// RFC 8620 Section 3.7: If the result reference evaluates to a single value and the argument
		// expects an Array (e.g. "ids", "emailIds", "destroy", "properties"), convert to a 1-element array.
		if _, isSlice := val.([]any); !isSlice && val != nil {
			if name == "ids" || name == "emailIds" || name == "mailboxIds" || name == "threadIds" || name == "destroy" || name == "typeNames" || name == "properties" {
				val = []any{val}
			}
		}
		resolved[name] = val
	}
	return resolved, "", ""
}

func (s *Server) resolveResultReference(m map[string]any, executed map[string]Invocation) (any, string) {
	resultOf, _ := m["resultOf"].(string)
	reqName, _ := m["name"].(string)
	path, _ := m["path"].(string)

	prevInv, ok := executed[resultOf]
	if !ok {
		return nil, "Referenced call ID " + resultOf + " not found"
	}

	if prevInv.Name != reqName {
		return nil, "Referenced method name mismatch: expected " + reqName + ", got " + prevInv.Name
	}

	val, err := EvaluateJSONPointer(prevInv.Args, path)
	if err != nil {
		return nil, "Failed to evaluate JSON pointer: " + err.Error()
	}

	return val, ""
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
	if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		info := GetVersionInfo()
		meta := map[string]any{
			"name":        "imap-jmap",
			"description": "JMAP Mail, Calendars, Contacts, and SMTP Server",
			"version":     info.Version,
			"endpoints": map[string]string{
				"session":    "/.well-known/jmap",
				"api":        "/jmap",
				"version":    "/version",
				"auth_login": "/jmap/login",
			},
		}
		if info.Commit != "" {
			meta["commit"] = info.Commit
		}
		if info.BuildTime != "" {
			meta["buildTime"] = info.BuildTime
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(meta)
		}
		return
	}
	http.NotFound(w, r)
}
