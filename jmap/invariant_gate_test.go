package jmap

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcontacts"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapfilenode"
	"imap-jmap/jmap/jmapmail"
	"imap-jmap/jmap/jmapsieve"
	"imap-jmap/jmap/managesieve"
	"imap-jmap/jmap/nextcloud"
)

// AGENTS.md §1 Invariant Gate: Zero Local Disk Writes.
// Statically verifies via AST analysis that no production backend code in imapsmtp,
// nextcloud, or managesieve writes authoritative user data to disk.
func TestInvariantGate_ZeroDiskWritesLint(t *testing.T) {
	pkgs := []string{"./imapsmtp", "./nextcloud", "./managesieve"}
	fset := token.NewFileSet()

	forbiddenCalls := map[string]map[string]bool{
		"os": {
			"Create":     true,
			"CreateTemp": true,
			"WriteFile":  true,
			"OpenFile":   true,
			"Mkdir":      true,
			"MkdirAll":   true,
			"Remove":     true,
			"RemoveAll":  true,
		},
		"ioutil": {
			"WriteFile": true,
			"TempFile":  true,
			"TempDir":   true,
		},
	}

	for _, pkgDir := range pkgs {
		pkgsParsed, err := parser.ParseDir(fset, pkgDir, func(fi fs.FileInfo) bool {
			// Skip test files and embedded test servers
			name := fi.Name()
			return !strings.HasSuffix(name, "_test.go") && name != "embedded.go"
		}, 0)
		if err != nil {
			t.Fatalf("Failed to parse package %s: %v", pkgDir, err)
		}

		for _, pkg := range pkgsParsed {
			for fileName, fileAst := range pkg.Files {
				ast.Inspect(fileAst, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					pkgIdent, ok := sel.X.(*ast.Ident)
					if !ok {
						return true
					}
					if funcs, ok := forbiddenCalls[pkgIdent.Name]; ok {
						if funcs[sel.Sel.Name] {
							pos := fset.Position(call.Pos())
							t.Errorf("AGENTS.md §1 violation: production code %s:%d calls %s.%s (stateless proxy must not write to local disk)",
								fileName, pos.Line, pkgIdent.Name, sel.Sel.Name)
						}
					}
					return true
				})
			}
		}
	}
}

