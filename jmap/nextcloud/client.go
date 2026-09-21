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
	"imap-jmap/jmap/jmapcore"
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
	// cacheDisabled controls the data caches (calendar/event bodies). Discovery
	// metadata (principal URL, home set, calendar paths, schedule-default calendar)
	// is always cached in memory: it is stable, bounded, not user content, and never
	// persisted to disk, so it does not make the proxy stateful.
	cacheDisabled bool
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

// NewClient creates a new Nextcloud client helper. Discovery metadata is cached in
// memory; calendar/event data caches are disabled unless explicitly enabled.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Transport: &retryTransport{base: http.DefaultTransport},
			Timeout:   15 * time.Second,
		},
		discovery:     newMemDiscoveryCache(),
		cacheDisabled: isCacheDisabledEnv(),
	}
}

// SetCacheDisabled enables or disables the calendar/event data caches. Discovery
// metadata stays cached in memory regardless.
func (c *Client) SetCacheDisabled(disabled bool) {
	c.cacheDisabled = disabled
}

// IsCacheDisabled reports whether the calendar/event data caches are disabled.
func (c *Client) IsCacheDisabled() bool {
	return c.cacheDisabled
}

// calendarSyncStatusesKey is the request-scope key for the memoized home-set
// PROPFIND result shared by the calendar listing and CalendarEvent state. It is scoped
// by user so a request that touches multiple accounts (e.g. CalendarEvent/copy) cannot
// reuse another account's collections.
type calendarSyncStatusesKey struct {
	user string
}

// discoveryFor returns the in-memory discovery cache (principal URL, home set, calendar
// paths, schedule-default calendar). It is stable, bounded metadata, not user content,
// and never persisted to disk.
func (c *Client) discoveryFor(ctx context.Context) discoveryCache {
	return c.discovery
}

