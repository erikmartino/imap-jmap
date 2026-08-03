package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC8984_Capability tests urn:ietf:params:jmap:calendars capability discovery per JMAP for Calendars.
func TestRFC8984_Capability(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/jmap")
	if err != nil {
		t.Fatalf("GET /.well-known/jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var session jmap.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		t.Fatalf("Failed to decode session: %v", err)
	}

	capRaw, ok := session.Capabilities[jmap.CalendarsCapabilityURI]
	if !ok {
		t.Fatalf("Capability %q missing in Session capabilities", jmap.CalendarsCapabilityURI)
	}

	capBytes, _ := json.Marshal(capRaw)
	var calendarsCap jmap.CalendarsCapability
	_ = json.Unmarshal(capBytes, &calendarsCap)

	if !calendarsCap.MayCreateCalendar {
		t.Errorf("Expected mayCreateCalendar true, got false")
	}
}

// TestRFC8984_Calendar_GetAndSet tests Calendar/get and Calendar/set.
func TestRFC8984_Calendar_GetAndSet(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Calendar/get default calendar
	reqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{"Calendar/get", map[string]any{"accountId": "primary"}, "c1"},
		},
	}

	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&jmapResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(jmapResp.MethodResponses) != 1 || jmapResp.MethodResponses[0].Name != "Calendar/get" {
		t.Fatalf("Unexpected response format: %v", jmapResp.MethodResponses)
	}

	args := jmapResp.MethodResponses[0].Args
	listRaw := args["list"].([]any)
	if len(listRaw) != 1 {
		t.Fatalf("Expected 1 default calendar, got %d", len(listRaw))
	}

	// 2. Calendar/set create new calendar
	setReqBody := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"Calendar/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"cal1": map[string]any{
							"name":  "Work Calendar",
							"color": "#ff0000",
						},
					},
				},
				"c2",
			},
		},
	}

	bodyBytes2, _ := json.Marshal(setReqBody)
	resp2, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes2))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	defer resp2.Body.Close()

	var setResp jmap.Response
	if err := json.NewDecoder(resp2.Body).Decode(&setResp); err != nil {
		t.Fatalf("Failed to decode set response: %v", err)
	}

	createdMap := setResp.MethodResponses[0].Args["created"].(map[string]any)
	createdCal := createdMap["cal1"].(map[string]any)
	calID := createdCal["id"].(string)
	if calID == "" {
		t.Fatalf("Expected non-empty calendar ID")
	}
	if createdCal["name"] != "Work Calendar" {
		t.Errorf("Expected calendar name 'Work Calendar', got %v", createdCal["name"])
	}
}

// TestRFC8984_CalendarEvent_GetSetQuery tests CalendarEvent methods & JSCalendar properties per RFC 8984.
func TestRFC8984_CalendarEvent_GetSetQuery(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. CalendarEvent/set create event with JSCalendar properties
	createReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"CalendarEvent/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"ev1": map[string]any{
							"title":       "Team Sync",
							"description": "Weekly alignment meeting",
							"start":       "2026-08-05T10:00:00Z",
							"duration":    "PT1H",
							"timeZone":    "UTC",
							"status":      "confirmed",
							"location": map[string]any{
								"name": "Meeting Room A",
							},
							"participants": map[string]any{
								"p1": map[string]any{
									"name":   "Alice Smith",
									"email":  "alice@example.com",
									"role":   "chair",
									"status": "accepted",
								},
							},
							"recurrenceRules": []any{
								map[string]any{
									"frequency": "weekly",
									"interval":  1,
									"byDay":     []string{"we"},
								},
							},
							"alerts": map[string]any{
								"a1": map[string]any{
									"trigger": "-PT15M",
									"action":  "display",
								},
							},
						},
					},
				},
				"c1",
			},
		},
	}

	bodyBytes, _ := json.Marshal(createReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("JMAP POST failed: %v", err)
	}
	defer resp.Body.Close()

	var createResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		t.Fatalf("Failed to decode create response: %v", err)
	}

	createdMap := createResp.MethodResponses[0].Args["created"].(map[string]any)
	createdEv := createdMap["ev1"].(map[string]any)
	evID := createdEv["id"].(string)
	if evID == "" {
		t.Fatalf("Expected non-empty event ID")
	}

	// 2. CalendarEvent/query by title
	queryReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"CalendarEvent/query",
				map[string]any{
					"accountId": "primary",
					"filter": map[string]any{
						"title": "Team Sync",
					},
				},
				"c2",
			},
		},
	}

	bodyBytesQuery, _ := json.Marshal(queryReq)
	respQuery, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytesQuery))
	if err != nil {
		t.Fatalf("JMAP POST query failed: %v", err)
	}
	defer respQuery.Body.Close()

	var queryResp jmap.Response
	if err := json.NewDecoder(respQuery.Body).Decode(&queryResp); err != nil {
		t.Fatalf("Failed to decode query response: %v", err)
	}

	idsRaw := queryResp.MethodResponses[0].Args["ids"].([]any)
	if len(idsRaw) != 1 || idsRaw[0].(string) != evID {
		t.Fatalf("Expected query to return event ID %s, got %v", evID, idsRaw)
	}

	// 3. CalendarEvent/get event details
	getReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"CalendarEvent/get",
				map[string]any{
					"accountId": "primary",
					"ids":       []string{evID},
				},
				"c3",
			},
		},
	}

	bodyBytesGet, _ := json.Marshal(getReq)
	respGet, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytesGet))
	if err != nil {
		t.Fatalf("JMAP POST get failed: %v", err)
	}
	defer respGet.Body.Close()

	var getResp jmap.Response
	if err := json.NewDecoder(respGet.Body).Decode(&getResp); err != nil {
		t.Fatalf("Failed to decode get response: %v", err)
	}

	list := getResp.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(list))
	}

	evData := list[0].(map[string]any)
	if evData["title"] != "Team Sync" {
		t.Errorf("Expected title 'Team Sync', got %v", evData["title"])
	}
	if evData["@type"] != "Event" {
		t.Errorf("Expected @type 'Event', got %v", evData["@type"])
	}
}

