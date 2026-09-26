package nextcloud

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
)

func stateTestCtx(user string) context.Context {
	ctx := context.Background()
	ctx = jmapauth.ContextWithAccountID(ctx, user)
	ctx = jmapauth.ContextWithSubject(ctx, user)
	ctx = jmapauth.ContextWithCredentials(ctx, user, user)
	return ctx
}

func newStateTestBackend(t *testing.T, user string) (*CalendarsBackend, context.Context, func()) {
	t.Helper()
	_, calBackend, _, _, _, cleanup := NewEmbeddedBackend(user)
	return calBackend, stateTestCtx(user), cleanup
}

func idsContain(ids []jmapcore.Id, want jmapcore.Id) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// --- Encoding / decoding -----------------------------------------------------

func TestEncodeSyncStateEmpty(t *testing.T) {
	if got := encodeSyncState(nil); got != "sync-v2:empty" {
		t.Errorf("encodeSyncState(nil) = %q, want sync-v2:empty", got)
	}
	got, err := decodeSyncState("sync-v2:empty")
	if err != nil || len(got) != 0 {
		t.Errorf("decodeSyncState(empty) = %v, %v; want empty map", got, err)
	}
}

func TestDecodeSyncStateErrors(t *testing.T) {
	cases := map[string]string{
		"unknown prefix": "0",
		"empty string":   "",
		"legacy numeric": "~5",
		"bad v2 base64":  "sync-v2:!!!not-base64!!!",
		"bad v2 json":    "sync-v2:" + base64.RawURLEncoding.EncodeToString([]byte("not json")),
		"bad v1 base64":  "sync-v1:!!!not-base64!!!",
		"bad v1 json":    "sync-v1:" + base64.RawURLEncoding.EncodeToString([]byte("[1,2,3]")),
	}
	for name, state := range cases {
		if _, err := decodeSyncState(state); err == nil {
			t.Errorf("%s: decodeSyncState(%q) expected error", name, state)
		}
	}
}

func TestIsSyncState(t *testing.T) {
	yes := []string{"sync-v1:empty", "sync-v2:empty", "sync-v2:abc", "sync-v1:abc"}
	no := []string{"", "~0", "0", "sync-v3:abc", "sync-v"}
	for _, s := range yes {
		if !isSyncState(s) {
			t.Errorf("isSyncState(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isSyncState(s) {
			t.Errorf("isSyncState(%q) = true, want false", s)
		}
	}
}

// --- CalendarEvent state vector ---------------------------------------------

func TestCalendarEventStateVector(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	state1 := be.CalendarEventState(ctx)
	if !strings.HasPrefix(state1, "sync-v2:") {
		t.Fatalf("expected sync-v2 state, got %q", state1)
	}
	// Deterministic: repeated calls with no changes must be byte-identical.
	if state2 := be.CalendarEventState(ctx); state2 != state1 {
		t.Errorf("CalendarEventState not deterministic:\n %q\n %q", state1, state2)
	}

	tokens, err := decodeSyncState(state1)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tokens) == 0 {
		t.Fatalf("expected at least one calendar token")
	}
	for calID, tok := range tokens {
		if tok.Token == "" {
			t.Errorf("calendar %q has empty token", calID)
		}
		if tok.Kind != syncTokenKindSync && tok.Kind != syncTokenKindCTag {
			t.Errorf("calendar %q has unknown kind %q", calID, tok.Kind)
		}
	}
}

