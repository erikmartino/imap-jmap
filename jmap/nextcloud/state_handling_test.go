package nextcloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcontacts"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapfilenode"
)

// concurrencyTrackingTransport records the maximum number of in-flight CalDAV
// requests; a serial implementation peaks at 1.
type concurrencyTrackingTransport struct {
	base http.RoundTripper
	cur  int32
	max  int32
}

func (t *concurrencyTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c := atomic.AddInt32(&t.cur, 1)
	for {
		m := atomic.LoadInt32(&t.max)
		if c <= m || atomic.CompareAndSwapInt32(&t.max, m, c) {
			break
		}
	}
	time.Sleep(15 * time.Millisecond)
	resp, err := t.base.RoundTrip(req)
	atomic.AddInt32(&t.cur, -1)
	return resp, err
}

// TestCalendarEventChangesRunConcurrently verifies that per-collection CalDAV
// sync/ETag requests for a single JMAP changes call overlap rather than running
// serially.
func TestCalendarEventChangesRunConcurrently(t *testing.T) {
	client, be, _, _, _, cleanup := NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := stateTestCtx("user@example.com")

	// Create several calendars each with an event so changes fans out.
	state0 := be.CalendarEventState(ctx)
	for i := 0; i < 3; i++ {
		cal, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "Perf"})
		if err != nil {
			t.Fatalf("CreateCalendar: %v", err)
		}
		if _, err := be.CreateCalendarEvent(ctx, &jmapcalendar.CalendarEvent{
			Title:       "Perf Event",
			Start:       "2027-09-09T10:00:00Z",
			Duration:    "PT30M",
			CalendarIDs: map[jmapcore.Id]bool{cal.ID: true},
		}); err != nil {
			t.Fatalf("CreateCalendarEvent: %v", err)
		}
	}

	tracker := &concurrencyTrackingTransport{base: client.HTTPClient.Transport}
	client.HTTPClient.Transport = tracker
	// Force each collection to be a distinct new calendar to the state.
	_, _, _, newState, _ := be.CalendarEventChanges(ctx, state0)
	if newState == "" {
		t.Fatalf("expected a valid new state")
	}
	if got := atomic.LoadInt32(&tracker.max); got < 2 {
		t.Errorf("expected concurrent CalDAV requests (max>1), got max=%d", got)
	}
}

// bodyCapturingTransport records the bodies of REPORT requests.
type bodyCapturingTransport struct {
	base   http.RoundTripper
	mu     sync.Mutex
	bodies []string
}

func (t *bodyCapturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == "REPORT" && req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(b))
		t.mu.Lock()
		t.bodies = append(t.bodies, string(b))
		t.mu.Unlock()
	}
	return t.base.RoundTrip(req)
}

// TestQueryExpandRecurrencesPushesTimeRange verifies that an expandRecurrences
// query without an explicit before bound still pushes a time-range down to
// CalDAV instead of scanning the whole collection.
func TestQueryExpandRecurrencesPushesTimeRange(t *testing.T) {
	client, be, _, _, _, cleanup := NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := stateTestCtx("user@example.com")

	tracker := &bodyCapturingTransport{base: client.HTTPClient.Transport}
	client.HTTPClient.Transport = tracker
	if _, _, err := be.QueryCalendarEvents(ctx, nil, nil, 0, nil, true); err != nil {
		t.Fatalf("QueryCalendarEvents: %v", err)
	}
	found := false
	for _, b := range tracker.bodies {
		if strings.Contains(b, "time-range") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a REPORT with a time-range filter, got %d bodies", len(tracker.bodies))
	}
}

