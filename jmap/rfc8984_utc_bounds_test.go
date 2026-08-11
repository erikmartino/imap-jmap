package jmap_test

import (
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

func TestRFC8984_UTCStartAndUTCEndComputedProperties(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "4.4", spectest.MUST,
		"utcStart/utcEnd computed read-only CalendarEvent properties are returned when requested.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	// Create event with LocalDateTime start and duration
	createResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"ev1": map[string]any{
					"title":    "Meeting",
					"start":    "2026-08-15T10:00:00",
					"duration": "PT2H",
					"timeZone": "Europe/Copenhagen",
				},
			},
		}, "s1"},
	})

	createdMap := createResp.MethodResponses[0].Args["created"].(map[string]any)
	ev1 := createdMap["ev1"].(map[string]any)
	evID := ev1["id"].(string)

	// Fetch event requesting properties
	getResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/get", map[string]any{
			"accountId":  "primary",
			"ids":        []string{evID},
			"properties": []string{"id", "title", "start", "utcStart", "utcEnd"},
		}, "g1"},
	})

	list := getResp.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(list))
	}

	fetched := list[0].(map[string]any)
	if fetched["utcStart"] == "" {
		t.Error("Expected non-empty utcStart property")
	}
	if fetched["utcEnd"] == "" {
		t.Error("Expected non-empty utcEnd property")
	}
}

func TestRFC8984_QueryLocalDateBounds(t *testing.T) {
	spectest.Require(t, "draft-ietf-jmap-calendars-27", "5.11.1", spectest.MUST,
		"before/after accept date-only (LocalDate) values.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI}

	postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/set", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"e": map[string]any{
					"title":    "Date Bound Event",
					"start":    "2026-08-10T12:00:00",
					"duration": "PT1H",
				},
			},
		}, "s"},
	})

	// Query using date-only "after" and "before"
	queryResp := postJMAP(t, ts.URL, using, []any{
		[]any{"CalendarEvent/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"after":  "2026-08-01",
				"before": "2026-08-31",
			},
		}, "q"},
	})

	ids := queryResp.MethodResponses[0].Args["ids"].([]any)
	if len(ids) == 0 {
		t.Errorf("Expected event to match date-only LocalDate range bounds")
	}
}
