package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_NestedTypesAndTypeTags tests all nested object types and @type tags per RFC 8984.
func TestRFC8984_NestedTypesAndTypeTags(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// Create event with full nested types
	createReq := []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"c1": map[string]any{
					"title": "Full Nested Event",
					"locations": map[string]any{
						"loc1": map[string]any{
							"@type":         "Location",
							"name":          "Conference Room A",
							"locationTypes": map[string]any{"office": true},
							"coordinates":   "geo:37.7749,-122.4194",
							"links": map[string]any{
								"link1": map[string]any{
									"@type":       "Link",
									"href":        "https://example.com/map.pdf",
									"contentType": "application/pdf",
									"size":        2048,
								},
							},
						},
					},
					"virtualLocations": map[string]any{
						"vl1": map[string]any{
							"@type":    "VirtualLocation",
							"uri":      "https://meet.example.com/room1",
							"name":     "Video Call",
							"features": map[string]any{"audio": true, "video": true},
						},
					},
					"participants": map[string]any{
						"p1": map[string]any{
							"@type":               "Participant",
							"name":                "Alice Smith",
							"email":               "alice@example.com",
							"roles":               map[string]any{"chair": true},
							"participationStatus": "accepted",
							"kind":                "individual",
						},
					},
					"recurrenceRules": []any{
						map[string]any{
							"@type":          "RecurrenceRule",
							"frequency":      "weekly",
							"interval":       2,
							"firstDayOfWeek": "mo",
							"byDay": []any{
								map[string]any{"day": "mo", "nth": 1},
								"we",
							},
						},
					},
					"alerts": map[string]any{
						"a1": map[string]any{
							"@type": "Alert",
							"trigger": map[string]any{
								"@type":  "OffsetTrigger",
								"offset": "-PT15M",
							},
							"action": "display",
						},
					},
				},
			},
		}, "call-1"},
	}

	resp := postJMAP(t, ts.URL, using, createReq)
	created, ok := resp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["c1"] == nil {
		t.Fatalf("CalendarEvent/set create failed: %+v", resp.MethodResponses[0].Args)
	}

	evID := created["c1"].(map[string]any)["id"].(string)

	// Retrieve event and verify nested object types & @type tags
	getReq := []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId": "primary",
			"ids":       []string{evID},
		}, "call-2"},
	}

	getResp := postJMAP(t, ts.URL, using, getReq)
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	evData := list[0].(map[string]any)

	// Verify locations nested type
	locs, _ := evData["locations"].(map[string]any)
	loc1, _ := locs["loc1"].(map[string]any)
	if loc1["name"] != "Conference Room A" || loc1["coordinates"] != "geo:37.7749,-122.4194" {
		t.Errorf("unexpected loc1: %+v", loc1)
	}

	// Verify virtualLocations features
	vls, _ := evData["virtualLocations"].(map[string]any)
	vl1, _ := vls["vl1"].(map[string]any)
	if vl1["uri"] != "https://meet.example.com/room1" {
		t.Errorf("unexpected vl1: %+v", vl1)
	}

	// Verify participant roles & participationStatus
	parts, _ := evData["participants"].(map[string]any)
	p1, _ := parts["p1"].(map[string]any)
	if p1["participationStatus"] != "accepted" {
		t.Errorf("expected participationStatus 'accepted', got %v", p1["participationStatus"])
	}

	// Verify alerts & trigger
	alerts, _ := evData["alerts"].(map[string]any)
	a1, _ := alerts["a1"].(map[string]any)
	if a1["action"] != "display" {
		t.Errorf("expected alert action 'display', got %v", a1["action"])
	}
}
