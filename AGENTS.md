# Agent Guidelines & Project Rules

## Guiding Principle: Indistinguishable From a Real Server
The overriding goal of this project is that **a client MUST NOT be able to determine that there is not a real, full-featured server behind the protocol.** Every endpoint, method, capability, error, and event MUST behave exactly as a production-grade server would. This principle governs and takes precedence over every other rule below: when a choice must be made, choose the behavior a real server would exhibit. Concretely, this means: no hardcoded/empty responses standing in for real logic, correct state and change tracking, correct error objects for invalid input, persistence that survives across requests, resolution of references and creation ids, and emission of the same push/notification events a real server sends. If a client cannot tell the difference, the feature is done; if it can, it is not.

## No Hardcoded or Default Usernames in Application Code Rule
Application and backend code MUST NOT contain hardcoded or default usernames (such as `user@example.com` or `"default"` account fallbacks). Account context, user subjects, and account IDs MUST ALWAYS be extracted dynamically from the request context or authentication headers. Standard fixed test accounts or sample seed users are permitted ONLY within test suites (`*_test.go`, Playwright e2e test files) or explicit server seed functions.

## RFC Validation & RFC 2119 Requirement Implementation Rule
All features, data model projections, protocol mappers, payload transformations, and server/client behaviors MUST be strictly validated against official IETF RFC standards.

Requirement selection and compliance boundaries are governed by [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119.html) and [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174.html) key words (`MUST`, `MUST NOT`, `REQUIRED`, `SHALL`, `SHALL NOT`, `SHOULD`, `SHOULD NOT`, `RECOMMENDED`, `MAY`, and `OPTIONAL`). 

Every requirement defined across all referenced RFC specifications—including all standard `MUST` clauses, `SHOULD` provisions, and optional `MAY` clauses—MUST be fully implemented and covered by dedicated unit test suites, unless a specific specification or feature is explicitly designated under the **Non-Goals & Out-of-Scope Specifications** section.

## Optional (MAY) Provision Implementation & Testing Requirement
All optional features, optional properties, optional query parameters, and `MAY` provisions defined across official IETF RFC specifications MUST be fully implemented and covered by dedicated unit tests. Do not limit implementation or testing solely to `MUST` or `SHOULD` requirements—every standard's `MAY` clauses MUST be verified to ensure maximum client interoperability, unless a feature is explicitly marked as a non-goal.

## Requirement Traceability & RFC 2119 Coverage Rule
Clause-level test presence is necessary but **not sufficient** — a requirement can have a test while an entire valid input *representation* of its types goes untested (this is exactly how the `CalendarEvent/query` `before`/`after` `LocalDateTime` bug shipped: a test existed, but it only used a `Z`-suffixed `UTCDate` value). To make spec gaps visible and gated:

1. **Requirement-traceability matrix (source of truth).** Every normative clause worked on MUST have a row in a `docs/conformance/<spec>.json` matrix: `spec` (e.g. `draft-ietf-jmap-calendars-27`, `RFC8984`, `RFC8620`), `section`, RFC 2119 `level` (`MUST`/`MUST NOT`/`SHOULD`/`SHOULD NOT`/`MAY`/`RECOMMENDED`/`OPTIONAL`), the verbatim requirement `text`, the `tests` that cover it, and a `status` of `covered`, `gap`, or `non-goal` (a `gap`/`non-goal` row carries no tests and is an explicit, reviewed acknowledgement). The `TestSpecCoverage` checker gates this: it fails on dangling test references, `covered` rows without tests, duplicate clauses, malformed levels/statuses, and out-of-order rows, and it reports every outstanding `MUST` gap. Add matrices to `conformanceMatrices` as new specs are covered.
2. **Cite the clause in the test.** Requirement tests MUST call `spectest.Require(t, spec, section, level, text)` for each clause they exercise, so every test is self-documenting and coverage is reportable via `go test -v`.
3. **Cover the type domain, not an example value.** For every field, derive inputs from the *type grammar* and cover each representation and boundary it admits — e.g. `LocalDateTime` as floating / date-only / zoned / DST-edge; `UTCDate` with `Z`; `Id[]` empty/duplicate; PatchObject null-to-remove; defaults (e.g. `timeZone` = `Etc/UTC`); and values at and beyond advertised capability limits (`minDateTime`/`maxDateTime`/`maxExpandedQueryDuration`). Positive **and** negative for each.
4. **Real-client interop is a gate, not a bonus.** Behaviors a real client depends on MUST be exercised by the Bulwark end-to-end suite (`e2e/`), and the exact request payloads real clients send SHOULD be captured as fixtures so their representations become permanent regressions. Synthetic tests written from the same reading as the code share the code's blind spots; the real client does not.

