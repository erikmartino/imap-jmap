package imapsmtp

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"imap-jmap/jmap"
)

func getTestTargetServers() (string, string) {
	imapAddr := os.Getenv("TEST_IMAP_SERVER")
	if imapAddr == "" {
		imapAddr = "127.0.0.1:1143"
	}

	smtpAddr := os.Getenv("TEST_SMTP_SERVER")
	if smtpAddr == "" {
		smtpAddr = "127.0.0.1:2525"
	}
	return imapAddr, smtpAddr
}

func isIMAPReachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func testContext() context.Context {
	ctx := context.Background()
	ctx = jmap.ContextWithSubject(ctx, "user@example.com")
	ctx = jmap.ContextWithAccountID(ctx, jmap.AccountIDForSubject("user@example.com"))
	ctx = jmap.ContextWithCredentials(ctx, "user@example.com", "user@example.com")
	return ctx
}

func TestClientPool_DovecotContainerConnection(t *testing.T) {
	imapAddr, _ := getTestTargetServers()
	if !isIMAPReachable(imapAddr) {
		t.Skip("IMAP server is not reachable at " + imapAddr)
	}

	pool := NewClientPool(imapAddr)
	ctx := context.Background()

	client, err := pool.GetClient(ctx, "user@example.com", "user@example.com")
	if err != nil {
		t.Fatalf("failed to connect/login to Docker Compose Dovecot server at %s: %v", imapAddr, err)
	}
	_ = client.Close()
}

func TestIMAPAuthBackend_SessionTokenFlow(t *testing.T) {
	imapAddr, _ := getTestTargetServers()
	if !isIMAPReachable(imapAddr) {
		t.Skip("IMAP server is not reachable at " + imapAddr)
	}
	pool := NewClientPool(imapAddr)
	authBackend := NewAuthBackend(pool, "test-secret-key-12345")

	ctx := context.Background()

	// 1. Authenticate with valid credentials -> returns encrypted session token
	token, err := authBackend.Authenticate(ctx, "user@example.com", "user@example.com")
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if token == "" {
		t.Fatalf("expected non-empty token")
	}

	// 2. Validate token
	accountID, subject, err := authBackend.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if subject != "user@example.com" {
		t.Errorf("expected subject user@example.com, got %s", subject)
	}
	if accountID != jmap.AccountIDForSubject("user@example.com") {
		t.Errorf("unexpected account ID: %s", accountID)
	}

	// 3. Extract credentials
	u, p, ok := authBackend.ExtractCredentials(ctx, token)
	if !ok || u != "user@example.com" || p != "user@example.com" {
		t.Errorf("ExtractCredentials failed, got u=%s, p=%s, ok=%t", u, p, ok)
	}

	// 4. Validate empty credentials
	_, err = authBackend.Authenticate(ctx, "", "")
	if err == nil {
		t.Fatalf("expected error for empty credentials")
	}
}

func TestMailboxLifecycle(t *testing.T) {
	imapAddr, smtpAddr := getTestTargetServers()
	if !isIMAPReachable(imapAddr) {
		t.Skip("IMAP server is not reachable at " + imapAddr)
	}
	be := New(imapAddr, smtpAddr)
	ctx := testContext()

	// 1. List Mailboxes
	mailboxes, err := be.GetAllMailboxes(ctx)
	if err != nil {
		t.Fatalf("GetAllMailboxes failed: %v", err)
	}
	if len(mailboxes) == 0 {
		t.Fatalf("expected at least 1 mailbox")
	}

	// Verify INBOX exists
	var inbox *jmap.Mailbox
	for _, mb := range mailboxes {
		if mb.Name == "INBOX" || (mb.Role != nil && *mb.Role == "inbox") {
			inbox = mb
			break
		}
	}
	if inbox == nil {
		t.Fatalf("INBOX mailbox not found")
	}

	// 2. Create custom mailbox
	testFolder := fmt.Sprintf("TestFolder_%d", time.Now().UnixNano())
	createdMb, err := be.CreateMailbox(ctx, &jmap.Mailbox{Name: testFolder})
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}
	if createdMb.ID == "" {
		t.Fatalf("expected created mailbox to have an ID")
	}

	// 3. Get created mailbox
	found, notFound, err := be.GetMailboxes(ctx, []jmap.Id{createdMb.ID})
	if err != nil || len(found) != 1 || len(notFound) != 0 {
		t.Fatalf("GetMailboxes failed: found=%d, notFound=%d, err=%v", len(found), len(notFound), err)
	}

	// 4. Rename mailbox
	renamedFolder := testFolder + "_Renamed"
	renamedMb, err := be.UpdateMailbox(ctx, createdMb.ID, map[string]any{"name": renamedFolder})
	if err != nil {
		t.Fatalf("UpdateMailbox rename failed: %v", err)
	}

	// 5. Delete mailbox
	ok, err := be.DeleteMailbox(ctx, renamedMb.ID, true)
	if err != nil || !ok {
		t.Fatalf("DeleteMailbox failed: ok=%t, err=%v", ok, err)
	}
}

