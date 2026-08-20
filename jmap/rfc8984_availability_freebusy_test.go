package jmap_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

// TestRFC8984_AvailabilityBusyWindows verifies Principal/getAvailability emits real busy windows:
// end = start + duration (not a zero-length window), and events that are "free", cancelled, or
// "secret" do not contribute to the free-busy shown to other principals.
func TestRFC8984_AvailabilityBusyWindows(t *testing.T) {
	pb := memory.NewMemoryPrincipalsBackend()
	cb := memory.NewMemoryCalendarsBackend()
	pb.SetCalendarsBackend(cb)

	srv := jmap.NewServer(nil, jmap.WithPrincipalsBackend(pb), jmap.WithCalendarsBackend(cb))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Busy 2h meeting; a free event; a secret event — all in the query window.
	if _, err := cb.CreateCalendarEvent(seedCtx(), &jmap.CalendarEvent{
		Title: "Busy Meeting", Start: "2026-08-10T09:00:00Z", Duration: "PT2H",
	}); err != nil {
		t.Fatalf("seed busy: %v", err)
	}
	if _, err := cb.CreateCalendarEvent(seedCtx(), &jmap.CalendarEvent{
		Title: "Free Block", Start: "2026-08-11T09:00:00Z", Duration: "PT1H", FreeBusyStatus: "free",
	}); err != nil {
		t.Fatalf("seed free: %v", err)
	}
	if _, err := cb.CreateCalendarEvent(seedCtx(), &jmap.CalendarEvent{
		Title: "Secret", Start: "2026-08-12T09:00:00Z", Duration: "PT1H", Privacy: "secret",
	}); err != nil {
		t.Fatalf("seed secret: %v", err)
	}

	payload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.PrincipalsCapabilityURI, jmap.AvailabilityCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{[]any{"Principal/getAvailability", map[string]any{
			"accountId":   "primary",
			"principalId": "p-primary",
			"utcStart":    "2026-08-01T00:00:00Z",
			"utcEnd":      "2026-08-31T23:59:59Z",
		}, "c1"}},
	}
	body, _ := json.Marshal(payload)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var jr jmap.Response
	_ = json.NewDecoder(resp.Body).Decode(&jr)

	list, _ := jr.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected exactly 1 busy window (free+secret excluded), got %d: %+v", len(list), list)
	}
	w := list[0].(map[string]any)
	if w["utcStart"] != "2026-08-10T09:00:00Z" || w["utcEnd"] != "2026-08-10T11:00:00Z" {
		t.Errorf("expected 09:00->11:00 window, got start=%v end=%v", w["utcStart"], w["utcEnd"])
	}
	if w["freeBusyStatus"] != "busy" {
		t.Errorf("expected freeBusyStatus busy, got %v", w["freeBusyStatus"])
	}
}

// TestRFC8984_AvailabilityCrossPrincipal verifies Principal/getAvailability resolves the target principal's
// distinct account context and returns busy windows from that target principal's calendars.
func TestRFC8984_AvailabilityCrossPrincipal(t *testing.T) {
	pb := memory.NewMemoryPrincipalsBackend()
	cb := memory.NewMemoryCalendarsBackend()
	pb.SetCalendarsBackend(cb)

	srv := jmap.NewServer(nil, jmap.WithPrincipalsBackend(pb), jmap.WithCalendarsBackend(cb))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	aliceAccID := jmap.AccountIDForSubject("alice@example.com")
	alicePrincipal := &jmap.Principal{
		ID:                 "p-alice",
		Type:               "individual",
		Name:               "Alice",
		Email:              "alice@example.com",
		CalendarAddress:    "mailto:alice@example.com",
		MayGetAvailability: true,
		MayShareWith:       true,
		AccountIDs:         map[string]bool{aliceAccID: true},
	}
	if _, err := pb.CreatePrincipal(seedCtx(), alicePrincipal); err != nil {
		t.Fatalf("seed alice principal: %v", err)
	}

	// Create event in Alice's distinct account context
	aliceCtx := jmap.ContextWithAccountID(seedCtx(), aliceAccID)
	if _, err := cb.CreateCalendarEvent(aliceCtx, &jmap.CalendarEvent{
		Title: "Alice Busy Meeting", Start: "2026-08-15T14:00:00Z", Duration: "PT1H",
	}); err != nil {
		t.Fatalf("seed alice event: %v", err)
	}

	payload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.PrincipalsCapabilityURI, jmap.AvailabilityCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{[]any{"Principal/getAvailability", map[string]any{
			"accountId":   "primary",
			"principalId": "p-alice",
			"utcStart":    "2026-08-01T00:00:00Z",
			"utcEnd":      "2026-08-31T23:59:59Z",
		}, "c1"}},
	}
	body, _ := json.Marshal(payload)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var jr jmap.Response
	_ = json.NewDecoder(resp.Body).Decode(&jr)

	list, _ := jr.MethodResponses[0].Args["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 busy window from Alice's account, got %d: %+v", len(list), list)
	}
	w := list[0].(map[string]any)
	if w["utcStart"] != "2026-08-15T14:00:00Z" || w["utcEnd"] != "2026-08-15T15:00:00Z" {
		t.Errorf("expected 14:00->15:00 window, got start=%v end=%v", w["utcStart"], w["utcEnd"])
	}
}