func TestCalendarEventChangesLifecycle(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	cals, err := be.GetAllCalendars(ctx)
	if err != nil || len(cals) == 0 {
		t.Fatalf("GetAllCalendars: %v (%d)", err, len(cals))
	}
	calID := cals[0].ID

	state0 := be.CalendarEventState(ctx)

	// Create.
	ev, err := be.CreateCalendarEvent(ctx, &jmapcalendar.CalendarEvent{
		Title:       "State Test",
		Start:       "2027-01-02T10:00:00Z",
		Duration:    "PT30M",
		CalendarIDs: map[jmapcore.Id]bool{calID: true},
	})
	if err != nil {
		t.Fatalf("CreateCalendarEvent: %v", err)
	}
	state1 := be.CalendarEventState(ctx)
	if state1 == state0 {
		t.Fatalf("state did not change after create")
	}
	created, updated, destroyed, newState, hasMore := be.CalendarEventChanges(ctx, state0)
	if hasMore {
		t.Errorf("hasMore should be false")
	}
	if !idsContain(created, ev.ID) || len(updated) != 0 || len(destroyed) != 0 {
		t.Errorf("create delta = created=%v updated=%v destroyed=%v", created, updated, destroyed)
	}
	if newState != state1 {
		t.Errorf("newState %q != CalendarEventState %q", newState, state1)
	}

	// Update from the intermediate state.
	if _, err := be.UpdateCalendarEvent(ctx, ev.ID, map[string]any{"title": "State Test v2"}); err != nil {
		t.Fatalf("UpdateCalendarEvent: %v", err)
	}
	state2 := be.CalendarEventState(ctx)
	created, updated, destroyed, newState, _ = be.CalendarEventChanges(ctx, state1)
	if !idsContain(updated, ev.ID) || len(created) != 0 || len(destroyed) != 0 {
		t.Errorf("update delta = created=%v updated=%v destroyed=%v", created, updated, destroyed)
	}
	if newState != state2 {
		t.Errorf("newState %q != state2 %q", newState, state2)
	}

	// Destroy from the intermediate state.
	if _, err := be.DeleteCalendarEvent(ctx, ev.ID); err != nil {
		t.Fatalf("DeleteCalendarEvent: %v", err)
	}
	state3 := be.CalendarEventState(ctx)
	created, updated, destroyed, newState, _ = be.CalendarEventChanges(ctx, state2)
	if !idsContain(destroyed, ev.ID) || len(created) != 0 || len(updated) != 0 {
		t.Errorf("destroy delta = created=%v updated=%v destroyed=%v", created, updated, destroyed)
	}
	if newState != state3 {
		t.Errorf("newState %q != state3 %q", newState, state3)
	}

	// No changes from the latest state.
	created, updated, destroyed, newState, hasMore = be.CalendarEventChanges(ctx, state3)
	if len(created) != 0 || len(updated) != 0 || len(destroyed) != 0 || hasMore {
		t.Errorf("expected no changes from latest state, got %v/%v/%v hasMore=%v", created, updated, destroyed, hasMore)
	}
	if newState != state3 {
		t.Errorf("newState %q != state3 %q", newState, state3)
	}
}

// TestCalendarEventChangesAnyPriorState verifies /changes can be computed from
// every state string the server previously returned (RFC 8620 §5.2).
func TestCalendarEventChangesAnyPriorState(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	cals, _ := be.GetAllCalendars(ctx)
	calID := cals[0].ID

	var states []string
	states = append(states, be.CalendarEventState(ctx))
	var ids []jmapcore.Id
	for i := 0; i < 3; i++ {
		ev, err := be.CreateCalendarEvent(ctx, &jmapcalendar.CalendarEvent{
			Title:       "Prior State",
			Start:       "2027-02-02T10:00:00Z",
			Duration:    "PT15M",
			CalendarIDs: map[jmapcore.Id]bool{calID: true},
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, ev.ID)
		states = append(states, be.CalendarEventState(ctx))
	}

	for i, from := range states {
		created, updated, destroyed, newState, _ := be.CalendarEventChanges(ctx, from)
		if newState != states[len(states)-1] {
			t.Errorf("state[%d]: newState=%q want %q", i, newState, states[len(states)-1])
		}
		// Every event created after `from` must be reported in created.
		for j := i; j < len(ids); j++ {
			if !idsContain(created, ids[j]) {
				t.Errorf("state[%d]: expected %s in created, got %v", i, ids[j], created)
			}
		}
		if len(updated) != 0 || len(destroyed) != 0 {
			t.Errorf("state[%d]: unexpected updated=%v destroyed=%v", i, updated, destroyed)
		}
	}
}

// TestCalendarEventChangesCalendarAdded verifies that a calendar discovered only
// in the new state has its resources enumerated as created (ETag listing).
func TestCalendarEventChangesCalendarAdded(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	state0 := be.CalendarEventState(ctx)

	cal, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "Newly Added"})
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}
	ev, err := be.CreateCalendarEvent(ctx, &jmapcalendar.CalendarEvent{
		Title:       "In New Calendar",
		Start:       "2027-03-03T10:00:00Z",
		Duration:    "PT30M",
		CalendarIDs: map[jmapcore.Id]bool{cal.ID: true},
	})
	if err != nil {
		t.Fatalf("CreateCalendarEvent: %v", err)
	}

	created, updated, destroyed, newState, _ := be.CalendarEventChanges(ctx, state0)
	if !idsContain(created, ev.ID) {
		t.Errorf("expected event %s in created after calendar added, got created=%v updated=%v", ev.ID, created, updated)
	}
	if len(updated) != 0 || len(destroyed) != 0 {
		t.Errorf("unexpected updated=%v destroyed=%v", updated, destroyed)
	}
	if newState != be.CalendarEventState(ctx) {
		t.Errorf("newState mismatch after calendar added")
	}
}

