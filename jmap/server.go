package jmap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
	OutboundSender    OutboundMailSender
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

// WithOutboundSender sets the OutboundMailSender used to relay submissions to external
// recipients via their domain's MX servers. When nil (tests, or no relay configured),
// external allow-listed recipients are refused with a transient error instead of being
// acknowledged as sent.
func WithOutboundSender(o OutboundMailSender) Option {
	return func(s *Server) {
		s.OutboundSender = o
	}
}

// WithCoreCapability sets custom CoreCapability limits on the server session.
func WithCoreCapability(cc CoreCapability) Option {
	return func(s *Server) {
		if s.Session == nil {
			s.Session = DefaultSession("", "user@example.com")
		}
		s.Session.Capabilities[CoreCapabilityURI] = cc
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

	RegisterMailHandlers(s.MethodRegistry, s.MailBackend, s.BlobBackend, s.AccountResolver, s.AllowedRecipients, s.OutboundSender)
	refs, _ := s.MailBackend.(BlobReferenceBackend)
	RegisterBlobHandlers(s.MethodRegistry, s.BlobBackend, refs)
	RegisterQuotaHandlers(s.MethodRegistry, s.MailBackend)
	RegisterContactsHandlers(s.MethodRegistry, s.ContactsBackend)
	RegisterCalendarHandlers(s.MethodRegistry, s.CalendarsBackend, s.MailBackend, s.PrincipalsBackend, s.BlobBackend, s.AccountResolver)
	RegisterSieveHandlers(s.MethodRegistry, s.SieveBackend)
	if s.IMAPAccessBackend != nil {
		RegisterIMAPAccessHandlers(s.MethodRegistry, s.IMAPAccessBackend)
	}
	RegisterFileNodeHandlers(s.MethodRegistry, s.FileNodeBackend)
	RegisterPrincipalsHandlers(s.MethodRegistry, s.PrincipalsBackend)

	if s.MailBackend != nil && s.Broadcaster != nil {
		s.Broadcaster.AddListener(func(accountID, typeName, newState string) {
			accountCtx := ContextWithAccountID(context.Background(), accountID)
			subs, err := s.MailBackend.GetAllPushSubscriptions(accountCtx)
			if err != nil || len(subs) == 0 {
				return
			}
			for _, sub := range subs {
				if sub == nil || sub.URL == "" {
					continue
				}
				go func(sub *PushSubscription) {
					err := DispatchWebPushStateChange(context.Background(), sub, accountID, typeName, newState, nil, "")
					if err == ErrSubscriptionGone {
						_, _ = s.MailBackend.DeletePushSubscription(accountCtx, sub.ID)
					}
				}(sub)
			}
		})
	}

	return s
}

// Handler returns an http.Handler wrapped with CORS middleware that routes requests.
// All endpoints except OPTIONS and /jmap/login are protected by authentication per RFC 8620.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	handleFlexRoute := func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/.well-known/jmap") || strings.HasSuffix(path, "/jmap/session"):
			s.handleWellKnownJMAP(w, r)
		case strings.HasSuffix(path, "/jmap/ws"):
			s.HandleWebSocket(w, r)
		case strings.HasSuffix(path, "/jmap/login"):
			s.handleLogin(w, r)
		case strings.HasSuffix(path, "/jmap"):
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				s.handleWellKnownJMAP(w, r)
			} else {
				s.handleAPI(w, r)
			}
		case strings.Contains(path, "/upload/"):
			s.HandleUpload(w, r)
		case strings.Contains(path, "/download/"):
			s.HandleDownload(w, r)
		case strings.HasSuffix(path, "/eventsource") || strings.Contains(path, "/eventsource"):
			s.HandleEventSource(w, r)
		case strings.HasSuffix(path, "/version"):
			s.handleVersion(w, r)
		case strings.HasSuffix(path, "/convert"):
			s.handleConvert(w, r)
		default:
			s.handleNotFound(w, r)
		}
	}

	mux.HandleFunc("/", handleFlexRoute)

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
		slog.Debug("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.statusCode,
			"origin", r.Header.Get("Origin"),
			"hasAuth", hasAuth,
		)
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
		reqHeaders := r.Header.Get("Access-Control-Request-Headers")
		if reqHeaders != "" {
			w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
		} else {
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-Requested-With, Link, If-Match, If-None-Match, Origin, Accept-Language, Cache-Control, Pragma, X-JMAP-Subprotocol, Sec-WebSocket-Protocol")
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, PATCH, PUT, DELETE, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "Link, ETag, Location, WWW-Authenticate, Content-Type, Content-Length, Access-Control-Allow-Origin")
		w.Header().Set("Access-Control-Max-Age", "86400")

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
	subject, _ := SubjectFromContext(r.Context())
	if !authed || accountID == "" {
		subject = "user@example.com"
		accountID = AccountIDForSubject(subject)
	} else if subject == "" {
		if subj, ok := SubjectForAccountID(accountID); ok && subj != "" {
			subject = subj
		} else {
			subject = accountID
		}
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
	if s.Session != nil {
		if customCore, ok := s.Session.Capabilities[CoreCapabilityURI].(CoreCapability); ok {
			caps[CoreCapabilityURI] = customCore
		}
	}
	sess.Capabilities = caps

	// Advertise absolute service URLs built from the request's externally-reachable
	// base (RFC 8620 Section 2 example uses absolute URLs; real servers such as
	// Fastmail/Cyrus do the same). A relative apiUrl is resolved by browsers against
	// the *page* origin, not the session resource URL, so a cross-origin web client
	// (e.g. Bulwark webmail) would POST to the wrong host.
	sess.APIURL = baseURL + "/jmap"
	sess.DownloadURL = baseURL + "/download/{accountId}/{blobId}/{name}?accept={type}"
	sess.UploadURL = baseURL + "/upload/{accountId}/"
	sess.EventSourceURL = baseURL + "/eventsource?types={types}&closeafter={closeafter}&ping={ping}"

	return &sess
}

