package nextcloud_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcontacts"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapprincipals"
	"imap-jmap/jmap/nextcloud"
)

func getNextcloudURL() string {
	return os.Getenv("NEXTCLOUD_URL")
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
	ctx = jmapauth.ContextWithAccountID(ctx, "user@example.com")
	ctx = jmapauth.ContextWithSubject(ctx, "user@example.com")
	ctx = jmapauth.ContextWithCredentials(ctx, "user@example.com", "user@example.com")
	return ctx
}

func TestNextcloudCalendarsBackend(t *testing.T) {
	url := getNextcloudURL()
	if url == "" || !isReachable(url) {
		t.Skip("Nextcloud not configured via NEXTCLOUD_URL or not reachable at " + url)
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
	ev := &jmapcalendar.CalendarEvent{
		Title:       "Sprint Planning Meeting",
		Description: "Nextcloud CalDAV JMAP Integration",
		Start:       "2026-11-15T09:00:00Z",
		Duration:    "PT1H",
		CalendarIDs: map[jmapcore.Id]bool{cals[0].ID: true},
	}
	created, err := backend.CreateCalendarEvent(ctx, ev)
	if err != nil {
		t.Fatalf("CreateCalendarEvent failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("Expected created event to have ID")
	}

	// 3. Get CalendarEvent
	fetched, notFound, err := backend.GetCalendarEvents(ctx, []jmapcore.Id{created.ID})
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
	if url == "" || !isReachable(url) {
		t.Skip("Nextcloud not configured via NEXTCLOUD_URL or not reachable at " + url)
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
	card := &jmapcontacts.Card{
		Name: &jmapcontacts.JSContactName{
			Full: "Bob Nextcloud",
		},
		Emails: map[string]*jmapcontacts.JSContactEmailAddress{
			"e1": {Address: "bob.nc@example.com"},
		},
		AddressBookIDs: map[jmapcore.Id]bool{abs[0].ID: true},
	}
	created, err := backend.CreateCard(ctx, card)
	if err != nil {
		t.Fatalf("CreateCard failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("Expected created card to have ID")
	}

	// 3. Get Card
	fetched, notFound, err := backend.GetCards(ctx, []jmapcore.Id{created.ID})
	if err != nil {
		t.Fatalf("GetCards failed: %v", err)
	}
	if len(notFound) > 0 || len(fetched) == 0 {
		t.Fatalf("GetCards could not find created card %s (notFound=%v)", created.ID, notFound)
	}
	if fetched[0].Name == nil || fetched[0].Name.Full != "Bob Nextcloud" {
		t.Errorf("Expected name 'Bob Nextcloud', got %v", fetched[0].Name)
	}

	// 4. Update Card
	updated, err := backend.UpdateCard(ctx, created.ID, map[string]any{
		"name/full": "Bob Nextcloud Updated",
	})
	if err != nil {
		t.Fatalf("UpdateCard failed: %v", err)
	}
	if updated.Name == nil || updated.Name.Full != "Bob Nextcloud Updated" {
		t.Errorf("Expected updated name 'Bob Nextcloud Updated', got %v", updated.Name)
	}

	// 5. Delete Card
	delOk, err := backend.DeleteCard(ctx, created.ID)
	if err != nil || !delOk {
		t.Fatalf("DeleteCard failed: %v", err)
	}
}

func TestNextcloudFileNodeBackend(t *testing.T) {
	url := getNextcloudURL()
	if url == "" || !isReachable(url) {
		t.Skip("Nextcloud not configured via NEXTCLOUD_URL or not reachable at " + url)
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

func TestEmbeddedNextcloudCalendars(t *testing.T) {
	_, calBackend, _, _, _, cleanup := nextcloud.NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := testContext()

	// 1. Get calendars
	cals, err := calBackend.GetAllCalendars(ctx)
	if err != nil {
		t.Fatalf("GetAllCalendars failed: %v", err)
	}
	if len(cals) == 0 {
		t.Fatalf("Expected at least 1 calendar, got 0")
	}
	if !cals[0].IsDefault {
		t.Errorf("Expected first calendar to have IsDefault=true via RFC 6638 discovery")
	}

	// 2. Create CalendarEvent
	ev := &jmapcalendar.CalendarEvent{
		Title:       "Embedded Sprint Planning",
		Description: "In-Process CalDAV Testing",
		Start:       "2026-11-15T09:00:00Z",
		Duration:    "PT1H",
		CalendarIDs: map[jmapcore.Id]bool{cals[0].ID: true},
	}
	created, err := calBackend.CreateCalendarEvent(ctx, ev)
	if err != nil {
		t.Fatalf("CreateCalendarEvent failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("Expected created event to have ID")
	}

	// 3. Get CalendarEvent
	fetched, notFound, err := calBackend.GetCalendarEvents(ctx, []jmapcore.Id{created.ID})
	if err != nil {
		t.Fatalf("GetCalendarEvents failed: %v", err)
	}
	if len(notFound) > 0 || len(fetched) == 0 {
		t.Fatalf("GetCalendarEvents could not find created event %s (notFound=%v)", created.ID, notFound)
	}
	if fetched[0].Title != "Embedded Sprint Planning" {
		t.Errorf("Expected title 'Embedded Sprint Planning', got %q", fetched[0].Title)
	}

	// 4. Query CalendarEvents
	ids, total, err := calBackend.QueryCalendarEvents(ctx, map[string]any{
		"text": "Sprint Planning",
	}, nil, 0, nil, false)
	if err != nil {
		t.Fatalf("QueryCalendarEvents failed: %v", err)
	}
	if total == 0 || len(ids) == 0 {
		t.Errorf("QueryCalendarEvents returned 0 matches for 'Sprint Planning'")
	}

	// 5. Delete CalendarEvent
	delOk, err := calBackend.DeleteCalendarEvent(ctx, created.ID)
	if err != nil || !delOk {
		t.Fatalf("DeleteCalendarEvent failed: %v", err)
	}
}

func TestEmbeddedNextcloudContacts(t *testing.T) {
	_, _, contactsBackend, _, _, cleanup := nextcloud.NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := testContext()

	// 1. Get AddressBooks
	abs, err := contactsBackend.GetAllAddressBooks(ctx)
	if err != nil {
		t.Fatalf("GetAllAddressBooks failed: %v", err)
	}
	if len(abs) == 0 {
		t.Fatalf("Expected at least 1 address book, got 0")
	}

	// 2. Create Card
	card := &jmapcontacts.Card{
		Name: &jmapcontacts.JSContactName{
			Full: "Alice Embedded",
		},
		Emails: map[string]*jmapcontacts.JSContactEmailAddress{
			"e1": {Address: "alice.emb@example.com"},
		},
		AddressBookIDs: map[jmapcore.Id]bool{abs[0].ID: true},
	}
	created, err := contactsBackend.CreateCard(ctx, card)
	if err != nil {
		t.Fatalf("CreateCard failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("Expected created card to have ID")
	}

	// 3. Get Card
	fetched, notFound, err := contactsBackend.GetCards(ctx, []jmapcore.Id{created.ID})
	if err != nil {
		t.Fatalf("GetCards failed: %v", err)
	}
	if len(notFound) > 0 || len(fetched) == 0 {
		t.Fatalf("GetCards could not find created card %s (notFound=%v)", created.ID, notFound)
	}
	if fetched[0].Name == nil || fetched[0].Name.Full != "Alice Embedded" {
		t.Errorf("Expected name 'Alice Embedded', got %v", fetched[0].Name)
	}

	// 4. Delete Card
	delOk, err := contactsBackend.DeleteCard(ctx, created.ID)
	if err != nil || !delOk {
		t.Fatalf("DeleteCard failed: %v", err)
	}
}

func TestEmbeddedNextcloudFileNodes(t *testing.T) {
	_, _, _, fileNodeBackend, _, cleanup := nextcloud.NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := testContext()

	// 1. Create FileNode folder
	folderNode := &jmap.FileNode{
		Name:     "TestFolder",
		IsFolder: true,
		Type:     "folder",
	}
	created, err := fileNodeBackend.CreateFileNode(ctx, folderNode)
	if err != nil {
		t.Fatalf("CreateFileNode failed: %v", err)
	}

	// 2. Get FileNodes
	nodes, err := fileNodeBackend.GetAllFileNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllFileNodes failed: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatalf("Expected created folder to appear, got 0 nodes")
	}

	// 3. Delete FileNode folder
	delOk, err := fileNodeBackend.DeleteFileNode(ctx, created.ID)
	if err != nil || !delOk {
		t.Fatalf("DeleteFileNode failed: %v", err)
	}
}

func TestEmbeddedNextcloudPrincipals(t *testing.T) {
	_, _, _, _, principalsBackend, cleanup := nextcloud.NewEmbeddedBackend("user@example.com")
	defer cleanup()
	ctx := testContext()

	principals, err := principalsBackend.GetAllPrincipals(ctx)
	if err != nil {
		t.Fatalf("GetAllPrincipals failed: %v", err)
	}
	if len(principals) == 0 {
		t.Fatalf("Expected seeded principals, got 0")
	}

	// Availability check
	windows, err := principalsBackend.GetAvailability(ctx, principals[0].ID, "2026-11-15T00:00:00Z", "2026-11-15T23:59:59Z")
	if err != nil {
		t.Fatalf("GetAvailability failed: %v", err)
	}
	_ = windows
}

func TestEmbeddedNextcloudGroupMembershipOnDemand(t *testing.T) {
	client, _, _, _, principalsBackend, cleanup := nextcloud.NewEmbeddedBackend("alice@example.com")
	defer cleanup()

	ctx := context.Background()
	ctx = jmapauth.ContextWithAccountID(ctx, "alice@example.com")
	ctx = jmapauth.ContextWithSubject(ctx, "alice@example.com")
	ctx = jmapauth.ContextWithCredentials(ctx, "alice@example.com", "alice@example.com")

	// 1. Verify user can query their own user info via Nextcloud OCS using user credentials
	user, err := client.GetCurrentUser(ctx)
	if err != nil {
		t.Fatalf("GetCurrentUser failed: %v", err)
	}
	if user.ID != "alice@example.com" {
		t.Errorf("Expected user ID 'alice@example.com', got %q", user.ID)
	}
	if len(user.Groups) == 0 {
		t.Errorf("Expected user to belong to at least 1 group, got %v", user.Groups)
	}

	// 2. Fetch specific group principal p-team on demand
	principals, notFound, err := principalsBackend.GetPrincipals(ctx, []jmapcore.Id{"p-team"})
	if err != nil {
		t.Fatalf("GetPrincipals for p-team failed: %v", err)
	}
	if len(notFound) > 0 || len(principals) == 0 {
		t.Fatalf("Expected p-team to be found, notFound=%v", notFound)
	}
	team := principals[0]
	if team.Type != "group" {
		t.Errorf("Expected type 'group', got %q", team.Type)
	}
	if team.Members == nil || !team.Members["p-alice@example.com"] {
		t.Errorf("Expected alice@example.com to be in p-team members: %v", team.Members)
	}

	// 3. Add a new user via user credentials
	bobCtx := context.Background()
	bobCtx = jmapauth.ContextWithAccountID(bobCtx, "bob@example.com")
	bobCtx = jmapauth.ContextWithSubject(bobCtx, "bob@example.com")
	bobCtx = jmapauth.ContextWithCredentials(bobCtx, "bob@example.com", "bob@example.com")

	err = principalsBackend.EnsureUser(bobCtx, "bob@example.com", "bob@example.com")
	if err != nil {
		t.Fatalf("EnsureUser failed: %v", err)
	}

	// 4. GetAllPrincipals should collect group memberships and contain both members
	all, err := principalsBackend.GetAllPrincipals(ctx)
	if err != nil {
		t.Fatalf("GetAllPrincipals failed: %v", err)
	}
	var teamGroup *jmapprincipals.Principal
	for _, p := range all {
		if p.ID == "p-team" {
			teamGroup = p
			break
		}
	}
	if teamGroup == nil {
		t.Fatalf("Expected p-team in GetAllPrincipals")
	}
	if !teamGroup.Members["p-alice@example.com"] {
		t.Errorf("Expected alice in teamGroup.Members: %v", teamGroup.Members)
	}
	if !teamGroup.Members["p-bob@example.com"] {
		t.Errorf("Expected bob in teamGroup.Members: %v", teamGroup.Members)
	}
}

func TestGroupNamesInjectionPrevention(t *testing.T) {
	// 1. IsValidGroupID tests
	validGroups := []string{
		"team",
		"all",
		"marketing-group",
		"eng_team",
		"dev.team",
		"sales team",
		"Level-1_Support.v2",
	}
	for _, g := range validGroups {
		if !nextcloud.IsValidGroupID(g) {
			t.Errorf("Expected valid group ID %q to pass validation", g)
		}
	}

	invalidGroups := []string{
		"",
		"../../admin",
		"../",
		"team/users",
		"team\\users",
		"team\r\nBcc: evil@example.com",
		"team\n",
		"team\r",
		"team\x00",
		"team?format=json",
		"team#section",
		"team%2f",
		"<script>alert(1)</script>",
		"team; rm -rf /",
		"team|cat /etc/passwd",
		"team\"quote",
		"team'quote",
		"team:colon",
	}
	for _, g := range invalidGroups {
		if nextcloud.IsValidGroupID(g) {
			t.Errorf("Expected invalid group ID %q to fail validation", g)
		}
	}

	// 2. Client API rejects invalid group IDs
	client, _, _, _, principalsBackend, cleanup := nextcloud.NewEmbeddedBackend("alice@example.com")
	defer cleanup()

	ctx := context.Background()
	ctx = jmapauth.ContextWithAccountID(ctx, "alice@example.com")
	ctx = jmapauth.ContextWithSubject(ctx, "alice@example.com")
	ctx = jmapauth.ContextWithCredentials(ctx, "alice@example.com", "alice@example.com")

	if _, err := client.GetGroupMembers(ctx, "../../admin"); err == nil {
		t.Errorf("Expected GetGroupMembers with path traversal to fail")
	}
	if err := client.CreateGroup(ctx, "team\r\nBcc:evil@example.com"); err == nil {
		t.Errorf("Expected CreateGroup with CRLF to fail")
	}
	if err := client.AddUserToGroup(ctx, "alice", "team/evil"); err == nil {
		t.Errorf("Expected AddUserToGroup with slash to fail")
	}

	// 3. PrincipalsBackend rejects malicious group IDs and names
	_, notFound, err := principalsBackend.GetPrincipals(ctx, []jmapcore.Id{"p-../../admin", "p-team\r\n"})
	if err != nil {
		t.Fatalf("GetPrincipals failed: %v", err)
	}
	if len(notFound) != 2 {
		t.Errorf("Expected both malicious IDs to be returned in notFound, got %v", notFound)
	}

	// 4. CreatePrincipal rejects malicious group name
	_, err = principalsBackend.CreatePrincipal(ctx, &jmapprincipals.Principal{
		Type: "group",
		Name: "../../admin",
	})
	if err == nil {
		t.Errorf("Expected CreatePrincipal with path traversal to fail")
	}

	_, err = principalsBackend.CreatePrincipal(ctx, &jmapprincipals.Principal{
		Type: "group",
		Name: "evil\r\nBcc: victim@example.com",
	})
	if err == nil {
		t.Errorf("Expected CreatePrincipal with CRLF to fail")
	}
}



