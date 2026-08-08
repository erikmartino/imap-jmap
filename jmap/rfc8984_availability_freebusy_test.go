package jmap_test

import (
	"bytes"
	"context"
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
	if _, err := cb.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		Title: "Busy Meeting", Start: "2026-08-10T09:00:00Z", Duration: "PT2H",
	}); err != nil {
		t.Fatalf("seed busy: %v", err)
	}
	if _, err := cb.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		Title: "Free Block", Start: "2026-08-11T09:00:00Z", Duration: "PT1H", FreeBusyStatus: "free",
	}); err != nil {
		t.Fatalf("seed free: %v", err)
	}
	if _, err := cb.CreateCalendarEvent(context.Background(), &jmap.CalendarEvent{
		Title: "Secret", Start: "2026-08-12T09:00:00Z", Duration: "PT1H", Privacy: "secret",
	}); err != nil {
		t.Fatalf("seed secret: %v", err)
	}

	payload := map[string]any{
		"using":       []string{jmap.CoreCapabilityURI, jmap.PrincipalsCapabilityURI, jmap.AvailabilityCapabilityURI, jmap.CalendarsCapabilityURI},
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