// TestRFC8984_JSCalendarFullEvent tests links and virtualLocations in JSCalendar events per RFC 8984.
func TestRFC8984_JSCalendarFullEvent(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	createReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"CalendarEvent/set",
				map[string]any{
					"accountId": "primary",
					"create": map[string]any{
						"evFull": map[string]any{
							"title":       "Architecture Review",
							"start":       "2026-10-10T14:00:00Z",
							"duration":    "PT2H",
							"timeZone":    "UTC",
							"description": "Design review for new specs",
							"virtualLocations": map[string]any{
								"vl1": map[string]any{
									"uri":         "https://meet.example.com/arch-review",
									"name":        "Video Conference Room",
									"description": "Join via web browser",
								},
							},
							"links": map[string]any{
								"l1": map[string]any{
									"href":  "https://example.com/docs/arch-spec.pdf",
									"rel":   "enclosure",
									"type":  "application/pdf",
									"title": "Architecture Specification",
								},
							},
						},
					},
				},
				"c1",
			},
		},
	}

	bodyBytes, _ := json.Marshal(createReq)
	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("CalendarEvent/set failed: %v", err)
	}
	defer resp.Body.Close()

	var createResp jmap.Response
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		t.Fatalf("Decode CalendarEvent/set response failed: %v", err)
	}

	createdMap := createResp.MethodResponses[0].Args["created"].(map[string]any)
	createdEv := createdMap["evFull"].(map[string]any)
	evID := createdEv["id"].(string)

	// Fetch event via CalendarEvent/get
	getReq := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{
				"CalendarEvent/get",
				map[string]any{
					"accountId": "primary",
					"ids":       []string{evID},
				},
				"c2",
			},
		},
	}

	bodyBytesGet, _ := json.Marshal(getReq)
	respGet, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(bodyBytesGet))
	if err != nil {
		t.Fatalf("CalendarEvent/get failed: %v", err)
	}
	defer respGet.Body.Close()

	var getResp jmap.Response
	if err := json.NewDecoder(respGet.Body).Decode(&getResp); err != nil {
		t.Fatalf("Decode CalendarEvent/get failed: %v", err)
	}

	list := getResp.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(list))
	}

	evData := list[0].(map[string]any)
	vlMap := evData["virtualLocations"].(map[string]any)
	if vl1, ok := vlMap["vl1"].(map[string]any); !ok || vl1["uri"] != "https://meet.example.com/arch-review" {
		t.Errorf("Expected virtualLocation uri 'https://meet.example.com/arch-review', got %v", vlMap["vl1"])
	}

	linksMap := evData["links"].(map[string]any)
	if l1, ok := linksMap["l1"].(map[string]any); !ok || l1["href"] != "https://example.com/docs/arch-spec.pdf" {
		t.Errorf("Expected link href 'https://example.com/docs/arch-spec.pdf', got %v", linksMap["l1"])
	}
}