func TestCompositeStateAndChanges(t *testing.T) {
	imapAddr, smtpAddr := getTestTargetServers()
	if !isIMAPReachable(imapAddr) {
		t.Skip("IMAP server is not reachable at " + imapAddr)
	}
	be := New(imapAddr, smtpAddr)
	ctx := testContext()

	state := be.State(ctx)
	if !strings.HasPrefix(state, "v1.") {
		t.Fatalf("expected composite state token with v1. prefix, got %s", state)
	}

	cs, err := DecodeCompositeState(state)
	if err != nil {
		t.Fatalf("DecodeCompositeState failed: %v", err)
	}
	if len(cs.Folders) == 0 {
		t.Fatalf("expected non-empty folders in composite state")
	}

	// Verify MailboxChanges with current state reports no changes
	created, updated, destroyed, _, newState, hasMore := be.MailboxChanges(ctx, state, nil)
	if len(created) != 0 || len(destroyed) != 0 {
		t.Errorf("expected 0 created/destroyed mailboxes, got created=%d, destroyed=%d", len(created), len(destroyed))
	}
	if hasMore {
		t.Errorf("expected hasMore=false")
	}
	_ = updated
	_ = newState
}

func TestEmailLifecycleAndFlags(t *testing.T) {
	imapAddr, smtpAddr := getTestTargetServers()
	if !isIMAPReachable(imapAddr) {
		t.Skip("IMAP server is not reachable at " + imapAddr)
	}
	be := New(imapAddr, smtpAddr)
	ctx := testContext()

	inboxID := MailboxIDForName("INBOX")

	// 1. Create / Append an email
	subjectText := fmt.Sprintf("JMAP Test Email %d", time.Now().UnixNano())
	partID := "1"
	email := &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{inboxID: true},
		Keywords:   map[string]bool{"$seen": true, "$flagged": true},
		From:       []jmap.EmailAddress{{Name: "Sender", Email: "sender@example.com"}},
		To:         []jmap.EmailAddress{{Name: "User", Email: "user@example.com"}},
		Subject:    subjectText,
		BodyValues: map[string]jmap.EmailBodyValue{
			"1": {Value: "Hello from JMAP IMAP test!"},
		},
		TextBody: []jmap.EmailBodyPart{
			{PartID: &partID, Type: "text/plain"},
		},
	}

	created, err := be.CreateEmail(ctx, email)
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected created email to have an ID")
	}

	// 2. Fetch the created email
	emails, notFound, err := be.GetEmails(ctx, []jmap.Id{created.ID})
	if err != nil || len(emails) != 1 || len(notFound) != 0 {
		t.Fatalf("GetEmails failed: found=%d, notFound=%d, err=%v", len(emails), len(notFound), err)
	}
	fetched := emails[0]
	if fetched.Subject != subjectText {
		t.Errorf("expected subject %s, got %s", subjectText, fetched.Subject)
	}
	if !fetched.Keywords["$seen"] || !fetched.Keywords["$flagged"] {
		t.Errorf("expected $seen and $flagged keywords, got: %v", fetched.Keywords)
	}

	// 3. Update keywords
	updated, err := be.UpdateEmail(ctx, created.ID, map[string]any{
		"keywords": map[string]any{
			"$seen":    true,
			"$flagged": false,
		},
	})
	if err != nil {
		t.Fatalf("UpdateEmail keywords failed: %v", err)
	}
	_ = updated

	// 4. Query email
	queryIDs, total, err := be.QueryEmails(ctx, map[string]any{"inMailbox": string(inboxID), "subject": subjectText}, nil, 0, nil)
	if err != nil {
		t.Fatalf("QueryEmails failed: %v", err)
	}
	if total == 0 || len(queryIDs) == 0 {
		t.Errorf("expected query to find email with subject %s, found %d", subjectText, total)
	}

	// 5. Delete email
	delOk, err := be.DeleteEmail(ctx, created.ID)
	if err != nil || !delOk {
		t.Fatalf("DeleteEmail failed: ok=%t, err=%v", delOk, err)
	}
}

