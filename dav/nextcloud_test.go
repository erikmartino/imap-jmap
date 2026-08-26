package dav_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func getNextcloudBaseURL() string {
	if u := os.Getenv("NEXTCLOUD_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8088"
}

func isNextcloudReachable(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/status.php")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

type testRetryTransport struct {
	base http.RoundTripper
}

func (t *testRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

func newTestHTTPClient() *http.Client {
	return &http.Client{
		Transport: &testRetryTransport{base: http.DefaultTransport},
		Timeout:   15 * time.Second,
	}
}

func TestNextcloud_CalDAVIntegration(t *testing.T) {
	baseURL := getNextcloudBaseURL()
	if !isNextcloudReachable(baseURL) {
		t.Skip("Nextcloud server is not reachable at " + baseURL)
	}

	client := newTestHTTPClient()
	user := "user@example.com"
	pass := "user@example.com"

	// 1. PROPFIND calendar home
	calHomeURL := baseURL + "/remote.php/dav/calendars/" + user + "/"
	req, _ := http.NewRequest("PROPFIND", calHomeURL, strings.NewReader(`<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <D:displayname/>
    <D:resourcetype/>
  </D:prop>
</D:propfind>`))
	req.SetBasicAuth(user, pass)
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PROPFIND %s failed: %v", calHomeURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		t.Fatalf("PROPFIND %s returned HTTP %d", calHomeURL, resp.StatusCode)
	}

	// 2. PUT event
	eventUID := fmt.Sprintf("nc-cal-test-uid-%d", time.Now().UnixNano())
	eventURL := baseURL + "/remote.php/dav/calendars/" + user + "/personal/" + eventUID + ".ics"
	icsPayload := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\nBEGIN:VEVENT\r\nUID:" + eventUID + "\r\nSUMMARY:Nextcloud Integration Meeting\r\nDESCRIPTION:Testing CalDAV on Nextcloud\r\nDTSTART:20261201T100000Z\r\nDURATION:PT1H\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

	putReq, _ := http.NewRequest("PUT", eventURL, strings.NewReader(icsPayload))
	putReq.SetBasicAuth(user, pass)
	putReq.Header.Set("Content-Type", "text/calendar; charset=utf-8")

	putResp, err := client.Do(putReq)
	if err != nil {
		t.Fatalf("PUT %s failed: %v", eventURL, err)
	}
	putBody, _ := io.ReadAll(putResp.Body)
	if putResp.StatusCode != http.StatusCreated && putResp.StatusCode != http.StatusNoContent && putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT event returned HTTP %d: %s", putResp.StatusCode, string(putBody))
	}

	// 3. GET event
	getReq, _ := http.NewRequest("GET", eventURL, nil)
	getReq.SetBasicAuth(user, pass)
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET %s failed: %v", eventURL, err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET event returned HTTP %d", getResp.StatusCode)
	}
	getBody, _ := io.ReadAll(getResp.Body)
	if !strings.Contains(string(getBody), "Nextcloud Integration Meeting") {
		t.Errorf("GET event body did not contain expected summary: %s", string(getBody))
	}

	// 4. DELETE event
	delReq, _ := http.NewRequest("DELETE", eventURL, nil)
	delReq.SetBasicAuth(user, pass)
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE %s failed: %v", eventURL, err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent && delResp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE event returned HTTP %d", delResp.StatusCode)
	}
}

