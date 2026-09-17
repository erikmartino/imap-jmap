package jmapmail

import (
	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmaphandler"
)

// RegisterMailHandlers registers all RFC 8621 JMAP Mail and RFC 9219 S/MIME methods into MethodRegistry.
// blobBackend is used by Email/import and Email/parse to read raw RFC 5322 message blobs, and by
// EmailSubmission/set to fetch the raw message bytes for outbound relay. outbound delivers
// submissions to external (allow-listed) recipients via their domain's MX servers.
func RegisterMailHandlers(r *jmaphandler.MethodRegistry, backend MailBackend, blobBackend jmapblob.BlobBackend, resolver jmapauth.AccountResolver, allowedRecipients map[string]bool, outbound OutboundMailSender) {
	// Mailbox (Section 2)
	r.Register("Mailbox/get", HandleMailboxGet(backend))
	r.Register("Mailbox/changes", HandleMailboxChanges(backend))
	r.Register("Mailbox/set", HandleMailboxSet(backend))
	r.Register("Mailbox/copy", HandleMailboxCopy(backend))
	r.Register("Mailbox/query", HandleMailboxQuery(backend))
	r.Register("Mailbox/queryChanges", HandleMailboxQueryChanges(backend))

	// Thread (Section 3)
	r.Register("Thread/get", HandleThreadGet(backend))
	r.Register("Thread/changes", HandleThreadChanges(backend))

	// Email (Section 4)
	r.Register("Email/get", HandleEmailGet(backend))
	r.Register("Email/changes", HandleEmailChanges(backend))
	r.Register("Email/set", HandleEmailSet(backend, blobBackend))
	r.Register("Email/copy", HandleEmailCopy(backend))
	r.Register("Email/query", HandleEmailQuery(backend))
	r.Register("Email/queryChanges", HandleEmailQueryChanges(backend))
	r.Register("Email/import", HandleEmailImport(backend, blobBackend))
	r.Register("Email/parse", HandleEmailParse(backend, blobBackend))

	// S/MIME Verification (RFC 9219 Section 4)
	r.Register("Email/verifySmime", HandleEmailVerifySmime(backend))

	// SearchSnippet (Section 5)
	r.Register("SearchSnippet/get", HandleSearchSnippetGet(backend))

	// Identity (Section 6)
	r.Register("Identity/get", HandleIdentityGet(backend))
	r.Register("Identity/changes", HandleIdentityChanges(backend))
	r.Register("Identity/set", HandleIdentitySet(backend))

	// VacationResponse (RFC 8621 Section 8): per-account singleton, get + set only.
	r.Register("VacationResponse/get", HandleVacationResponseGet(backend))
	r.Register("VacationResponse/set", HandleVacationResponseSet(backend))

	// EmailSubmission (Section 7)
	r.Register("EmailSubmission/get", HandleEmailSubmissionGet(backend))
	r.Register("EmailSubmission/changes", HandleEmailSubmissionChanges(backend))
	r.Register("EmailSubmission/set", HandleEmailSubmissionSet(backend, blobBackend, resolver, allowedRecipients, outbound))
	r.Register("EmailSubmission/query", HandleEmailSubmissionQuery(backend))
	r.Register("EmailSubmission/queryChanges", HandleEmailSubmissionQueryChanges(backend))

	// MDN (RFC 9007 Section 3)
	r.Register("MDN/send", HandleMDNSend(backend))
	r.Register("MDN/parse", HandleMDNParse(backend))

	// PushSubscription (RFC 8620 Section 7.2 + RFC 9749)
	r.Register("PushSubscription/get", HandlePushSubscriptionGet(backend))
	r.Register("PushSubscription/set", HandlePushSubscriptionSet(backend))
}