// TestTargetedGetRunsConcurrently verifies that a targeted CalendarEvent/get
// issues its per-calendar calendar-multiget REPORTs concurrently.
func TestTargetedGetRunsConcurrently(t *testing.T) {
	client, be, _, _, _, cleanup := NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := stateTestCtx("user@example.com")

	var ids []jmapcore.Id
	for i := 0; i < 3; i++ {
		cal, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "Perf Get"})
		if err != nil {
			t.Fatalf("CreateCalendar: %v", err)
		}
		ev, err := be.CreateCalendarEvent(ctx, &jmapcalendar.CalendarEvent{
			Title:       "Targeted",
			Start:       "2027-10-10T10:00:00Z",
			Duration:    "PT30M",
			CalendarIDs: map[jmapcore.Id]bool{cal.ID: true},
		})
		if err != nil {
			t.Fatalf("CreateCalendarEvent: %v", err)
		}
		ids = append(ids, ev.ID)
	}

	tracker := &concurrencyTrackingTransport{base: client.HTTPClient.Transport}
	client.HTTPClient.Transport = tracker
	events, notFound, err := be.GetCalendarEvents(ctx, ids)
	if err != nil {
		t.Fatalf("GetCalendarEvents: %v", err)
	}
	if len(events) != len(ids) || len(notFound) != 0 {
		t.Fatalf("expected all %d events, got %d (notFound %v)", len(ids), len(events), notFound)
	}
	if got := atomic.LoadInt32(&tracker.max); got < 2 {
		t.Errorf("expected concurrent multiget requests (max>1), got max=%d", got)
	}
}

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

// TestUpstreamDerivedCalendarProperties verifies that Color/SortOrder are taken
// from the CalDAV calendar-color/calendar-order properties when set upstream,
// and that a proxy-local override still wins.
func TestUpstreamDerivedCalendarProperties(t *testing.T) {
	be, ctx, cleanup := newStateTestBackend(t, "user@example.com")
	defer cleanup()

	cals, _, err := be.GetCalendars(ctx, nil)
	if err != nil {
		t.Fatalf("GetCalendars: %v", err)
	}
	var personal *jmapcalendar.Calendar
	for _, c := range cals {
		if c.ID == "personal" {
			personal = c
			break
		}
	}
	if personal == nil {
		t.Fatalf("seeded personal calendar not found in %v", cals)
	}
	if personal.Color == nil || *personal.Color != "#3a87adFF" {
		t.Errorf("expected upstream calendar-color #3a87adFF, got %v", personal.Color)
	}
	if personal.SortOrder != 1 {
		t.Errorf("expected upstream calendar-order 1, got %d", personal.SortOrder)
	}

	// A proxy-local override must still win over the upstream value.
	if _, err := be.UpdateCalendar(ctx, personal.ID, map[string]any{"color": "#000000"}); err != nil {
		t.Fatalf("UpdateCalendar: %v", err)
	}
	cals, _, _ = be.GetCalendars(ctx, nil)
	for _, c := range cals {
		if c.ID == "personal" {
			if c.Color == nil || *c.Color != "#000000" {
				t.Errorf("proxy-local color override should win, got %v", c.Color)
			}
		}
	}
}

// TestCalendarMetadataPersistedUpstream verifies that Calendar metadata updates
// are written to the upstream CalDAV server (PROPPATCH) and are visible to a
// fresh backend that holds no proxy-local state.
func TestCalendarMetadataPersistedUpstream(t *testing.T) {
	client, be, _, _, _, cleanup := NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := stateTestCtx("user@example.com")

	cal, err := be.CreateCalendar(ctx, &jmapcalendar.Calendar{Name: "Original"})
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}
	if _, err := be.UpdateCalendar(ctx, cal.ID, map[string]any{
		"name":                  "Renamed",
		"color":                 "#123456",
		"isSubscribed":          true,
		"includeInAvailability": "attending",
	}); err != nil {
		t.Fatalf("UpdateCalendar: %v", err)
	}

	// A fresh backend has no calProps; it must read the metadata from upstream.
	be2 := NewCalendarsBackend(client)
	cals, _, err := be2.GetCalendars(ctx, nil)
	if err != nil {
		t.Fatalf("GetCalendars: %v", err)
	}
	for _, c := range cals {
		if c.ID != cal.ID {
			continue
		}
		if c.Name != "Renamed" {
			t.Errorf("expected upstream name Renamed, got %q", c.Name)
		}
		if c.Color == nil || *c.Color != "#123456" {
			t.Errorf("expected upstream color #123456, got %v", c.Color)
		}
		if !c.IsSubscribed {
			t.Errorf("expected upstream isSubscribed=true")
		}
		if c.IncludeInAvailability != "attending" {
			t.Errorf("expected upstream includeInAvailability=attending, got %q", c.IncludeInAvailability)
		}
		return
	}
	t.Fatalf("calendar %s not found in fresh backend", cal.ID)
}