// TestCalendarEventChangesCalendarRemoved verifies a removed collection fails
// closed: its events cannot be enumerated, so the delta must be reported as
// cannotCalculateChanges rather than silently omitted.
func TestCalendarEventChangesCalendarRemoved(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	cal, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "Doomed"})
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}
	if _, err := be.CreateCalendarEvent(ctx, &jmapcalendar.CalendarEvent{
		Title:       "Removed With Calendar",
		Start:       "2027-04-04T10:00:00Z",
		Duration:    "PT30M",
		CalendarIDs: map[jmapcore.Id]bool{cal.ID: true},
	}); err != nil {
		t.Fatalf("CreateCalendarEvent: %v", err)
	}
	state0 := be.CalendarEventState(ctx)

	if ok, err := be.DeleteCalendar(ctx, cal.ID); err != nil || !ok {
		t.Fatalf("DeleteCalendar: ok=%v err=%v", ok, err)
	}

	_, _, _, newState, _ := be.CalendarEventChanges(ctx, state0)
	if newState != "" {
		t.Errorf("expected cannotCalculateChanges (empty newState) after calendar removal, got %q", newState)
	}
}

// TestCalendarEventChangesCTagOnlyFailsClosed verifies that a state carrying a
// CTag (which cannot enumerate a delta) fails closed when the collection changed.
func TestCalendarEventChangesCTagOnlyFailsClosed(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	statuses, err := be.client.GetCalendarSyncStatuses(ctx)
	if err != nil || len(statuses) == 0 {
		t.Fatalf("GetCalendarSyncStatuses: %v", err)
	}
	var calID string
	for id := range statuses {
		calID = id
		break
	}

	// A CTag that does not match the current token => collection "changed".
	bogus := encodeSyncState(map[string]syncToken{calID: {Kind: syncTokenKindCTag, Token: "bogus-ctag"}})
	if _, _, _, newState, _ := be.CalendarEventChanges(ctx, bogus); newState != "" {
		t.Errorf("changed CTag-only collection should fail closed, got newState %q", newState)
	}
}

// TestCalendarEventChangesCTagUnchanged verifies an unchanged CTag-only vector
// reports no changes rather than failing.
func TestCalendarEventChangesCTagUnchanged(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	statuses, err := be.client.GetCalendarSyncStatuses(ctx)
	if err != nil || len(statuses) == 0 {
		t.Fatalf("GetCalendarSyncStatuses: %v", err)
	}
	current := syncTokensFromStatuses(statuses)
	ctagOnly := make(map[string]syncToken, len(current))
	for calID, tok := range current {
		ctagOnly[calID] = syncToken{Kind: syncTokenKindCTag, Token: tok.Token}
	}
	created, updated, destroyed, newState, hasMore := be.CalendarEventChanges(ctx, encodeSyncState(ctagOnly))
	if len(created) != 0 || len(updated) != 0 || len(destroyed) != 0 || hasMore {
		t.Errorf("unchanged CTag vector should yield no changes, got %v/%v/%v hasMore=%v", created, updated, destroyed, hasMore)
	}
	if newState == "" {
		t.Errorf("unchanged CTag vector should yield a valid newState")
	}
}

