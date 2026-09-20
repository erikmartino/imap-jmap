package nextcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapprincipals"

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
		Groups:      []string{"team", "all"},
		Enabled:     true,
	}
	if s.groups["team"] != nil {
		s.groups["team"][userid] = true
	}
	if s.groups["all"] != nil {
		s.groups["all"][userid] = true
	}
}

func (s *memOCSStore) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	p := strings.TrimPrefix(r.URL.Path, "/ocs/v1.php/cloud/")

	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case p == "user" && r.Method == http.MethodGet:
		authU, _, _ := r.BasicAuth()
		if authU == "" {
			authU = userFromCtx(r.Context())
		}
		u, ok := s.users[authU]
		if !ok {
			u = &UserDetails{
				ID:          authU,
				DisplayName: authU,
				Email:       authU,
				Groups:      []string{"team", "all"},
				Enabled:     true,
			}
			s.users[authU] = u
		}
		if s.groups["team"] != nil {
			s.groups["team"][authU] = true
		}
		if s.groups["all"] != nil {
			s.groups["all"][authU] = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ocs": map[string]any{
				"meta": map[string]any{"status": "ok", "statuscode": 100, "message": "OK"},
				"data": u,
			},
		})
	case p == "users" && r.Method == http.MethodPost:
		_ = r.ParseForm()
		uid := r.FormValue("userid")
		pw := r.FormValue("password")
		email := r.FormValue("email")
		s.users[uid] = &UserDetails{
			ID:          uid,
			DisplayName: uid,
			Email:       email,
			Groups:      []string{"team", "all"},
			Enabled:     true,
		}
		if s.groups["team"] != nil {
			s.groups["team"][uid] = true
		}
		if s.groups["all"] != nil {
			s.groups["all"][uid] = true
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
			if !IsValidGroupID(group) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ocs": map[string]any{
						"meta": map[string]any{"status": "failure", "statuscode": 400, "message": "Invalid group ID"},
					},
				})
				return
			}
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
		if !IsValidGroupID(gid) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ocs": map[string]any{
					"meta": map[string]any{"status": "failure", "statuscode": 400, "message": "Invalid group ID"},
				},
			})
			return
		}
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
		gid = strings.TrimSuffix(gid, "/users")
		gid = strings.TrimSuffix(gid, "/")
		if !IsValidGroupID(gid) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ocs": map[string]any{
					"meta": map[string]any{"status": "failure", "statuscode": 400, "message": "Invalid group ID"},
				},
			})
			return
		}
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

// memWebDAVStore manages per-user in-memory WebDAV file systems with Nextcloud initial test data.
type memWebDAVStore struct {
	mu     sync.RWMutex
	userFS map[string]xwebdav.FileSystem
	userLS map[string]xwebdav.LockSystem
}

func newMemWebDAVStore() *memWebDAVStore {
	return &memWebDAVStore{
		userFS: make(map[string]xwebdav.FileSystem),
		userLS: make(map[string]xwebdav.LockSystem),
	}
}