// TestAddressBookMetadataPersistedUpstream verifies that AddressBook metadata updates
// and default address book selections are written to the upstream CardDAV server (PROPPATCH)
// and are visible to a fresh backend that holds no proxy-local state.
func TestAddressBookMetadataPersistedUpstream(t *testing.T) {
	client, _, be, _, _, cleanup := NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := stateTestCtx("user@example.com")

	desc := "Work team contacts"
	ab, err := be.CreateAddressBook(ctx, &jmapcontacts.AddressBook{
		Name:         "Team",
		Description:  &desc,
		SortOrder:    50,
		IsSubscribed: true,
	})
	if err != nil {
		t.Fatalf("CreateAddressBook: %v", err)
	}

	// Set as default address book.
	if err := be.SetDefaultAddressBook(ctx, ab.ID); err != nil {
		t.Fatalf("SetDefaultAddressBook: %v", err)
	}

	// Update metadata via patch.
	newDesc := "Updated team contacts"
	if _, err := be.UpdateAddressBook(ctx, ab.ID, map[string]any{
		"name":         "Engineering Team",
		"description":  newDesc,
		"sortOrder":    float64(20),
		"isSubscribed": false,
	}); err != nil {
		t.Fatalf("UpdateAddressBook: %v", err)
	}

	// A fresh backend has no local state; it must read the metadata from upstream.
	be2 := NewContactsBackend(client)
	abs, _, err := be2.GetAddressBooks(ctx, nil)
	if err != nil {
		t.Fatalf("GetAddressBooks on fresh backend: %v", err)
	}

	found := false
	for _, a := range abs {
		if a.ID != ab.ID {
			continue
		}
		found = true
		if a.Name != "Engineering Team" {
			t.Errorf("expected name %q, got %q", "Engineering Team", a.Name)
		}
		if a.Description == nil || *a.Description != newDesc {
			t.Errorf("expected description %q, got %v", newDesc, a.Description)
		}
		if a.SortOrder != 20 {
			t.Errorf("expected sortOrder 20, got %d", a.SortOrder)
		}
		if a.IsSubscribed != false {
			t.Errorf("expected isSubscribed false, got %v", a.IsSubscribed)
		}
		if !a.IsDefault {
			t.Errorf("expected isDefault true on default address book")
		}
	}
	if !found {
		t.Fatalf("address book %s not found in fresh backend", ab.ID)
	}

	// Verify the other address book (default "contacts") is not marked default.
	for _, a := range abs {
		if a.ID == "contacts" && a.IsDefault {
			t.Errorf("contacts address book should not be default when another default is set")
		}
	}

	// Delete the default address book.
	if _, err := be2.DeleteAddressBook(ctx, ab.ID, false); err != nil {
		t.Fatalf("DeleteAddressBook: %v", err)
	}

	// Fresh backend 3: verifies deleted address book is gone and default is restored.
	be3 := NewContactsBackend(client)
	abs3, _, err := be3.GetAddressBooks(ctx, nil)
	if err != nil {
		t.Fatalf("GetAddressBooks on third backend: %v", err)
	}
	for _, a := range abs3 {
		if a.ID == ab.ID {
			t.Errorf("deleted address book %s still present in fresh backend", ab.ID)
		}
		if a.ID == "contacts" && !a.IsDefault {
			t.Errorf("contacts address book should be promoted to default after default book is deleted")
		}
	}
}