func TestBlobStorage(t *testing.T) {
	imapAddr, smtpAddr := getTestTargetServers()
	if !isIMAPReachable(imapAddr) {
		t.Skip("IMAP server is not reachable at " + imapAddr)
	}
	be := New(imapAddr, smtpAddr)
	ctx := testContext()

	data := []byte("Sample blob content for JMAP attachment")
	blob, err := be.PutBlob(ctx, "user-account", "text/plain", data)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}
	if blob.ID == "" || blob.Size != int64(len(data)) {
		t.Fatalf("invalid PutBlob result: %+v", blob)
	}

	fetchedBlob, ok, err := be.GetBlob(ctx, "user-account", blob.ID)
	if err != nil || !ok || string(fetchedBlob.Data) != string(data) {
		t.Fatalf("GetBlob failed: ok=%t, err=%v, data=%s", ok, err, string(fetchedBlob.Data))
	}
}

func TestEmailSubmissionAndSMTP(t *testing.T) {
	imapAddr, smtpAddr := getTestTargetServers()
	if !isIMAPReachable(imapAddr) {
		t.Skip("IMAP server is not reachable at " + imapAddr)
	}
	be := New(imapAddr, smtpAddr)
	ctx := testContext()

	inboxID := MailboxIDForName("INBOX")
	partID := "1"
	email := &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{inboxID: true},
		From:       []jmap.EmailAddress{{Name: "Sender", Email: "user@example.com"}},
		To:         []jmap.EmailAddress{{Name: "Recipient", Email: "recipient@example.com"}},
		Subject:    "Outbound SMTP Test",
		BodyValues: map[string]jmap.EmailBodyValue{
			"1": {Value: "Test outbound email message body."},
		},
		TextBody: []jmap.EmailBodyPart{
			{PartID: &partID, Type: "text/plain"},
		},
	}
	createdEmail, err := be.CreateEmail(ctx, email)
	if err != nil {
		t.Fatalf("CreateEmail for submission failed: %v", err)
	}

	sub := &jmap.EmailSubmission{
		EmailID: createdEmail.ID,
		Envelope: &jmap.SubmissionEnvelope{
			MailFrom: jmap.SubmissionAddress{Email: "user@example.com"},
			RcptTo:   []jmap.SubmissionAddress{{Email: "recipient@example.com"}},
		},
	}

	createdSub, err := be.CreateSubmission(ctx, sub)
	if err != nil {
		t.Fatalf("CreateSubmission failed: %v", err)
	}
	if createdSub.ID == "" || createdSub.SendAt == "" {
		t.Errorf("expected valid ID and SendAt on submission: %+v", createdSub)
	}

	// Clean up created email
	_, _ = be.DeleteEmail(ctx, createdEmail.ID)
}

