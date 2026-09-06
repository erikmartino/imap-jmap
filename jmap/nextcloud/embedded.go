package nextcloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/emersion/go-webdav/carddav"
	xwebdav "golang.org/x/net/webdav"
)

type userContextKey struct{}

func withUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

func userFromCtx(ctx context.Context) string {
	if u, ok := ctx.Value(userContextKey{}).(string); ok && u != "" {
		return u
	}
	return "user@example.com"
}

// memCalDAVBackend implements caldav.Backend in memory per user.
type memCalDAVBackend struct {
	mu        sync.RWMutex
	calendars map[string]map[string]*caldav.Calendar
	objects   map[string]map[string]*caldav.CalendarObject
}

func newMemCalDAVBackend() *memCalDAVBackend {
	return &memCalDAVBackend{
		calendars: make(map[string]map[string]*caldav.Calendar),
		objects:   make(map[string]map[string]*caldav.CalendarObject),
	}
}

func (b *memCalDAVBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return "/remote.php/dav/principals/users/" + userFromCtx(ctx) + "/", nil
}

func (b *memCalDAVBackend) CalendarHomeSetPath(ctx context.Context) (string, error) {
	return "/remote.php/dav/calendars/" + userFromCtx(ctx) + "/", nil
}

func (b *memCalDAVBackend) ensureUserCalendarsLocked(u string) {
	if b.calendars[u] == nil {
		b.calendars[u] = make(map[string]*caldav.Calendar)
		b.objects[u] = make(map[string]*caldav.CalendarObject)
		defaultPath := "/remote.php/dav/calendars/" + u + "/personal/"
		b.calendars[u][defaultPath] = &caldav.Calendar{
			Path:                  defaultPath,
			Name:                  "Personal Calendar",
			Description:           "Default personal calendar",
			SupportedComponentSet: []string{"VEVENT", "VTODO", "VJOURNAL"},
		}
	}
}

func (b *memCalDAVBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserCalendarsLocked(u)

	var list []caldav.Calendar
	for _, c := range b.calendars[u] {
		list = append(list, *c)
	}
	return list, nil
}

func (b *memCalDAVBackend) GetCalendar(ctx context.Context, p string) (*caldav.Calendar, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserCalendarsLocked(u)

	cleanPath := strings.TrimRight(p, "/") + "/"
	if c, ok := b.calendars[u][cleanPath]; ok {
		return c, nil
	}
	return nil, webdav.NewHTTPError(http.StatusNotFound, errors.New("calendar not found"))
}

func (b *memCalDAVBackend) CreateCalendar(ctx context.Context, calendar *caldav.Calendar) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserCalendarsLocked(u)

	cleanPath := strings.TrimRight(calendar.Path, "/") + "/"
	calendar.Path = cleanPath
	b.calendars[u][cleanPath] = calendar
	return nil
}

func (b *memCalDAVBackend) DeleteCalendar(ctx context.Context, p string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserCalendarsLocked(u)

	cleanPath := strings.TrimRight(p, "/") + "/"
	delete(b.calendars[u], cleanPath)
	for objPath := range b.objects[u] {
		if strings.HasPrefix(objPath, cleanPath) {
			delete(b.objects[u], objPath)
		}
	}
	return nil
}

func (b *memCalDAVBackend) PutCalendarObject(ctx context.Context, p string, calendar *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserCalendarsLocked(u)

	parentPath := path.Dir(p) + "/"
	if _, ok := b.calendars[u][parentPath]; !ok {
		return nil, webdav.NewHTTPError(http.StatusNotFound, errors.New("parent calendar does not exist"))
	}

	etag := fmt.Sprintf("\"%d\"", time.Now().UnixNano())
	co := &caldav.CalendarObject{
		Path:    p,
		ModTime: time.Now(),
		ETag:    etag,
		Data:    calendar,
	}
	b.objects[u][p] = co
	return co, nil
}

func (b *memCalDAVBackend) GetCalendarObject(ctx context.Context, p string, req *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	u := userFromCtx(ctx)
	if objs, ok := b.objects[u]; ok {
		if co, exists := objs[p]; exists {
			return co, nil
		}
	}
	return nil, webdav.NewHTTPError(http.StatusNotFound, errors.New("calendar object not found"))
}

func (b *memCalDAVBackend) ListCalendarObjects(ctx context.Context, p string, req *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserCalendarsLocked(u)

	calPrefix := strings.TrimRight(p, "/") + "/"
	var list []caldav.CalendarObject
	for objPath, co := range b.objects[u] {
		if strings.HasPrefix(objPath, calPrefix) {
			list = append(list, *co)
		}
	}
	return list, nil
}

