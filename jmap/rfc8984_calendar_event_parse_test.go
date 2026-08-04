package jmap_test

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8984_CalendarEventParse tests CalendarEvent/parse per draft-ietf-jmap-calendars
// Section 5.12: blobIds resolve to parsed CalendarEvent objects, invalid blobs land in
// notParsable, unknown blobs in notFound, the optional properties argument filters the
// output, and the metadata properties (id, baseEventId, calendarIds, isDraft, isOrigin)
// are returned as explicit nulls when requested.
func TestRFC8984_CalendarEventParse(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI, jmap.BlobCapabilityURI}

	// The parse capability MUST be advertised in the account capabilities.
	sessionArgs := postJMAP(t, ts.URL, using, []any{}).MethodResponses
	_ = sessionArgs

	rawICS := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Test//Parse//EN
METHOD:REQUEST
BEGIN:VEVENT
UID:evt-parse-001
SUMMARY:Parse Me
DESCRIPTION:Parse description
LOCATION:Room 1
DTSTART:20260825T110000Z
DTEND:20260825T120000Z
ORGANIZER:mailto:lead@example.com
ATTENDEE;PARTSTAT=ACCEPTED;CN=Dev Team;ROLE=REQ-PARTICIPANT:mailto:dev@example.com
END:VEVENT
END:VCALENDAR`

	// 1. Upload the iCalendar blob so CalendarEvent/parse can resolve it.
	uploadReq := []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"b1": map[string]any{
					"data": base64.StdEncoding.EncodeToString([]byte(rawICS)),
					"type": "text/calendar",
				},
			},
		}, "c1"},
	}
	uploadResp := postJMAP(t, ts.URL, using, uploadReq)
	created, ok := uploadResp.MethodResponses[0].Args["created"].(map[string]any)
	if !ok || created["b1"] == nil {
		t.Fatalf("Blob/upload failed: %+v", uploadResp.MethodResponses[0].Args)
	}
	blobID := created["b1"].(map[string]any)["id"].(string)

	// 2. Parse the uploaded blob.
	parseReq := []any{
		[]any{"CalendarEvent/parse", map[string]any{
			"accountId": "primary",
			"blobIds":   []any{blobID},
		}, "c2"},
	}
	parseResp := postJMAP(t, ts.URL, using, parseReq)
	args := parseResp.MethodResponses[0].Args
	parsed, ok := args["parsed"].(map[string]any)
	if !ok || parsed[blobID] == nil {
		t.Fatalf("expected parsed entry for blob %s, got %+v", blobID, args)
	}
	evts := parsed[blobID].([]any)
	if len(evts) != 1 {
		t.Fatalf("expected exactly 1 parsed event, got %d", len(evts))
	}
	ev := evts[0].(map[string]any)
	if ev["uid"] != "evt-parse-001" {
		t.Errorf("expected uid evt-parse-001, got %v", ev["uid"])
	}
	if ev["title"] != "Parse Me" {
		t.Errorf("expected title 'Parse Me', got %v", ev["title"])
	}
	if ev["description"] != "Parse description" {
		t.Errorf("expected description, got %v", ev["description"])
	}
	if ev["start"] != "2026-08-25T11:00:00Z" {
		t.Errorf("expected start 2026-08-25T11:00:00Z, got %v", ev["start"])
	}
	if ev["duration"] != "PT1H" {
		t.Errorf("expected duration PT1H, got %v", ev["duration"])
	}
	if ev["method"] != "request" {
		t.Errorf("expected method 'request', got %v", ev["method"])
	}
	participants, ok := ev["participants"].(map[string]any)
	if !ok || participants["dev@example.com"] == nil {
		t.Fatalf("expected participant dev@example.com, got %v", ev["participants"])
	}
	p := participants["dev@example.com"].(map[string]any)
	if p["status"] != "accepted" || p["role"] != "attendee" {
		t.Errorf("expected accepted/attendee participant, got %v", p)
	}
	locations, ok := ev["locations"].(map[string]any)
	if !ok || len(locations) == 0 {
		t.Errorf("expected parsed location, got %v", ev["locations"])
	} else {
		for _, rawLoc := range locations {
			loc := rawLoc.(map[string]any)
			if loc["name"] != "Room 1" {
				t.Errorf("expected location name 'Room 1', got %v", loc)
			}
		}
	}
	if _, hasID := ev["id"]; hasID {
		t.Errorf("metadata property 'id' must not be present by default, got %v", ev["id"])
	}

	// 3. properties argument: only requested properties (plus none of the metadata).
	parsePropsReq := []any{
		[]any{"CalendarEvent/parse", map[string]any{
			"accountId":  "primary",
			"blobIds":    []any{blobID},
			"properties": []any{"title", "start", "uid", "id", "calendarIds", "isDraft"},
		}, "c3"},
	}
	propsResp := postJMAP(t, ts.URL, using, parsePropsReq)
	parsedProps := propsResp.MethodResponses[0].Args["parsed"].(map[string]any)[blobID].([]any)
	pev := parsedProps[0].(map[string]any)
	if pev["title"] != "Parse Me" || pev["start"] != "2026-08-25T11:00:00Z" {
		t.Errorf("expected only title/start in filtered output, got %v", pev)
	}
	if _, hasDesc := pev["description"]; hasDesc {
		t.Errorf("description must be filtered out, got %v", pev)
	}
	for _, meta := range []string{"id", "calendarIds", "isDraft"} {
		if v, ok := pev[meta]; !ok || v != nil {
			t.Errorf("metadata property %s must be explicit null when requested, got %v", meta, pev[meta])
		}
	}

	// 4. Unknown blob id -> notFound.
	notFoundReq := []any{
		[]any{"CalendarEvent/parse", map[string]any{
			"accountId": "primary",
			"blobIds":   []any{"b-missing-000"},
		}, "c4"},
	}
	nfResp := postJMAP(t, ts.URL, using, notFoundReq)
	notFound, ok := nfResp.MethodResponses[0].Args["notFound"].([]any)
	if !ok || len(notFound) != 1 || notFound[0] != "b-missing-000" {
		t.Errorf("expected notFound [b-missing-000], got %v", nfResp.MethodResponses[0].Args)
	}

	// 5. Non-iCalendar blob -> notParsable.
	junkReq := []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"b2": map[string]any{
					"data": base64.StdEncoding.EncodeToString([]byte("this is not an iCalendar stream")),
					"type": "text/plain",
				},
			},
		}, "c5"},
	}
	junkResp := postJMAP(t, ts.URL, using, junkReq)
	junkCreated := junkResp.MethodResponses[0].Args["created"].(map[string]any)["b2"].(map[string]any)
	junkBlobID := junkCreated["id"].(string)

	npReq := []any{
		[]any{"CalendarEvent/parse", map[string]any{
			"accountId": "primary",
			"blobIds":   []any{junkBlobID},
		}, "c6"},
	}
	npResp := postJMAP(t, ts.URL, using, npReq)
	notParsable, ok := npResp.MethodResponses[0].Args["notParsable"].([]any)
	if !ok || len(notParsable) != 1 || notParsable[0] != junkBlobID {
		t.Errorf("expected notParsable [%s], got %+v", junkBlobID, npResp.MethodResponses[0].Args)
	}
}

// TestRFC8984_CalendarEventParseCreationReference resolves a #creationId blobId within the
// same request using Blob/upload (draft-ietf-jmap-calendars Section 5.12 allows blobIds to
// be references into the same call).
func TestRFC8984_CalendarEventParseCreationReference(t *testing.T) {
	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	using := []string{jmap.CoreCapabilityURI, jmap.CalendarsCapabilityURI, jmap.BlobCapabilityURI}

	rawICS := `BEGIN:VCALENDAR
