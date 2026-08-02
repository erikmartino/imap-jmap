package jmap

// RegisterMailHandlers registers all RFC 8621 JMAP Mail and RFC 9219 S/MIME methods into MethodRegistry.
func RegisterMailHandlers(r *MethodRegistry, backend MailBackend) {
	// Mailbox (Section 2)
	r.Register("Mailbox/get", handleMailboxGet(backend))
	r.Register("Mailbox/changes", handleMailboxChanges(backend))
	r.Register("Mailbox/set", handleMailboxSet(backend))
	r.Register("Mailbox/copy", handleMailboxCopy(backend))
	r.Register("Mailbox/query", handleMailboxQuery(backend))
	r.Register("Mailbox/queryChanges", handleMailboxQueryChanges(backend))

	// Thread (Section 3)
	r.Register("Thread/get", handleThreadGet(backend))
	r.Register("Thread/changes", handleThreadChanges(backend))

	// Email (Section 4)
	r.Register("Email/get", handleEmailGet(backend))
	r.Register("Email/changes", handleEmailChanges(backend))
	r.Register("Email/set", handleEmailSet(backend))
	r.Register("Email/copy", handleEmailCopy(backend))
	r.Register("Email/query", handleEmailQuery(backend))
	r.Register("Email/queryChanges", handleEmailQueryChanges(backend))
	r.Register("Email/import", handleEmailImport(backend))
	r.Register("Email/parse", handleEmailParse(backend))

	// S/MIME Verification (RFC 9219 Section 4)
	r.Register("Email/verifySmime", handleEmailVerifySmime(backend))

	// SearchSnippet (Section 5)
	r.Register("SearchSnippet/get", handleSearchSnippetGet(backend))

	// Identity (Section 6)
	r.Register("Identity/get", handleIdentityGet(backend))
	r.Register("Identity/changes", handleIdentityChanges(backend))
	r.Register("Identity/set", handleIdentitySet(backend))

	// EmailSubmission (Section 7)
	r.Register("EmailSubmission/get", handleEmailSubmissionGet(backend))
	r.Register("EmailSubmission/changes", handleEmailSubmissionChanges(backend))
	r.Register("EmailSubmission/set", handleEmailSubmissionSet(backend))
	r.Register("EmailSubmission/query", handleEmailSubmissionQuery(backend))
	r.Register("EmailSubmission/queryChanges", handleEmailSubmissionQueryChanges(backend))

	// MDN (RFC 9007 Section 3)
	r.Register("MDN/send", handleMDNSend(backend))
	r.Register("MDN/parse", handleMDNParse(backend))

	// PushSubscription (RFC 8620 Section 7.2 + RFC 9749)
	r.Register("PushSubscription/get", handlePushSubscriptionGet(backend))
	r.Register("PushSubscription/set", handlePushSubscriptionSet(backend))
}
