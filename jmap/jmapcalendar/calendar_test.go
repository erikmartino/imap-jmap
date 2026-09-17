package jmapcalendar

import (
	"context"
	"testing"
	"time"
)

func TestCalendarRights(t *testing.T) {
	rights := FullCalendarRights()
	if !rights.MayReadFreeBusy || !rights.MayWriteAll || !rights.MayDelete {
		t.Errorf("FullCalendarRights expected all true, got %+v", rights)
	}
}

func TestComputeUTCStartAndEnd(t *testing.T) {
	start := "2026-09-17T10:00:00"
	timeZone := "UTC"
	duration := "PT1H"

	utcStart := ComputeUTCStart(start, timeZone)
	if utcStart != "2026-09-17T10:00:00Z" {
		t.Errorf("expected 2026-09-17T10:00:00Z, got %s", utcStart)
	}

	utcEnd := ComputeUTCEnd(start, duration, timeZone)
	if utcEnd != "2026-09-17T11:00:00Z" {
		t.Errorf("expected 2026-09-17T11:00:00Z, got %s", utcEnd)
	}
}

func TestParseISODuration(t *testing.T) {
	d, ok := ParseISODuration("PT1H30M")
	if !ok {
		t.Fatalf("failed to parse valid ISO duration PT1H30M")
	}
	expected := 90 * time.Minute
	if d != expected {
		t.Errorf("expected %v, got %v", expected, d)
	}

	_, ok = ParseISODuration("invalid")
	if ok {
		t.Errorf("expected false for invalid duration")
	}
}

func TestLoadLocation(t *testing.T) {
	loc := LoadLocation("America/New_York")
	if loc == nil || loc.String() != "America/New_York" {
		t.Errorf("expected America/New_York, got %v", loc)
	}

	locUTC := LoadLocation("Invalid/TZ")
	if locUTC != time.UTC {
		t.Errorf("expected fallback to UTC for invalid timezone, got %v", locUTC)
	}
}

func TestExpandGroupRecipientsEmpty(t *testing.T) {
	res := ExpandGroupRecipients(context.Background(), nil, map[string]string{"foo": "bar"})
	if len(res) != 1 || res["foo"] != "bar" {
		t.Errorf("expected unchanged map when backend is nil, got %v", res)
	}
}