func TestCalendarEventChangesMalformedStateFailsClosed(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	for _, state := range []string{"sync-v2:!!!not-base64!!!", "sync-v1:invalid-base64-!!!"} {
		_, _, _, newState, _ := be.CalendarEventChanges(ctx, state)
		if newState != "" {
			t.Errorf("malformed state %q should fail closed, got %q", state, newState)
		}
	}
}

func TestCalendarEventChangesTrackerStateFallback(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	// A non-sync (legacy tracker) state must be delegated to the in-memory tracker
	// rather than misparsed as a token vector.
	_, _, _, newState, _ := be.CalendarEventChanges(ctx, "~0")
	if newState == "" {
		t.Errorf("expected tracker fallback to return a state for ~0")
	}
}

// TestCalendarEventChangesMultipleCalendars exercises deltas spanning several
// collections, including a per-collection update.
func TestCalendarEventChangesMultipleCalendars(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	calA, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "A"})
	if err != nil {
		t.Fatalf("CreateCalendar A: %v", err)
	}
	calB, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "B"})
	if err != nil {
		t.Fatalf("CreateCalendar B: %v", err)
	}
	state0 := be.CalendarEventState(ctx)

	mk := func(calID jmapcore.Id, title string) jmapcore.Id {
		ev, err := be.CreateCalendarEvent(ctx, &jmapcalendar.CalendarEvent{
			Title:       title,
			Start:       "2027-05-05T10:00:00Z",
			Duration:    "PT30M",
			CalendarIDs: map[jmapcore.Id]bool{calID: true},
		})
		if err != nil {
			t.Fatalf("create %s: %v", title, err)
		}
		return ev.ID
	}
	evA := mk(calA.ID, "A event")
	evB := mk(calB.ID, "B event")

	created, updated, destroyed, newState, hasMore := be.CalendarEventChanges(ctx, state0)
	if hasMore {
		t.Errorf("hasMore should be false")
	}
	if !idsContain(created, evA) || !idsContain(created, evB) {
		t.Errorf("expected both events created, got %v", created)
	}
	if len(updated) != 0 || len(destroyed) != 0 {
		t.Errorf("unexpected updated=%v destroyed=%v", updated, destroyed)
	}
	if newState != be.CalendarEventState(ctx) {
		t.Errorf("newState mismatch")
	}

	// Update only B from the post-create state; A's collection token is unchanged.
	state1 := be.CalendarEventState(ctx)
	if _, err := be.UpdateCalendarEvent(ctx, evB, map[string]any{"title": "B event v2"}); err != nil {
		t.Fatalf("update B: %v", err)
	}
	created, updated, destroyed, _, _ = be.CalendarEventChanges(ctx, state1)
	if !idsContain(updated, evB) {
		t.Errorf("expected B updated, got updated=%v", updated)
	}
	if idsContain(updated, evA) || idsContain(created, evA) {
		t.Errorf("A should be untouched, got created=%v updated=%v", created, updated)
	}
	if len(created) != 0 || len(destroyed) != 0 {
		t.Errorf("unexpected created=%v destroyed=%v", created, destroyed)
	}
}