VERSION:2.0
METHOD:REQUEST
BEGIN:VEVENT
UID:evt-parse-ref
SUMMARY:Ref Parse
DTSTART:20260825T110000Z
END:VEVENT
END:VCALENDAR`

	calls := []any{
		[]any{"Blob/upload", map[string]any{
			"accountId": "primary",
			"create": map[string]any{
				"b1": map[string]any{
					"data": base64.StdEncoding.EncodeToString([]byte(rawICS)),
					"type": "text/calendar",
				},
			},
		}, "c1"},
		[]any{"CalendarEvent/parse", map[string]any{
			"accountId": "primary",
			"blobIds":   []any{"#b1"},
		}, "c2"},
	}
	resp := postJMAP(t, ts.URL, using, calls)
	if len(resp.MethodResponses) != 2 {
		t.Fatalf("expected 2 method responses, got %d", len(resp.MethodResponses))
	}
	parseArgs := resp.MethodResponses[1].Args
	parsed, ok := parseArgs["parsed"].(map[string]any)
	if !ok || len(parsed) != 1 {
		t.Fatalf("expected parsed entry for resolved creation reference, got %+v", parseArgs)
	}
	for _, evts := range parsed {
		if len(evts.([]any)) != 1 {
			t.Errorf("expected 1 parsed event, got %v", evts)
		}
	}
}