func (b *memCalDAVBackend) QueryCalendarObjects(ctx context.Context, p string, query *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	cos, err := b.ListCalendarObjects(ctx, p, nil)
	if err != nil {
		return nil, err
	}
	return caldav.Filter(query, cos)
}

func (b *memCalDAVBackend) DeleteCalendarObject(ctx context.Context, p string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	if objs, ok := b.objects[u]; ok {
		delete(objs, p)
	}
	return nil
}

// memCardDAVBackend implements carddav.Backend in memory per user.
type memCardDAVBackend struct {
	mu           sync.RWMutex
	addressBooks map[string]map[string]*carddav.AddressBook
	objects      map[string]map[string]*carddav.AddressObject
}

func newMemCardDAVBackend() *memCardDAVBackend {
	return &memCardDAVBackend{
		addressBooks: make(map[string]map[string]*carddav.AddressBook),
		objects:      make(map[string]map[string]*carddav.AddressObject),
	}
}

func (b *memCardDAVBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return "/remote.php/dav/principals/users/" + userFromCtx(ctx) + "/", nil
}

func (b *memCardDAVBackend) AddressBookHomeSetPath(ctx context.Context) (string, error) {
	return "/remote.php/dav/addressbooks/users/" + userFromCtx(ctx) + "/", nil
}

func (b *memCardDAVBackend) ensureUserAddressBooksLocked(u string) {
	if b.addressBooks[u] == nil {
		b.addressBooks[u] = make(map[string]*carddav.AddressBook)
		b.objects[u] = make(map[string]*carddav.AddressObject)
		defaultPath := "/remote.php/dav/addressbooks/users/" + u + "/contacts/"
		b.addressBooks[u][defaultPath] = &carddav.AddressBook{
			Path:        defaultPath,
			Name:        "Contacts",
			Description: "Default address book",
		}
	}
}

func (b *memCardDAVBackend) ListAddressBooks(ctx context.Context) ([]carddav.AddressBook, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserAddressBooksLocked(u)

	var list []carddav.AddressBook
	for _, ab := range b.addressBooks[u] {
		list = append(list, *ab)
	}
	return list, nil
}

func (b *memCardDAVBackend) GetAddressBook(ctx context.Context, p string) (*carddav.AddressBook, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserAddressBooksLocked(u)

	cleanPath := strings.TrimRight(p, "/") + "/"
	if ab, ok := b.addressBooks[u][cleanPath]; ok {
		return ab, nil
	}
	return nil, webdav.NewHTTPError(http.StatusNotFound, errors.New("address book not found"))
}

func (b *memCardDAVBackend) CreateAddressBook(ctx context.Context, addressBook *carddav.AddressBook) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserAddressBooksLocked(u)

	cleanPath := strings.TrimRight(addressBook.Path, "/") + "/"
	addressBook.Path = cleanPath
	b.addressBooks[u][cleanPath] = addressBook
	return nil
}

func (b *memCardDAVBackend) DeleteAddressBook(ctx context.Context, p string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserAddressBooksLocked(u)

	cleanPath := strings.TrimRight(p, "/") + "/"
	delete(b.addressBooks[u], cleanPath)
	for objPath := range b.objects[u] {
		if strings.HasPrefix(objPath, cleanPath) {
			delete(b.objects[u], objPath)
		}
	}
	return nil
}

func (b *memCardDAVBackend) PutAddressObject(ctx context.Context, p string, card vcard.Card, opts *carddav.PutAddressObjectOptions) (*carddav.AddressObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserAddressBooksLocked(u)

	etag := fmt.Sprintf("\"%d\"", time.Now().UnixNano())
	ao := &carddav.AddressObject{
		Path:    p,
		ModTime: time.Now(),
		ETag:    etag,
		Card:    card,
	}
	b.objects[u][p] = ao
	return ao, nil
}

func (b *memCardDAVBackend) GetAddressObject(ctx context.Context, p string, req *carddav.AddressDataRequest) (*carddav.AddressObject, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	u := userFromCtx(ctx)
	if objs, ok := b.objects[u]; ok {
		if ao, exists := objs[p]; exists {
			return ao, nil
		}
	}
	return nil, webdav.NewHTTPError(http.StatusNotFound, errors.New("address object not found"))
}

func (b *memCardDAVBackend) ListAddressObjects(ctx context.Context, p string, req *carddav.AddressDataRequest) ([]carddav.AddressObject, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	b.ensureUserAddressBooksLocked(u)

	abPrefix := strings.TrimRight(p, "/") + "/"
	var list []carddav.AddressObject
	for objPath, ao := range b.objects[u] {
		if strings.HasPrefix(objPath, abPrefix) {
			list = append(list, *ao)
		}
	}
	return list, nil
}

