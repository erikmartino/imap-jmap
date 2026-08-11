package jmap_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionEndpoint(t *testing.T) {
	srv, _ := newAuthTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/version")
	if err != nil {
		t.Fatalf("GET /version failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var info map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("Decode version response failed: %v", err)
	}

	if info["version"] == "" {
		t.Error("Expected version key in response")
	}
}