func TestEmailQuerySearchWildcardsAndOperators(t *testing.T) {
	imapAddr, smtpAddr := getTestTargetServers()
	if !isIMAPReachable(imapAddr) {
		t.Skip("IMAP server is not reachable at " + imapAddr)
	}
	be := New(imapAddr, smtpAddr)
	ctx := testContext()

	inboxID := MailboxIDForName("INBOX")
	part1 := "1"
	uniqueSubj := fmt.Sprintf("SearchableSubject_%d", time.Now().UnixNano())
	email := &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{inboxID: true},
		From:       []jmap.EmailAddress{{Name: "Search Sender", Email: "searcher@example.com"}},
		To:         []jmap.EmailAddress{{Name: "Recipient", Email: "recipient@example.com"}},
		Subject:    uniqueSubj,
		BodyValues: map[string]jmap.EmailBodyValue{
			"1": {Value: "UniqueBodyKeyword search testing payload."},
		},
		TextBody: []jmap.EmailBodyPart{
			{PartID: &part1, Type: "text/plain"},
		},
	}
	created, err := be.CreateEmail(ctx, email)
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}
	defer func() { _, _ = be.DeleteEmail(ctx, created.ID) }()

	// 1. Search with wildcard on subject: "SearchableSubject*"
	ids, total, err := be.QueryEmails(ctx, map[string]any{
		"subject": uniqueSubj + "*",
	}, nil, 0, nil)
	if err != nil {
		t.Fatalf("QueryEmails subject wildcard failed: %v", err)
	}
	if total == 0 || len(ids) == 0 {
		t.Errorf("expected to find email with wildcard subject %q, got 0", uniqueSubj+"*")
	}

	// 2. Search with wildcard on free text: "UniqueBodyKeyword*"
	ids, total, err = be.QueryEmails(ctx, map[string]any{
		"text": "UniqueBodyKeyword*",
	}, nil, 0, nil)
	if err != nil {
		t.Fatalf("QueryEmails text wildcard failed: %v", err)
	}
	if total == 0 || len(ids) == 0 {
		t.Errorf("expected to find email with wildcard text 'UniqueBodyKeyword*', got 0")
	}

	// 3. Search with OR operator
	ids, total, err = be.QueryEmails(ctx, map[string]any{
		"operator": "OR",
		"conditions": []any{
			map[string]any{"subject": "NonExistentSubject9999*"},
			map[string]any{"text": "UniqueBodyKeyword*"},
		},
	}, nil, 0, nil)
	if err != nil {
		t.Fatalf("QueryEmails OR query failed: %v", err)
	}
	if total == 0 || len(ids) == 0 {
		t.Errorf("expected to find email with OR operator containing matching branch, got 0")
	}
}

func TestIMAPIdlePushNotification(t *testing.T) {
	imapAddr, smtpAddr := getTestTargetServers()
	if !isIMAPReachable(imapAddr) {
		t.Skip("IMAP server is not reachable at " + imapAddr)
	}

	be := New(imapAddr, smtpAddr)
	defer be.Pool().Close()

	broadcaster := jmap.NewBroadcaster()
	be.SetBroadcaster(broadcaster)

	ch := broadcaster.Subscribe()
	defer broadcaster.Unsubscribe(ch)

	ctx := testContext()
	be.RecordAccount(ctx)

	// Trigger an email creation
	inboxID := MailboxIDForName("INBOX")
	_, err := be.CreateEmail(ctx, &jmap.Email{
		MailboxIDs: map[jmap.Id]bool{inboxID: true},
		Subject:    "Push Test Message",
		TextBody: []jmap.EmailBodyPart{
			{
				PartID: stringPtr("1"),
				Type:   "text/plain",
			},
		},
		BodyValues: map[string]jmap.EmailBodyValue{
			"1": {Value: "Push test content"},
		},
	})
	if err != nil {
		t.Fatalf("CreateEmail failed: %v", err)
	}

	accountID := jmap.AccountIDForSubject("user@example.com")
	select {
	case sc := <-ch:
		if sc == nil || sc.Changed[accountID] == nil || sc.Changed[accountID]["Email"] == "" {
			t.Errorf("expected StateChange with Email state for %s, got: %+v", accountID, sc)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for push StateChange notification")
	}
}

func stringPtr(s string) *string {
	return &s
}