`TestSpecCoverage` verifies structure (existence, sortedness, MUST-has-a-test); it cannot judge whether a test exercises a clause *correctly* or across every representation — rules 3 and 4 and honest `gap` rows are what close that.

## Complete Case Coverage Rule
Every case, branch, condition, edge case, error path, and behavioral variation **mentioned anywhere in the referenced RFCs** MUST be implemented and covered by a dedicated test — not just the primary happy path of each method. This includes (non-exhaustively): each named error type (`notFound`, `invalidProperties`, `invalidArguments`, `stateMismatch`, `forbidden`, `overQuota`, `tooLarge`, `rateLimit`, `cannotCalculateChanges`, `singleton`, etc.), `notCreated`/`notUpdated`/`notDestroyed` outcomes, `ifInState` state-mismatch handling, partial success within a batch, empty/absent argument handling, `sinceState` older than the retained history (`hasMoreChanges`/`cannotCalculateChanges`), pagination boundaries (`position`, `limit`, negative/overflowing positions, `anchor`/`anchorOffset`), sort/collation variations, reference resolution (result references and creation ids), and every enumerated value a property may take. If an RFC mentions a case, there MUST be code that handles it and a test that exercises it.

## Comprehensive Property & Filter Condition Test Coverage Rule
When implementing or modifying query methods (`*/query`, `*/queryChanges`), search filters (`FilterCondition`), data model properties, or protocol mappers:
1. **Property-by-Property Test Coverage**: Every single property defined in a filter condition (e.g. `body`, `cc`, `bcc`, `hasKeyword`, `notKeyword`, `header`, `text`, `inMailbox`, `inMailboxOtherThan`, `before`, `after`, `minSize`, `maxSize`, `hasAttachment`) MUST have dedicated unit tests verifying BOTH positive matching (returning matching items) AND negative filtering (excluding non-matching items).
2. **No Fallthrough Match Defaults**: Filter evaluation functions MUST explicitly test and validate every condition parameter rather than allowing unhandled properties to silently return `true`.
3. **Advertised Capability Backend Registration**: All server initialization paths (including `main.go` and server factory functions) MUST register handlers and backend instances for all advertised session capabilities so client requests to advertised endpoints never return `Unknown method`.

## Memory Backend & Functional Feature Test Coverage Rule
Every feature MUST be implemented against a backend interface and wired into the in-memory backend (`jmap/memory/`), and its tests MUST exercise that backend to verify the feature's actual behavior—not merely its protocol surface.
1. **Back Every Feature With an Interface + Memory Implementation**: A feature is not complete when its handlers only return hardcoded or empty responses. Define a backend interface (e.g. `FileNodeBackend`) alongside the existing ones in `jmap/backend.go`, provide a concrete in-memory implementation in `jmap/memory/` with a compile-time assertion (`var _ jmap.XxxBackend = (*MemoryXxxBackend)(nil)`), and register it on all server initialization paths.
2. **Tests MUST Use the Memory Backend**: Unit and integration tests MUST drive requests through the real in-memory backend (via the standard test server), never against no-op stubs that ignore storage.
3. **Test the Feature, Not Just the Wiring**: Verifying that a capability is advertised and that method calls return responses with the expected names is necessary but NOT sufficient. Tests MUST prove the feature works end-to-end—for every stored object, perform a round-trip such as `set` (create) → `get`/`query` (retrieve) → assert the returned properties, then `set` (update) and `set` (destroy), asserting state changes, `notCreated`/`notUpdated`/`notDestroyed`, and change-tracking (`changes`/`queryChanges`) results. Never assert only on method names or response shape while ignoring the payload contents.
4. **Memory Backend Is the Reference for the Real Backend**: The in-memory backend is the canonical, complete reference implementation of every feature. A production/real backend will be built later and MUST be guided by the memory backend's behavior—same interfaces, same semantics, same state/change/error handling. Keep the memory backend correct and complete enough that porting it to a real datastore is a mechanical exercise, not a redesign.

