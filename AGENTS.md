# Agent Guidelines & Project Rules

## 1. Core Architectural Principles

### Guiding Principle: Indistinguishable From a Real Server
The overriding goal of this project is that **a client MUST NOT be able to determine that there is not a real, full-featured server behind the protocol.** Every endpoint, method, capability, error, and event MUST behave exactly as a production-grade server would. When a choice must be made, choose the behavior a real server exhibits:
- No hardcoded or empty responses standing in for real logic.
- Correct state and change tracking (`state`, `sinceState`, `hasMoreChanges`, `cannotCalculateChanges`).
- Precise error objects (`invalidProperties`, `notFound`, `forbidden`, `stateMismatch`, etc.) for invalid input.
- Real persistence across requests, full resolution of back-references and `#creationId` placeholders.
- Real-time emission of push/notification events across SSE and WebSocket connections.

### Zero Hardcoded or Default Accounts
Application and backend code MUST NOT contain hardcoded or default usernames (such as `user@example.com` or `"default"` account fallbacks). Account context, user subjects, and account IDs MUST ALWAYS be extracted dynamically from request context or authentication headers. Standard fixed test accounts or sample seed users are permitted ONLY within test suites (`*_test.go`, Playwright e2e test files) or explicit server seed functions.

### Zero Admin Passwords or Elevated Privileges
In production environments, `imap-jmap` runs without administrative access, and admin passwords are not available. Code MUST NOT use, expect, or rely on admin passwords, admin usernames, or privileged admin endpoints at any time:
- All upstream protocols (IMAP, SMTP, WebDAV, CalDAV, CardDAV, ManageSieve) MUST authenticate strictly using the end-user's credentials provided in the request context or authentication headers.
- Never use admin credentials for WebDAV file access, CalDAV calendars, CardDAV address books, user provisioning, or discovery.
- The server MUST operate fully and gracefully when no admin credentials exist. Do not rely on `NEXTCLOUD_ADMIN_USER` or `NEXTCLOUD_ADMIN_PASSWORD` in any production codepaths.

### Fully Stateless & Zero Local Filesystem Sync
`imap-jmap` is a fully stateless proxy and protocol translation server. Upstream data (mail, files, calendars, contacts, blobs) MUST NOT be synced or mirrored to the local host filesystem, and persistent local caches on disk are strictly prohibited:
- All reads and writes MUST go directly to upstream wire servers (IMAP, SMTP, WebDAV, CalDAV, CardDAV, ManageSieve) on demand.
- Directory and collection listings MUST NOT download or cache full file/message bodies in memory or on disk. Metadata must be retrieved on demand and payload bodies streamed only when explicitly requested.

---

## 2. Layering, Module Boundaries & Backend Encapsulation