// invalidateRequestCalendarCollections drops the request-scoped home-set PROPFIND memo
// after a calendar or event mutation, so later calls in the same JMAP request observe
// the new CTag/sync-token rather than the pre-mutation snapshot.
func invalidateRequestCalendarCollections(ctx context.Context, u string) {
	if rs := jmapcore.RequestScopeFrom(ctx); rs != nil {
		rs.Delete(calendarSyncStatusesKey{user: u})
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

func (c *Client) getPrincipal(ctx context.Context, calClient *caldav.Client, u string) string {
	disc := c.discoveryFor(ctx)
	if p, ok := disc.GetPrincipal(u); ok {
		return p
	}
	principal, err := calClient.FindCurrentUserPrincipal(ctx)
	if err == nil && principal != "" {
		disc.SetPrincipal(u, principal)
		return principal
	}
	return ""
}

func (c *Client) getScheduleDefaultCalendar(ctx context.Context, principal, u string) string {
	disc := c.discoveryFor(ctx)
	if calID, ok := disc.GetScheduleDefaultCal(u); ok {
		return calID
	}
	calID := c.FindScheduleDefaultCalendar(ctx, principal)
	if calID != "" {
		disc.SetScheduleDefaultCal(u, calID)
	}
	return calID
}

func (c *Client) getCalendarHomeSet(ctx context.Context, calClient *caldav.Client, u string) string {
	if hs, ok := c.discoveryFor(ctx).GetHomeSet(u); ok {
		return hs
	}
	principal := c.getPrincipal(ctx, calClient, u)
	return c.getCalendarHomeSetForPrincipal(ctx, calClient, u, principal)
}

// getCalendarHomeSetForPrincipal resolves the calendar home set using an already
// discovered principal, so callers that also need the principal do not trigger a
// second PROPFIND.
func (c *Client) getCalendarHomeSetForPrincipal(ctx context.Context, calClient *caldav.Client, u, principal string) string {
	disc := c.discoveryFor(ctx)
	if hs, ok := disc.GetHomeSet(u); ok {
		return hs
	}
	if principal != "" {
		homeSet, err := calClient.FindCalendarHomeSet(ctx, principal)
		if err == nil && homeSet != "" {
			disc.SetHomeSet(u, homeSet)
			return homeSet
		}
	}
	defaultHS := "calendars/" + u + "/"
	disc.SetHomeSet(u, defaultHS)
	return defaultHS
}

func (c *Client) getCalPath(ctx context.Context, calClient *caldav.Client, u, calID string) string {
	disc := c.discoveryFor(ctx)
	if p, ok := disc.GetCalPath(u, calID); ok {
		return p
	}
	homeSet := c.getCalendarHomeSet(ctx, calClient, u)
	var calPath string
	if homeSet != "" {
		calPath = strings.TrimRight(homeSet, "/") + "/" + calID + "/"
	} else {
		calPath = "calendars/" + u + "/" + calID + "/"
	}
	disc.SetCalPath(u, calID, calPath)
	return calPath
}

// ListCalendars retrieves all calendars for the authenticated user from Nextcloud.
// It derives the listing from the same home-set PROPFIND that supplies the CalDAV
// CTag/sync-token, so a query performs only one calendar-collection round-trip.
func (c *Client) ListCalendars(ctx context.Context) ([]*CalendarInfo, string, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, "", err
	}
	// Resolve the principal once and reuse it for both the home-set and the
	// schedule-default discovery PROPFINDs.
	principal := c.getPrincipal(ctx, calClient, u)
	_ = c.getCalendarHomeSetForPrincipal(ctx, calClient, u, principal)
	collections, err := c.getCalendarCollections(ctx)
	if err != nil {
		return nil, u, err
	}

	scheduleDefaultCalID := c.getScheduleDefaultCalendar(ctx, principal, u)

	list := make([]*CalendarInfo, 0, len(collections.ordered))
	for _, cal := range collections.ordered {
		name := cal.Name
		if name == "" {
			name = cal.ID
		}
		isDefault := false
		if scheduleDefaultCalID != "" {
			isDefault = (cal.ID == scheduleDefaultCalID)
		} else if len(list) == 0 {
			isDefault = true
		}
		list = append(list, &CalendarInfo{
			ID:          cal.ID,
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
	err = calClient.Mkdir(ctx, calPath)
	invalidateRequestCalendarCollections(ctx, u)
	return err
}

// DeleteCalendar removes a calendar collection in Nextcloud.
func (c *Client) DeleteCalendar(ctx context.Context, calID string) error {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return err
	}
	calPath := c.getCalPath(ctx, calClient, u, calID)
	c.discoveryFor(ctx).DeleteCal(u, calID)
	err = calClient.RemoveAll(ctx, calPath)
	invalidateRequestCalendarCollections(ctx, u)
	return err
}

// QueryCalendarObjects queries all calendar objects in a Nextcloud calendar collection.
func (c *Client) QueryCalendarObjects(ctx context.Context, calID string) ([]*CalendarObjectInfo, error) {
	return c.QueryCalendarObjectsWithFilter(ctx, calID, nil)
}

// QueryCalendarObjectsInRange queries the VEVENT resources in a Nextcloud calendar
// collection that overlap the half-open time range [start, end). A zero start or end
// leaves that side of the range open. The range is translated into a CalDAV
// time-range component filter (RFC 4791 Section 9.9), so the server evaluates
// recurrences and transfers only the resources that fall inside the range instead of
// the whole collection.
func (c *Client) QueryCalendarObjectsInRange(ctx context.Context, calID string, start, end time.Time) ([]*CalendarObjectInfo, error) {
	compFilter := caldav.CompFilter{Name: "VCALENDAR"}
	if !start.IsZero() || !end.IsZero() {
		// The CalDAV time-range attributes MUST be "date with UTC time" (RFC 4791
		// Section 9.9). Normalise to UTC explicitly: the XML formatter renders the
		// wall-clock fields verbatim with a trailing "Z", so a zoned time would
		// otherwise be serialised with its local wall clock and misread as UTC.
		compFilter.Comps = []caldav.CompFilter{{
			Name:  "VEVENT",
			Start: start.UTC(),
			End:   end.UTC(),
		}}
	}
	return c.QueryCalendarObjectsWithFilter(ctx, calID, &caldav.CalendarQuery{CompFilter: compFilter})
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
		res[i] = &CalendarObjectInfo{
			ID:      path.Base(obj.Path),
			Data:    obj.Data,
			ModTime: obj.ModTime,
			ETag:    obj.ETag,
		}
	}
	return res, nil
}

// GetCalendarObject retrieves a single calendar object from a Nextcloud calendar collection.
// The JMAP CalendarEvent id is the CalDAV resource name verbatim, so the id-to-path
// mapping is a plain concatenation and is invertible regardless of the resource's file
// extension (RFC 4791 Section 5.3.1 only says URLs "may" end in ".ics").
func (c *Client) GetCalendarObject(ctx context.Context, calID, eventID string) (*CalendarObjectInfo, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, err
	}
	eventPath := c.getCalPath(ctx, calClient, u, calID) + eventID
	obj, err := calClient.GetCalendarObject(ctx, eventPath)
	if err != nil {
		return nil, err
	}
	return &CalendarObjectInfo{
		ID:      path.Base(obj.Path),
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
	eventPath := c.getCalPath(ctx, calClient, u, calID) + eventID
	_, err = calClient.PutCalendarObject(ctx, eventPath, calObj)
	invalidateRequestCalendarCollections(ctx, u)
	return err
}

// DeleteCalendarObject deletes a calendar object from a Nextcloud calendar collection.
func (c *Client) DeleteCalendarObject(ctx context.Context, calID, eventID string) error {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return err
	}
	eventPath := c.getCalPath(ctx, calClient, u, calID) + eventID
	err = calClient.RemoveAll(ctx, eventPath)
	invalidateRequestCalendarCollections(ctx, u)
	return err
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
	ID          string
	Path        string
	Name        string
	Description string
	CTag        string
	SyncToken   string
}

// calendarCollections is the result of the home-set PROPFIND: the calendar collections
// in document order plus an id-indexed view.
type calendarCollections struct {
	ordered []*CalendarSyncStatus
	byID    map[string]*CalendarSyncStatus
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

// getCalendarCollections queries the calendar home set with PROPFIND Depth: 1 and
// returns the calendar collections in document order together with their CTag and
// sync-token. The result is memoized for the lifetime of a single JMAP request so the
// calendar listing and the CalendarEvent state derive from one CalDAV round-trip.
func (c *Client) getCalendarCollections(ctx context.Context) (*calendarCollections, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, err
	}
	key := calendarSyncStatusesKey{user: u}
	if rs := jmapcore.RequestScopeFrom(ctx); rs != nil {
		if v, ok := rs.Load(key); ok {
			if cc, ok := v.(*calendarCollections); ok {
				return cc, nil
			}
		}
	}
	homeSet := c.getCalendarHomeSet(ctx, calClient, u)
	if homeSet == "" {
		return nil, fmt.Errorf("caldav: could not find calendar home set for %s", u)
	}

	urlStr := c.buildURL(homeSet)

	reqXML := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:CS="http://calendarserver.org/ns/">
  <D:prop>
    <D:resourcetype/>
    <D:displayname/>
    <C:calendar-description/>
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
					Description string `xml:"urn:ietf:params:xml:ns:caldav calendar-description"`
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
	var ordered []*CalendarSyncStatus

	for _, r := range ms.Responses {
		cleanHref := strings.TrimRight(r.Href, "/") + "/"
		if cleanHref == cleanHome || strings.HasSuffix(cleanHome, cleanHref) || strings.HasSuffix(cleanHref, cleanHome) {
			continue
		}

		calID := path.Base(strings.TrimRight(r.Href, "/"))
		if calID == "" || calID == "." || calID == "/" || calID == "inbox" || calID == "outbox" || calID == "trashbin" {
			continue
		}

		var dispName, description, ctag, syncToken string
		isCalendar := false

		for _, ps := range r.Propstat {
			if strings.Contains(ps.Status, "200") {
				if bytes.Contains(ps.Prop.ResourceType.InnerXML, []byte("calendar")) {
					isCalendar = true
				}
				if ps.Prop.DisplayName != "" {
					dispName = ps.Prop.DisplayName
				}
				if ps.Prop.Description != "" {
					description = ps.Prop.Description
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

		c.discoveryFor(ctx).SetCalPath(u, calID, r.Href)

		st := &CalendarSyncStatus{
			ID:          calID,
			Path:        r.Href,
			Name:        dispName,
			Description: description,
			CTag:        ctag,
			SyncToken:   syncToken,
		}
		res[calID] = st
		ordered = append(ordered, st)
	}

	cc := &calendarCollections{ordered: ordered, byID: res}
	if rs := jmapcore.RequestScopeFrom(ctx); rs != nil {
		rs.Store(key, cc)
	}

	return cc, nil
}

// GetCalendarSyncStatuses returns the CTag/sync-token for every calendar, memoized for
// the duration of the JMAP request.
func (c *Client) GetCalendarSyncStatuses(ctx context.Context) (map[string]*CalendarSyncStatus, error) {
	cc, err := c.getCalendarCollections(ctx)
	if err != nil {
		return nil, err
	}
	return cc.byID, nil
}

// ListCalendarObjectETags lists the calendar object resources in a collection together
// with their ETags. It uses go-webdav's WebDAV ReadDir (a body-less PROPFIND Depth:1),
// so change detection can discover added resources from CalDAV ETags alone instead of
// downloading and parsing every event body. The returned map is keyed by event id.
func (c *Client) ListCalendarObjectETags(ctx context.Context, calID string) (map[string]string, error) {
	calClient, u, err := c.CalDAV(ctx)
	if err != nil {
		return nil, err
	}
	calPath := c.getCalPath(ctx, calClient, u, calID)
	entries, err := calClient.ReadDir(ctx, calPath, false)
	if err != nil {
		return nil, err
	}

	res := make(map[string]string, len(entries))
	for _, fi := range entries {
		if fi.IsDir {
			continue
		}
		res[path.Base(fi.Path)] = fi.ETag
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
		if filename == "" || filename == "/" || filename == "." {
			continue
		}
		eventID := filename

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

// Sharee is a share target discovered through the Nextcloud sharee search API
// (/ocs/v2.php/apps/files_sharing/api/v1/sharees), which is available to regular
// users with their own credentials and returns only what they are permitted to see.
type Sharee struct {
	// ShareType follows Nextcloud OCS: 0 = user, 1 = group (others ignored).
	ShareType int
	// ID is the Nextcloud user or group id ("shareWith").
	ID string
	// Label is the human-readable display label.
	Label string
}

type ocsShareeValue struct {
	ShareType int    `json:"shareType"`
	ShareWith string `json:"shareWith"`
}

type ocsSharee struct {
	Label string         `json:"label"`
	Value ocsShareeValue `json:"value"`
}

type ocsDataSharees struct {
	Exact struct {
		Users  []ocsSharee `json:"users"`
		Groups []ocsSharee `json:"groups"`
	} `json:"exact"`
	Users  []ocsSharee `json:"users"`
	Groups []ocsSharee `json:"groups"`
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

// GetSharees searches the Nextcloud share targets (users and groups) visible to the
// authenticated user, using the same endpoint the Nextcloud web UI uses. It requires no
// admin rights and returns only principals the caller is permitted to share with. An
// empty search returns the caller's visible directory (paginated upstream).
func (c *Client) GetSharees(ctx context.Context, search string) ([]Sharee, error) {
	q := url.Values{}
	q.Set("search", search)
	q.Set("itemType", "file")
	q.Set("perPage", "200")
	q.Set("format", "json")
	respBytes, err := c.userRequest(ctx, http.MethodGet, "/ocs/v2.php/apps/files_sharing/api/v1/sharees?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var env ocsEnvelope[ocsDataSharees]
	if err := json.Unmarshal(respBytes, &env); err != nil {
		return nil, err
	}
	// OCS v2 uses 200 for success; v1 uses 100. Accept both.
	if env.OCS.Meta.StatusCode != 100 && env.OCS.Meta.StatusCode != 200 {
		return nil, fmt.Errorf("sharee search failed: %s (code %d)", env.OCS.Meta.Message, env.OCS.Meta.StatusCode)
	}

	var out []Sharee
	seen := make(map[string]bool)
	appendAll := func(list []ocsSharee) {
		for _, s := range list {
			if s.Value.ShareWith == "" || s.Value.ShareType > 1 {
				continue
			}
			key := fmt.Sprintf("%d:%s", s.Value.ShareType, s.Value.ShareWith)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Sharee{ShareType: s.Value.ShareType, ID: s.Value.ShareWith, Label: s.Label})
		}
	}
	appendAll(env.OCS.Data.Exact.Users)
	appendAll(env.OCS.Data.Exact.Groups)
	appendAll(env.OCS.Data.Users)
	appendAll(env.OCS.Data.Groups)
	return out, nil
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