## Partial Requests & Partial Updates Rule
Always support and use **partial requests and partial updates** wherever the protocol allows it—this is how a real server behaves and clients depend on it.
1. **Partial updates (patches)**: `*/set` `update` MUST apply a partial patch that changes only the addressed properties (including JSON-pointer patch paths like `keywords/$flag` or `parentId`) and leaves every unaddressed property untouched. Never require or assume the client resends the whole object. (See also *Data-Loss Prevention on Update & Merge*.)
2. **Partial fetches**: `*/get` MUST honor the `properties` argument, returning only the requested properties (plus the always-required `id`), and MUST accept a `null`/absent `ids` to mean "all". `*/query` MUST honor `position`, `limit`, `anchor`, and `filter`/`sort` partial specifications.
3. **Partial success**: A batch `set` with a mix of valid and invalid items MUST apply the valid ones and report the rest in `notCreated`/`notUpdated`/`notDestroyed`—never fail the whole batch for one bad item (unless `ifInState` mandates atomicity).

## Creation References & Virtual Temporary IDs Rule
The server MUST support **creation references ("virtual temporary ids", RFC 8620 Section 5.3)** so composite updates work in a single round trip. Within one `/set`, any field that takes an Id (and update keys / destroy ids) MUST accept a `#creationId` placeholder that resolves to the real id the server assigns to an object created in the same call. Implementations MUST handle forward references (a child listed before its parent) by deferring until dependencies resolve, and MUST reject missing or cyclic references via `notCreated`. Back-references across method calls (result references, RFC 8620 Section 3.3) MUST likewise resolve. Every feature that creates linked objects MUST have a test that creates a parent and a child referencing it via `#creationId` in one request.

## Push / State-Change Event Rule
JMAP push is defined in **RFC 8620 Section 7** (`StateChange` object and the `EventSource`/SSE resource in §7.1; `PushSubscription` web-push in §7.2, which builds on RFC 8030 Web Push, RFC 8291 encryption, and RFC 9749 VAPID), with WebSocket push defined in **RFC 8887 Section 5**. Every data mutation MUST emit a `StateChange` event for the affected type on the account so that any subscribed UI updates itself, exactly as a real server would. Wire each memory backend to the `Broadcaster` (via `SetBroadcaster`) on all initialization paths, and publish the new state token on every create/update/destroy. Every feature MUST have a test that connects to `/eventsource` (or the WebSocket), triggers a mutation, and asserts a `StateChange` naming the correct type and account is delivered.

## Data-Loss Prevention on Update & Merge
When implementing updates, patches, or merges of any stored object (mails, mailboxes, contacts/cards, calendars/events, sieve scripts, blobs, submissions), treat user data as sacrosanct: a partial or malformed patch MUST fail the whole operation (or only that record) rather than silently dropping, overwriting, or zeroing fields that were not explicitly addressed. Never replace an entire object with a server- or client-supplied default, never fabricate values for data that was not provided, and never ignore or truncate existing properties during a merge. Destructive mutations (deletes, moves, role/default changes, privilege changes, token invalidation) MUST be rejected with an error when the target does not exist or cannot be safely located, and MUST never be silently no-oped while pretending success. Preserve server-set fields (created/updated timestamps, IDs) and, where the RFC mandates it, record and return oldState/newState plus notCreated/notUpdated/notDestroyed information so clients can reconcile. When in doubt, choose the operation that preserves data over the one that discards it, and add a regression test proving the previously-missing field survives an update.

## Standard Parsers & Encoders Only — Never Ad-Hoc Parsing
Wire formats and structured, potentially-untrusted input (MIME messages, MIME parts and their
`Content-Transfer-Encoding`, iCalendar/vCard, JSON, JMAP request envelopes, email addresses and
headers, dates/times, URIs, base64/quoted-printable, etc.) MUST be handled with a proper,
standards-conformant parser/encoder — the format's real grammar — **never** with ad-hoc
`strings.Index` / `strings.Split` / substring / regex scanning chosen "because it is easier."

1. **This is a security requirement, not a style preference.** Ad-hoc scanning is a primary source
   of vulnerabilities: **parser differentials** (the security check and the consuming/display
   parser disagree about where a part begins/ends, enabling spoofing and smuggling — e.g. splicing
   across MIME boundaries with `strings.Index("BEGIN:VCALENDAR")` / `LastIndex("END:VCALENDAR")`),
   **transfer-encoding bypass** (missing base64/quoted-printable decoding so malicious content is
   seen differently by different readers), header/CRLF injection, and denial-of-service on
   malformed or deeply-nested input. Treat all such input as hostile.