### Strict Layering & Upstream Protocol Abstraction
Individual JMAP modules and domain backends MUST maintain strict layer separation. Upstream wire protocols (CalDAV, CardDAV, WebDAV, IMAP, SMTP, ManageSieve) MUST be completely encapsulated within their respective adapter client layers (`jmap/nextcloud/client.go`, `jmap/imapsmtp`) and MUST NEVER leak into domain logic:
1. **Domain Modules Must Not Assume or Import Upstream Protocols**: High-level domain packages (`jmap/jmapcalendar`, `jmap/jmapcontacts`, `jmap/calendar_handlers.go`, and backend adapters `jmap/nextcloud/calendars.go`, `jmap/nextcloud/contacts.go`) MUST NOT import upstream protocol packages (`caldav`, `carddav`), construct or parse wire paths (e.g. `/remote.php/dav/...`), manipulate file extensions (`.ics`, `.vcf`), or invoke raw protocol methods (`client.CalDAV()`, `client.CardDAV()`). They must operate strictly on domain models (`*Calendar`, `*CalendarEvent`, `*Card`, `*AddressBook`) and high-level storage operations (`ListCalendars`, `CreateCalendar`, `DeleteCalendar`, `QueryCalendarObjects`, `PutCalendarObject`, `DeleteCalendarObject`).
2. **Protocol Mechanics Encapsulated in Backend Clients**: The backend client (`*Client` in `jmap/nextcloud/client.go`) is the sole component permitted to speak to upstream protocol endpoints, map domain IDs to wire paths (e.g. appending/stripping `.ics`), discover collection URLs, and execute protocol calls via standard client libraries. Protocol clients MUST NOT be leaked to outer modules.
3. **Zero Heuristics Across Module Boundaries**: Code MUST NOT guess default collections, personal calendars, or address books by name (e.g., assuming `"personal"`, `"Personal Calendar"`, or `"Default"`). Upstream protocol standards (e.g. RFC 6638 §9.2.1 `schedule-default-calendar-URL` for CalDAV default calendar discovery) MUST be used. Code MUST NOT make assumptions about username formats (no splitting on `@` to guess local parts or splitting on `.` to guess names).
4. **Hermetic Test Backends Must Provide Complete Protocol Fulfillment**: Embedded reference adapters used for in-process testing (`jmap/nextcloud/embedded.go` with `memCalDAVBackend`, `memCardDAVBackend`) MUST implement separate, complete protocol fulfillment for any new operations or standards-based discovery mechanisms (e.g. RFC 6638 principal PROPFIND). Tests and production MUST exercise the exact same discovery pathways without bypasses or fallback stubs.

### Reference Backend Implementations
1. **IMAP/SMTP Reference Backend (`imapsmtp`)**: The legacy in-memory backend (`jmap/memory/`) has been completely retired. `jmap/imapsmtp` is the primary and sole reference backend for Mail (`Email`, `Mailbox`, `Thread`), Blob, Submission, Identity, VacationResponse, PushSubscription, and Quota functionality, with compile-time assertions verifying interface fulfillment.
2. **Production Reference Adapters for Non-Mail Domains**: Non-mail domains use embedded production reference adapters (`jmap/nextcloud` for CalDAV/CardDAV/WebDAV calendars, contacts/cards, principals, and filenodes; `jmap/managesieve` for Sieve scripts; dedicated stores for auth and imapaccess). Mock stores (`jmap/testmock/`) have been completely retired.
3. **Hermetic In-Process Testing**: Unit and integration tests MUST drive requests through embedded in-process adapters (`imapsmtp.NewEmbeddedBackend`, `nextcloud.NewEmbeddedBackend`, `managesieve.NewEmbeddedBackend`) with zero external daemon or Docker requirements.
4. **Test the Feature, Not Just the Wiring**: Tests MUST prove features work end-to-end via round-trips (`set` create → `get`/`query` retrieve → assert properties → `set` update → `set` destroy → assert change tracking). Never assert only on response shape or method names while ignoring payload contents.

---

## 3. Standard Parsers & Zero Custom Serialization

### Standard Parsers & Canonical Libraries Only
**Never accept or introduce custom serializers, deserializers, string formatters, or parsers when an established, battle-tested standard library exists.** Hand-rolled formatting (e.g. manual `fmt.Fprintf`/`strings.Builder` text assembly for iCalendar, vCard, MIME, HTTP, Sieve, XML, JSON) or ad-hoc string parsing (`strings.Split`, `strings.Index`, regex) is strictly prohibited. Hand-rolled implementations invariably introduce protocol incompatibilities, line-folding bugs, escaping/injection vulnerabilities, and timezone/DST edge cases.