func seedUserWebDAVFiles(fs xwebdav.FileSystem) {
	ctx := context.Background()
	_ = fs.Mkdir(ctx, "Photos", 0755)
	if wc, err := fs.OpenFile(ctx, "welcome.txt", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil && wc != nil {
		_, _ = wc.Write([]byte("Welcome to Nextcloud on JMAP!"))
		_ = wc.Close()
	}
	if wc, err := fs.OpenFile(ctx, "Readme.md", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil && wc != nil {
		_, _ = wc.Write([]byte("# Welcome to Nextcloud\nThis is your personal cloud storage."))
		_ = wc.Close()
	}
	if wc, err := fs.OpenFile(ctx, "Photos/banner.jpg", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil && wc != nil {
		_, _ = wc.Write([]byte("Sample JPEG image data for Nextcloud Photos."))
		_ = wc.Close()
	}
}

func (s *memWebDAVStore) ensureUserLocked(u string) (xwebdav.FileSystem, xwebdav.LockSystem) {
	if s.userFS[u] == nil {
		fs := xwebdav.NewMemFS()
		ls := xwebdav.NewMemLS()
		seedUserWebDAVFiles(fs)
		s.userFS[u] = fs
		s.userLS[u] = ls
	}
	return s.userFS[u], s.userLS[u]
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
	davStore := newMemWebDAVStore()

	for _, u := range usernames {
		calMem.ensureUserCalendarsLocked(u)
		cardMem.ensureUserAddressBooksLocked(u)
		ocsMem.AddUser(u, u, u, u)
		davStore.mu.Lock()
		davStore.ensureUserLocked(u)
		davStore.mu.Unlock()
	}

	webdavH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _, ok := r.BasicAuth()
		if !ok || u == "" {
			u = userFromCtx(r.Context())
		}
		if u == "" && len(usernames) > 0 {
			u = usernames[0]
		}
		davStore.mu.Lock()
		fs, ls := davStore.ensureUserLocked(u)
		davStore.mu.Unlock()

		h := &xwebdav.Handler{
			Prefix:     "/remote.php/webdav",
			FileSystem: fs,
			LockSystem: ls,
		}
		h.ServeHTTP(w, r)
	})

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
			if r.Method == "PROPFIND" {
				bodyBytes, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				if bytes.Contains(bodyBytes, []byte("schedule-default-calendar-URL")) {
					defaultCalURL := "/remote.php/dav/calendars/" + u + "/personal/"
					type embeddedHref struct {
						Href string `xml:"href"`
					}
					type embeddedPropfindResp struct {
						XMLName  xml.Name `xml:"DAV: multistatus"`
						Response struct {
							Href     string `xml:"href"`
							Propstat struct {
								Prop struct {
									ScheduleDefaultCalendarURL embeddedHref `xml:"urn:ietf:params:xml:ns:caldav schedule-default-calendar-URL"`
									ScheduleInboxURL           embeddedHref `xml:"urn:ietf:params:xml:ns:caldav schedule-inbox-URL"`
								} `xml:"prop"`
								Status string `xml:"status"`
							} `xml:"propstat"`
						} `xml:"response"`
					}
					resp := embeddedPropfindResp{}
					resp.Response.Href = r.URL.Path
					resp.Response.Propstat.Status = "HTTP/1.1 200 OK"
					resp.Response.Propstat.Prop.ScheduleDefaultCalendarURL.Href = defaultCalURL
					resp.Response.Propstat.Prop.ScheduleInboxURL.Href = "/remote.php/dav/calendars/" + u + "/inbox/"
					w.Header().Set("Content-Type", "application/xml; charset=utf-8")
					w.WriteHeader(http.StatusMultiStatus)
					_ = xml.NewEncoder(w).Encode(resp)
					return
				}
			}
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
	blobBackend := NewBlobBackend(client, fileNodeBackend)
	fileNodeBackend.SetBlobBackend(blobBackend)
	principalsBackend := NewPrincipalsBackend(client, calBackend)
	calBackend.SetPrincipalsBackend(principalsBackend)

	for _, u := range usernames {
		id := jmapcore.Id("")
		if u == "user@example.com" {
			id = "p-primary"
		}
		displayName := u
		if u == "jdoe@example.com" {
			displayName = "John Doe"
		} else if u == "jane.smith@example.com" {
			displayName = "Jane Smith"
		} else if u == "user@example.com" {
			displayName = "User Example"
		}
		principalsBackend.SeedUser(id, u, displayName)
	}

	// Seed sample users and groups in the in-process test adapter for hermetic test execution
	sampleUsers := []struct {
		id, email, displayName string
	}{
		{"p-primary", "user@example.com", "User Example"},
		{"p-alice", "alice@example.com", "Alice Smith"},
		{"p-bob", "bob@example.com", "Bob Jones"},
		{"p-carol", "carol@example.com", "Carol Danvers"},
	}
	for _, su := range sampleUsers {
		principalsBackend.SeedUser(jmapcore.Id(su.id), su.email, su.displayName)
	}
	principalsBackend.SeedPrincipal(&jmapprincipals.Principal{
		ID:                 "p-team",
		Type:               "group",
		Name:               "Engineering Team",
		Email:              "team@example.com",
		CalendarAddress:    "mailto:team@example.com",
		MayGetAvailability: false,
		MayShareWith:       true,
		Members:            map[string]bool{"p-alice": true, "p-bob": true, "p-carol": true, "p-primary": true},
	})
	principalsBackend.SeedPrincipal(&jmapprincipals.Principal{
		ID:                 "p-all",
		Type:               "group",
		Name:               "All Staff",
		Email:              "all@example.com",
		CalendarAddress:    "mailto:all@example.com",
		MayGetAvailability: false,
		MayShareWith:       true,
		Members:            map[string]bool{"p-alice": true, "p-bob": true, "p-carol": true, "p-primary": true},
	})
	principalsBackend.SeedPrincipal(&jmapprincipals.Principal{
		ID:                 "p-marketing",
		Type:               "group",
		Name:               "Marketing",
		Email:              "marketing@example.com",
		CalendarAddress:    "mailto:marketing@example.com",
		MayGetAvailability: false,
		MayShareWith:       true,
		Members:            map[string]bool{"p-carol": true},
	})

	return client, calBackend, contactsBackend, fileNodeBackend, principalsBackend, cleanup
}

// NewEmbeddedBackendWithBlobs initializes production Nextcloud backends including BlobBackend.
func NewEmbeddedBackendWithBlobs(usernames ...string) (*Client, *CalendarsBackend, *ContactsBackend, *FileNodeBackend, *PrincipalsBackend, *BlobBackend, func()) {
	client, calBackend, contactsBackend, fileNodeBackend, principalsBackend, cleanup := NewEmbeddedBackend(usernames...)
	return client, calBackend, contactsBackend, fileNodeBackend, principalsBackend, fileNodeBackend.BlobBackend(), cleanup
}

// SeedDefaultWebDAVFiles seeds realistic sample files into Nextcloud WebDAV for an account.
func SeedDefaultWebDAVFiles(ctx context.Context, client *Client) error {
	fs, _, err := client.WebDAV(ctx)
	if err != nil {
		return err
	}
	_ = fs.Mkdir(ctx, "Photos")
	if wc, err := fs.Create(ctx, "welcome.txt"); err == nil && wc != nil {
		_, _ = wc.Write([]byte("Welcome to Nextcloud on JMAP!"))
		_ = wc.Close()
	}
	if wc, err := fs.Create(ctx, "Readme.md"); err == nil && wc != nil {
		_, _ = wc.Write([]byte("# Welcome to Nextcloud\nThis is your personal cloud storage."))
		_ = wc.Close()
	}
	if wc, err := fs.Create(ctx, "Photos/banner.jpg"); err == nil && wc != nil {
		_, _ = wc.Write([]byte("Sample JPEG image data for Nextcloud Photos."))
		_ = wc.Close()
	}
	return nil
}
