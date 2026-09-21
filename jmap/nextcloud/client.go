package nextcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
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

	user, _, _ := req.BasicAuth()

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

		start := time.Now()
		resp, err = base.RoundTrip(reqCopy)
		duration := time.Since(start)

		status := 0
		if resp != nil {
			status = resp.StatusCode
		}

		slog.Info("Nextcloud/DAV request",
			"user", user,
			"method", req.Method,
			"path", req.URL.Path,
			"status", status,
			"duration_ms", duration.Milliseconds(),
			"attempt", attempt+1,
			"error", err,
		)

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
	discovery  discoveryCache
}

func isCacheDisabledEnv() bool {
	if isEnvTrue("ENABLE_CACHE") || isEnvTrue("ENABLE_CALENDAR_CACHE") {
		return false
	}
	if isEnvFalse("DISABLE_CACHE") || isEnvFalse("DISABLE_CALENDAR_CACHE") {
		return false
	}
	return true
}

func isEnvTrue(key string) bool {
	v := os.Getenv(key)
	return v == "1" || strings.EqualFold(v, "true")
}

func isEnvFalse(key string) bool {
	v := os.Getenv(key)
	return v == "0" || strings.EqualFold(v, "false")
}

// NewClient creates a new Nextcloud client helper with disabled dummy cache by default.
func NewClient(baseURL string) *Client {
	var disc discoveryCache = &dummyDiscoveryCache{}
	if !isCacheDisabledEnv() {
		disc = newMemDiscoveryCache()
	}

	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Transport: &retryTransport{base: http.DefaultTransport},
			Timeout:   15 * time.Second,
		},
		discovery: disc,
	}
}

// SetCacheDisabled enables or disables client-side caching of discovery paths and tokens.
func (c *Client) SetCacheDisabled(disabled bool) {
	if disabled {
		c.discovery = &dummyDiscoveryCache{}
	} else {
		c.discovery = newMemDiscoveryCache()
	}
}

