package nextcloud

import (
	"testing"

	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
)

func TestDummyCacheAlwaysShowsEmpty(t *testing.T) {
	// 1. RequestCache dummy
	rc := jmapcore.NewDummyRequestCache()
	rc.Store("foo", "bar")
	if val, ok := rc.Load("foo"); ok || val != nil {
		t.Fatalf("expected dummy request cache to return nil, false, got %v, %v", val, ok)
	}

	// 2. discoveryCache dummy
	var dc discoveryCache = &dummyDiscoveryCache{}
	dc.SetPrincipal("user", "principal")
	if p, ok := dc.GetPrincipal("user"); ok || p != "" {
		t.Fatalf("expected dummy discovery cache to return empty, false, got %q, %v", p, ok)
	}
	dc.SetHomeSet("user", "homeset")
	if hs, ok := dc.GetHomeSet("user"); ok || hs != "" {
		t.Fatalf("expected dummy discovery cache to return empty, false, got %q, %v", hs, ok)
	}
	dc.SetCalPath("user", "cal-1", "/path/")
	if cp, ok := dc.GetCalPath("user", "cal-1"); ok || cp != "" {
		t.Fatalf("expected dummy discovery cache to return empty, false, got %q, %v", cp, ok)
	}

	// 3. backendCache dummy
	var bc backendCache = &dummyBackendCache{}
	bc.SetCals("user", []*jmapcalendar.Calendar{{ID: "cal-1"}})
	if cals, ok := bc.GetCals("user"); ok || len(cals) > 0 {
		t.Fatalf("expected dummy backend cache to return nil, false, got %v, %v", cals, ok)
	}
	bc.StoreEvent("user", &jmapcalendar.CalendarEvent{ID: "ev-1"})
	if ev, ok := bc.GetEvent("user", "ev-1"); ok || ev != nil {
		t.Fatalf("expected dummy backend cache to return nil, false, got %v, %v", ev, ok)
	}
	if known := bc.GetCalIDsForEvent("user", "ev-1"); len(known) > 0 {
		t.Fatalf("expected dummy backend cache to return nil, got %v", known)
	}
}

func TestMemCacheStoresAndRetrieves(t *testing.T) {
	// 1. discoveryCache mem
	var dc discoveryCache = newMemDiscoveryCache()
	dc.SetPrincipal("user", "principal")
	if p, ok := dc.GetPrincipal("user"); !ok || p != "principal" {
		t.Fatalf("expected principal to be found, got %q, %v", p, ok)
	}
	dc.SetHomeSet("user", "homeset")
	if hs, ok := dc.GetHomeSet("user"); !ok || hs != "homeset" {
		t.Fatalf("expected homeset to be found, got %q, %v", hs, ok)
	}
	dc.SetCalPath("user", "cal-1", "/path/")
	if cp, ok := dc.GetCalPath("user", "cal-1"); !ok || cp != "/path/" {
		t.Fatalf("expected calPath to be found, got %q, %v", cp, ok)
	}

	// 2. backendCache mem
	var bc backendCache = newMemBackendCache()
	bc.SetCals("user", []*jmapcalendar.Calendar{{ID: "cal-1"}})
	if cals, ok := bc.GetCals("user"); !ok || len(cals) != 1 || cals[0].ID != "cal-1" {
		t.Fatalf("expected cals to be found, got %v, %v", cals, ok)
	}
	bc.StoreEvent("user", &jmapcalendar.CalendarEvent{
		ID:          "ev-1",
		CalendarIDs: map[jmapcore.Id]bool{"cal-1": true},
	})
	if ev, ok := bc.GetEvent("user", "ev-1"); !ok || ev == nil || ev.ID != "ev-1" {
		t.Fatalf("expected event to be found, got %v, %v", ev, ok)
	}
	if known := bc.GetCalIDsForEvent("user", "ev-1"); len(known) != 1 || known[0] != "cal-1" {
		t.Fatalf("expected calIDs to be found, got %v", known)
	}
}

func TestCacheDisabledByDefault(t *testing.T) {
	client := NewClient("http://example.com")
	if !client.IsCacheDisabled() {
		t.Fatalf("expected client cache to be disabled by default")
	}

	backend := NewCalendarsBackend(client)
	if _, ok := backend.cache.(*dummyBackendCache); !ok {
		t.Fatalf("expected backend cache to be dummyBackendCache by default, got %T", backend.cache)
	}
}
