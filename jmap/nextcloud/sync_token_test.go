package nextcloud_test

import (
	"context"
	"strings"
	"testing"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/nextcloud"
)

func TestEmbeddedCalendarSyncTokenTranslation(t *testing.T) {
	_, calBackend, _, _, _, cleanup := nextcloud.NewEmbeddedBackend("user@example.com")
	defer cleanup()

	ctx := context.Background()
	ctx = jmapauth.ContextWithAccountID(ctx, "user@example.com")
	ctx = jmapauth.ContextWithSubject(ctx, "user@example.com")
	ctx = jmapauth.ContextWithCredentials(ctx, "user@example.com", "user@example.com")

	// 1. Initial state must start with sync-v1:
	state0 := calBackend.CalendarEventState(ctx)
	if !strings.HasPrefix(state0, "sync-v1:") {
		t.Fatalf("Expected state0 to start with 'sync-v1:', got %q", state0)
	}

	// 2. Changes with same state must return no changes and hasMore=false
	created, updated, destroyed, newState, hasMore := calBackend.CalendarEventChanges(ctx, state0)
	if hasMore {
		t.Errorf("Expected hasMore=false for unchanged state")
	}
	if len(created) != 0 || len(updated) != 0 || len(destroyed) != 0 {
		t.Errorf("Expected zero changes for identical state, got created=%v, updated=%v, destroyed=%v", created, updated, destroyed)
	}
	if newState != state0 {
		t.Errorf("Expected newState == state0, got %q vs %q", newState, state0)
	}

	// 3. Create an event
	cals, err := calBackend.GetAllCalendars(ctx)
	if err != nil || len(cals) == 0 {
		t.Fatalf("Failed to get calendars: %v", err)
	}
	calID := cals[0].ID

	ev := &jmapcalendar.CalendarEvent{
		Title:       "Sync Token Test Meeting",
		Start:       "2026-12-01T10:00:00Z",
		Duration:    "PT30M",
		CalendarIDs: map[jmapcore.Id]bool{calID: true},
	}
	createdEv, err := calBackend.CreateCalendarEvent(ctx, ev)
	if err != nil {
		t.Fatalf("CreateCalendarEvent failed: %v", err)
	}

	// 4. State must change
	state1 := calBackend.CalendarEventState(ctx)
	if state1 == state0 {
		t.Fatalf("Expected state1 != state0 after creating event")
	}
	if !strings.HasPrefix(state1, "sync-v1:") {
		t.Fatalf("Expected state1 to start with 'sync-v1:', got %q", state1)
	}

	// 5. CalendarEventChanges since state0 must report the event as created
	created, updated, destroyed, newState, hasMore = calBackend.CalendarEventChanges(ctx, state0)
	if hasMore {
		t.Errorf("Expected hasMore=false")
	}
	found := false
	for _, id := range created {
		if id == createdEv.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected event %s in created list, got created=%v, updated=%v", createdEv.ID, created, updated)
	}
	if len(updated) != 0 {
		t.Errorf("Expected 0 updated events, got %v", updated)
	}
	if len(destroyed) != 0 {
		t.Errorf("Expected 0 destroyed events, got %v", destroyed)
	}
	if newState != state1 {
		t.Errorf("Expected newState == state1 (%q), got %q", state1, newState)
	}

	// 5b. Update the event and verify it is reported as updated
	_, err = calBackend.UpdateCalendarEvent(ctx, createdEv.ID, map[string]any{"title": "Updated Title"})
	if err != nil {
		t.Fatalf("UpdateCalendarEvent failed: %v", err)
	}
	state1b := calBackend.CalendarEventState(ctx)
	created, updated, destroyed, newState, hasMore = calBackend.CalendarEventChanges(ctx, state1)
	if hasMore {
		t.Errorf("Expected hasMore=false")
	}
	foundUpdated := false
	for _, id := range updated {
		if id == createdEv.ID {
			foundUpdated = true
			break
		}
	}
	if !foundUpdated {
		t.Errorf("Expected event %s in updated list, got created=%v, updated=%v", createdEv.ID, created, updated)
	}
	if len(created) != 0 {
		t.Errorf("Expected 0 created events, got %v", created)
	}
	if len(destroyed) != 0 {
		t.Errorf("Expected 0 destroyed events, got %v", destroyed)
	}

	// 6. Delete the event
	ok, err := calBackend.DeleteCalendarEvent(ctx, createdEv.ID)
	if err != nil || !ok {
		t.Fatalf("DeleteCalendarEvent failed: %v", err)
	}

	// 7. State must change again
	state2 := calBackend.CalendarEventState(ctx)
	if state2 == state1b || state2 == state1 || state2 == state0 {
		t.Fatalf("Expected state2 != state1b, got %q", state2)
	}

	// 8. CalendarEventChanges since state1b must report the event as destroyed
	created, updated, destroyed, newState, hasMore = calBackend.CalendarEventChanges(ctx, state1b)
	if hasMore {
		t.Errorf("Expected hasMore=false")
	}
	foundDestroyed := false
	for _, id := range destroyed {
		if id == createdEv.ID {
			foundDestroyed = true
			break
		}
	}
	if !foundDestroyed {
		t.Errorf("Expected event %s in destroyed list, got %v", createdEv.ID, destroyed)
	}

	// 9. CalendarEventChanges since state2 must report 0 changes
	created, updated, destroyed, newState, hasMore = calBackend.CalendarEventChanges(ctx, state2)
	if len(created) != 0 || len(updated) != 0 || len(destroyed) != 0 {
		t.Errorf("Expected 0 changes after state2, got created=%v, updated=%v, destroyed=%v", created, updated, destroyed)
	}
	if newState != state2 {
		t.Errorf("Expected newState == state2, got %q vs %q", newState, state2)
	}

	// 10. Malformed sync-v1 state must return empty newState (cannotCalculateChanges)
	_, _, _, malformedNewState, _ := calBackend.CalendarEventChanges(ctx, "sync-v1:invalid-base64-!!!")
	if malformedNewState != "" {
		t.Errorf("Expected empty newState for malformed state, got %q", malformedNewState)
	}
}

