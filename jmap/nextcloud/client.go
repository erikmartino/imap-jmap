package nextcloud

import (
	"bytes"
	"context"
	"io"
	"net/http"
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

	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(50*(1<<attempt)) * time.Millisecond)
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
			if resp.StatusCode != http.StatusInternalServerError && resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != 423 {
				return resp, nil
			}
			resp.Body.Close()
			continue
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