// TestCalendarEventStateUserIsolation verifies a state vector from one account
// does not cause another account's changes to leak.
func TestCalendarEventStateUserIsolation(t *testing.T) {
	_, be, _, _, _, cleanup := NewEmbeddedBackend("a@example.com", "b@example.com")
	defer cleanup()
	ctxA := stateTestCtx("a@example.com")
	ctxB := stateTestCtx("b@example.com")

	calsA, _ := be.GetAllCalendars(ctxA)
	evA, err := be.CreateCalendarEvent(ctxA, &jmapcalendar.CalendarEvent{
		Title:       "A secret",
		Start:       "2027-06-06T10:00:00Z",
		Duration:    "PT30M",
		CalendarIDs: map[jmapcore.Id]bool{calsA[0].ID: true},
	})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	stateA := be.CalendarEventState(ctxA)

	created, updated, destroyed, _, _ := be.CalendarEventChanges(ctxB, stateA)
	if idsContain(created, evA.ID) || idsContain(updated, evA.ID) || idsContain(destroyed, evA.ID) {
		t.Errorf("account B must not observe account A's event %s: created=%v updated=%v destroyed=%v", evA.ID, created, updated, destroyed)
	}
}

// TestCalendarEventStateUnaffectedByWindowedFetch guards the invariant that a
// partial (time-windowed) fetch never defines the collection state.
func TestCalendarEventStateUnaffectedByWindowedFetch(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	cals, _ := be.GetAllCalendars(ctx)
	if _, err := be.CreateCalendarEvent(ctx, &jmapcalendar.CalendarEvent{
		Title:       "Windowed",
		Start:       "2027-07-07T10:00:00Z",
		Duration:    "PT30M",
		CalendarIDs: map[jmapcore.Id]bool{cals[0].ID: true},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := be.CalendarEventState(ctx)

	// A bounded query only reads a window; it must not change the state.
	start := time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := be.getCalendarEventsWindowed(ctx, nil, start, end, cals); err != nil {
		t.Fatalf("windowed fetch: %v", err)
	}
	if after := be.CalendarEventState(ctx); after != before {
		t.Errorf("windowed fetch changed state: %q -> %q", before, after)
	}
}

// --- Calendar collection state (content-addressed) ---------------------------

func TestCalendarStateIsContentAddressed(t *testing.T) {
	client, be, _, _, _, cleanup := NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := stateTestCtx("user@example.com")

	s1 := be.CalendarState(ctx)
	if !strings.HasPrefix(s1, "cal-v1:") {
		t.Fatalf("expected cal-v1 state, got %q", s1)
	}
	if s2 := be.CalendarState(ctx); s2 != s1 {
		t.Errorf("CalendarState not deterministic: %q vs %q", s1, s2)
	}
	// For calendars with no proxy-local metadata overrides, the state is derived
	// entirely from upstream data, so a fresh backend over the same store
	// (simulating a process restart) yields the identical state.
	be2 := NewCalendarsBackend(client)
	if s3 := be2.CalendarState(ctx); s3 != s1 {
		t.Errorf("CalendarState is not stable across instances: %q vs %q", s1, s3)
	}
	// A created calendar's membership is content-addressed and survives a new
	// backend; only proxy-local metadata overrides (kept in memory) can differ.
	if _, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "Content Addressed"}); err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}
	s4 := be.CalendarState(ctx)
	be3 := NewCalendarsBackend(client)
	if s5 := be3.CalendarState(ctx); s5 == s1 {
		t.Errorf("a new calendar must appear in the state vector")
	}
	created, _, _, _, _ := be.CalendarChanges(ctx, s1)
	if len(created) != 1 {
		t.Errorf("expected the new calendar in Calendar/changes created, got %v", created)
	}
	_ = s4
}

// TestCalendarStateUnaffectedByEventChanges guards the design: Calendar objects
// do not change when events are added, so Calendar state must not change.
func TestCalendarStateUnaffectedByEventChanges(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	cals, _ := be.GetAllCalendars(ctx)
	before := be.CalendarState(ctx)
	if _, err := be.CreateCalendarEvent(ctx, &jmapcalendar.CalendarEvent{
		Title:       "Does not affect Calendar",
		Start:       "2027-08-08T10:00:00Z",
		Duration:    "PT30M",
		CalendarIDs: map[jmapcore.Id]bool{cals[0].ID: true},
	}); err != nil {
		t.Fatalf("CreateCalendarEvent: %v", err)
	}
	if after := be.CalendarState(ctx); after != before {
		t.Errorf("adding an event must not change Calendar state: %q -> %q", before, after)
	}
}

