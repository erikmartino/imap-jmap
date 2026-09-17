package jmap

import (
	"context"

	"imap-jmap/jmap/jmapmail"
)

// InboxMailboxID returns the id of the account's INBOX mailbox (role "inbox") as
// reported by the backend, or "" when it cannot be determined. Backends use
// different id schemes (the memory backend uses "mb-inbox", gateway backends
// derive ids from the folder name), so delivery code must never hardcode one.
func InboxMailboxID(ctx context.Context, backend MailBackend) Id {
	return jmapmail.InboxMailboxID(ctx, backend)
}

// MailboxIDByName returns the ID of a mailbox matching name or role for the given context.
func MailboxIDByName(ctx context.Context, backend MailBackend, name string) Id {
	return jmapmail.MailboxIDByName(ctx, backend, name)
}

func handleEmailSubmissionGet(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailSubmissionGet(backend)
}

func handleEmailSubmissionChanges(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailSubmissionChanges(backend)
}

func handleEmailSubmissionSet(backend MailBackend, blobBackend BlobBackend, resolver AccountResolver, allowedRecipients map[string]bool, outbound OutboundMailSender) MethodHandler {
	return jmapmail.HandleEmailSubmissionSet(backend, blobBackend, resolver, allowedRecipients, outbound)
}

func handleEmailSubmissionQuery(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailSubmissionQuery(backend)
}

func handleEmailSubmissionQueryChanges(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailSubmissionQueryChanges(backend)
}
