# Agent Guidelines & Project Rules

## 1. Core Architectural Principles
- **Real Server Behavior**: Indistinguishable from a production server. No stubs, mocks, or empty fallbacks. Precise state and change tracking (`state`, `sinceState`), standard JMAP error objects, full persistence, back-references, `#creationId` resolution, and real-time push events (SSE/WebSocket).
- **Zero Hardcoded Accounts**: Extract usernames, subjects, and account IDs dynamically from request context or auth headers. Never hardcode usernames (e.g. `user@example.com` or `"default"`). Standard seed users allowed only in tests.
- **Zero Admin Passwords or Elevated Privileges**: Authenticate against upstream servers (IMAP, SMTP, WebDAV, CalDAV, CardDAV, ManageSieve) strictly using end-user credentials. Never require or use admin passwords or endpoints in production codepaths.
- **Stateless Proxy & Zero Local Filesystem Sync**: Direct on-demand proxying to upstream wire servers. No local disk caches, sync databases, or host filesystem mirroring. Stream bodies only on explicit request.

## 2. Layering & Module Encapsulation
- **Protocol Encapsulation**: Upstream wire protocols (CalDAV, CardDAV, WebDAV, IMAP, SMTP, ManageSieve) must remain strictly encapsulated inside backend client layers (`jmap/nextcloud/client.go`, `jmap/imapsmtp`). Domain modules (`jmap/jmapcalendar`, `jmap/jmapcontacts`, `jmap/jmapmail`) must operate solely on domain models (`Calendar`, `Card`, `Email`) and high-level storage APIs. Never leak protocol clients, wire paths (`/remote.php/dav/...`), or file extensions (`.ics`, `.vcf`) into domain logic.
- **Zero Heuristics**: Do not guess default collections or address books by name (e.g. `"Personal"`, `"Default"`). Use protocol discovery standards (e.g. RFC 6638 §9.2.1 `schedule-default-calendar-URL`). Never guess email or username formats by splitting on `@` or `.`.
- **Hermetic In-Process Testing**: Unit and integration tests run against embedded in-process adapters (`imapsmtp.NewEmbeddedBackend`, `nextcloud.NewEmbeddedBackend`, `managesieve.NewEmbeddedBackend`) with zero external daemons or Docker. Test end-to-end round-trips (`set` create → `get`/`query` → `set` update → `set` destroy → changes), asserting payload contents, not just shapes.

## 3. Standard Parsers, Zero Custom Serialization & Robust Validation
- **Never Build Formats by String Concatenation**: **You MUST NOT construct XML, JSON, MIME, iCalendar, vCard, Sieve, HTTP, or ANY data format by string concatenation, `fmt.Fprintf`, `strings.Builder`, or manual template assembly when a standard or canonical library exists.** Always serialize using structured encoders (`encoding/xml`, `encoding/json`, `go-ical`, `go-vcard`, `go-message`). Hand-rolled string assembly causes line-folding bugs, escaping/injection vulnerabilities, and encoding mismatches.
- **Never Use Brittle Hand-Rolled Validation**: **Never implement ad-hoc or brittle validation using custom string scanning (`strings.Contains`, `strings.Index`, `strings.Split`) or hand-rolled regular expressions when an established parser or library is available.** Always validate formats (email addresses, MIME headers, dates, URIs, XML, JSON, iCalendar, vCard) by parsing them with standard libraries (e.g. `net/mail.ParseAddress`, `net/url.Parse`, `time.Parse`, standard format unmarshalers). Fail closed: if input fails standard parsing, reject it immediately (`invalidProperties`).
- **Standard Libraries Only**: Use Go stdlib (`encoding/xml`, `encoding/json`, `net/mail`, `mime`, `mime/multipart`, `mime/quotedprintable`, `encoding/base64`, `net/url`, `time`) and repository canonical dependencies (`github.com/emersion/go-message`, `github.com/emersion/go-ical`, `github.com/emersion/go-vcard`, `github.com/emersion/go-webdav`, `github.com/teambition/rrule-go`, `github.com/foxcpp/go-sieve`).
- **Extract, Then Interpret**: Locate sub-documents within their format grammar (e.g. walk MIME parts to `text/calendar`, decode transfer encoding), then pass isolated bytes to the domain parser. Never scan raw unstructured streams.

## 4. State Mutations & Protocol Invariants
- **Partial Updates & Fetches**: `*/set` update must apply partial patches (including JSON Pointer paths) without requiring full objects. `*/get` must honor `properties` (omitted/null `ids` means "all"). `*/query` must honor pagination (`position`, `limit`, `anchor`, `filter`, `sort`).
- **Partial Success & Creation References**: Apply valid items in batch `set` operations and report per-item errors in `notCreated`/`notUpdated`/`notDestroyed`. Support `#creationId` resolution (RFC 8620 §5.3) within and across method calls, resolving dependencies and deferring forward references.
- **Push Events**: Emit `StateChange` push events via `jmappush.Broadcaster` on every create/update/destroy across all paths (RFC 8620 §7, RFC 8887 §5, RFC 9749).
- **Data-Loss Prevention**: Never drop, overwrite, or zero unaddressed fields during a patch. Nonexistent targets or failed merges must return errors, never silent success.

## 5. RFC Compliance, Traceability & Testing
- **Complete RFC 2119 Coverage**: Fully implement and test every mandatory `MUST`/`MUST NOT` and `SHOULD` requirement across referenced specs. Cover all error variants, filter conditions, and pagination boundaries. Advertised session capabilities must always have registered handlers.
- **Requirement Traceability**: Register all normative clauses in `spec/` (`spec.Matrices`), verified by `TestSpecCoverage`. Tests must call `spectest.Require(t, spec, section, level, text)` or `spectest.Cover(t, clause)`. Keep `docs/SPEC_COVERAGE.md` and `docs/SPEC_TRACEABILITY_REPORT.md` updated (`UPDATE_DOCS=1 go test ./spec`).
- **Real-Client End-to-End Gate**: Validate workflows with the Bulwark Playwright test suite (`e2e/`).

## 6. Scope Boundaries & Test Exceptions
- **Non-Goals**: Serving CalDAV (RFC 4791) or CardDAV (RFC 6352) server endpoints is explicitly out-of-scope (JMAP only). Legacy XML autodiscovery schemas (Mozilla AutoConfig, Microsoft AutoDiscover) are out-of-scope; discovery is strictly JMAP-native (RFC 8620 §2.1 `/.well-known/jmap`, DNS SRV, IETF PACC).
- **Fastmail Conformance Exceptions**: Fastmail tests requiring pristine accounts to have no non-Inbox mailboxes (`t/Mailbox/get/no-existing-entities.t`, `t/Mailbox/query/no-existing-entities.t`) are ignored. Standard role mailboxes (`sent`, `drafts`, `trash`, `junk`, `archive`) must always be provisioned.
- **Test Instructions**: See [`TESTING.md`](./TESTING.md) for running internal Go tests, Fastmail `JMAP-TestSuite`, `jmap-perl`, and Playwright e2e suites.