func TestCalendarStateChangesMetadata(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	cals, _ := be.GetAllCalendars(ctx)
	id := cals[0].ID
	s0 := be.CalendarState(ctx)

	if _, err := be.UpdateCalendar(ctx, id, map[string]any{"color": "#ff0000"}); err != nil {
		t.Fatalf("UpdateCalendar: %v", err)
	}
	s1 := be.CalendarState(ctx)
	if s1 == s0 {
		t.Fatalf("metadata change must change Calendar state")
	}
	created, updated, destroyed, newState, hasMore := be.CalendarChanges(ctx, s0)
	if hasMore {
		t.Errorf("hasMore should be false")
	}
	if !idsContain(updated, id) || len(created) != 0 || len(destroyed) != 0 {
		t.Errorf("metadata delta = created=%v updated=%v destroyed=%v", created, updated, destroyed)
	}
	if newState != s1 {
		t.Errorf("newState %q != CalendarState %q", newState, s1)
	}
}

func TestCalendarStateChangesAnyPriorState(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	states := []string{be.CalendarState(ctx)}
	var ids []jmapcore.Id
	for i := 0; i < 3; i++ {
		cal, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "Prior Cal"})
		if err != nil {
			t.Fatalf("CreateCalendar: %v", err)
		}
		ids = append(ids, cal.ID)
		states = append(states, be.CalendarState(ctx))
	}
	for i, from := range states {
		created, _, _, newState, _ := be.CalendarChanges(ctx, from)
		if newState != states[len(states)-1] {
			t.Errorf("state[%d]: newState=%q want %q", i, newState, states[len(states)-1])
		}
		for j := i; j < len(ids); j++ {
			if !idsContain(created, ids[j]) {
				t.Errorf("state[%d]: expected %s in created, got %v", i, ids[j], created)
			}
		}
	}
}

func TestCalendarStateMalformedAndLegacy(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	if _, _, _, newState, _ := be.CalendarChanges(ctx, "cal-v1:!!!not-base64!!!"); newState != "" {
		t.Errorf("malformed cal-v1 state should fail closed, got %q", newState)
	}
	// A non-cal-v1 state is delegated to the in-memory tracker fallback.
	if _, _, _, newState, _ := be.CalendarChanges(ctx, "~0"); newState == "" {
		t.Errorf("legacy tracker state should fall back, got empty newState")
	}
}

// --- Calendar collection state lifecycle -------------------------------------

func TestCalendarStateChangesLifecycle(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	state0 := be.CalendarState(ctx)
	cal, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "Stateful Cal"})
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}
	state1 := be.CalendarState(ctx)
	if state1 == state0 {
		t.Fatalf("CalendarState did not change after create")
	}
	created, _, _, newState, _ := be.CalendarChanges(ctx, state0)
	if !idsContain(created, cal.ID) {
		t.Errorf("expected %s in Calendar/changes created, got %v", cal.ID, created)
	}
	if newState != state1 {
		t.Errorf("newState %q != state1 %q", newState, state1)
	}

	if _, err := be.UpdateCalendar(ctx, cal.ID, map[string]any{"name": "Renamed"}); err != nil {
		t.Fatalf("UpdateCalendar: %v", err)
	}
	_, updated, _, state2, _ := be.CalendarChanges(ctx, state1)
	if !idsContain(updated, cal.ID) {
		t.Errorf("expected %s in Calendar/changes updated, got %v", cal.ID, updated)
	}
	if state2 != be.CalendarState(ctx) {
		t.Errorf("newState %q != CalendarState %q", state2, be.CalendarState(ctx))
	}

	if ok, err := be.DeleteCalendar(ctx, cal.ID); err != nil || !ok {
		t.Fatalf("DeleteCalendar: ok=%v err=%v", ok, err)
	}
	_, _, destroyed, _, _ := be.CalendarChanges(ctx, state2)
	if !idsContain(destroyed, cal.ID) {
		t.Errorf("expected %s in Calendar/changes destroyed, got %v", cal.ID, destroyed)
	}
}
