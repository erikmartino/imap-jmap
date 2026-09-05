package jmap_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConvert_JSContactToVCard_Success(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	jsContact := map[string]any{
		"@type": "Card",
		"name": map[string]any{
			"components": []any{
				map[string]any{"kind": "given", "value": "Jane"},
				map[string]any{"kind": "surname", "value": "Doe"},
			},
		},
		"emails": map[string]any{
			"e1": map[string]any{
				"address": "jane.doe@example.com",
			},
		},
	}
	body, _ := json.Marshal(jsContact)

	resp, err := http.Post(ts.URL+"/convert", "application/jscontact+json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /convert failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200 OK, got %d: %s", resp.StatusCode, string(respBody))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/vcard") {
		t.Errorf("Expected text/vcard Content-Type, got %q", ct)
	}

	vcardBytes, _ := io.ReadAll(resp.Body)
	vcardStr := string(vcardBytes)
	if !strings.Contains(vcardStr, "BEGIN:VCARD") || !strings.Contains(vcardStr, "END:VCARD") {
		t.Errorf("Expected vCard envelope, got %q", vcardStr)
	}
	if !strings.Contains(vcardStr, "jane.doe@example.com") {
		t.Errorf("Expected email in vCard, got %q", vcardStr)
	}
}

func TestConvert_VCardToJSContact_Success(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	vcardStr := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:John Smith\r\nEMAIL:john.smith@example.com\r\nEND:VCARD\r\n"

	resp, err := http.Post(ts.URL+"/convert", "text/vcard", strings.NewReader(vcardStr))
	if err != nil {
		t.Fatalf("POST /convert failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200 OK, got %d: %s", resp.StatusCode, string(respBody))
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/jscontact+json") {
		t.Errorf("Expected application/jscontact+json Content-Type, got %q", ct)
	}

	var card map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("Failed to decode JSContact response: %v", err)
	}

	if card["@type"] != "Card" {
		t.Errorf("Expected @type 'Card', got %v", card["@type"])
	}
	emails, ok := card["emails"].(map[string]any)
	if !ok || len(emails) == 0 {
		t.Errorf("Expected emails map in Card, got %v", card["emails"])
	}
}

func TestConvert_InvalidJSContact_422(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Invalid card schema (missing required properties or invalid types)
	invalidCard := []byte(`{"@type":"BogusType"}`)
	resp, err := http.Post(ts.URL+"/convert", "application/jscontact+json", bytes.NewReader(invalidCard))
	if err != nil {
		t.Fatalf("POST /convert failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("Expected 422 Unprocessable Entity, got %d", resp.StatusCode)
	}
}

func TestConvert_InvalidVCard_422(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/convert", "text/vcard", strings.NewReader("Not a vCard at all"))
	if err != nil {
		t.Fatalf("POST /convert failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("Expected 422 Unprocessable Entity, got %d", resp.StatusCode)
	}
}

func TestConvert_MethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/convert")
	if err != nil {
		t.Fatalf("GET /convert failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 Method Not Allowed, got %d", resp.StatusCode)
	}
	allow := resp.Header.Get("Allow")
	if !strings.Contains(allow, "POST") {
		t.Errorf("Expected Allow header containing POST, got %q", allow)
	}
}

func TestConvert_UnsupportedMediaType(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/convert", "application/pdf", strings.NewReader("fake pdf"))
	if err != nil {
		t.Fatalf("POST /convert failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("Expected 415 Unsupported Media Type, got %d", resp.StatusCode)
	}
}

func TestConvert_AutoDetectContentType(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. JSON auto-detected without Content-Type header
	jsContact := map[string]any{
		"@type": "Card",
		"name": map[string]any{
			"components": []any{
				map[string]any{"kind": "given", "value": "Alice"},
			},
		},
	}
	body, _ := json.Marshal(jsContact)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/convert", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /convert failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for auto-detected JSON, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. vCard auto-detected without Content-Type header
	vcardStr := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Bob\r\nEND:VCARD\r\n"
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/convert", strings.NewReader(vcardStr))
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST /convert failed: %v", err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for auto-detected vCard, got %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}