1. **Security & Reliability Requirement**: Ad-hoc scanning causes **parser differentials** (security checks disagree with display parsers, enabling spoofing/smuggling), **transfer-encoding bypasses** (missing base64/quoted-printable decoding), CRLF injection, and DoS.
2. **Standard & Canonical Libraries**: Always use Go's `net/mail`, `mime`, `mime/multipart`, `mime/quotedprintable`, `encoding/base64`, `encoding/xml`, `net/url`, `time`, `encoding/json`, and repository dependencies (`github.com/emersion/go-message`, `github.com/emersion/go-ical`, `github.com/emersion/go-vcard`, `github.com/emersion/go-webdav`, `github.com/teambition/rrule-go`, `github.com/foxcpp/go-sieve`).
3. **Extract, Then Interpret**: Locate a sub-document with the format's own grammar (walk MIME parts to `text/calendar` and decode CTE) and only then hand the decoded, isolated bytes to the format parser. Never run a format parser over an unparsed raw message.
4. **Fail Closed**: When input cannot be parsed by the standards parser, reject or ignore it—do not fall back to lenient scanning, and never mutate stored state from unvalidated input.

---

## 4. State Mutations & Protocol Invariants

### Partial Requests & Partial Updates
1. **Partial Updates (Patches)**: `*/set` `update` MUST apply a partial patch modifying only explicitly addressed properties (including JSON-pointer paths like `keywords/$flag` or `parentId`) and leaving unaddressed properties untouched. Never require clients to resend the whole object.
2. **Partial Fetches**: `*/get` MUST honor the `properties` argument, returning only requested properties (plus the mandatory `id`), and MUST treat null/absent `ids` as "all". `*/query` MUST honor `position`, `limit`, `anchor`, `filter`, and `sort`.
3. **Partial Success**: Batch `set` operations with mixed valid and invalid items MUST apply valid items and return per-item errors in `notCreated`/`notUpdated`/`notDestroyed`—never fail an entire batch for one bad item (unless `ifInState` mandates atomicity).

### Creation References & Virtual Temporary IDs
The server MUST support **creation references (RFC 8620 §5.3)**. Within one `/set`, any field taking an `Id` (plus update keys and destroy ids) MUST accept a `#creationId` placeholder resolving to the assigned ID of an object created in the same call. Implementations MUST handle forward references by deferring execution until dependencies resolve, and reject missing or cyclic references via `notCreated`. Back-references across method calls (result references, RFC 8620 §3.3) MUST likewise resolve.

### Push & State-Change Events
JMAP push is defined in **RFC 8620 §7** (`StateChange`, `/eventsource`), **RFC 8887 §5** (WebSockets), and **RFC 8620 §7.2 / RFC 9749** (Web Push / VAPID). Every data mutation MUST emit a `StateChange` event naming the affected type and account so subscribed clients update immediately. Wire backends to `jmappush.Broadcaster` (via `SetBroadcaster`) on all paths, and emit state tokens on every create/update/destroy.

### Data-Loss Prevention on Update & Merge
User data is sacrosanct: a partial or malformed patch MUST fail that record rather than silently dropping, overwriting, or zeroing unaddressed fields. Never replace an object with a default, never fabricate missing values, and never ignore or truncate existing properties during a merge. Destructive operations on nonexistent targets MUST return errors, never silent success.

---

## 5. RFC Standards Compliance, Coverage & Traceability

### RFC 2119 / 8174 Compliance & Complete Case Coverage
Implementation and testing boundaries are governed by RFC 2119 and RFC 8174 key words (`MUST`, `MUST NOT`, `REQUIRED`, `SHALL`, `SHALL NOT`, `SHOULD`, `SHOULD NOT`, `RECOMMENDED`, `MAY`, `OPTIONAL`):
1. **Mandatory & Optional Provisions**: Every requirement—including all `MUST` clauses, `SHOULD` provisions, and optional `MAY` clauses—MUST be fully implemented and verified by unit tests unless explicitly excluded under Non-Goals.
2. **Complete Case Coverage**: Every condition, edge case, error variant (`notFound`, `invalidProperties`, `invalidArguments`, `stateMismatch`, `forbidden`, `overQuota`, `tooLarge`, `rateLimit`, `cannotCalculateChanges`, `singleton`, etc.), and pagination boundary (`position`, `limit`, `anchor`, negative offsets) mentioned in referenced RFCs MUST be implemented and tested.
3. **Filter Condition Coverage**: Every property defined in a filter condition (e.g. `body`, `cc`, `bcc`, `hasKeyword`, `notKeyword`, `header`, `text`, `inMailbox`, `before`, `after`, `minSize`, `hasAttachment`) MUST have tests verifying BOTH positive matching and negative filtering. Filter evaluation MUST NOT fall through to permissive defaults.
4. **Advertised Capabilities Registered**: Server initialization MUST register handlers and backend instances for all advertised session capabilities so requests to advertised endpoints never return "Unknown method".