func (s *Server) handleWellKnownJMAP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/.well-known/jmap") && !strings.HasSuffix(r.URL.Path, "/jmap/session") && !strings.HasSuffix(r.URL.Path, "/jmap") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		sess := s.sessionForRequest(r)
		sessBytes, _ := json.Marshal(sess)
		slog.Debug("JMAP Session Response", "remote", r.RemoteAddr, "payload", string(sessBytes))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(sessBytes)
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

func requiredCapabilityForMethod(name string) string {
	switch {
	case strings.HasPrefix(name, "Core/"):
		return CoreCapabilityURI
	case strings.HasPrefix(name, "Mailbox/"), strings.HasPrefix(name, "Email/"), strings.HasPrefix(name, "Thread/"), strings.HasPrefix(name, "SearchSnippet/"):
		return MailCapabilityURI
	case strings.HasPrefix(name, "EmailSubmission/"):
		return SubmissionCapabilityURI
	case strings.HasPrefix(name, "VacationResponse/"):
		return VacationResponseCapabilityURI
	case strings.HasPrefix(name, "Identity/"):
		return SubmissionCapabilityURI
	case strings.HasPrefix(name, "PushSubscription/"):
		return CoreCapabilityURI
	case strings.HasPrefix(name, "Calendar/"), strings.HasPrefix(name, "CalendarEvent/"), strings.HasPrefix(name, "CalendarEventNotification/"), strings.HasPrefix(name, "ParticipantIdentity/"):
		return CalendarsCapabilityURI
	case strings.HasPrefix(name, "AddressBook/"), strings.HasPrefix(name, "ContactCard/"), strings.HasPrefix(name, "ContactCardGroup/"), strings.HasPrefix(name, "Contact/"):
		return ContactsCapabilityURI
	case strings.HasPrefix(name, "SieveScript/"):
		return SieveCapabilityURI
	case strings.HasPrefix(name, "Principal/"):
		return PrincipalsCapabilityURI
	case strings.HasPrefix(name, "Blob/"):
		return BlobCapabilityURI
	case strings.HasPrefix(name, "Quota/"):
		return QuotaCapabilityURI
	default:
		return CoreCapabilityURI
	}
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/jmap") {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") && !strings.HasPrefix(strings.ToLower(ct), "application/problem+json") {
		http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
		return
	}

	limits := s.coreLimits(r)
	maxSize := int64(limits.MaxSizeRequest)
	if maxSize <= 0 {
		maxSize = int64(DefaultMaxSizeRequest)
	}

	if r.ContentLength > maxSize {
		s.writeRequestError(w, http.StatusRequestEntityTooLarge, ErrorLimit, fmt.Sprintf("The request size (%d octets) exceeded maxSizeRequest (%d octets)", r.ContentLength, maxSize), "maxSizeRequest")
		return
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxSize+1))
	if err != nil {
		s.writeRequestError(w, http.StatusBadRequest, ErrorNotJSON, "Cannot read request body")
		return
	}
	if int64(len(bodyBytes)) > maxSize {
		s.writeRequestError(w, http.StatusRequestEntityTooLarge, ErrorLimit, fmt.Sprintf("The request size (%d octets) exceeded maxSizeRequest (%d octets)", len(bodyBytes), maxSize), "maxSizeRequest")
		return
	}

	slog.Debug("JMAP Request Body", "remote", r.RemoteAddr, "payload", string(bodyBytes))

	var rawMap map[string]any
	if err := json.Unmarshal(bodyBytes, &rawMap); err != nil {
		s.writeRequestError(w, http.StatusBadRequest, ErrorNotJSON, "The request body could not be parsed as valid JSON.")
		return
	}

	usingRaw, hasUsing := rawMap["using"]
	methodCallsRaw, hasCalls := rawMap["methodCalls"]
	if !hasUsing || !hasCalls {
		s.writeRequestError(w, http.StatusBadRequest, ErrorNotRequest, "Request MUST have 'using' and 'methodCalls' properties.")
		return
	}
	if _, ok := usingRaw.([]any); !ok {
		s.writeRequestError(w, http.StatusBadRequest, ErrorNotRequest, "'using' must be an array.")
		return
	}
	callsList, ok := methodCallsRaw.([]any)
	if !ok {
		s.writeRequestError(w, http.StatusBadRequest, ErrorNotRequest, "'methodCalls' must be an array.")
		return
	}

	if limits.MaxCallsInRequest > 0 && uint64(len(callsList)) > limits.MaxCallsInRequest {
		s.writeRequestError(w, http.StatusBadRequest, ErrorLimit, fmt.Sprintf("The number of method calls (%d) exceeded maxCallsInRequest (%d)", len(callsList), limits.MaxCallsInRequest), "maxCallsInRequest")
		return
	}

	var req Request
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		s.writeRequestError(w, http.StatusBadRequest, ErrorNotRequest, "Invalid request format.")
		return
	}

	// Validate 'using' capabilities (RFC 8620 Section 3.1 & 3.6.1)
	usingSet := make(map[string]bool, len(req.Using))
	for _, capURI := range req.Using {
		if !s.capabilitySupported(capURI) {
			s.writeRequestError(w, http.StatusBadRequest, ErrorUnknownCapability, "Unknown capability: "+capURI)
			return
		}
		usingSet[capURI] = true
	}

	var responses []Invocation
	executedMap := make(map[string]Invocation) // clientCallID -> response invocation

	// Request-scoped creation id map: created in one method call may be referenced by a
	// "#creationId" in a later call (RFC 8620 Sections 3.3 & 5.3).
	refs := NewCreationRefs(req.CreatedIds)
	reqCtx := WithCreationRefs(r.Context(), refs)
	reqCtx = WithUsingCapabilities(reqCtx, req.Using)
	reqCtx = WithCoreLimits(reqCtx, limits)
	var calCap CalendarsCapability
	if s.Session != nil && s.Session.Capabilities != nil {
		if c, ok := s.Session.Capabilities[CalendarsCapabilityURI].(CalendarsCapability); ok {
			calCap = c
		}
	}
	reqCtx = WithCalendarsCapability(reqCtx, calCap)
	reqCtx = withResponseSpill(reqCtx)

	for _, call := range req.MethodCalls {
		// Check that required capability for method is present in 'using'
		reqCap := requiredCapabilityForMethod(call.Name)
		if !usingSet[reqCap] && !usingSet[CoreCapabilityURI] {
			respInv := Invocation{
				Name:         "error",
				Args:         MethodErrorArgs(MethodErrorUnknownMethod, "Method requires capability "+reqCap+" which is not in 'using'"),
				ClientCallID: call.ClientCallID,
			}
			responses = append(responses, respInv)
			executedMap[call.ClientCallID] = respInv
			continue
		}
		if len(req.Using) == 0 {
			respInv := Invocation{
				Name:         "error",
				Args:         MethodErrorArgs(MethodErrorUnknownMethod, "Method calls require non-empty 'using'"),
				ClientCallID: call.ClientCallID,
			}
			responses = append(responses, respInv)
			executedMap[call.ClientCallID] = respInv
			continue
		}

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

		// RFC 8620 Section 2.2 & 5.1: maxObjectsInGet enforcement
		if strings.HasSuffix(call.Name, "/get") {
			if rawIDs, hasIDs := resolvedArgs["ids"]; hasIDs && rawIDs != nil {
				if idsSlice, ok := rawIDs.([]any); ok {
					if limits.MaxObjectsInGet > 0 && uint64(len(idsSlice)) > limits.MaxObjectsInGet {
						respInv := Invocation{
							Name:         "error",
							Args:         MethodErrorArgs(MethodErrorRequestTooLarge, fmt.Sprintf("Number of requested ids (%d) exceeds maxObjectsInGet (%d)", len(idsSlice), limits.MaxObjectsInGet)),
							ClientCallID: call.ClientCallID,
						}
						responses = append(responses, respInv)
						executedMap[call.ClientCallID] = respInv
						continue
					}
				}
			}
		}

		// RFC 8620 Section 2.2 & 5.3: maxObjectsInSet enforcement
		if strings.HasSuffix(call.Name, "/set") {
			var setCount uint64
			if c, ok := resolvedArgs["create"].(map[string]any); ok {
				setCount += uint64(len(c))
			}
			if u, ok := resolvedArgs["update"].(map[string]any); ok {
				setCount += uint64(len(u))
			}
			if d, ok := resolvedArgs["destroy"].([]any); ok {
				setCount += uint64(len(d))
			}
			if limits.MaxObjectsInSet > 0 && setCount > limits.MaxObjectsInSet {
				respInv := Invocation{
					Name:         "error",
					Args:         MethodErrorArgs(MethodErrorRequestTooLarge, fmt.Sprintf("Total objects in set (%d) exceeds maxObjectsInSet (%d)", setCount, limits.MaxObjectsInSet)),
					ClientCallID: call.ClientCallID,
				}
				responses = append(responses, respInv)
				executedMap[call.ClientCallID] = respInv
				continue
			}
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

		principalAccountID, _ := AccountIDFromContext(reqCtx)
		var targetAccountID string

		if call.Name == "Core/echo" {
			// Core/echo returns arguments untouched
			methodCallCtx := reqCtx
			respName, respArgs := handler(methodCallCtx, resolvedArgs, call.ClientCallID)
			respInv := Invocation{
				Name:         respName,
				Args:         respArgs,
				ClientCallID: call.ClientCallID,
			}
			responses = append(responses, respInv)
			executedMap[call.ClientCallID] = respInv
			continue
		} else if strings.HasPrefix(call.Name, "PushSubscription/") {
			targetAccountID = principalAccountID
		} else {
			rawAcct, hasAcct := resolvedArgs["accountId"]
			if !hasAcct || rawAcct == nil {
				respInv := Invocation{
					Name:         "error",
					Args:         MethodErrorArgs(MethodErrorInvalidArguments, "accountId is required"),
					ClientCallID: call.ClientCallID,
				}
				responses = append(responses, respInv)
				executedMap[call.ClientCallID] = respInv
				continue
			}
			acctStr, ok := rawAcct.(string)
			if !ok || acctStr == "" {
				respInv := Invocation{
					Name:         "error",
					Args:         MethodErrorArgs(MethodErrorInvalidArguments, "accountId must be a non-empty string"),
					ClientCallID: call.ClientCallID,
				}
				responses = append(responses, respInv)
				executedMap[call.ClientCallID] = respInv
				continue
			}

			if rawIDs, hasIDs := resolvedArgs["ids"]; hasIDs && rawIDs != nil {
				if _, ok := rawIDs.([]any); !ok {
					respInv := Invocation{
						Name:         "error",
						Args:         MethodErrorArgs(MethodErrorInvalidArguments, "ids must be an array or null"),
						ClientCallID: call.ClientCallID,
					}
					responses = append(responses, respInv)
					executedMap[call.ClientCallID] = respInv
					continue
				}
			}

			targetAccountID = acctStr
			if targetAccountID == "primary" || targetAccountID == principalAccountID || AccountIDForSubject(targetAccountID) == principalAccountID {
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
		}

		methodCallCtx := ContextWithAccountID(reqCtx, targetAccountID)
		respName, respArgs := handler(methodCallCtx, resolvedArgs, call.ClientCallID)
		normalizeSetResult(respName, respArgs)
		slog.Debug("JMAP invocation",
			"method", call.Name,
			"response", respName,
			"reqArgs", resolvedArgs,
			"respArgs", respArgs,
		)
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
			normalizeSetResult(extra.Name, extra.Args)
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

	respBytes, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	slog.Debug("JMAP Response Body", "remote", r.RemoteAddr, "payload", string(respBytes))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBytes)
}

// normalizeSetResult applies RFC 8620 Section 5.3 to every set-family method response:
// the six set result properties (created, updated, destroyed, notCreated, notUpdated,
// notDestroyed) are typed "Id[...]|null" (or "Id[]|null" for destroyed) and MUST
// serialize as JSON null when empty, never as "{}"/"[]". Clients such as Bulwark
// webmail treat a truthy empty object as an error marker.
func normalizeSetResult(respName string, args map[string]any) {
	inSet := strings.HasSuffix(respName, "/set") ||
		strings.HasSuffix(respName, "/copy")
	if !inSet {
		return
	}
	for _, k := range []string{"created", "updated", "destroyed", "notCreated", "notUpdated", "notDestroyed"} {
		if v, ok := args[k]; ok {
			args[k] = nilIfEmpty(v)
		}
	}
}

// coreLimits returns the CoreCapability limits for the request or server defaults.
func (s *Server) coreLimits(r *http.Request) CoreCapability {
	var sess *Session
	if r != nil {
		sess = s.sessionForRequest(r)
	}
	if sess == nil {
		sess = s.Session
	}
	if sess != nil {
		if cc, ok := sess.Capabilities[CoreCapabilityURI].(CoreCapability); ok {
			return cc
		}
	}
	return CoreCapability{
		MaxSizeUpload:         DefaultMaxSizeUpload,
		MaxConcurrentUpload:   DefaultMaxConcurrentUpload,
		MaxSizeRequest:        DefaultMaxSizeRequest,
		MaxConcurrentRequests: DefaultMaxConcurrentRequests,
		MaxCallsInRequest:     DefaultMaxCallsInRequest,
		MaxObjectsInGet:       DefaultMaxObjectsInGet,
		MaxObjectsInSet:       DefaultMaxObjectsInSet,
		CollationAlgorithms:   []string{"i;ascii-casemap", "i;octet"},
	}
}

// resolveResultReferences replaces every result-reference argument (an argument whose name is
// prefixed with "#", per RFC 8620 Section 3.7) with the value obtained from the response of an
// earlier method call in the same request, recursively resolving references in nested objects,
// arrays, and patches.
func (s *Server) resolveResultReferences(args map[string]any, executed map[string]Invocation) (map[string]any, string, string) {
	if args == nil {
		return make(map[string]any), "", ""
	}

	res, errType, errDetail := s.resolveValueResultReferences(args, executed, true)
	if errDetail != "" {
		return nil, errType, errDetail
	}

	if m, ok := res.(map[string]any); ok {
		return m, "", ""
	}
	return make(map[string]any), "", ""
}

func (s *Server) resolveValueResultReferences(val any, executed map[string]Invocation, isTopLevelArgs bool) (any, string, string) {
	switch v := val.(type) {
	case map[string]any:
		resolved := make(map[string]any, len(v))
		for k, item := range v {
			if strings.HasPrefix(k, "#") {
				refMap, isRef := item.(map[string]any)
				if isTopLevelArgs || (isRef && IsResultReference(refMap)) {
					name := strings.TrimPrefix(k, "#")
					if _, dup := v[name]; dup {
						return nil, MethodErrorInvalidArguments,
							fmt.Sprintf("Argument %q is given in both normal and referenced form", name)
					}

					if !isRef || !IsResultReference(refMap) {
						return nil, MethodErrorInvalidResultReference,
							fmt.Sprintf("Argument %q is not a valid ResultReference object", k)
					}

					resolvedVal, refErr := s.resolveResultReference(refMap, executed)
					if refErr != "" {
						return nil, MethodErrorInvalidResultReference, refErr
					}

					// RFC 8620 Section 3.7: If the result reference evaluates to a single value and the argument
					// expects an Array (e.g. "ids", "emailIds", "destroy", "properties"), convert to a 1-element array.
					if isTopLevelArgs {
						if _, isSlice := resolvedVal.([]any); !isSlice && resolvedVal != nil {
							if name == "ids" || name == "emailIds" || name == "mailboxIds" || name == "threadIds" || name == "destroy" || name == "typeNames" || name == "properties" {
								resolvedVal = []any{resolvedVal}
							}
						}
					}

					resolved[name] = resolvedVal
					continue
				}
			}

			// Not a result reference: recurse into nested value
			childVal, errType, errDetail := s.resolveValueResultReferences(item, executed, false)
			if errDetail != "" {
				return nil, errType, errDetail
			}
			resolved[k] = childVal
		}
		return resolved, "", ""

	case []any:
		resolved := make([]any, len(v))
		for i, item := range v {
			childVal, errType, errDetail := s.resolveValueResultReferences(item, executed, false)
			if errDetail != "" {
				return nil, errType, errDetail
			}
			resolved[i] = childVal
		}
		return resolved, "", ""

	default:
		return v, "", ""
	}
}

func (s *Server) resolveResultReference(m map[string]any, executed map[string]Invocation) (any, string) {
	resultOf, _ := m["resultOf"].(string)
	reqName, _ := m["name"].(string)
	path, _ := m["path"].(string)

	if resultOf == "" || reqName == "" || path == "" {
		return nil, "resultOf, name, and path must be non-empty strings in ResultReference"
	}

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

func (s *Server) writeRequestError(w http.ResponseWriter, status int, errType string, detail string, limit ...string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	errObj := RequestError{
		Type:   errType,
		Status: status,
		Detail: detail,
	}
	if len(limit) > 0 && limit[0] != "" {
		errObj.Limit = limit[0]
	}
	_ = json.NewEncoder(w).Encode(errObj)
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
