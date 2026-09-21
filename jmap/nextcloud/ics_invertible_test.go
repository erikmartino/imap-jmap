package nextcloud_test

import (
	"testing"

	"github.com/emersion/go-ical"

	"imap-jmap/jmap/nextcloud"
)

func testEventCalendar(summary string) *ical.Calendar {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropProductID, "-//test//test//EN")
	cal.Props.SetText(ical.PropVersion, "2.0")
	ev := ical.NewComponent(ical.CompEvent)
	ev.Props.SetText(ical.PropUID, summary+"@example.com")
	ev.Props.SetText(ical.PropSummary, summary)
	start := ical.NewProp(ical.PropDateTimeStart)
	start.Value = "20270901T100000Z"
	ev.Props.Set(start)
	end := ical.NewProp(ical.PropDateTimeEnd)
	end.Value = "20270901T110000Z"
	ev.Props.Set(end)
	stamp := ical.NewProp(ical.PropDateTimeStamp)
	stamp.Value = "20270901T100000Z"
	ev.Props.Set(stamp)
	cal.Children = append(cal.Children, ev)
	return cal
}

// TestCalendarObjectIdInvertible verifies that a JMAP CalendarEvent id is the CalDAV
// resource name verbatim, so it round-trips without loss regardless of the resource's
// file extension. RFC 4791 Section 5.3.1 only says object URLs "may" end in ".ics", so
// the id-to-path mapping must not strip or append an extension.
func TestCalendarObjectIdInvertible(t *testing.T) {
	_, client, cleanup := nextcloud.NewEmbeddedServer("user@example.com")
	defer cleanup()

	ctx := testContext()
	cals, _, err := client.ListCalendars(ctx)
	if err != nil || len(cals) == 0 {
		t.Fatalf("ListCalendars failed: %v", err)
	}
	calID := cals[0].ID

	ids := []string{"foo", "foo.ics", "foo.ics.ics", "no-extension"}
	for _, id := range ids {
		if err := client.PutCalendarObject(ctx, calID, id, testEventCalendar(id)); err != nil {
			t.Fatalf("PutCalendarObject(%q) failed: %v", id, err)
		}
	}

	objs, err := client.QueryCalendarObjects(ctx, calID)
	if err != nil {
		t.Fatalf("QueryCalendarObjects failed: %v", err)
	}
	seen := make(map[string]bool, len(objs))
	for _, obj := range objs {
		seen[obj.ID] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Fatalf("expected id %q to round-trip, got ids %v", id, seen)
		}
		got, err := client.GetCalendarObject(ctx, calID, id)
		if err != nil {
			t.Fatalf("GetCalendarObject(%q) failed: %v", id, err)
		}
		if got.ID != id {
			t.Fatalf("GetCalendarObject(%q) returned id %q", id, got.ID)
		}
	}
	if len(seen) != len(ids) {
		t.Fatalf("expected %d distinct ids, got %v", len(ids), seen)
	}
}

// TestGetCalendarObjectsMultiGet verifies that multiple objects are fetched in one
// calendar-multiget REPORT and that missing resources are ignored rather than failing
// the whole batch.
func TestGetCalendarObjectsMultiGet(t *testing.T) {
	_, client, cleanup := nextcloud.NewEmbeddedServer("user@example.com")
	defer cleanup()

	ctx := testContext()
	cals, _, err := client.ListCalendars(ctx)
	if err != nil || len(cals) == 0 {
		t.Fatalf("ListCalendars failed: %v", err)
	}
	calID := cals[0].ID

	ids := []string{"a", "b", "c"}
	for _, id := range ids {
		if err := client.PutCalendarObject(ctx, calID, id, testEventCalendar(id)); err != nil {
			t.Fatalf("PutCalendarObject(%q) failed: %v", id, err)
		}
	}

	objs, err := client.GetCalendarObjects(ctx, calID, append(append([]string{}, ids...), "does-not-exist"))
	if err != nil {
		t.Fatalf("GetCalendarObjects failed: %v", err)
	}
	if len(objs) != len(ids) {
		t.Fatalf("expected %d objects, got %d (%v)", len(ids), len(objs), objs)
	}
	for _, id := range ids {
		if objs[id] == nil {
			t.Fatalf("expected object %q in result", id)
		}
	}
	if objs["does-not-exist"] != nil {
		t.Fatalf("missing resource should be absent")
	}
}
