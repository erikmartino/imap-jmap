# Agent Guidelines & Project Rules

## RFC Validation & RFC 2119 Requirement Implementation Rule
All features, data model projections, protocol mappers, payload transformations, and server/client behaviors MUST be strictly validated against official IETF RFC standards.

Requirement selection and compliance boundaries are governed by [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119.html) and [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174.html) key words (`MUST`, `MUST NOT`, `REQUIRED`, `SHALL`, `SHALL NOT`, `SHOULD`, `SHOULD NOT`, `RECOMMENDED`, `MAY`, and `OPTIONAL`). 

Every requirement defined across all referenced RFC specifications—including all standard `MUST` clauses, `SHOULD` provisions, and optional `MAY` clauses—MUST be fully implemented and covered by dedicated unit test suites, unless a specific specification or feature is explicitly designated under the **Non-Goals & Out-of-Scope Specifications** section.

## Optional (MAY) Provision Implementation & Testing Requirement
All optional features, optional properties, optional query parameters, and `MAY` provisions defined across official IETF RFC specifications MUST be fully implemented and covered by dedicated unit tests. Do not limit implementation or testing solely to `MUST` or `SHOULD` requirements—every standard's `MAY` clauses MUST be verified to ensure maximum client interoperability, unless a feature is explicitly marked as a non-goal.

## Comprehensive Property & Filter Condition Test Coverage Rule
When implementing or modifying query methods (`*/query`, `*/queryChanges`), search filters (`FilterCondition`), data model properties, or protocol mappers:
1. **Property-by-Property Test Coverage**: Every single property defined in a filter condition (e.g. `body`, `cc`, `bcc`, `hasKeyword`, `notKeyword`, `header`, `text`, `inMailbox`, `inMailboxOtherThan`, `before`, `after`, `minSize`, `maxSize`, `hasAttachment`) MUST have dedicated unit tests verifying BOTH positive matching (returning matching items) AND negative filtering (excluding non-matching items).
2. **No Fallthrough Match Defaults**: Filter evaluation functions MUST explicitly test and validate every condition parameter rather than allowing unhandled properties to silently return `true`.
3. **Advertised Capability Backend Registration**: All server initialization paths (including `main.go` and server factory functions) MUST register handlers and backend instances for all advertised session capabilities so client requests to advertised endpoints never return `Unknown method`.

## Data-Loss Prevention on Update & Merge
When implementing updates, patches, or merges of any stored object (mails, mailboxes, contacts/cards, calendars/events, sieve scripts, blobs, submissions), treat user data as sacrosanct: a partial or malformed patch MUST fail the whole operation (or only that record) rather than silently dropping, overwriting, or zeroing fields that were not explicitly addressed. Never replace an entire object with a server- or client-supplied default, never fabricate values for data that was not provided, and never ignore or truncate existing properties during a merge. Destructive mutations (deletes, moves, role/default changes, privilege changes, token invalidation) MUST be rejected with an error when the target does not exist or cannot be safely located, and MUST never be silently no-oped while pretending success. Preserve server-set fields (created/updated timestamps, IDs) and, where the RFC mandates it, record and return oldState/newState plus notCreated/notUpdated/notDestroyed information so clients can reconcile. When in doubt, choose the operation that preserves data over the one that discards it, and add a regression test proving the previously-missing field survives an update.

### Official Specification References:

#### Requirement Level Specifications
- **RFC Requirement Levels**: [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119.html) / [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174.html) — *Key words for use in RFCs to Indicate Requirement Levels*