func (b *memCardDAVBackend) QueryAddressObjects(ctx context.Context, p string, query *carddav.AddressBookQuery) ([]carddav.AddressObject, error) {
	aos, err := b.ListAddressObjects(ctx, p, nil)
	if err != nil {
		return nil, err
	}
	return carddav.Filter(query, aos)
}

func (b *memCardDAVBackend) DeleteAddressObject(ctx context.Context, p string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := userFromCtx(ctx)
	if objs, ok := b.objects[u]; ok {
		delete(objs, p)
	}
	return nil
}

// memOCSStore stores Nextcloud users and groups in memory.
type memOCSStore struct {
	mu     sync.RWMutex
	users  map[string]*UserDetails
	groups map[string]map[string]bool // group -> set of userids
}

func newMemOCSStore() *memOCSStore {
	s := &memOCSStore{
		users:  make(map[string]*UserDetails),
		groups: make(map[string]map[string]bool),
	}
	s.groups["team"] = make(map[string]bool)
	s.groups["all"] = make(map[string]bool)
	return s
}

func (s *memOCSStore) AddUser(userid, password, email, displayname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if displayname == "" {
		displayname = userid
	}
	s.users[userid] = &UserDetails{
		ID:          userid,
		DisplayName: displayname,
		Email:       email,
		Groups:      []string{},
		Enabled:     true,
	}
}

func (s *memOCSStore) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	p := strings.TrimPrefix(r.URL.Path, "/ocs/v1.php/cloud/")

	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case p == "users" && r.Method == http.MethodPost:
		_ = r.ParseForm()
		uid := r.FormValue("userid")
		pw := r.FormValue("password")
		email := r.FormValue("email")
		s.users[uid] = &UserDetails{
			ID:          uid,
			DisplayName: uid,
			Email:       email,
			Groups:      []string{},
			Enabled:     true,
		}
		_ = pw
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ocs": map[string]any{
				"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
				"data": map[string]any{"id": uid},
			},
		})
	case p == "users" && r.Method == http.MethodGet:
		var uids []string
		for uid := range s.users {
			uids = append(uids, uid)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ocs": map[string]any{
				"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
				"data": map[string]any{"users": uids},
			},
		})
	case strings.HasPrefix(p, "users/"):
		rest := strings.TrimPrefix(p, "users/")
		parts := strings.Split(rest, "/")
		uid := parts[0]
		if len(parts) == 1 {
			if r.Method == http.MethodGet {
				u, ok := s.users[uid]
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"ocs": map[string]any{
							"meta": map[string]any{"status": "failure", "statuscode": 404, "message": "User not found"},
						},
					})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ocs": map[string]any{
						"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
						"data": u,
					},
				})
				return
			} else if r.Method == http.MethodPut || r.Method == http.MethodPost {
				_ = r.ParseForm()
				if u, ok := s.users[uid]; ok {
					if r.FormValue("key") == "displayname" {
						u.DisplayName = r.FormValue("value")
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ocs": map[string]any{
						"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
						"data": map[string]any{},
					},
				})
				return
			}
		} else if len(parts) == 2 && parts[1] == "groups" && r.Method == http.MethodPost {
			_ = r.ParseForm()
			group := r.FormValue("groupid")
			if s.groups[group] == nil {
				s.groups[group] = make(map[string]bool)
			}
			s.groups[group][uid] = true
			if u, ok := s.users[uid]; ok {
				u.Groups = append(u.Groups, group)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ocs": map[string]any{
					"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
					"data": map[string]any{},
				},
			})
			return
		}
	case p == "groups" && r.Method == http.MethodGet:
		var grps []string
		for g := range s.groups {
			grps = append(grps, g)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ocs": map[string]any{
				"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
				"data": map[string]any{"groups": grps},
			},
		})
	case p == "groups" && r.Method == http.MethodPost:
		_ = r.ParseForm()
		gid := r.FormValue("groupid")
		if s.groups[gid] == nil {
			s.groups[gid] = make(map[string]bool)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ocs": map[string]any{
				"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
				"data": map[string]any{},
			},
		})
	case strings.HasPrefix(p, "groups/"):
		gid := strings.TrimPrefix(p, "groups/")
		var members []string
		if s.groups[gid] != nil {
			for u := range s.groups[gid] {
				members = append(members, u)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ocs": map[string]any{
				"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
				"data": map[string]any{"users": members},
			},
		})
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ocs": map[string]any{
				"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
				"data": map[string]any{},
			},
		})
	}
}