2. **Use the standard library or the already-vendored parser.** Prefer Go's `net/mail`, `mime`,
   `mime/multipart`, `mime/quotedprintable`, `encoding/base64`, `net/url`, `time`, `encoding/json`,
   and the repository's existing `github.com/emersion/go-message` / `go-ical` dependencies. Do not
   add a second, hand-rolled path for a format a real parser already covers, and do not let the
   security-relevant view of the bytes diverge from the view a client would take.
3. **Extract, then interpret.** Locate a sub-document with the format's own structure (walk MIME
   parts to the `text/calendar` part and decode its CTE) and only then hand the *decoded, isolated*
   bytes to the format parser. Never run a format parser over a whole raw message and hope it finds
   the right span.
4. **Fail closed.** When input cannot be parsed by the real parser, reject or ignore it — do not
   fall back to a lenient ad-hoc scan that accepts what the strict parser refused, and never mutate
   stored state from input a standards parser could not validate. Every parser added or touched
   MUST have tests covering malformed, encoded (base64/quoted-printable), multipart, and adversarial
   inputs, not just the happy path.

"It was easier / faster to write" is never a justification for hand-rolling a parser for a
standardized format. If a suitable parser is genuinely missing, add the dependency or write a real
one with a grammar and tests — do not scan.

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
- **Web Push Protocol**: [RFC 8030](https://www.rfc-editor.org/rfc/rfc8030.html) — *Generic Event Delivery Using HTTP Push* (transport for JMAP PushSubscription per RFC 8620 §7.2)
- **Web Push Message Encryption**: [RFC 8291](https://www.rfc-editor.org/rfc/rfc8291.html) — *Message Encryption for Web Push*
- **JMAP Keywords & Attributes**: [RFC 9979](https://www.rfc-editor.org/rfc/rfc9979.html) — *IMAP/JMAP Keywords and Mailbox Name Attributes*
- **Sieve Language**: [RFC 5228](https://www.rfc-editor.org/rfc/rfc5228.html) — *Sieve: An Email Filtering Language*

#### JMAP Internet-Drafts (work in progress — verified current 2026-08-08)
These JMAP extensions have **not** been published as RFCs yet; cite the latest draft revision.
- **JMAP for Calendars**: [draft-ietf-jmap-calendars-27](https://datatracker.ietf.org/doc/html/draft-ietf-jmap-calendars-27) — *JSON Meta Application Protocol (JMAP) for Calendars* (defines the `Calendar`, `CalendarEvent`, `CalendarEventNotification`, and `ParticipantIdentity` types and methods, plus the `urn:ietf:params:jmap:calendars` / `:calendars:parse` capabilities). Per repo convention its method tests are filed under `rfc8984_*_test.go`.
- **JMAP for Principals & Availability**: [draft-ietf-jmap-principals](https://datatracker.ietf.org/doc/html/draft-ietf-jmap-principals) — *JMAP for Principals* (`urn:ietf:params:jmap:principals`, `:principals:availability`, `:principals:owner`).

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

---

## Running External Conformance Test Suites (JMAP-TestSuite)

In addition to the internal Go unit tests and Playwright E2E suites, the server can be verified against the official Fastmail Perl test suite located in `~/git/fastmail/JMAP-TestSuite`.

### 1. Start the JMAP Server
Ensure the Go server is running locally (e.g., on port `8181`):
```bash
cd ~/git/imap-jmap
go run . -port 8181 -https-port 8444 -smtp-port 1026
```

### 2. Configure the Server Adapter
In `~/git/fastmail/JMAP-TestSuite`, the `ImapJmap` server adapter (`lib/JMAP/TestSuite/ServerAdapter/ImapJmap.pm`) connects to the running server using `imap-jmap.json`:
```json
{
  "adapter": "ImapJmap",
  "base_uri": "http://localhost:8181",
  "credentials": [{
    "username": "user@example.com",
    "password": "user@example.com"
  }]
}
```

### 3. Run the Tests
From `~/git/fastmail/JMAP-TestSuite`:
> **Note on Prerequisites**: Only run `cpanm --installdeps .` (or `cpanm -l ~/perl5 --installdeps .`) if dependencies are missing or required. If dependencies are already installed, skip this step as dependency installation takes a long time.

```bash
cd ~/git/fastmail/JMAP-TestSuite

# Run a single test file (verbose)
JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lv t/basic.t

# Run a specific test subsystem
JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lr t/Mailbox/
JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lr t/Email/

# Run with full JMAP request/response telemetry logged to STDERR
JMTS_TELEMETRY=1 JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lv t/basic.t

# Run over WebSockets transport (RFC 8887)
JMTS_USE_WEBSOCKETS=1 JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lv t/basic.t
```

