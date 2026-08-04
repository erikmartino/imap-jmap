package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_CreateSetValidation tests rejection of unknown properties and invalid enum values on CalendarEvent/set per RFC 8620 §5.3 / RFC 8984.
func TestRFC8984_CreateSetValidation(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// 1. Create with unknown property 'bogusProperty' -> MUST return invalidProperties in notCreated
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title":         "Valid Title",
					"bogusProperty": "invalid",
				},
				"c2": map[string]any{
					"title":  "Invalid Status Event",
					"status": "super-invalid-status",
				},
			},
		}, "call-1"},
	}

	resp := postJMAP(t, ts.URL, using, createReq)
	notCreated, ok := resp.MethodResponses[0].Args["notCreated"].(map[string]any)
	if !ok || notCreated["c1"] == nil || notCreated["c2"] == nil {
		t.Fatalf("expected notCreated entries for unknown property and invalid status, got %+v", resp.MethodResponses[0].Args)
	}

	errC1, _ := notCreated["c1"].(map[string]any)
	if errC1["type"] != "invalidProperties" {
		t.Errorf("expected type invalidProperties for c1, got %v", errC1["type"])
	}

	errC2, _ := notCreated["c2"].(map[string]any)
	if errC2["type"] != "invalidProperties" {
		t.Errorf("expected type invalidProperties for c2, got %v", errC2["type"])
	}

	// 2. Create valid event then attempt update with invalid privacy value
	validCreateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c3": map[string]any{
					"title":  "Valid Event for Update",
					"status": "confirmed",
				},
			},
		}, "call-2"},
	}

	resp2 := postJMAP(t, ts.URL, using, validCreateReq)
	created, _ := resp2.MethodResponses[0].Args["created"].(map[string]any)
	evID := created["c3"].(map[string]any)["id"].(string)

	updateReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"update": map[string]any{
				evID: map[string]any{
					"privacy": "top-secret-invalid",
				},
			},
		}, "call-3"},
	}

	updateResp := postJMAP(t, ts.URL, using, updateReq)
	notUpdated, ok := updateResp.MethodResponses[0].Args["notUpdated"].(map[string]any)
	if !ok || notUpdated[evID] == nil {
		t.Fatalf("expected notUpdated entry for invalid privacy value update, got %+v", updateResp.MethodResponses[0].Args)
	}
}