// AGENTS.md §1 Invariant Gate: Multi-Instance Cross-Process State Determinism.
// Asserts that a completely fresh backend process (new backend instances over the
// exact same upstream with zero shared in-memory state or caches) returns identical
// */get payloads and state vectors across all domains.
func TestInvariantGate_FreshBackendInstancesIdenticalPayloadsAndStates(t *testing.T) {
	user := "user@example.com"
	ctx := context.Background()
	ctx = jmapauth.ContextWithSubject(ctx, user)
	ctx = jmapauth.ContextWithAccountID(ctx, jmapauth.AccountIDForSubject(user))
	ctx = jmapauth.ContextWithCredentials(ctx, user, user)

	// ==========================================
	// 1. IMAP/SMTP Gateway Domain
	// ==========================================
	b1Mail, cleanupMail := imapsmtp.NewEmbeddedBackend(user)
	defer cleanupMail()

	// Mutate on b1Mail
	ident, err := b1Mail.CreateIdentity(ctx, &jmapmail.Identity{
		Name:  "User Work",
		Email: "work@example.com",
	})
	if err != nil {
		t.Fatalf("b1Mail.CreateIdentity failed: %v", err)
	}

	vacBody := "Out of office"
	_, err = b1Mail.UpdateVacationResponse(ctx, map[string]any{
		"isEnabled": true,
		"textBody":  vacBody,
	})
	if err != nil {
		t.Fatalf("b1Mail.UpdateVacationResponse failed: %v", err)
	}

	inboxID := imapsmtp.MailboxIDForName("INBOX")
	_, err = b1Mail.CreateEmail(ctx, &jmapmail.Email{
		MailboxIDs: map[jmapcore.Id]bool{inboxID: true},
		Subject:    "Multi-Instance Invariant Test",
		From:       []jmapmail.EmailAddress{{Name: "Sender", Email: "sender@example.com"}},
		To:         []jmapmail.EmailAddress{{Name: "User", Email: user}},
		BodyValues: map[string]jmapmail.EmailBodyValue{"1": {Value: "Payload test"}},
		TextBody:   []jmapmail.EmailBodyPart{{PartID: stringPtr("1"), Type: "text/plain"}},
	})
	if err != nil {
		t.Fatalf("b1Mail.CreateEmail failed: %v", err)
	}

	// Capture states on b1Mail
	state1Ident := b1Mail.IdentityState(ctx)
	state1Mailbox := b1Mail.MailboxState(ctx)
	state1Vacation := b1Mail.VacationResponseState(ctx)

	// Spin up completely fresh b2Mail over same upstream IMAP server
	b2Mail := imapsmtp.New(b1Mail.IMAPAddr(), "")
	defer b2Mail.Close()

	// Verify states and payloads on b2Mail match b1Mail
	state2Ident := b2Mail.IdentityState(ctx)
	if state2Ident != state1Ident {
		t.Errorf("IdentityState mismatch across fresh instances: b1=%s, b2=%s", state1Ident, state2Ident)
	}
	state2Mailbox := b2Mail.MailboxState(ctx)
	if state2Mailbox != state1Mailbox {
		t.Errorf("MailboxState mismatch across fresh instances: b1=%s, b2=%s", state1Mailbox, state2Mailbox)
	}
	state2Vacation := b2Mail.VacationResponseState(ctx)
	if state2Vacation != state1Vacation {
		t.Errorf("VacationResponseState mismatch across fresh instances: b1=%s, b2=%s", state1Vacation, state2Vacation)
	}

	idents2, err := b2Mail.GetIdentities(ctx)
	if err != nil {
		t.Fatalf("b2Mail.GetIdentities failed: %v", err)
	}
	foundIdent := false
	for _, it := range idents2 {
		if it.ID == ident.ID && it.Email == "work@example.com" {
			foundIdent = true
			break
		}
	}
	if !foundIdent {
		t.Errorf("b2Mail did not return identity created by b1Mail")
	}

	vac2, err := b2Mail.GetVacationResponse(ctx)
	if err != nil || vac2 == nil || !vac2.IsEnabled || vac2.TextBody == nil || *vac2.TextBody != vacBody {
		t.Errorf("b2Mail did not return identical VacationResponse, got %+v", vac2)
	}

	// ==========================================
	// 2. Nextcloud Calendars & Contacts Domain
	// ==========================================
	client, b1Cal, b1Contacts, b1FileNode, _, cleanupNC := nextcloud.NewEmbeddedBackend(user)
	defer cleanupNC()

	// Mutate on b1Cal
	cal, err := b1Cal.CreateCalendar(ctx, &jmapcalendar.Calendar{
		Name:  "Test Calendar",
		Color: stringPtr("#00aa00"),
	})
	if err != nil {
		t.Fatalf("b1Cal.CreateCalendar failed: %v", err)
	}

	pi, err := b1Cal.CreateParticipantIdentity(ctx, &jmapcalendar.ParticipantIdentity{
		Name:            "Calendar Participant",
		CalendarAddress: "mailto:user@example.com",
	})
	if err != nil {
		t.Fatalf("b1Cal.CreateParticipantIdentity failed: %v", err)
	}

	// Mutate on b1Contacts
	ab, err := b1Contacts.CreateAddressBook(ctx, &jmapcontacts.AddressBook{
		Name: "Test AddressBook",
	})
	if err != nil {
		t.Fatalf("b1Contacts.CreateAddressBook failed: %v", err)
	}

	// Mutate on b1FileNode
	fn, err := b1FileNode.CreateFileNode(ctx, &jmapfilenode.FileNode{
		Name: "testdoc.txt",
	})
	if err != nil {
		t.Fatalf("b1FileNode.CreateFileNode failed: %v", err)
	}

	// Capture states on b1
	state1Cal := b1Cal.CalendarState(ctx)
	state1PI := b1Cal.ParticipantIdentityState(ctx)
	state1AB := b1Contacts.AddressBookState(ctx)
	state1FN := b1FileNode.FileNodeState(ctx)

	// Spin up completely fresh b2 backends over same Nextcloud client
	b2Cal := nextcloud.NewCalendarsBackend(client)
	b2Contacts := nextcloud.NewContactsBackend(client)
	b2FileNode := nextcloud.NewFileNodeBackend(client)

	// Verify states match across fresh instances
	state2Cal := b2Cal.CalendarState(ctx)
	if state2Cal != state1Cal {
		t.Errorf("CalendarState mismatch across fresh instances: b1=%s, b2=%s", state1Cal, state2Cal)
	}
	state2PI := b2Cal.ParticipantIdentityState(ctx)
	if state2PI != state1PI {
		t.Errorf("ParticipantIdentityState mismatch across fresh instances: b1=%s, b2=%s", state1PI, state2PI)
	}
	state2AB := b2Contacts.AddressBookState(ctx)
	if state2AB != state1AB {
		t.Errorf("AddressBookState mismatch across fresh instances: b1=%s, b2=%s", state1AB, state2AB)
	}
	state2FN := b2FileNode.FileNodeState(ctx)
	if state2FN != state1FN {
		t.Errorf("FileNodeState mismatch across fresh instances: b1=%s, b2=%s", state1FN, state2FN)
	}

	// Verify payloads on b2 match b1
	cals2, _, err := b2Cal.GetCalendars(ctx, []jmapcore.Id{cal.ID})
	if err != nil || len(cals2) != 1 || cals2[0].Name != "Test Calendar" {
		t.Errorf("b2Cal did not return identical Calendar: %v, err=%v", cals2, err)
	}

	pis2, _, err := b2Cal.GetParticipantIdentities(ctx, []jmapcore.Id{pi.ID})
	if err != nil || len(pis2) != 1 || pis2[0].Name != "Calendar Participant" {
		t.Errorf("b2Cal did not return identical ParticipantIdentity: %v, err=%v", pis2, err)
	}

	abs2, _, err := b2Contacts.GetAddressBooks(ctx, []jmapcore.Id{ab.ID})
	if err != nil || len(abs2) != 1 || abs2[0].Name != "Test AddressBook" {
		t.Errorf("b2Contacts did not return identical AddressBook: %v, err=%v", abs2, err)
	}

	fns2, _, err := b2FileNode.GetFileNodes(ctx, []jmapcore.Id{fn.ID})
	if err != nil || len(fns2) != 1 || fns2[0].Name != "testdoc.txt" {
		t.Errorf("b2FileNode did not return identical FileNode: %v, err=%v", fns2, err)
	}

	// ==========================================
	// 3. ManageSieve Domain
	// ==========================================
	srvSieve, b1Sieve, cleanupSieve := managesieve.NewEmbeddedBackend(user)
	defer cleanupSieve()

	sieveScript, err := b1Sieve.CreateSieveScript(ctx, &jmapsieve.SieveScript{
		Name:    "invariants_script",
		Content: "# sieve script\nkeep;\n",
	})
	if err != nil {
		t.Fatalf("b1Sieve.CreateSieveScript failed: %v", err)
	}

	state1Sieve := b1Sieve.SieveScriptState(ctx)

	// Spin up completely fresh b2Sieve over same ManageSieve server
	b2Sieve := managesieve.NewBackend(srvSieve.Addr())

	state2Sieve := b2Sieve.SieveScriptState(ctx)
	if state2Sieve != state1Sieve {
		t.Errorf("SieveScriptState mismatch across fresh instances: b1=%s, b2=%s", state1Sieve, state2Sieve)
	}

	scripts2, _, err := b2Sieve.GetSieveScripts(ctx, []jmapcore.Id{sieveScript.ID})
	if err != nil || len(scripts2) != 1 || scripts2[0].Name != "invariants_script" {
		t.Errorf("b2Sieve did not return identical SieveScript: %v, err=%v", scripts2, err)
	}
}

func stringPtr(s string) *string {
	return &s
}