#### Core JMAP Specifications
- **JMAP Core**: [RFC 8620](https://www.rfc-editor.org/rfc/rfc8620.html) — *The JSON Meta Application Protocol (JMAP)*
- **JMAP Mail**: [RFC 8621](https://www.rfc-editor.org/rfc/rfc8621.html) — *The JSON Meta Application Protocol (JMAP) for Mail*
- **JMAP WebSockets**: [RFC 8887](https://www.rfc-editor.org/rfc/rfc8887.html) — *JMAP Subprotocol for WebSocket*
- **JMAP MDN**: [RFC 9007](https://www.rfc-editor.org/rfc/rfc9007.html) — *Message Disposition Notifications in JMAP*
- **JMAP S/MIME**: [RFC 9219](https://www.rfc-editor.org/rfc/rfc9219.html) — *S/MIME Signature Verification Extension*
- **JMAP Blob Management**: [RFC 9404](https://www.rfc-editor.org/rfc/rfc9404.html) — *JMAP Blob Management Extension*
- **JMAP Quotas**: [RFC 9425](https://www.rfc-editor.org/rfc/rfc9425.html) — *JMAP for Quotas*
- **JMAP for Contacts**: [RFC 9610](https://www.rfc-editor.org/rfc/rfc9610.html) — *JMAP for Contacts*
- **JMAP for Sieve Scripts**: [RFC 9661](https://www.rfc-editor.org/rfc/rfc9661.html) — *JMAP for Sieve Scripts*
- **JMAPACCESS (IMAP)**: [RFC 9698](https://www.rfc-editor.org/rfc/rfc9698.html) — *JMAPACCESS Extension for IMAP*
- **JMAP Push VAPID**: [RFC 9749](https://www.rfc-editor.org/rfc/rfc9749.html) — *VAPID Identification in JMAP Web Push*
- **JMAP Keywords & Attributes**: [RFC 9979](https://www.rfc-editor.org/rfc/rfc9979.html) — *IMAP/JMAP Keywords and Mailbox Name Attributes*
- **Sieve Language**: [RFC 5228](https://www.rfc-editor.org/rfc/rfc5228.html) — *Sieve: An Email Filtering Language*

#### Data Representation Specifications
- **JSContact (Card Specification)**: [RFC 9553](https://www.rfc-editor.org/rfc/rfc9553.html) — *JSContact: A JSON Representation of Contact Data*
- **JSCalendar (Calendar Specification)**: [RFC 8984](https://www.rfc-editor.org/rfc/rfc8984.html) — *JSCalendar: A JSON Representation of Calendar Data*

#### CardDAV & CalDAV Protocol Specifications
- **WebDAV Base**: [RFC 4918](https://www.rfc-editor.org/rfc/rfc4918.html) — *HTTP Extensions for Web Distributed Authoring and Versioning (WebDAV)*
- **CardDAV**: [RFC 6352](https://www.rfc-editor.org/rfc/rfc6352.html) — *CardDAV: vCard Extensions to WebDAV*
- **CalDAV**: [RFC 4791](https://www.rfc-editor.org/rfc/rfc4791.html) — *CalDAV: Calendaring Extensions to WebDAV*
- **CalDAV Scheduling**: [RFC 6638](https://www.rfc-editor.org/rfc/rfc6638.html) — *CalDAV Scheduling Extensions to iTIP*
- **vCard 4.0**: [RFC 6350](https://www.rfc-editor.org/rfc/rfc6350.html) — *vCard Format Specification*
- **vCard 3.0**: [RFC 2426](https://www.rfc-editor.org/rfc/rfc2426.html) — *vCard MIME Directory Profile*
- **iCalendar**: [RFC 5545](https://www.rfc-editor.org/rfc/rfc5545.html) — *Internet Calendaring and Email Object Specification*
- **iTIP (Scheduling)**: [RFC 5546](https://www.rfc-editor.org/rfc/rfc5546.html) — *iCalendar Transport-Independent Interoperability Protocol*
- **iMIP (Email Scheduling)**: [RFC 6047](https://www.rfc-editor.org/rfc/rfc6047.html) — *Message Binding for iTIP*

#### IMAP Protocol & Message Specifications
- **IMAP4rev1**: [RFC 3501](https://www.rfc-editor.org/rfc/rfc3501.html) — *INTERNET MESSAGE ACCESS PROTOCOL - VERSION 4rev1*
- **IMAP4rev2**: [RFC 9051](https://www.rfc-editor.org/rfc/rfc9051.html) — *Internet Message Access Protocol (IMAP) - Version 4rev2*
- **IMAP CONDSTORE & QRESYNC**: [RFC 7162](https://www.rfc-editor.org/rfc/rfc7162.html) — *IMAP Extensions: Quick Mailbox Resynchronization (QRESYNC) and Conditional STORE (CONDSTORE)*
- **IMAP IDLE**: [RFC 2177](https://www.rfc-editor.org/rfc/rfc2177.html) — *IMAP4 IDLE command*
- **IMAP MOVE**: [RFC 6851](https://www.rfc-editor.org/rfc/rfc6851.html) — *Internet Message Access Protocol (IMAP) - MOVE Extension*
- **IMAP SPECIAL-USE**: [RFC 6154](https://www.rfc-editor.org/rfc/rfc6154.html) — *IMAP LIST Extension for Special-Use Mailboxes*
- **IMAP UIDPLUS**: [RFC 4315](https://www.rfc-editor.org/rfc/rfc4315.html) — *Internet Message Access Protocol (IMAP) - UIDPLUS extension*
- **IMAP Keywords**: [RFC 5788](https://www.rfc-editor.org/rfc/rfc5788.html) — *IMAP Keyword Extension*
- **Internet Message Format**: [RFC 5322](https://www.rfc-editor.org/rfc/rfc5322.html) — *Internet Message Format*
- **MIME Media Types**: [RFC 2045](https://www.rfc-editor.org/rfc/rfc2045.html) — *Multipurpose Internet Mail Extensions (MIME) Part One*

#### SMTP & Mail Transport Specifications
- **SMTP**: [RFC 5321](https://www.rfc-editor.org/rfc/rfc5321.html) — *Simple Mail Transfer Protocol*
- **SMTP Submission**: [RFC 6409](https://www.rfc-editor.org/rfc/rfc6409.html) — *Message Submission for Mail*
- **SMTP Authentication**: [RFC 4954](https://www.rfc-editor.org/rfc/rfc4954.html) — *SMTP Service Extension for Authentication*
- **SMTP STARTTLS**: [RFC 3207](https://www.rfc-editor.org/rfc/rfc3207.html) — *SMTP Service Extension for Secure SMTP over Transport Layer Security*
- **SMTP SIZE Extension**: [RFC 1870](https://www.rfc-editor.org/rfc/rfc1870.html) — *SMTP Service Extension for Message Size Declaration*
- **SMTP DSN (Delivery Status Notifications)**: [RFC 3461](https://www.rfc-editor.org/rfc/rfc3461.html) — *Simple Mail Transfer Protocol (SMTP) Service Extension for Delivery Status Notifications*
- **SMTP Internationalized Email (UTF8):** [RFC 6531](https://www.rfc-editor.org/rfc/rfc6531.html) — *SMTP Extension for Internationalized Email*

---

### Non-Goals & Out-of-Scope Specifications
- **JMAP Sharing**: [RFC 9670](https://www.rfc-editor.org/rfc/rfc9670.html) — *JMAP Sharing* (Explicitly set as a Non-Goal for this server implementation).