func TestNextcloud_CardDAVIntegration(t *testing.T) {
	baseURL := getNextcloudBaseURL()
	if !isNextcloudReachable(baseURL) {
		t.Skip("Nextcloud server is not reachable at " + baseURL)
	}

	client := newTestHTTPClient()
	user := "user@example.com"
	pass := "user@example.com"

	// 1. PROPFIND addressbooks
	abHomeURL := baseURL + "/remote.php/dav/addressbooks/users/" + user + "/"
	req, _ := http.NewRequest("PROPFIND", abHomeURL, strings.NewReader(`<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:carddav">
  <D:prop>
    <D:displayname/>
    <D:resourcetype/>
  </D:prop>
</D:propfind>`))
	req.SetBasicAuth(user, pass)
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PROPFIND %s failed: %v", abHomeURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		t.Fatalf("PROPFIND %s returned HTTP %d", abHomeURL, resp.StatusCode)
	}

	// 2. PUT vCard contact
	cardUID := fmt.Sprintf("nc-card-test-uid-%d", time.Now().UnixNano())
	cardURL := baseURL + "/remote.php/dav/addressbooks/users/" + user + "/contacts/" + cardUID + ".vcf"
	vcfPayload := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + cardUID + "\r\nFN:Alice Nextcloud\r\nN:Nextcloud;Alice;;;\r\nEMAIL;TYPE=INTERNET;TYPE=HOME:alice.nc@example.com\r\nTEL;TYPE=CELL:+15551234567\r\nEND:VCARD\r\n"

	putReq, _ := http.NewRequest("PUT", cardURL, strings.NewReader(vcfPayload))
	putReq.SetBasicAuth(user, pass)
	putReq.Header.Set("Content-Type", "text/vcard; charset=utf-8")

	putResp, err := client.Do(putReq)
	if err != nil {
		t.Fatalf("PUT %s failed: %v", cardURL, err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated && putResp.StatusCode != http.StatusNoContent && putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT card returned HTTP %d", putResp.StatusCode)
	}

	// 3. GET vCard contact
	getReq, _ := http.NewRequest("GET", cardURL, nil)
	getReq.SetBasicAuth(user, pass)
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET %s failed: %v", cardURL, err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET card returned HTTP %d", getResp.StatusCode)
	}
	getBody, _ := io.ReadAll(getResp.Body)
	if !strings.Contains(string(getBody), "Alice Nextcloud") {
		t.Errorf("GET card body did not contain expected FN: %s", string(getBody))
	}

	// 4. DELETE vCard contact
	delReq, _ := http.NewRequest("DELETE", cardURL, nil)
	delReq.SetBasicAuth(user, pass)
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE %s failed: %v", cardURL, err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent && delResp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE card returned HTTP %d", delResp.StatusCode)
	}
}

func TestNextcloud_WebDAVFileStorageIntegration(t *testing.T) {
	baseURL := getNextcloudBaseURL()
	if !isNextcloudReachable(baseURL) {
		t.Skip("Nextcloud server is not reachable at " + baseURL)
	}

	client := newTestHTTPClient()
	user := "user@example.com"
	pass := "user@example.com"

	// 1. Upload file via WebDAV PUT
	fileName := fmt.Sprintf("test-integration-file-%d.txt", time.Now().UnixNano())
	fileURL := baseURL + "/remote.php/dav/files/" + user + "/" + fileName
	content := []byte("Hello Nextcloud WebDAV Storage Integration Testing!")

	putReq, _ := http.NewRequest("PUT", fileURL, bytes.NewReader(content))
	putReq.SetBasicAuth(user, pass)
	putReq.Header.Set("Content-Type", "text/plain")

	putResp, err := client.Do(putReq)
	if err != nil {
		t.Fatalf("PUT %s failed: %v", fileURL, err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusCreated && putResp.StatusCode != http.StatusNoContent && putResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT file returned HTTP %d", putResp.StatusCode)
	}

	// 2. Download file via WebDAV GET
	getReq, _ := http.NewRequest("GET", fileURL, nil)
	getReq.SetBasicAuth(user, pass)
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET %s failed: %v", fileURL, err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET file returned HTTP %d", getResp.StatusCode)
	}
	getBody, _ := io.ReadAll(getResp.Body)
	if string(getBody) != string(content) {
		t.Errorf("Downloaded file content = %q, want %q", string(getBody), string(content))
	}

	// 3. Delete file via WebDAV DELETE
	delReq, _ := http.NewRequest("DELETE", fileURL, nil)
	delReq.SetBasicAuth(user, pass)
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE %s failed: %v", fileURL, err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent && delResp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE file returned HTTP %d", delResp.StatusCode)
	}
}