// TestFileNodeDeterministicIDsAcrossInstances verifies that FileNode IDs are derived
// deterministically from upstream WebDAV paths, are stable and identical across separate
// backend instances with zero local state, and allow operations across fresh instances.
func TestFileNodeDeterministicIDsAcrossInstances(t *testing.T) {
	client, _, _, fileNodeBackend, _, _, cleanup := NewEmbeddedBackendWithBlobs("user@example.com")
	defer cleanup()
	ctx := stateTestCtx("user@example.com")

	// 1. Create a folder and a child file.
	folder, err := fileNodeBackend.CreateFileNode(ctx, &jmapfilenode.FileNode{
		Name:     "Projects",
		IsFolder: true,
	})
	if err != nil {
		t.Fatalf("CreateFileNode folder failed: %v", err)
	}

	file, err := fileNodeBackend.CreateFileNode(ctx, &jmapfilenode.FileNode{
		Name:     "report.txt",
		ParentID: &folder.ID,
		Type:     "text/plain",
	})
	if err != nil {
		t.Fatalf("CreateFileNode file failed: %v", err)
	}

	// 2. Verify IDs match FileNodeIDForPath deterministically.
	expectedFolderID := FileNodeIDForPath("Projects")
	expectedFileID := FileNodeIDForPath("Projects/report.txt")
	if folder.ID != expectedFolderID {
		t.Errorf("folder ID mismatch: got %s, want %s", folder.ID, expectedFolderID)
	}
	if file.ID != expectedFileID {
		t.Errorf("file ID mismatch: got %s, want %s", file.ID, expectedFileID)
	}

	// 3. Verify PathForFileNodeID decodes the paths back cleanly.
	pFolder, err := PathForFileNodeID(folder.ID)
	if err != nil || pFolder != "Projects" {
		t.Errorf("PathForFileNodeID(folder.ID) = %q, %v; want 'Projects'", pFolder, err)
	}
	pFile, err := PathForFileNodeID(file.ID)
	if err != nil || pFile != "Projects/report.txt" {
		t.Errorf("PathForFileNodeID(file.ID) = %q, %v; want 'Projects/report.txt'", pFile, err)
	}

	// 4. Create a completely fresh FileNodeBackend instance with zero proxy-local state.
	be2 := NewFileNodeBackend(client)
	nodes, notFound, err := be2.GetFileNodes(ctx, []jmapcore.Id{folder.ID, file.ID})
	if err != nil {
		t.Fatalf("be2.GetFileNodes failed: %v", err)
	}
	if len(notFound) > 0 {
		t.Fatalf("be2.GetFileNodes reported notFound: %v", notFound)
	}
	if len(nodes) != 2 {
		t.Fatalf("be2.GetFileNodes expected 2 nodes, got %d", len(nodes))
	}

	foundFolder := false
	foundFile := false
	for _, n := range nodes {
		if n.ID == folder.ID {
			foundFolder = true
			if !n.IsFolder || n.Name != "Projects" {
				t.Errorf("fresh backend folder mismatch: %#v", n)
			}
		}
		if n.ID == file.ID {
			foundFile = true
			if n.IsFolder || n.Name != "report.txt" || n.ParentID == nil || *n.ParentID != folder.ID {
				t.Errorf("fresh backend file mismatch: %#v", n)
			}
		}
	}
	if !foundFolder || !foundFile {
		t.Errorf("failed to discover both nodes on fresh backend: foundFolder=%v, foundFile=%v", foundFolder, foundFile)
	}

	// 5. Delete file using the fresh backend instance.
	ok, err := be2.DeleteFileNode(ctx, file.ID)
	if err != nil || !ok {
		t.Fatalf("be2.DeleteFileNode failed: ok=%v, err=%v", ok, err)
	}

	// 6. Third fresh backend verifies file is gone upstream.
	be3 := NewFileNodeBackend(client)
	_, notFound3, err := be3.GetFileNodes(ctx, []jmapcore.Id{file.ID})
	if err != nil {
		t.Fatalf("be3.GetFileNodes failed: %v", err)
	}
	if len(notFound3) != 1 || notFound3[0] != file.ID {
		t.Errorf("expected file %s in notFound on fresh backend, got %v", file.ID, notFound3)
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