// TestRFC8984_CalendarAndEventCopy tests Calendar/copy and CalendarEvent/copy per RFC 8984 Section 4.
func TestRFC8984_CalendarAndEventCopy(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqPayload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{
			[]any{"Calendar/copy", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"cal1": map[string]any{"name": "Copied Calendar"},
				},
			}, "c1"},
			[]any{"CalendarEvent/copy", map[string]any{
				"accountId": "primary",
				"create": map[string]any{
					"ev1": map[string]any{
						"calendarIds": map[string]bool{"cal-default": true},
						"title":       "Copied Event",
						"start":       "2026-11-01T10:00:00Z",
					},
				},
			}, "c2"},
		},
	}
	body, _ := json.Marshal(reqPayload)

	resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /jmap failed: %v", err)
	}
	defer resp.Body.Close()

	var jmapResp jmap.Response
	_ = json.NewDecoder(resp.Body).Decode(&jmapResp)

	if len(jmapResp.MethodResponses) != 2 {
		t.Fatalf("Expected 2 method responses, got %d", len(jmapResp.MethodResponses))
	}

	calResp := jmapResp.MethodResponses[0]
	if calResp.Name != "Calendar/copy" {
		t.Errorf("Expected 'Calendar/copy', got %q", calResp.Name)
	}

	evResp := jmapResp.MethodResponses[1]
	if evResp.Name != "CalendarEvent/copy" {
		t.Errorf("Expected 'CalendarEvent/copy', got %q", evResp.Name)
	}
}

// TestRFC8984_CalendarCopyRoundTrip proves Calendar/copy actually copies an existing calendar
// (by source id, with a property override) into a new object, per RFC 8620 Section 5.4.
func TestRFC8984_CalendarCopyRoundTrip(t *testing.T) {
	calBackend := memory.NewMemoryCalendarsBackend()
	srv := jmap.NewServer(nil, jmap.WithCalendarsBackend(calBackend))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(calls []any) jmap.Response {
		payload := map[string]any{
			"using":       []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI},
			"methodCalls": calls,
		}
		body, _ := json.Marshal(payload)
		resp, err := http.Post(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /jmap failed: %v", err)
		}
		defer resp.Body.Close()
		var jr jmap.Response
		_ = json.NewDecoder(resp.Body).Decode(&jr)
		return jr
	}

	// Create the source calendar.
	createResp := post([]any{
		[]any{"Calendar/set", map[string]any{
			"accountId": "primary",
			"create":    map[string]any{"orig": map[string]any{"name": "Original"}},
		}, "c1"},
	})
	srcID := createResp.MethodResponses[0].Args["created"].(map[string]any)["orig"].(map[string]any)["id"].(string)
	if srcID == "" {
		t.Fatal("no source calendar id")
	}

	// Copy it, overriding the name.
	copyResp := post([]any{
		[]any{"Calendar/copy", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"dup": map[string]any{"id": srcID, "name": "Copied Calendar"},
			},
		}, "c2"},
	})
	args := copyResp.MethodResponses[0].Args
	if nc, ok := args["notCreated"].(map[string]any); ok && len(nc) != 0 {
		t.Fatalf("copy reported failures: %#v", nc)
	}
	dup, ok := args["created"].(map[string]any)["dup"].(map[string]any)
	if !ok {
		t.Fatalf("copy did not create 'dup': %#v", args["created"])
	}
	if dup["id"] == srcID || dup["id"] == "" {
		t.Errorf("copy MUST assign a new id, got %v (src %q)", dup["id"], srcID)
	}
	if dup["name"] != "Copied Calendar" {
		t.Errorf("override not applied: got name %v", dup["name"])
	}

	// Both the original and the copy MUST now exist.
	getResp := post([]any{
		[]any{"Calendar/get", map[string]any{"accountId": "primary"}, "c3"},
	})
	list, _ := getResp.MethodResponses[0].Args["list"].([]any)
	ids := map[string]bool{}
	for _, item := range list {
		if c, ok := item.(map[string]any); ok {
			ids[c["id"].(string)] = true
		}
	}
	if !ids[srcID] || !ids[dup["id"].(string)] {
		t.Errorf("expected both original %q and copy %q to exist, got ids %v", srcID, dup["id"], ids)
	}
}
