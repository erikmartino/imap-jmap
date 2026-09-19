package nextcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/emersion/go-webdav/carddav"

	"imap-jmap/jmap/jmapauth"
)

type retryTransport struct {
	base http.RoundTripper
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
	}

	for attempt := 0; attempt < 8; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(100*attempt) * time.Millisecond)
		}

		reqCopy := req.Clone(req.Context())
		if bodyBytes != nil {
			reqCopy.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		base := t.base
		if base == nil {
			base = http.DefaultTransport
		}

		resp, err = base.RoundTrip(reqCopy)
		if err == nil && resp != nil {
			if resp.StatusCode != http.StatusInternalServerError && resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != 423 && resp.StatusCode != 429 {
				return resp, nil
			}
			if attempt < 7 {
				resp.Body.Close()
				continue
			}
			return resp, nil
		}
	}
	return resp, err
}

// Client provides typed CalDAV, CardDAV, and WebDAV clients using github.com/emersion/go-webdav.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client

	mu       sync.RWMutex
	homeSets map[string]string
	calPaths map[string]map[string]string
}

// NewClient creates a new Nextcloud client helper.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Transport: &retryTransport{base: http.DefaultTransport},
			Timeout:   15 * time.Second,
		},
		homeSets: make(map[string]string),
		calPaths: make(map[string]map[string]string),
	}
}

func (c *Client) getUserAndPass(ctx context.Context) (string, string) {
	accountID, hasAccount := jmapauth.AccountIDFromContext(ctx)
	creds, hasCreds := jmapauth.CredentialsFromContext(ctx)

	if hasAccount && accountID != "" {
		if subj, okSub := jmapauth.SubjectForAccountID(accountID); okSub && subj != "" {
			if !hasCreds || creds.Username == "" || creds.Username != subj {
				return subj, subj
			}
		}
	}

	if hasCreds && creds.Username != "" {
		return creds.Username, creds.Password
	}

	subject, ok := jmapauth.SubjectFromContext(ctx)
	if ok && subject != "" {
		return subject, subject
	}

	if hasAccount && accountID != "" {
		if subj, okSub := jmapauth.SubjectForAccountID(accountID); okSub && subj != "" {
			return subj, subj
		}
		return accountID, accountID
	}

	return "", ""
}

// CalDAV returns an authenticated caldav.Client from github.com/emersion/go-webdav/caldav.
func (c *Client) CalDAV(ctx context.Context) (*caldav.Client, string, error) {
	user, pass := c.getUserAndPass(ctx)
	hc := webdav.HTTPClientWithBasicAuth(c.HTTPClient, user, pass)
	endpoint := c.BaseURL + "/remote.php/dav/"
	client, err := caldav.NewClient(hc, endpoint)
	if err != nil {
		return nil, "", err
	}
	return client, user, nil
}

type propfindScheduleReq struct {
	XMLName xml.Name             `xml:"DAV: propfind"`
	Prop    propfindScheduleProp `xml:"prop"`
}

type propfindScheduleProp struct {
	ScheduleDefaultCalendarURL *struct{} `xml:"urn:ietf:params:xml:ns:caldav schedule-default-calendar-URL,omitempty"`
	ScheduleInboxURL           *struct{} `xml:"urn:ietf:params:xml:ns:caldav schedule-inbox-URL,omitempty"`
}