// NewEmbeddedServer creates an in-process Nextcloud-compatible HTTP server
// supporting CalDAV, CardDAV, WebDAV, and OCS APIs.
func NewEmbeddedServer(usernames ...string) (*httptest.Server, *Client, func()) {
	if len(usernames) == 0 {
		usernames = []string{"user@example.com"}
	}

	calMem := newMemCalDAVBackend()
	cardMem := newMemCardDAVBackend()
	ocsMem := newMemOCSStore()
	webdavFS := xwebdav.NewMemFS()
	webdavLS := xwebdav.NewMemLS()
	webdavH := &xwebdav.Handler{
		Prefix:     "/remote.php/webdav",
		FileSystem: webdavFS,
		LockSystem: webdavLS,
	}

	for _, u := range usernames {
		calMem.ensureUserCalendarsLocked(u)
		cardMem.ensureUserAddressBooksLocked(u)
		ocsMem.AddUser(u, u, u, u)
	}

	calH := &caldav.Handler{
		Backend: calMem,
		Prefix:  "/remote.php/dav",
	}

	cardH := &carddav.Handler{
		Backend: cardMem,
		Prefix:  "/remote.php/dav",
	}

	mux := http.NewServeMux()

	// 1. OCS API
	mux.HandleFunc("/ocs/v1.php/cloud/", ocsMem.HandleHTTP)
	mux.HandleFunc("/ocs/v2.php/apps/files_sharing/api/v1/shares", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ocs": map[string]any{
				"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
				"data": map[string]any{},
			},
		})
	})

	// 2. WebDAV Files
	mux.Handle("/remote.php/webdav/", webdavH)
	mux.Handle("/remote.php/webdav", webdavH)

	// 3. CalDAV & CardDAV & Principal Root Dispatcher
	davDispatcher := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := path.Clean(r.URL.Path)

		// Principal Discovery & Home Sets
		if reqPath == "/remote.php/dav" || reqPath == "/remote.php/dav/" || strings.HasPrefix(reqPath, "/remote.php/dav/principals") {
			u := userFromCtx(r.Context())
			webdav.ServePrincipal(w, r, &webdav.ServePrincipalOptions{
				CurrentUserPrincipalPath: "/remote.php/dav/principals/users/" + u + "/",
				HomeSets: []webdav.BackendSuppliedHomeSet{
					caldav.NewCalendarHomeSet("/remote.php/dav/calendars/" + u + "/"),
					carddav.NewAddressBookHomeSet("/remote.php/dav/addressbooks/users/" + u + "/"),
				},
			})
			return
		}

		// CalDAV Routing
		if strings.HasPrefix(reqPath, "/remote.php/dav/calendars") {
			calH.ServeHTTP(w, r)
			return
		}

		// CardDAV Routing
		if strings.HasPrefix(reqPath, "/remote.php/dav/addressbooks") {
			cardH.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})

	// Auth & Context Middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, ok := r.BasicAuth()
		if !ok || user == "" {
			user = usernames[0]
		}
		// Dynamic auto-provisioning: first-time user gets initialized
		calMem.mu.Lock()
		calMem.ensureUserCalendarsLocked(user)
		calMem.mu.Unlock()

		cardMem.mu.Lock()
		cardMem.ensureUserAddressBooksLocked(user)
		cardMem.mu.Unlock()

		ocsMem.mu.Lock()
		if _, exists := ocsMem.users[user]; !exists {
			ocsMem.users[user] = &UserDetails{
				ID:          user,
				DisplayName: user,
				Email:       user,
				Groups:      []string{},
				Enabled:     true,
			}
		}
		ocsMem.mu.Unlock()

		ctx := withUser(r.Context(), user)
		davDispatcher.ServeHTTP(w, r.WithContext(ctx))
	})

	mux.Handle("/remote.php/dav/", handler)
	mux.Handle("/remote.php/dav", handler)

	// Status endpoint
	mux.HandleFunc("/status.php", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"installed":true,"maintenance":false,"needsDbUpgrade":false,"version":"34.0.0.0","versionstring":"34.0.0"}`))
	})

	ts := httptest.NewServer(mux)
	client := NewClient(ts.URL)

	cleanup := func() {
		ts.Close()
	}

	return ts, client, cleanup
}

// NewEmbeddedBackend initializes production Nextcloud backends running against an in-process Nextcloud server.
func NewEmbeddedBackend(usernames ...string) (*Client, *CalendarsBackend, *ContactsBackend, *FileNodeBackend, *PrincipalsBackend, func()) {
	_, client, cleanup := NewEmbeddedServer(usernames...)

	calBackend := NewCalendarsBackend(client)
	contactsBackend := NewContactsBackend(client)
	fileNodeBackend := NewFileNodeBackend(client)
	principalsBackend := NewPrincipalsBackend(client, calBackend)

	return client, calBackend, contactsBackend, fileNodeBackend, principalsBackend, cleanup
}
