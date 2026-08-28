package nextcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
	"github.com/emersion/go-webdav/carddav"

	"imap-jmap/jmap"
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
}

// NewClient creates a new Nextcloud client helper.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Transport: &retryTransport{base: http.DefaultTransport},
			Timeout:   15 * time.Second,
		},
	}
}

func (c *Client) getUserAndPass(ctx context.Context) (string, string) {
	creds, ok := jmap.CredentialsFromContext(ctx)
	if ok && creds.Username != "" {
		return creds.Username, creds.Password
	}

	subject, ok := jmap.SubjectFromContext(ctx)
	if ok && subject != "" {
		return subject, subject
	}

	accountID, ok := jmap.AccountIDFromContext(ctx)
	if ok && accountID != "" {
		if subj, okSub := jmap.SubjectForAccountID(accountID); okSub && subj != "" {
			return subj, subj
		}
		return accountID, accountID
	}

	return "user@example.com", "user@example.com"
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
	endpoint := c.BaseURL + "/remote.php/dav/files/" + user + "/"
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
	endpoint := fmt.Sprintf("/ocs/v1.php/cloud/groups/%s?format=json", url.PathEscape(groupid))
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
		email = userid + "@example.com"
	}
	if password == "" {
		password = email
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
