package jmap_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// testUsername is the default account every test client authenticates as. The memory
// auth backend accepts any credentials where username == password, so tests authenticate
// with username and password both set to testUsername.
const testUsername = "user@example.com"

// seedCtx returns a context scoped to the default test user's account so direct backend
// seeding lands in the same per-account store that authenticated HTTP requests use
// (the memory backends key all state by the accountID in the context).
func seedCtx() context.Context {
	return jmap.ContextWithAccountID(context.Background(), jmap.AccountIDForSubject(testUsername))
}

// basicAuthHeader returns an Authorization header with Basic credentials for the default
// test user, for transports that only accept headers (e.g. WebSocket dialing).
func basicAuthHeader() http.Header {
	return http.Header{"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte(testUsername + ":" + testUsername))}}
}

// authedRequest returns an HTTP request authenticated as the default test user.
func authedRequest(t *testing.T, method, url string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("NewRequest(%s %s) failed: %v", method, url, err)
	}
	req.SetBasicAuth(testUsername, testUsername)
	return req
}

// authedPost performs an authenticated POST for the default test user. It mirrors
// http.Post's signature so call sites can swap to it without changing error handling.
func authedPost(url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.SetBasicAuth(testUsername, testUsername)
	return http.DefaultClient.Do(req)
}

// authedGet performs an authenticated GET for the default test user. It mirrors
// http.Get's signature so call sites can swap to it without changing error handling.
func authedGet(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(testUsername, testUsername)
	return http.DefaultClient.Do(req)
}

func newTestServer(opts ...jmap.Option) *jmap.Server {
	// Wire every advertised capability's backend so the default test server behaves like a
	// real, full-featured server (no advertised method returns "unknown method").
	mb := memory.NewMemoryBackend()
	bb := memory.NewMemoryBlobBackend()
	fb := memory.NewMemoryFileNodeBackend()
	cal := memory.NewMemoryCalendarsBackend()
	contacts := memory.NewMemoryContactsBackend()
	sieve := memory.NewMemorySieveBackend()
	imap := memory.NewMemoryIMAPAccessBackend()
	memAuth := memory.NewMemoryAuthBackend()

	allOpts := []jmap.Option{
		jmap.WithMailBackend(mb),
		jmap.WithBlobBackend(bb),
		jmap.WithFileNodeBackend(fb),
		jmap.WithCalendarsBackend(cal),
		jmap.WithContactsBackend(contacts),
		jmap.WithSieveBackend(sieve),
		jmap.WithIMAPAccessBackend(imap),
		jmap.WithAuthBackend(memAuth),
	}
	allOpts = append(allOpts, opts...)

	srv := jmap.NewServer(nil, allOpts...)
	if memAuth, ok := srv.AuthBackend.(*memory.MemoryAuthBackend); ok {
		memAuth.SetBackends(mb, srv.BlobBackend, cal, contacts, fb)
	}
	mb.SetBroadcaster(srv.Broadcaster)
	cal.SetBroadcaster(srv.Broadcaster)
	contacts.SetBroadcaster(srv.Broadcaster)
	sieve.SetBroadcaster(srv.Broadcaster)
	fb.SetBroadcaster(srv.Broadcaster)

	return srv
}