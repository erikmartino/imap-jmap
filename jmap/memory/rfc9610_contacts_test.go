package memory

import (
	"context"
	"testing"

	"imap-jmap/jmap"
)

// TestRFC8620_SeedSampleData verifies that SeedSampleData populates sample emails, calendars, and events.
func TestRFC8620_SeedSampleData(t *testing.T) {
	mb := NewMemoryBackend()
	cb := NewMemoryCalendarsBackend()
	ctx := context.Background()

	SeedSampleData(mb, cb)

	emails, err := mb.GetAllEmails(ctx)
	if err != nil || len(emails) < 5 {
		t.Errorf("Expected at least 5 seeded emails, got %d", len(emails))
	}

	cals, _, err := cb.GetCalendars(ctx, nil)
	if err != nil || len(cals) < 2 {
		t.Errorf("Expected at least 2 seeded calendars, got %d", len(cals))
	}

	events, _, err := cb.GetCalendarEvents(ctx, nil)
	if err != nil || len(events) < 3 {
		t.Errorf("Expected at least 3 seeded calendar events, got %d", len(events))
	}
}

// TestRFC9610_UpdateCard_PreservesUntouchedFields verifies that a partial Card patch
// patches only the addressed fields and never zeroes out properties omitted
// from the patch (data-loss prevention) per RFC 9610.
func TestRFC9610_UpdateCard_PreservesUntouchedFields(t *testing.T) {
	b := NewMemoryContactsBackend()
	ctx := context.Background()

	created, err := b.CreateCard(ctx, &jmap.Card{
		AddressBookIDs: map[jmap.Id]bool{"ab-default": true},
		Kind:           "individual",
		Uid:            "uid-1",
		Name:           &jmap.JSContactName{Full: "Jane Doe"},
		Emails: map[string]*jmap.JSContactEmailAddress{
			"e1": {Address: "jane@example.com"},
		},
		Phones: map[string]*jmap.JSContactPhone{
			"p1": {Number: "+1-555-0100"},
		},
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	createdAt := created.Created

	patched, err := b.UpdateCard(ctx, created.ID, map[string]any{
		"phones": map[string]any{
			"p1": map[string]any{"number": "+1-555-0199"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateCard: %v", err)
	}

	if patched.Uid != "uid-1" {
		t.Errorf("uid was clobbered by patch: %q", patched.Uid)
	}
	if patched.Created != createdAt {
		t.Errorf("created was clobbered: %q (was %q)", patched.Created, createdAt)
	}
	if patched.Kind != "individual" {
		t.Errorf("kind was clobbered: %q", patched.Kind)
	}
	if patched.Emails == nil || patched.Emails["e1"] == nil || patched.Emails["e1"].Address != "jane@example.com" {
		t.Errorf("emails were dropped by patch: %v", patched.Emails)
	}
	if patched.Phones == nil || patched.Phones["p1"] == nil || patched.Phones["p1"].Number != "+1-555-0199" {
		t.Errorf("phone patch not applied: %v", patched.Phones)
	}
	if patched.Name == nil || patched.Name.Full != "Jane Doe" {
		t.Errorf("name was dropped by patch: %v", patched.Name)
	}
	if patched.Updated == "" {
		t.Errorf("updated timestamp not set by patch")
	}
}

// TestRFC9610_UpdateCard_ChangesTracked tests RFC 9610 Card state changes.
func TestRFC9610_UpdateCard_ChangesTracked(t *testing.T) {
	b := NewMemoryContactsBackend()
	ctx := context.Background()

	state0 := b.CardState(ctx)
	created, err := b.CreateCard(ctx, &jmap.Card{
		AddressBookIDs: map[jmap.Id]bool{"ab-default": true},
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	createdID := created.ID

	createdList, _, _, newState, _ := b.CardChanges(ctx, state0)
	if len(createdList) != 1 || createdList[0] != createdID {
		t.Fatalf("expected created=[%s], got %v", createdID, createdList)
	}

	updated, err := b.UpdateCard(ctx, createdID, map[string]any{"kind": "individual"})
	if err != nil || updated == nil {
		t.Fatalf("UpdateCard: %v", err)
	}

	_, updatedList, _, _, _ := b.CardChanges(ctx, newState)
	if len(updatedList) != 1 || updatedList[0] != createdID {
		t.Errorf("expected updated=[%s], got %v", createdID, updatedList)
	}
}

// TestRFC9610_UpdateAddressBook_PreservesUntouchedFields tests RFC 9610 AddressBook patch updates.
func TestRFC9610_UpdateAddressBook_PreservesUntouchedFields(t *testing.T) {
	b := NewMemoryContactsBackend()
	ctx := context.Background()

	desc := "main list"
	ab, err := b.CreateAddressBook(ctx, &jmap.AddressBook{
		Name:        "Family",
		Description: &desc,
		SortOrder:   5,
	})
	if err != nil {
		t.Fatalf("CreateAddressBook: %v", err)
	}

	updated, err := b.UpdateAddressBook(ctx, ab.ID, map[string]any{
		"name":      "Friends",
		"sortOrder": float64(9),
	})
	if err != nil {
		t.Fatalf("UpdateAddressBook: %v", err)
	}
	if updated.Name != "Friends" || updated.SortOrder != 9 {
		t.Errorf("patch not applied: %+v", updated)
	}
	if updated.Description == nil || *updated.Description != "main list" {
		t.Errorf("description dropped by patch: %+v", updated.Description)
	}
}

// TestRFC9610_QueryCards_FilterEmail tests RFC 9610 Card query email filtering.
func TestRFC9610_QueryCards_FilterEmail(t *testing.T) {
	b := NewMemoryContactsBackend()
	ctx := context.Background()

	c1, err := b.CreateCard(ctx, &jmap.Card{
		AddressBookIDs: map[jmap.Id]bool{"ab-default": true},
		Emails:         map[string]*jmap.JSContactEmailAddress{"e1": {Address: "jane@example.com"}},
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	b.CreateCard(ctx, &jmap.Card{
		AddressBookIDs: map[jmap.Id]bool{"ab-default": true},
		Emails:         map[string]*jmap.JSContactEmailAddress{"e1": {Address: "bob@example.com"}},
	})

	ids, total, err := b.QueryCards(ctx, map[string]any{"email": "jane@example.com"}, nil, 0, nil)
	if err != nil {
		t.Fatalf("QueryCards: %v", err)
	}
	if total != 1 || len(ids) != 1 || ids[0] != c1.ID {
		t.Errorf("query email filter failed: ids=%v total=%d", ids, total)
	}
}

// TestRFC8984_UpdateCalendarEvent_FullPatch tests RFC 8984 CalendarEvent patch updates.
func TestRFC8984_UpdateCalendarEvent_FullPatch(t *testing.T) {
	b := NewMemoryCalendarsBackend()
	ctx := context.Background()

	ev, err := b.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
		Title:       "Team Sync",
		Description: "weekly",
		Start:       "2026-08-03T09:00:00Z",
		Duration:    "PT1H",
		Status:      "confirmed",
		Locations:   map[string]*jmap.JSCalendarLocation{"loc-1": {Name: "Room A"}},
	})
	if err != nil {
		t.Fatalf("CreateCalendarEvent: %v", err)
	}
	createdAt := ev.Created

	updated, err := b.UpdateCalendarEvent(ctx, ev.ID, map[string]any{
		"title":    "Team Sync (rescheduled)",
		"duration": "PT2H",
	})
	if err != nil {
		t.Fatalf("UpdateCalendarEvent: %v", err)
	}
	if updated.Title != "Team Sync (rescheduled)" || updated.Duration != "PT2H" {
		t.Errorf("patch not applied: %+v", updated)
	}
	if updated.Start != "2026-08-03T09:00:00Z" {
		t.Errorf("start clobbered: %s", updated.Start)
	}
	if updated.Status != "confirmed" {
		t.Errorf("status clobbered: %s", updated.Status)
	}
	if updated.Locations == nil || updated.Locations["loc-1"] == nil || updated.Locations["loc-1"].Name != "Room A" {
		t.Errorf("location dropped: %+v", updated.Locations)
	}
	if updated.Created != createdAt {
		t.Errorf("created clobbered: %s (was %s)", updated.Created, createdAt)
	}
	if updated.Updated == "" {
		t.Errorf("updated not set")
	}
}

// TestRFC8984_QueryCalendarEvents_Filters tests RFC 8984 CalendarEvent query filtering.
func TestRFC8984_QueryCalendarEvents_Filters(t *testing.T) {
	b := NewMemoryCalendarsBackend()
	ctx := context.Background()

	ev1, err := b.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
		Title:    "Breakfast",
		Start:    "2026-08-04T08:00:00Z",
		Duration: "PT30M",
	})
	if err != nil {
		t.Fatalf("CreateCalendarEvent: %v", err)
	}
	b.CreateCalendarEvent(ctx, &jmap.CalendarEvent{
		Title:    "Dinner",
		Start:    "2026-08-04T20:00:00Z",
		Duration: "PT1H",
	})

	before, total, _ := b.QueryCalendarEvents(ctx, map[string]any{"before": "2026-08-04T12:00:00Z"}, nil, 0, nil, false)
	if total != 1 || len(before) != 1 || before[0] != ev1.ID {
		t.Errorf("before filter failed: ids=%v total=%d", before, total)
	}

	after, total, _ := b.QueryCalendarEvents(ctx, map[string]any{"after": "2026-08-04T12:00:00Z"}, nil, 0, nil, false)
	if total != 1 || len(after) != 1 {
		t.Errorf("after filter failed: ids=%v total=%d", after, total)
	}
	_ = total
}

// TestRFC9661_SieveScriptChangesTracked tests RFC 9661 SieveScript state change tracking.
func TestRFC9661_SieveScriptChangesTracked(t *testing.T) {
	b := NewMemorySieveBackend()
	ctx := context.Background()

	got0 := b.SieveScriptState(ctx)

	script, err := b.CreateSieveScript(ctx, &jmap.SieveScript{
		Name:    "filter-one",
		Content: `if header :contains "subject" "spam" { discard; }`,
	})
	if err != nil {
		t.Fatalf("CreateSieveScript: %v", err)
	}
	if script.ID == "" {
		t.Fatalf("CreateSieveScript did not assign an ID")
	}

	created, _, _, _, _ := b.SieveScriptChanges(ctx, got0)
	if len(created) != 1 || created[0] != script.ID {
		t.Fatalf("expected created=[%s], got %v", script.ID, created)
	}
	afterCreate := b.SieveScriptState(ctx)

	if _, err := b.UpdateSieveScript(ctx, script.ID, map[string]any{
		"name":    "filter-two",
		"content": `if true { keep; }`,
	}); err != nil {
		t.Fatalf("UpdateSieveScript: %v", err)
	}
	_, updated, _, _, _ := b.SieveScriptChanges(ctx, afterCreate)
	if len(updated) != 1 || updated[0] != script.ID {
		t.Errorf("expected updated=[%s], got %v", script.ID, updated)
	}

	if _, err := b.DeleteSieveScript(ctx, script.ID); err != nil {
		t.Fatalf("DeleteSieveScript: %v", err)
	}
	_, _, destroyed, _, _ := b.SieveScriptChanges(ctx, afterCreate)
	if len(destroyed) != 1 || destroyed[0] != script.ID {
		t.Errorf("expected destroyed=[%s], got %v", script.ID, destroyed)
	}
}

// TestRFC9007_ParseMDN_UnknownBlobReturnsErr tests RFC 9007 MDN parsing on unknown blob.
func TestRFC9007_ParseMDN_UnknownBlobReturnsErr(t *testing.T) {
	mb := NewMemoryBackend()
	_, err := mb.ParseMDN(context.Background(), "no-such-blob")
	if err == nil {
		t.Fatalf("expected error for unknown blob, got nil")
	}
	if err != jmap.ErrBlobNotFound {
		t.Errorf("expected jmap.ErrBlobNotFound, got %v", err)
	}
}

// TestRFC9007_ParseMDN_KnownBlobParses tests RFC 9007 MDN parsing on existing blob.
func TestRFC9007_ParseMDN_KnownBlobParses(t *testing.T) {
	mb := NewMemoryBackend()
	mdn, err := mb.ParseMDN(context.Background(), "blob-stub-1")
	if err != nil {
		t.Fatalf("expected parse of existing blob, got %v", err)
	}
	_ = mdn
}

// TestRFC8620_AuthTokenExpiryAndRevocation tests RFC 8620 token expiration and revocation.
func TestRFC8620_AuthTokenExpiryAndRevocation(t *testing.T) {
	a := NewMemoryAuthBackend()
	ctx := context.Background()

	token, err := a.Authenticate(ctx, "alice", "alice")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if _, _, err := a.ValidateToken(ctx, token); err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}

	a.RevokeToken(token)
	if acct, _, err := a.ValidateToken(ctx, token); err == nil || acct != "" {
		t.Errorf("revoked token still accepted: %q", acct)
	}

	// With a zero TTL set, an issued token must be immediately expired.
	a.SetTokenTTL(-1)
	token2, err := a.Authenticate(ctx, "bob", "bob")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if acct, _, err := a.ValidateToken(ctx, token2); err == nil || acct != "" {
		t.Errorf("expired token still accepted: %q", acct)
	}
}