// IsCacheDisabled reports whether caching is disabled.
func (c *Client) IsCacheDisabled() bool {
	_, ok := c.discovery.(*dummyDiscoveryCache)
	return ok
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

func (c *Client) getPrincipal(ctx context.Context, calClient *caldav.Client, u string) string {
	if p, ok := c.discovery.GetPrincipal(u); ok {
		return p
	}
	principal, err := calClient.FindCurrentUserPrincipal(ctx)
	if err == nil && principal != "" {
		c.discovery.SetPrincipal(u, principal)
		return principal
	}
	return ""
}

func (c *Client) getScheduleDefaultCalendar(ctx context.Context, principal, u string) string {
	if calID, ok := c.discovery.GetScheduleDefaultCal(u); ok {
		return calID
	}
	calID := c.FindScheduleDefaultCalendar(ctx, principal)
	if calID != "" {
		c.discovery.SetScheduleDefaultCal(u, calID)
	}
	return calID
}

func (c *Client) getCalendarHomeSet(ctx context.Context, calClient *caldav.Client, u string) string {
	if hs, ok := c.discovery.GetHomeSet(u); ok {
		return hs
	}
	principal := c.getPrincipal(ctx, calClient, u)
	if principal != "" {
		homeSet, err := calClient.FindCalendarHomeSet(ctx, principal)
		if err == nil && homeSet != "" {
			c.discovery.SetHomeSet(u, homeSet)
			return homeSet
		}
	}
	defaultHS := "calendars/" + u + "/"
	c.discovery.SetHomeSet(u, defaultHS)
	return defaultHS
}

func (c *Client) getCalPath(ctx context.Context, calClient *caldav.Client, u, calID string) string {
	if p, ok := c.discovery.GetCalPath(u, calID); ok {
		return p
	}
	homeSet := c.getCalendarHomeSet(ctx, calClient, u)
	var calPath string
	if homeSet != "" {
		calPath = strings.TrimRight(homeSet, "/") + "/" + calID + "/"
	} else {
		calPath = "calendars/" + u + "/" + calID + "/"
	}
	c.discovery.SetCalPath(u, calID, calPath)
	return calPath
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

	principal := c.getPrincipal(ctx, calClient, u)
	scheduleDefaultCalID := c.getScheduleDefaultCalendar(ctx, principal, u)

	var list []*CalendarInfo
	for _, cal := range calList {
		calID := path.Base(strings.TrimRight(cal.Path, "/"))
		if calID == "inbox" || calID == "outbox" || calID == "trashbin" {
			continue
		}
		c.discovery.SetCalPath(u, calID, cal.Path)
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
	c.discovery.DeleteCal(u, calID)
	return calClient.RemoveAll(ctx, calPath)
}

// QueryCalendarObjects queries all calendar objects in a Nextcloud calendar collection.
func (c *Client) QueryCalendarObjects(ctx context.Context, calID string) ([]*CalendarObjectInfo, error) {
	return c.QueryCalendarObjectsWithFilter(ctx, calID, nil)
}

// QueryCalendarObjectsWithFilter queries calendar objects in a Nextcloud calendar collection using a CalDAV query.
func (c *Client) QueryCalendarObjectsWithFilter(ctx context.Context, calID string, query *caldav.CalendarQuery) ([]*CalendarObjectInfo, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, err
	}
	calPath := c.getCalPath(ctx, calClient, u, calID)
	if query == nil {
		query = &caldav.CalendarQuery{
			CompFilter: caldav.CompFilter{
				Name: "VCALENDAR",
			},
		}
	}
	objs, err := calClient.QueryCalendar(ctx, calPath, query)
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

// GetCalendarObject retrieves a single calendar object from a Nextcloud calendar collection.
func (c *Client) GetCalendarObject(ctx context.Context, calID, eventID string) (*CalendarObjectInfo, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, err
	}
	eventFilename := eventID
	if !strings.HasSuffix(eventID, ".ics") {
		eventFilename = eventID + ".ics"
	}
	eventPath := c.getCalPath(ctx, calClient, u, calID) + eventFilename
	obj, err := calClient.GetCalendarObject(ctx, eventPath)
	if err != nil {
		return nil, err
	}
	return &CalendarObjectInfo{
		ID:      strings.TrimSuffix(path.Base(obj.Path), ".ics"),
		Data:    obj.Data,
		ModTime: obj.ModTime,
		ETag:    obj.ETag,
	}, nil
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

func (c *Client) buildURL(endpoint string) string {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint
	}
	if !strings.HasPrefix(endpoint, "/") {
		return c.BaseURL + "/" + endpoint
	}
	return c.BaseURL + endpoint
}

// CalendarSyncStatus contains synchronization tokens and metadata for a CalDAV calendar collection.
type CalendarSyncStatus struct {
	ID        string
	Path      string
	Name      string
	CTag      string
	SyncToken string
}

// SyncCollectionChange represents an added/modified or deleted event in a CalDAV calendar.
type SyncCollectionChange struct {
	EventID string
	Href    string
	ETag    string
	Created bool
	Deleted bool
}

// SyncCollectionResult represents the outcome of an RFC 6578 sync-collection REPORT.
type SyncCollectionResult struct {
	NewSyncToken string
	Changes      []SyncCollectionChange
}

// ErrInvalidSyncToken is returned when CalDAV rejects a sync token as expired or invalid.
var ErrInvalidSyncToken = errors.New("caldav: invalid or expired sync-token")

// GetCalendarSyncStatuses queries the calendar home set with PROPFIND Depth: 1 to retrieve
// the collection tag (CS:getctag) and sync-token (D:sync-token) for all user calendars in a single request.
func (c *Client) GetCalendarSyncStatuses(ctx context.Context) (map[string]*CalendarSyncStatus, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, err
	}
	homeSet := c.getCalendarHomeSet(ctx, calClient, u)
	if homeSet == "" {
		return nil, fmt.Errorf("caldav: could not find calendar home set for %s", u)
	}

	urlStr := c.buildURL(homeSet)

	reqXML := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:CS="http://calendarserver.org/ns/">
  <D:prop>
    <D:resourcetype/>
    <D:displayname/>
    <CS:getctag/>
    <D:sync-token/>
  </D:prop>
</D:propfind>`

	req, err := http.NewRequestWithContext(ctx, "PROPFIND", urlStr, strings.NewReader(reqXML))
	if err != nil {
		return nil, err
	}
	user, pass := c.getUserAndPass(ctx)
	req.SetBasicAuth(user, pass)
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")

	start := time.Now()
	resp, err := c.HTTPClient.Do(req)
	duration := time.Since(start)
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}
	slog.Info("Nextcloud/DAV request",
		"user", user,
		"method", req.Method,
		"path", req.URL.Path,
		"status", statusCode,
		"duration_ms", duration.Milliseconds(),
		"attempt", 1,
		"error", err,
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("caldav: PROPFIND failed with status %d", resp.StatusCode)
	}

	type propfindMultiStatus struct {
		XMLName   xml.Name `xml:"multistatus"`
		Responses []struct {
			Href     string `xml:"href"`
			Propstat []struct {
				Prop struct {
					ResourceType struct {
						InnerXML []byte `xml:",innerxml"`
					} `xml:"resourcetype"`
					DisplayName string `xml:"displayname"`
					GetCTag     string `xml:"getctag"`
					SyncToken   string `xml:"sync-token"`
				} `xml:"prop"`
				Status string `xml:"status"`
			} `xml:"propstat"`
		} `xml:"response"`
	}

	var ms propfindMultiStatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("caldav: failed to decode PROPFIND response: %w", err)
	}

	cleanHome := strings.TrimRight(homeSet, "/") + "/"
	res := make(map[string]*CalendarSyncStatus)

	for _, r := range ms.Responses {
		cleanHref := strings.TrimRight(r.Href, "/") + "/"
		if cleanHref == cleanHome || strings.HasSuffix(cleanHome, cleanHref) || strings.HasSuffix(cleanHref, cleanHome) {
			continue
		}

		calID := path.Base(strings.TrimRight(r.Href, "/"))
		if calID == "" || calID == "." || calID == "/" || calID == "inbox" || calID == "outbox" || calID == "trashbin" {
			continue
		}

		var dispName, ctag, syncToken string
		isCalendar := false

		for _, ps := range r.Propstat {
			if strings.Contains(ps.Status, "200") {
				if bytes.Contains(ps.Prop.ResourceType.InnerXML, []byte("calendar")) {
					isCalendar = true
				}
				if ps.Prop.DisplayName != "" {
					dispName = ps.Prop.DisplayName
				}
				if ps.Prop.GetCTag != "" {
					ctag = ps.Prop.GetCTag
				}
				if ps.Prop.SyncToken != "" {
					syncToken = ps.Prop.SyncToken
				}
			}
		}

		if !isCalendar {
			continue
		}

		c.discovery.SetCalPath(u, calID, r.Href)

		res[calID] = &CalendarSyncStatus{
			ID:        calID,
			Path:      r.Href,
			Name:      dispName,
			CTag:      ctag,
			SyncToken: syncToken,
		}
	}

	return res, nil
}

// SyncCalendarCollection performs an RFC 6578 sync-collection REPORT to retrieve changes since the specified sync token.
func (c *Client) SyncCalendarCollection(ctx context.Context, calID, syncToken string) (*SyncCollectionResult, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, err
	}
	calPath := c.getCalPath(ctx, calClient, u, calID)
	urlStr := c.buildURL(calPath)

	type syncPropReq struct {
		GetETag *struct{} `xml:"DAV: getetag"`
	}
	type syncReq struct {
		XMLName   xml.Name    `xml:"DAV: sync-collection"`
		SyncToken string      `xml:"DAV: sync-token"`
		SyncLevel string      `xml:"DAV: sync-level"`
		Prop      syncPropReq `xml:"DAV: prop"`
	}

	reqBody, err := xml.Marshal(&syncReq{
		SyncToken: syncToken,
		SyncLevel: "1",
		Prop: syncPropReq{
			GetETag: &struct{}{},
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "REPORT", urlStr, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	user, pass := c.getUserAndPass(ctx)
	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")

	start := time.Now()
	resp, err := c.HTTPClient.Do(req)
	duration := time.Since(start)
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}
	slog.Info("Nextcloud/DAV request",
		"user", user,
		"method", req.Method,
		"path", req.URL.Path,
		"status", statusCode,
		"duration_ms", duration.Milliseconds(),
		"attempt", 1,
		"error", err,
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusBadRequest {
		return nil, ErrInvalidSyncToken
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("caldav: sync-collection failed with status %d", resp.StatusCode)
	}

	type syncMultiStatus struct {
		XMLName   xml.Name `xml:"multistatus"`
		SyncToken string   `xml:"sync-token"`
		Responses []struct {
			Href     string `xml:"href"`
			Status   string `xml:"status"`
			Propstat []struct {
				Prop struct {
					GetETag string `xml:"getetag"`
				} `xml:"prop"`
				Status string `xml:"status"`
			} `xml:"propstat"`
		} `xml:"response"`
	}

	var ms syncMultiStatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("caldav: failed to decode sync-collection response: %w", err)
	}

	result := &SyncCollectionResult{
		NewSyncToken: ms.SyncToken,
		Changes:      make([]SyncCollectionChange, 0, len(ms.Responses)),
	}

	for _, r := range ms.Responses {
		filename := path.Base(r.Href)
		if !strings.HasSuffix(filename, ".ics") {
			continue
		}
		eventID := strings.TrimSuffix(filename, ".ics")

		isCreated := strings.Contains(r.Status, "201")
		isDeleted := false
		if strings.Contains(r.Status, "404") {
			isDeleted = true
		}

		var etag string
		for _, ps := range r.Propstat {
			if strings.Contains(ps.Status, "200") && ps.Prop.GetETag != "" {
				etag = ps.Prop.GetETag
			}
		}

		result.Changes = append(result.Changes, SyncCollectionChange{
			EventID: eventID,
			Href:    r.Href,
			ETag:    etag,
			Created: isCreated,
			Deleted: isDeleted,
		})
	}

	return result, nil
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

func (c *Client) userRequest(ctx context.Context, method, endpoint string, body url.Values) ([]byte, error) {
	user, pass := c.getUserAndPass(ctx)
	if user == "" {
		return nil, fmt.Errorf("nextcloud user credentials not configured in context")
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
	req.SetBasicAuth(user, pass)
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

// CreateUser provisions a user in Nextcloud using the caller's credentials.
func (c *Client) CreateUser(ctx context.Context, userid, password, email, displayname string) error {
	data := url.Values{
		"userid":   {userid},
		"password": {password},
		"email":    {email},
	}
	respBytes, err := c.userRequest(ctx, http.MethodPost, "/ocs/v1.php/cloud/users", data)
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

// IsValidGroupID validates that a group ID does not contain path traversal,
// control characters, URL injection characters, or header injection characters.
func IsValidGroupID(groupid string) bool {
	if groupid == "" || len(groupid) > 255 {
		return false
	}
	// Prevent path traversal
	if strings.Contains(groupid, "..") || strings.Contains(groupid, "/") || strings.Contains(groupid, "\\") {
		return false
	}
	// Prevent control characters, CRLF, URL delimiters, and injection characters
	for _, r := range groupid {
		if r < 0x20 || r == 0x7f {
			return false
		}
		switch r {
		case '?', '#', '&', '%', '<', '>', '"', '\'', ';', ':', '|', '\x00':
			return false
		}
	}
	return true
}

// IsValidUserID validates that a user ID does not contain path traversal,
// control characters, or injection sequences.
func IsValidUserID(userid string) bool {
	if userid == "" || len(userid) > 255 {
		return false
	}
	if strings.Contains(userid, "..") || strings.Contains(userid, "/") || strings.Contains(userid, "\\") {
		return false
	}
	for _, r := range userid {
		if r < 0x20 || r == 0x7f {
			return false
		}
		switch r {
		case '?', '#', '&', '%', '<', '>', '"', '\'', ';', ':', '|', '\x00':
			return false
		}
	}
	return true
}

// SetUserDisplayName sets the display name for a Nextcloud user.
func (c *Client) SetUserDisplayName(ctx context.Context, userid, displayname string) error {
	if !IsValidUserID(userid) {
		return fmt.Errorf("invalid user id %q", userid)
	}
	cleanDisplayName := strings.ReplaceAll(strings.ReplaceAll(displayname, "\r", ""), "\n", " ")
	endpoint := fmt.Sprintf("/ocs/v1.php/cloud/users/%s", url.PathEscape(userid))
	data := url.Values{
		"key":   {"displayname"},
		"value": {cleanDisplayName},
	}
	_, err := c.userRequest(ctx, http.MethodPut, endpoint, data)
	return err
}

// GetUsers lists all user IDs in Nextcloud accessible to the authenticated user.
func (c *Client) GetUsers(ctx context.Context) ([]string, error) {
	respBytes, err := c.userRequest(ctx, http.MethodGet, "/ocs/v1.php/cloud/users?format=json", nil)
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

// GetCurrentUser gets the authenticated user's details from Nextcloud.
func (c *Client) GetCurrentUser(ctx context.Context) (*UserDetails, error) {
	respBytes, err := c.userRequest(ctx, http.MethodGet, "/ocs/v1.php/cloud/user?format=json", nil)
	if err != nil {
		return nil, err
	}
	var env ocsEnvelope[UserDetails]
	if err := json.Unmarshal(respBytes, &env); err != nil {
		return nil, err
	}
	if env.OCS.Meta.StatusCode != 100 {
		return nil, fmt.Errorf("get current user failed: %s (code %d)", env.OCS.Meta.Message, env.OCS.Meta.StatusCode)
	}
	return &env.OCS.Data, nil
}

// GetUserDetails gets user details from Nextcloud.
func (c *Client) GetUserDetails(ctx context.Context, userid string) (*UserDetails, error) {
	if !IsValidUserID(userid) {
		return nil, fmt.Errorf("invalid user id %q", userid)
	}
	endpoint := fmt.Sprintf("/ocs/v1.php/cloud/users/%s?format=json", url.PathEscape(userid))
	respBytes, err := c.userRequest(ctx, http.MethodGet, endpoint, nil)
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
	if !IsValidGroupID(groupid) {
		return fmt.Errorf("invalid group id %q: contains prohibited characters or injection sequence", groupid)
	}
	data := url.Values{"groupid": {groupid}}
	respBytes, err := c.userRequest(ctx, http.MethodPost, "/ocs/v1.php/cloud/groups", data)
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

// GetGroups lists groups accessible to the authenticated user in Nextcloud.
func (c *Client) GetGroups(ctx context.Context) ([]string, error) {
	respBytes, err := c.userRequest(ctx, http.MethodGet, "/ocs/v1.php/cloud/groups?format=json", nil)
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
	if !IsValidGroupID(groupid) {
		return nil, fmt.Errorf("invalid group id %q: contains prohibited characters or injection sequence", groupid)
	}
	endpoint := fmt.Sprintf("/ocs/v1.php/cloud/groups/%s/users?format=json", url.PathEscape(groupid))
	respBytes, err := c.userRequest(ctx, http.MethodGet, endpoint, nil)
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
	if !IsValidUserID(userid) {
		return fmt.Errorf("invalid user id %q", userid)
	}
	if !IsValidGroupID(groupid) {
		return fmt.Errorf("invalid group id %q: contains prohibited characters or injection sequence", groupid)
	}
	endpoint := fmt.Sprintf("/ocs/v1.php/cloud/users/%s/groups", url.PathEscape(userid))
	data := url.Values{"groupid": {groupid}}
	respBytes, err := c.userRequest(ctx, http.MethodPost, endpoint, data)
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
	if !IsValidGroupID(groupid) {
		return fmt.Errorf("invalid group id %q: contains prohibited characters or injection sequence", groupid)
	}
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
	if userid == "" {
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
