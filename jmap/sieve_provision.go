package jmap

import (
	"context"

	"imap-jmap/jmap/jmapsieve"
)

// defaultSieveScript tags incoming iTIP/iMIP mail with the $itip keyword so the
// calendar can find and apply it (see Server.ProcessMailboxITIP). It only adds the
// flag; the implicit keep still delivers the message to the INBOX.
const defaultSieveScript = `require ["imap4flags", "body"];
if body :raw :contains "text/calendar" {
    addflag "$itip";
}
`

// EnsureDefaultSieveScript installs and activates the default iTIP-tagging Sieve script
// for a user who has no Sieve scripts yet. It is invoked on first login (when the user's
// credentials are available) and is a no-op when the user already has any script, so it
// never clobbers user rules.
func EnsureDefaultSieveScript(ctx context.Context, backend jmapsieve.SieveBackend) error {
	if backend == nil {
		return nil
	}
	scripts, err := backend.GetAllSieveScripts(ctx)
	if err != nil {
		return err
	}
	if len(scripts) > 0 {
		return nil
	}
	_, err = backend.CreateSieveScript(ctx, &jmapsieve.SieveScript{
		Name:     "imap-jmap",
		Content:  defaultSieveScript,
		IsActive: true,
	})
	return err
}