func TestLiveNextcloudCalendarSyncTokenTranslation(t *testing.T) {
	url := getNextcloudURL()
	if url == "" || !isReachable(url) {
		t.Skip("Nextcloud not configured via NEXTCLOUD_URL or not reachable at " + url)
	}

	client := nextcloud.NewClient(url)
	calBackend := nextcloud.NewCalendarsBackend(client)
	ctx := testContext()

	// 1. Initial state
	state0 := calBackend.CalendarEventState(ctx)
	if !strings.HasPrefix(state0, "sync-v1:") {
		t.Fatalf("Expected state0 to start with 'sync-v1:', got %q", state0)
	}

	// 2. Unchanged state must return immediately with 0 changes
	created, updated, destroyed, newState, hasMore := calBackend.CalendarEventChanges(ctx, state0)
	if hasMore {
		t.Errorf("Expected hasMore=false for unchanged state")
	}
	if len(created) != 0 || len(updated) != 0 || len(destroyed) != 0 {
		t.Errorf("Expected 0 changes, got created=%v, updated=%v, destroyed=%v", created, updated, destroyed)
	}
	if newState != state0 {
		t.Errorf("Expected newState == state0, got %q vs %q", newState, state0)
	}

	// 3. Create an event on live Nextcloud
	cals, err := calBackend.GetAllCalendars(ctx)
	if err != nil || len(cals) == 0 {
		t.Fatalf("Failed to get calendars: %v", err)
	}
	calID := cals[0].ID

	ev := &jmapcalendar.CalendarEvent{
		Title:       "Live Nextcloud Sync Token Test",
		Start:       "2026-12-05T14:00:00Z",
		Duration:    "PT1H",
		CalendarIDs: map[jmapcore.Id]bool{calID: true},
	}
	createdEv, err := calBackend.CreateCalendarEvent(ctx, ev)
	if err != nil {
		t.Fatalf("CreateCalendarEvent failed: %v", err)
	}
	defer calBackend.DeleteCalendarEvent(ctx, createdEv.ID)

	// 4. State must change
	state1 := calBackend.CalendarEventState(ctx)
	if state1 == state0 {
		t.Fatalf("Expected state1 != state0 on live Nextcloud")
	}

	// 5. Changes since state0 must report createdEv.ID in created
	created, updated, destroyed, newState, hasMore = calBackend.CalendarEventChanges(ctx, state0)
	found := false
	for _, id := range created {
		if id == createdEv.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected event %s in created list on live Nextcloud, got created=%v, updated=%v", createdEv.ID, created, updated)
	}
}