// TestRFC8984_AvailabilityIncludeInAvailability verifies that the calendar-level includeInAvailability
// setting ("all", "none", "attending") is strictly respected when computing free-busy.
func TestRFC8984_AvailabilityIncludeInAvailability(t *testing.T) {
	pb := memory.NewMemoryPrincipalsBackend()
	cb := memory.NewMemoryCalendarsBackend()
	pb.SetCalendarsBackend(cb)

	srv := jmap.NewServer(nil, jmap.WithPrincipalsBackend(pb), jmap.WithCalendarsBackend(cb))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := seedCtx()

	// 1. Calendar with includeInAvailability = "none"
	calNone, err := cb.CreateCalendar(ctx, &jmap.Calendar{
		Name:                  "External Holidays",
		IncludeInAvailability: "none",
	})
	if err != nil {
		t.Fatalf("create calNone: %v", err)
	}

	// 2. Calendar with includeInAvailability = "attending"
	calAttending, err := cb.CreateCalendar(ctx, &jmap.Calendar{
		Name:                  "Optional Team Events",
		IncludeInAvailability: "attending",
	})
	if err != nil {
		t.Fatalf("create calAttending: %v", err)
	}

	// 3. Calendar with includeInAvailability = "all" (default)
	calAll, err := cb.CreateCalendar(ctx, &jmap.Calendar{
		Name:                  "Work",
		IncludeInAvailability: "all",
	})
	if err != nil {
		t.Fatalf("create calAll: %v", err)
	}

	// Event on "none" calendar -> must NOT appear
	if _, err := cb.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
		Title: "Holiday Event", Start: "2026-08-20T10:00:00Z", Duration: "PT1H",
		CalendarIDs: map[jmap.Id]bool{calNone.ID: true},
	}); err != nil {
		t.Fatalf("seed none event: %v", err)
	}

	// Event on "all" calendar -> must appear
	if _, err := cb.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
		Title: "Important Work", Start: "2026-08-21T10:00:00Z", Duration: "PT1H",
		CalendarIDs: map[jmap.Id]bool{calAll.ID: true},
	}); err != nil {
		t.Fatalf("seed all event: %v", err)
	}

	// Event on "attending" calendar where user has accepted -> must appear
	if _, err := cb.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
		Title: "Accepted Team Sync", Start: "2026-08-22T10:00:00Z", Duration: "PT1H",
		CalendarIDs: map[jmap.Id]bool{calAttending.ID: true},
		Participants: map[string]*jmap.JSCalendarParticipant{
			"p1": {
				Email:               "user@example.com",
				ParticipationStatus: "accepted",
			},
		},
	}); err != nil {
		t.Fatalf("seed attending event accepted: %v", err)
	}

	// Event on "attending" calendar where user declined / needs-action -> must NOT appear
	if _, err := cb.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
		Title: "Ignored Team Lunch", Start: "2026-08-23T10:00:00Z", Duration: "PT1H",
		CalendarIDs: map[jmap.Id]bool{calAttending.ID: true},
		Participants: map[string]*jmap.JSCalendarParticipant{
			"p1": {
				Email:               "user@example.com",
				ParticipationStatus: "declined",
			},
		},
	}); err != nil {
		t.Fatalf("seed attending event declined: %v", err)
	}

	payload := map[string]any{
		"using": []string{jmap.CoreCapabilityURI, jmap.PrincipalsCapabilityURI, jmap.AvailabilityCapabilityURI, jmap.CalendarsCapabilityURI},
		"methodCalls": []any{[]any{"Principal/getAvailability", map[string]any{
			"accountId":   "primary",
			"principalId": "p-primary",
			"utcStart":    "2026-08-01T00:00:00Z",
			"utcEnd":      "2026-08-31T23:59:59Z",
		}, "c1"}},
	}
	body, _ := json.Marshal(payload)
	resp, err := authedPost(ts.URL+"/jmap", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	var jr jmap.Response
	_ = json.NewDecoder(resp.Body).Decode(&jr)

	list, _ := jr.MethodResponses[0].Args["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("expected exactly 2 busy windows (Work + Accepted Team Sync), got %d: %+v", len(list), list)
	}
}
