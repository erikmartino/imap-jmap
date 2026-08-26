package nextcloud_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/nextcloud"
)

func getNextcloudURL() string {
	if u := os.Getenv("NEXTCLOUD_URL"); u != "" {
		return u
	}
	return "http://localhost:8088"
}

func isReachable(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url + "/status.php")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func testContext() context.Context {
	ctx := context.Background()
	ctx = jmap.ContextWithAccountID(ctx, "user@example.com")
	ctx = jmap.ContextWithSubject(ctx, "user@example.com")
	ctx = jmap.ContextWithCredentials(ctx, "user@example.com", "user@example.com")
	return ctx
}

func TestNextcloudCalendarsBackend(t *testing.T) {
	url := getNextcloudURL()
	if !isReachable(url) {
		t.Skip("Nextcloud not reachable at " + url)
	}

	client := nextcloud.NewClient(url)
	backend := nextcloud.NewCalendarsBackend(client)
	ctx := testContext()

	// 1. Get calendars
	cals, err := backend.GetAllCalendars(ctx)
	if err != nil {
		t.Fatalf("GetAllCalendars failed: %v", err)
	}
	if len(cals) == 0 {
		t.Fatalf("Expected at least 1 calendar, got 0")
	}

	// 2. Create CalendarEvent
	ev := &jmap.CalendarEvent{
		Title:       "Sprint Planning Meeting",
		Description: "Nextcloud CalDAV JMAP Integration",
		Start:       "2026-11-15T09:00:00Z",
		Duration:    "PT1H",
		CalendarIDs: map[jmap.Id]bool{cals[0].ID: true},
	}
	created, err := backend.CreateCalendarEvent(ctx, ev)
	if err != nil {
		t.Fatalf("CreateCalendarEvent failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("Expected created event to have ID")
	}

	// 3. Get CalendarEvent
	fetched, notFound, err := backend.GetCalendarEvents(ctx, []jmap.Id{created.ID})
	if err != nil {
		t.Fatalf("GetCalendarEvents failed: %v", err)
	}
	if len(notFound) > 0 || len(fetched) == 0 {
		t.Fatalf("GetCalendarEvents could not find created event %s (notFound=%v)", created.ID, notFound)
	}
	if fetched[0].Title != "Sprint Planning Meeting" {
		t.Errorf("Expected title 'Sprint Planning Meeting', got %q", fetched[0].Title)
	}

	// 4. Query CalendarEvents
	ids, total, err := backend.QueryCalendarEvents(ctx, map[string]any{
		"text": "Sprint Planning",
	}, nil, 0, nil, false)
	if err != nil {
		t.Fatalf("QueryCalendarEvents failed: %v", err)
	}
	if total == 0 || len(ids) == 0 {
		t.Errorf("QueryCalendarEvents returned 0 matches for 'Sprint Planning'")
	}

	// 5. Delete CalendarEvent
	delOk, err := backend.DeleteCalendarEvent(ctx, created.ID)
	if err != nil || !delOk {
		t.Fatalf("DeleteCalendarEvent failed: %v", err)
	}
}

func TestNextcloudContactsBackend(t *testing.T) {
	url := getNextcloudURL()
	if !isReachable(url) {
		t.Skip("Nextcloud not reachable at " + url)
	}

	client := nextcloud.NewClient(url)
	backend := nextcloud.NewContactsBackend(client)
	ctx := testContext()

	// 1. Get AddressBooks
	abs, err := backend.GetAllAddressBooks(ctx)
	if err != nil {
		t.Fatalf("GetAllAddressBooks failed: %v", err)
	}
	if len(abs) == 0 {
		t.Fatalf("Expected at least 1 address book, got 0")
	}

	// 2. Create Card
	card := &jmap.Card{
		Name: &jmap.JSContactName{
			Full: "Bob Nextcloud",
		},
		Emails: map[string]*jmap.JSContactEmailAddress{
			"e1": {Address: "bob.nc@example.com"},
		},
		AddressBookIDs: map[jmap.Id]bool{abs[0].ID: true},
	}
	created, err := backend.CreateCard(ctx, card)
	if err != nil {
		t.Fatalf("CreateCard failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("Expected created card to have ID")
	}

	// 3. Get Card
	fetched, notFound, err := backend.GetCards(ctx, []jmap.Id{created.ID})
	if err != nil {
		t.Fatalf("GetCards failed: %v", err)
	}
	if len(notFound) > 0 || len(fetched) == 0 {
		t.Fatalf("GetCards could not find created card %s (notFound=%v)", created.ID, notFound)
	}
	if fetched[0].Name == nil || fetched[0].Name.Full != "Bob Nextcloud" {
		t.Errorf("Expected name 'Bob Nextcloud', got %v", fetched[0].Name)
	}

	// 4. Delete Card
	delOk, err := backend.DeleteCard(ctx, created.ID)
	if err != nil || !delOk {
		t.Fatalf("DeleteCard failed: %v", err)
	}
}

func TestNextcloudFileNodeBackend(t *testing.T) {
	url := getNextcloudURL()
	if !isReachable(url) {
		t.Skip("Nextcloud not reachable at " + url)
	}

	client := nextcloud.NewClient(url)
	backend := nextcloud.NewFileNodeBackend(client)
	ctx := testContext()

	// 1. Get FileNodes
	nodes, err := backend.GetAllFileNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllFileNodes failed: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatalf("Expected files in nextcloud root directory, got 0")
	}

	// 2. Create FileNode folder
	folderName := "JMAPTestFolder"
	folderNode := &jmap.FileNode{
		Name:     folderName,
		IsFolder: true,
		Type:     "folder",
	}
	created, err := backend.CreateFileNode(ctx, folderNode)
	if err != nil {
		t.Fatalf("CreateFileNode failed: %v", err)
	}

	// 3. Delete FileNode folder
	delOk, err := backend.DeleteFileNode(ctx, created.ID)
	if err != nil || !delOk {
		t.Fatalf("DeleteFileNode failed: %v", err)
	}
}
