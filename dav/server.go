package dav

import (
	"net/http"

	"github.com/emersion/go-webdav/caldav"
	"github.com/emersion/go-webdav/carddav"

	"imap-jmap/dav/memory"
	"imap-jmap/jmap"
)

// Server wraps CalDAV and CardDAV WebDAV HTTP handlers.
type Server struct {
	CalDAVHandler  http.Handler
	CardDAVHandler http.Handler
}

// NewServer initializes CalDAV and CardDAV handlers linked to JMAP backends.
func NewServer(calBackend jmap.CalendarsBackend, contactsBackend jmap.ContactsBackend) *Server {
	calDAVBackend := memory.NewCalDAVBackend(calBackend)
	cardDAVBackend := memory.NewCardDAVBackend(contactsBackend)

	return &Server{
		CalDAVHandler: &caldav.Handler{
			Backend: calDAVBackend,
		},
		CardDAVHandler: &carddav.Handler{
			Backend: cardDAVBackend,
		},
	}
}

// Handler returns an http.Handler that routes /caldav/ and /carddav/ requests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/caldav/", s.CalDAVHandler)
	mux.Handle("/carddav/", s.CardDAVHandler)
	return mux
}