### Requirement Traceability & Type Grammar Coverage
1. **Traceability Matrix (`spec.Matrices`)**: Every normative clause worked on MUST be registered in the Go-native `spec/` package (`Spec`, `Section`, `Level`, `Text`, `Tests`, `Status`). The `TestSpecCoverage` checker gates this: it fails on dangling test references, covered rows without tests, malformed entries, or unreviewed gaps. Keep `docs/SPEC_TRACEABILITY_REPORT.md` and `docs/SPEC_COVERAGE.md` updated (`UPDATE_DOCS=1 go test ./spec`).
2. **Self-Documenting Tests**: Tests MUST call `spectest.Require(t, spec, section, level, text)` or `spectest.Cover(t, clause)` for each clause exercised.
3. **Type Domain Coverage**: Cover the type grammar's full input domain, not just single example values (e.g. `LocalDateTime` floating / date-only / zoned / DST-edge; `UTCDate` with `Z`; empty vs duplicate `Id[]`; `null`-to-remove patches; advertised capability limits).
4. **Real-Client End-to-End Gate**: Client workflows MUST be validated via the Bulwark Playwright e2e suite (`e2e/`), capturing real-client payloads as regression fixtures.

---

## 6. Official Specification References & Non-Goals

### Standards References
- **Requirement Levels**: [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119.html), [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174.html)
- **Core JMAP**: [RFC 8620](https://www.rfc-editor.org/rfc/rfc8620.html) (Core), [RFC 8621](https://www.rfc-editor.org/rfc/rfc8621.html) (Mail), [RFC 8887](https://www.rfc-editor.org/rfc/rfc8887.html) (WebSockets), [RFC 9007](https://www.rfc-editor.org/rfc/rfc9007.html) (MDN), [RFC 9219](https://www.rfc-editor.org/rfc/rfc9219.html) (S/MIME), [RFC 9404](https://www.rfc-editor.org/rfc/rfc9404.html) (Blob Management), [RFC 9425](https://www.rfc-editor.org/rfc/rfc9425.html) (Quotas), [RFC 9610](https://www.rfc-editor.org/rfc/rfc9610.html) (Contacts), [RFC 9661](https://www.rfc-editor.org/rfc/rfc9661.html) (Sieve), [RFC 9670](https://www.rfc-editor.org/rfc/rfc9670.html) (Sharing), [RFC 9698](https://www.rfc-editor.org/rfc/rfc9698.html) (JMAPACCESS IMAP), [RFC 9749](https://www.rfc-editor.org/rfc/rfc9749.html) (Push VAPID), [RFC 9979](https://www.rfc-editor.org/rfc/rfc9979.html) (Keywords & Mailbox Attributes)
- **Web Push**: [RFC 8030](https://www.rfc-editor.org/rfc/rfc8030.html) (HTTP Push), [RFC 8291](https://www.rfc-editor.org/rfc/rfc8291.html) (Message Encryption), [RFC 8188](https://www.rfc-editor.org/rfc/rfc8188.html) (Encrypted Content-Encoding), [RFC 8292](https://www.rfc-editor.org/rfc/rfc8292.html) (VAPID), [RFC 6750](https://www.rfc-editor.org/rfc/rfc6750.html) (Bearer Tokens)
- **JMAP Drafts**: [draft-ietf-jmap-calendars-27](https://datatracker.ietf.org/doc/html/draft-ietf-jmap-calendars-27) (Calendars), [draft-ietf-jmap-principals](https://datatracker.ietf.org/doc/html/draft-ietf-jmap-principals) (Principals & Availability)
- **Data Formats**: [RFC 8984](https://www.rfc-editor.org/rfc/rfc8984.html) (JSCalendar), [RFC 9553](https://www.rfc-editor.org/rfc/rfc9553.html) (JSContact), [RFC 9554](https://www.rfc-editor.org/rfc/rfc9554.html) (JSContact vCard Extensions), [RFC 9555](https://www.rfc-editor.org/rfc/rfc9555.html) (JSContact-vCard Conversion), [RFC 6901](https://www.rfc-editor.org/rfc/rfc6901.html) (JSON Pointer), [RFC 3339](https://www.rfc-editor.org/rfc/rfc3339.html) (Timestamps)
- **CalDAV & CardDAV**: [RFC 4918](https://www.rfc-editor.org/rfc/rfc4918.html) (WebDAV), [RFC 3744](https://www.rfc-editor.org/rfc/rfc3744.html) (WebDAV ACL), [RFC 4791](https://www.rfc-editor.org/rfc/rfc4791.html) (CalDAV), [RFC 6638](https://www.rfc-editor.org/rfc/rfc6638.html) (CalDAV Scheduling), [RFC 6352](https://www.rfc-editor.org/rfc/rfc6352.html) (CardDAV), [RFC 5545](https://www.rfc-editor.org/rfc/rfc5545.html) (iCalendar), [RFC 5546](https://www.rfc-editor.org/rfc/rfc5546.html) (iTIP), [RFC 6047](https://www.rfc-editor.org/rfc/rfc6047.html) (iMIP), [RFC 6350](https://www.rfc-editor.org/rfc/rfc6350.html) (vCard 4.0), [RFC 2426](https://www.rfc-editor.org/rfc/rfc2426.html) (vCard 3.0), [RFC 6868](https://www.rfc-editor.org/rfc/rfc6868.html) (Caret Encoding)
- **Sieve & ManageSieve**: [RFC 5228](https://www.rfc-editor.org/rfc/rfc5228.html) (Sieve), [RFC 5230](https://www.rfc-editor.org/rfc/rfc5230.html) (Vacation), [RFC 5232](https://www.rfc-editor.org/rfc/rfc5232.html) (Imap4flags), [RFC 5429](https://www.rfc-editor.org/rfc/rfc5429.html) (Reject), [RFC 5804](https://www.rfc-editor.org/rfc/rfc5804.html) (ManageSieve)
- **IMAP & MIME**: [RFC 3501](https://www.rfc-editor.org/rfc/rfc3501.html) (IMAP4rev1), [RFC 9051](https://www.rfc-editor.org/rfc/rfc9051.html) (IMAP4rev2), [RFC 4314](https://www.rfc-editor.org/rfc/rfc4314.html) (ACL), [RFC 5256](https://www.rfc-editor.org/rfc/rfc5256.html) (SORT/THREAD), [RFC 7162](https://www.rfc-editor.org/rfc/rfc7162.html) (CONDSTORE/QRESYNC), [RFC 2177](https://www.rfc-editor.org/rfc/rfc2177.html) (IDLE), [RFC 6851](https://www.rfc-editor.org/rfc/rfc6851.html) (MOVE), [RFC 6154](https://www.rfc-editor.org/rfc/rfc6154.html) (SPECIAL-USE), [RFC 4315](https://www.rfc-editor.org/rfc/rfc4315.html) (UIDPLUS), [RFC 5788](https://www.rfc-editor.org/rfc/rfc5788.html) (Keywords), [RFC 5322](https://www.rfc-editor.org/rfc/rfc5322.html) (Internet Message Format), [RFC 2045](https://www.rfc-editor.org/rfc/rfc2045.html)–[2047](https://www.rfc-editor.org/rfc/rfc2047.html) (MIME), [RFC 3834](https://www.rfc-editor.org/rfc/rfc3834.html) (Auto-Responses), [RFC 8098](https://www.rfc-editor.org/rfc/rfc8098.html) (MDN)
- **SMTP & Transport**: [RFC 5321](https://www.rfc-editor.org/rfc/rfc5321.html) (SMTP), [RFC 6409](https://www.rfc-editor.org/rfc/rfc6409.html) (Submission), [RFC 4954](https://www.rfc-editor.org/rfc/rfc4954.html) (AUTH), [RFC 3207](https://www.rfc-editor.org/rfc/rfc3207.html) (STARTTLS), [RFC 1870](https://www.rfc-editor.org/rfc/rfc1870.html) (SIZE), [RFC 3461](https://www.rfc-editor.org/rfc/rfc3461.html) (DSN), [RFC 3463](https://www.rfc-editor.org/rfc/rfc3463.html) (Status Codes), [RFC 6531](https://www.rfc-editor.org/rfc/rfc6531.html) (SMTPUTF8), [RFC 7208](https://www.rfc-editor.org/rfc/rfc7208.html) (SPF), [RFC 6376](https://www.rfc-editor.org/rfc/rfc6376.html) (DKIM), [RFC 7489](https://www.rfc-editor.org/rfc/rfc7489.html) (DMARC), [RFC 1123](https://www.rfc-editor.org/rfc/rfc1123.html) (Host Requirements), [RFC 1035](https://www.rfc-editor.org/rfc/rfc1035.html) (DNS)

### Non-Goals & Out-of-Scope Specifications
- **CalDAV & CardDAV Server Endpoints**: The server is strictly a JMAP-native server. Serving CalDAV (RFC 4791, RFC 6638) or CardDAV (RFC 6352) HTTP endpoints is explicitly out-of-scope / non-goal. The server uses CalDAV/CardDAV client libraries only to bridge upstream calendar/contacts stores (e.g. Nextcloud) to JMAP.
- **Legacy XML Mail Auto-Configuration**: Legacy XML autodiscovery schemas (Mozilla AutoConfig `config-v1.1.xml` and Microsoft AutoDiscover `Autodiscover.xml`) designed for legacy IMAP/POP/SMTP/Exchange are out-of-scope. Discovery for this server is strictly JMAP-native, standardized via **RFC 8620 §2.1 Session Discovery (`/.well-known/jmap`)**, **RFC 8620 DNS SRV/TXT bootstrapping (`_jmaps._tcp`, `_jmap._tcp`)**, and **IETF PACC JSON autoconfiguration (`draft-ietf-mailmaint-pacc`, `/.well-known/user-agent-configuration.json`)**.

### External Conformance-Test Exceptions
- **Fastmail pristine-account mailbox tests**: `t/Mailbox/get/no-existing-entities.t` and `t/Mailbox/query/no-existing-entities.t` from Fastmail `JMAP-TestSuite` are intentionally ignored. They require a pristine account to have no non-Inbox mailboxes. This server MUST provision the standard role mailboxes (`sent`, `drafts`, `trash`, `junk`, and `archive`) for every account to support real-client compose, draft, send, archive, and delete workflows. Agents MUST NOT change account provisioning, hide these roles, or otherwise alter compliant server behavior to make either ignored upstream test pass. See `TESTING.md` for the exception list and rationale.

---

## 7. Running Test Suites & Conformance Verification
See [`TESTING.md`](./TESTING.md) for full instructions on running, configuring, and debugging:
- **Internal Go Unit & RFC Conformance Tests** (`go test ./...`, timeout rules, requirement traceability matrices).
- **Fastmail `JMAP-TestSuite`** (Core and Mail protocol conformance).
- **`jmapio/jmap-perl` Test Suite** (Core, Mail, Calendar, and Contact protocol tests).
- **Playwright End-to-End Suite** (`e2e/`).