// FindScheduleDefaultCalendar finds the default calendar collection for scheduling
// per RFC 6638 Section 9.2.1 by querying the user's principal resource or scheduling inbox.
func (c *Client) FindScheduleDefaultCalendar(ctx context.Context, principal string) string {
	if principal == "" {
		return ""
	}
	user, pass := c.getUserAndPass(ctx)
	if user == "" {
		return ""
	}
	urlStr := c.BaseURL + principal
	if !strings.HasPrefix(principal, "/") {
		urlStr = c.BaseURL + "/" + principal
	}
	reqData, err := xml.Marshal(&propfindScheduleReq{
		Prop: propfindScheduleProp{
			ScheduleDefaultCalendarURL: &struct{}{},
			ScheduleInboxURL:           &struct{}{},
		},
	})
	if err != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", urlStr, bytes.NewReader(reqData))
	if err != nil {
		return ""
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Depth", "0")
	req.Header.Set("Content-Type", "application/xml")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ""
	}

	var multi struct {
		XMLName  xml.Name `xml:"multistatus"`
		Response struct {
			Propstat []struct {
				Prop struct {
					ScheduleDefaultCalendarURL struct {
						Href string `xml:"href"`
					} `xml:"schedule-default-calendar-URL"`
					ScheduleInboxURL struct {
						Href string `xml:"href"`
					} `xml:"schedule-inbox-URL"`
				} `xml:"prop"`
				Status string `xml:"status"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&multi); err != nil {
		return ""
	}
	var inboxURL string
	for _, ps := range multi.Response.Propstat {
		if strings.Contains(ps.Status, "200") {
			if ps.Prop.ScheduleDefaultCalendarURL.Href != "" {
				return path.Base(strings.TrimRight(ps.Prop.ScheduleDefaultCalendarURL.Href, "/"))
			}
			if ps.Prop.ScheduleInboxURL.Href != "" {
				inboxURL = ps.Prop.ScheduleInboxURL.Href
			}
		}
	}
	if inboxURL != "" && inboxURL != principal {
		return c.FindScheduleDefaultCalendar(ctx, inboxURL)
	}
	return ""
}

// CalendarInfo represents calendar collection metadata retrieved from Nextcloud.
type CalendarInfo struct {
	ID          string
	Name        string
	Description string
	IsDefault   bool
}

// CalendarObjectInfo represents a calendar object (event/todo) retrieved from Nextcloud.
type CalendarObjectInfo struct {
	ID      string
	Data    *ical.Calendar
	ModTime time.Time
	ETag    string
}

func (c *Client) getCalendarHomeSet(ctx context.Context, calClient *caldav.Client, u string) string {
	c.mu.RLock()
	if hs, ok := c.homeSets[u]; ok && hs != "" {
		c.mu.RUnlock()
		return hs
	}
	c.mu.RUnlock()

	principal, err := calClient.FindCurrentUserPrincipal(ctx)
	if err == nil && principal != "" {
		homeSet, err := calClient.FindCalendarHomeSet(ctx, principal)
		if err == nil && homeSet != "" {
			c.mu.Lock()
			c.homeSets[u] = homeSet
			c.mu.Unlock()
			return homeSet
		}
	}
	defaultHS := "calendars/" + u + "/"
	c.mu.Lock()
	c.homeSets[u] = defaultHS
	c.mu.Unlock()
	return defaultHS
}

func (c *Client) getCalPath(ctx context.Context, calClient *caldav.Client, u, calID string) string {
	c.mu.RLock()
	if c.calPaths[u] != nil {
		if p, ok := c.calPaths[u][calID]; ok && p != "" {
			c.mu.RUnlock()
			return p
		}
	}
	c.mu.RUnlock()

	homeSet := c.getCalendarHomeSet(ctx, calClient, u)
	if homeSet != "" {
		return strings.TrimRight(homeSet, "/") + "/" + calID + "/"
	}
	return "calendars/" + u + "/" + calID + "/"
}

// ListCalendars retrieves all calendars for the authenticated user from Nextcloud.
func (c *Client) ListCalendars(ctx context.Context) ([]*CalendarInfo, string, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, "", err
	}
	homeSet := c.getCalendarHomeSet(ctx, calClient, u)
	calList, err := calClient.FindCalendars(ctx, homeSet)
	if err != nil {
		return nil, u, err
	}

	principal, _ := calClient.FindCurrentUserPrincipal(ctx)
	scheduleDefaultCalID := c.FindScheduleDefaultCalendar(ctx, principal)

	var list []*CalendarInfo
	c.mu.Lock()
	if c.calPaths[u] == nil {
		c.calPaths[u] = make(map[string]string)
	}
	for _, cal := range calList {
		calID := path.Base(strings.TrimRight(cal.Path, "/"))
		if calID == "inbox" || calID == "outbox" || calID == "trashbin" {
			continue
		}
		c.calPaths[u][calID] = cal.Path
		name := cal.Name
		if name == "" {
			name = calID
		}
		isDefault := false
		if scheduleDefaultCalID != "" {
			isDefault = (calID == scheduleDefaultCalID)
		} else if len(list) == 0 {
			isDefault = true
		}
		list = append(list, &CalendarInfo{
			ID:          calID,
			Name:        name,
			Description: cal.Description,
			IsDefault:   isDefault,
		})
	}
	c.mu.Unlock()
	return list, u, nil
}

// CreateCalendar creates a new calendar collection in Nextcloud.
func (c *Client) CreateCalendar(ctx context.Context, calID string) error {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return err
	}
	calPath := c.getCalPath(ctx, calClient, u, calID)
	return calClient.Mkdir(ctx, calPath)
}

// DeleteCalendar removes a calendar collection in Nextcloud.
func (c *Client) DeleteCalendar(ctx context.Context, calID string) error {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return err
	}
	calPath := c.getCalPath(ctx, calClient, u, calID)
	c.mu.Lock()
	if c.calPaths[u] != nil {
		delete(c.calPaths[u], calID)
	}
	c.mu.Unlock()
	return calClient.RemoveAll(ctx, calPath)
}

// QueryCalendarObjects queries all calendar objects in a Nextcloud calendar collection.
func (c *Client) QueryCalendarObjects(ctx context.Context, calID string) ([]*CalendarObjectInfo, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, err
	}
	calPath := c.getCalPath(ctx, calClient, u, calID)
	objs, err := calClient.QueryCalendar(ctx, calPath, &caldav.CalendarQuery{
		CompFilter: caldav.CompFilter{
			Name: "VCALENDAR",
		},
	})
	if err != nil {
		return nil, err
	}
	res := make([]*CalendarObjectInfo, len(objs))
	for i, obj := range objs {
		name := path.Base(obj.Path)
		eventID := strings.TrimSuffix(name, ".ics")
		res[i] = &CalendarObjectInfo{
			ID:      eventID,
			Data:    obj.Data,
			ModTime: obj.ModTime,
			ETag:    obj.ETag,
		}
	}
	return res, nil
}

// PutCalendarObject creates or replaces a calendar object in a Nextcloud calendar collection.
func (c *Client) PutCalendarObject(ctx context.Context, calID, eventID string, calObj *ical.Calendar) error {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return err
	}
	eventFilename := eventID
	if !strings.HasSuffix(eventID, ".ics") {
		eventFilename = eventID + ".ics"
	}
	eventPath := c.getCalPath(ctx, calClient, u, calID) + eventFilename
	_, err = calClient.PutCalendarObject(ctx, eventPath, calObj)
	return err
}

// DeleteCalendarObject deletes a calendar object from a Nextcloud calendar collection.
func (c *Client) DeleteCalendarObject(ctx context.Context, calID, eventID string) error {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return err
	}
	eventFilename := eventID
	if !strings.HasSuffix(eventID, ".ics") {
		eventFilename = eventID + ".ics"
	}
	eventPath := c.getCalPath(ctx, calClient, u, calID) + eventFilename
	return calClient.RemoveAll(ctx, eventPath)
}

// CardDAV returns an authenticated carddav.Client from github.com/emersion/go-webdav/carddav.
func (c *Client) CardDAV(ctx context.Context) (*carddav.Client, string, error) {
	user, pass := c.getUserAndPass(ctx)
	hc := webdav.HTTPClientWithBasicAuth(c.HTTPClient, user, pass)
	endpoint := c.BaseURL + "/remote.php/dav/"
	client, err := carddav.NewClient(hc, endpoint)
	if err != nil {
		return nil, "", err
	}
	return client, user, nil
}

// WebDAV returns an authenticated webdav.Client from github.com/emersion/go-webdav.
func (c *Client) WebDAV(ctx context.Context) (*webdav.Client, string, error) {
	user, pass := c.getUserAndPass(ctx)
	hc := webdav.HTTPClientWithBasicAuth(c.HTTPClient, user, pass)
	endpoint := c.BaseURL + "/remote.php/webdav/"
	client, err := webdav.NewClient(hc, endpoint)
	if err != nil {
		return nil, "", err
	}
	return client, user, nil
}

// UserDetails represents Nextcloud user details returned by OCS API.
type UserDetails struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayname"`
	Email       string   `json:"email"`
	Groups      []string `json:"groups"`
	Enabled     bool     `json:"enabled"`
}

type ocsDataUsers struct {
	Users []string `json:"users"`
}

type ocsDataGroups struct {
	Groups []string `json:"groups"`
}

type ocsDataGroupMembers struct {
	Users []string `json:"users"`
}

type ocsEnvelope[T any] struct {
	OCS struct {
		Meta struct {
			Status     string `json:"status"`
			StatusCode int    `json:"statuscode"`
			Message    string `json:"message"`
		} `json:"meta"`
		Data T `json:"data"`
	} `json:"ocs"`
}

// HasAdminAuth reports whether Nextcloud admin credentials are configured in the environment.
func (c *Client) HasAdminAuth() bool {
	return os.Getenv("NEXTCLOUD_ADMIN_USER") != "" && os.Getenv("NEXTCLOUD_ADMIN_PASSWORD") != ""
}

// AdminAuth returns admin credentials from environment, or false if not configured.
func (c *Client) AdminAuth() (string, string, bool) {
	adminUser := os.Getenv("NEXTCLOUD_ADMIN_USER")
	adminPass := os.Getenv("NEXTCLOUD_ADMIN_PASSWORD")
	if adminUser == "" || adminPass == "" {
		return "", "", false
	}
	return adminUser, adminPass, true
}

func (c *Client) adminRequest(ctx context.Context, method, endpoint string, body url.Values) ([]byte, error) {
	adminUser, adminPass, ok := c.AdminAuth()
	if !ok {
		return nil, fmt.Errorf("nextcloud admin credentials not configured")
	}
	reqURL := c.BaseURL + endpoint
	var reqBody io.Reader
	if body != nil {
		reqBody = strings.NewReader(body.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(adminUser, adminPass)
	req.Header.Set("OCS-APIRequest", "true")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return respBytes, fmt.Errorf("nextcloud OCS error (status %d): %s", resp.StatusCode, string(respBytes))
	}
	return respBytes, nil
}

// CreateUser provisions a user in Nextcloud.
func (c *Client) CreateUser(ctx context.Context, userid, password, email, displayname string) error {
	data := url.Values{
		"userid":   {userid},
		"password": {password},
		"email":    {email},
	}
	respBytes, err := c.adminRequest(ctx, http.MethodPost, "/ocs/v1.php/cloud/users", data)
	if err != nil {
		return err
	}
	var env ocsEnvelope[any]
	if jErr := json.Unmarshal(respBytes, &env); jErr == nil {
		// 100 = OK, 102 = user already exists
		if env.OCS.Meta.StatusCode != 100 && env.OCS.Meta.StatusCode != 102 {
			return fmt.Errorf("nextcloud create user failed: %s (code %d)", env.OCS.Meta.Message, env.OCS.Meta.StatusCode)
		}
	}
	if displayname != "" {
		_ = c.SetUserDisplayName(ctx, userid, displayname)
	}
	return nil
}

// SetUserDisplayName sets the display name for a Nextcloud user.
func (c *Client) SetUserDisplayName(ctx context.Context, userid, displayname string) error {
	endpoint := fmt.Sprintf("/ocs/v1.php/cloud/users/%s", url.PathEscape(userid))
	data := url.Values{
		"key":   {"displayname"},
		"value": {displayname},
	}
	_, err := c.adminRequest(ctx, http.MethodPut, endpoint, data)
	return err
}

// GetUsers lists all user IDs in Nextcloud.
func (c *Client) GetUsers(ctx context.Context) ([]string, error) {
	respBytes, err := c.adminRequest(ctx, http.MethodGet, "/ocs/v1.php/cloud/users?format=json", nil)
	if err != nil {
		return nil, err
	}
	var env ocsEnvelope[ocsDataUsers]
	if err := json.Unmarshal(respBytes, &env); err != nil {
		return nil, err
	}
	if env.OCS.Meta.StatusCode != 100 {
		return nil, fmt.Errorf("get users failed: %s (code %d)", env.OCS.Meta.Message, env.OCS.Meta.StatusCode)
	}
	return env.OCS.Data.Users, nil
}

// GetUserDetails gets user details from Nextcloud.
func (c *Client) GetUserDetails(ctx context.Context, userid string) (*UserDetails, error) {
	endpoint := fmt.Sprintf("/ocs/v1.php/cloud/users/%s?format=json", url.PathEscape(userid))
	respBytes, err := c.adminRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var env ocsEnvelope[UserDetails]
	if err := json.Unmarshal(respBytes, &env); err != nil {
		return nil, err
	}
	if env.OCS.Meta.StatusCode != 100 {
		return nil, fmt.Errorf("user %q not found or error: %s (code %d)", userid, env.OCS.Meta.Message, env.OCS.Meta.StatusCode)
	}
	return &env.OCS.Data, nil
}

// CreateGroup creates a group in Nextcloud.
func (c *Client) CreateGroup(ctx context.Context, groupid string) error {
	data := url.Values{"groupid": {groupid}}
	respBytes, err := c.adminRequest(ctx, http.MethodPost, "/ocs/v1.php/cloud/groups", data)
	if err != nil {
		return err
	}
	var env ocsEnvelope[any]
	if jErr := json.Unmarshal(respBytes, &env); jErr == nil {
		// 100 = OK, 102 = group already exists
		if env.OCS.Meta.StatusCode != 100 && env.OCS.Meta.StatusCode != 102 {
			return fmt.Errorf("create group failed: %s (code %d)", env.OCS.Meta.Message, env.OCS.Meta.StatusCode)
		}
	}
	return nil
}

// GetGroups lists all groups in Nextcloud.
func (c *Client) GetGroups(ctx context.Context) ([]string, error) {
	respBytes, err := c.adminRequest(ctx, http.MethodGet, "/ocs/v1.php/cloud/groups?format=json", nil)
	if err != nil {
		return nil, err
	}
	var env ocsEnvelope[ocsDataGroups]
	if err := json.Unmarshal(respBytes, &env); err != nil {
		return nil, err
	}
	if env.OCS.Meta.StatusCode != 100 {
		return nil, fmt.Errorf("get groups failed: %s (code %d)", env.OCS.Meta.Message, env.OCS.Meta.StatusCode)
	}
	return env.OCS.Data.Groups, nil
}

// GetGroupMembers lists members in a group in Nextcloud.
func (c *Client) GetGroupMembers(ctx context.Context, groupid string) ([]string, error) {
	endpoint := fmt.Sprintf("/ocs/v1.php/cloud/groups/%s/users?format=json", url.PathEscape(groupid))
	respBytes, err := c.adminRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var env ocsEnvelope[ocsDataGroupMembers]
	if err := json.Unmarshal(respBytes, &env); err != nil {
		return nil, err
	}
	if env.OCS.Meta.StatusCode != 100 {
		return nil, fmt.Errorf("get group members failed: %s (code %d)", env.OCS.Meta.Message, env.OCS.Meta.StatusCode)
	}
	return env.OCS.Data.Users, nil
}

// AddUserToGroup adds a user to a group in Nextcloud.
func (c *Client) AddUserToGroup(ctx context.Context, userid, groupid string) error {
	endpoint := fmt.Sprintf("/ocs/v1.php/cloud/users/%s/groups", url.PathEscape(userid))
	data := url.Values{"groupid": {groupid}}
	respBytes, err := c.adminRequest(ctx, http.MethodPost, endpoint, data)
	if err != nil {
		return err
	}
	var env ocsEnvelope[any]
	if jErr := json.Unmarshal(respBytes, &env); jErr == nil {
		// 100 = OK, 102 = already in group
		if env.OCS.Meta.StatusCode != 100 && env.OCS.Meta.StatusCode != 102 {
			return fmt.Errorf("add user to group failed: %s (code %d)", env.OCS.Meta.Message, env.OCS.Meta.StatusCode)
		}
	}
	return nil
}

// EnsureGroup creates a group in Nextcloud if it does not exist.
func (c *Client) EnsureGroup(ctx context.Context, groupid string) error {
	groups, err := c.GetGroups(ctx)
	if err == nil {
		for _, g := range groups {
			if strings.EqualFold(g, groupid) {
				return nil
			}
		}
	}
	return c.CreateGroup(ctx, groupid)
}

// EnsureUserInTeam ensures a user exists in Nextcloud and is added to the "team" group.
func (c *Client) EnsureUserInTeam(ctx context.Context, userid, password, email, displayname string) error {
	if !c.HasAdminAuth() || userid == "" {
		return nil
	}
	if displayname == "" {
		displayname = userid
	}
	if email == "" {
		email = userid
	}
	if password == "" {
		password = userid
	}

	// Check if user exists
	details, err := c.GetUserDetails(ctx, userid)
	if err != nil || details == nil || details.ID == "" {
		_ = c.CreateUser(ctx, userid, password, email, displayname)
	} else if displayname != "" && (details.DisplayName == "" || details.DisplayName == userid) {
		_ = c.SetUserDisplayName(ctx, userid, displayname)
	}

	// Ensure "team" and "all" groups exist
	_ = c.EnsureGroup(ctx, "team")
	_ = c.EnsureGroup(ctx, "all")

	// Add user to "team" and "all"
	_ = c.AddUserToGroup(ctx, userid, "team")
	_ = c.AddUserToGroup(ctx, userid, "all")
	return nil
}
