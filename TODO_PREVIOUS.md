# TODO — Previous Completed Milestones & RFC Feature Verification

**Authoritative Reference**: See [`AGENTS.md`](./AGENTS.md) for core principles.

---

## Completed Milestones

### 1. External Conformance Test Suites Verification
- [x] **`jmapio/jscontact-tests` (Python)**: Full RFC 9553 / RFC 9554 / RFC 9555 JSContact ↔ vCard 4.0 bidirectional conversion:
  - Implemented `jmap/vcardconv` bidirectional converter.
  - Live CNR-style `/convert` endpoint (`application/jscontact+json` ⇄ `text/vcard`).
  - Vendored all 55 vectors in `jmap/vcardconv/vectors_test.go` (`spectest.Require` citations, matrix in `docs/conformance/jscontact.json`).
  - 55/55 tests PASS (100% green).
- [x] **MIME Torture Test Suite**:
  - Vendored 13 canonical torture vectors in `jmap/testdata/mime_torture/` (Mark Crispin, Ryan Finnie, 25-level nested multiparts, malformed boundaries, mixed CTEs, header folding stress, header injection resistance, circular RFC 822, obsolete syntax, adversarial payloads).
  - Executed `TestEmailParse_MIMETorture` and `TestEmailParse_AdversarialEdgeCases` in `jmap/` (100% green, 0 panics).
  - Executed `TestSMTPReceiver_MIMETorture` and `TestSMTPReceiver_OversizedMessageDATA` in `smtp/` (100% green, 0 panics).
  - Hardened `smtp.ParseMessageToEmail` with bounded `MaxMIMEParts` recursion protection and `strconv.Itoa` part formatting.
  - Added requirement traceability in `docs/conformance/jmap-mail.json` and `docs/conformance/smtp.json`.

### 2. Architecture Consolidation & Reference Backends
- [x] **Phase 0 — Retire In-Memory Mail Backend (`jmap/memory/`)**: Completely transitioned to `jmap/imapsmtp/` as sole mail reference backend.
- [x] **Phase 1 — Complete Elimination of `testmock`**:
  - In-process Nextcloud server (`jmap/nextcloud/embedded.go`) mounting CalDAV, CardDAV, WebDAV, OCS APIs.
  - Real state & delta sync (`created`, `updated`, `destroyed`, `cannotCalculateChanges`) in `jmap/nextcloud/`.
  - Migrated JMAP server and test suites to Nextcloud backends.
  - ManageSieve reference backend and IMAPAccess/Auth consolidated into reference adapters.
  - Complete deletion of `jmap/testmock/` with 0 remaining imports.

### 3. Core Protocol Conformance & Security Hardening
- [x] **AUTH-2 — Retire `password == email` in Production**: The OIDC backend fails closed (rejects all credential attempts) unless an explicit `AUTH_DEV_FALLBACK=true` attaches the in-memory dev credential backend.
- [x] **SEC-1 — Sender Authentication**: SPF/DKIM/DMARC verification gates iTIP auto-apply; unauthenticated messages fail closed (delivered to mailbox only).
- [x] **SEC-4 — Real SMTP Auth Boundary**: Unauthenticated inbound MX transport separated from authenticated submission (RFC 6409 / RFC 4954); scheduling trust gated on the transport boundary.
- [x] **Fastmail `JMAP-TestSuite` (Perl)**: 89/89 test files PASS (Core RFC 8620, Mail RFC 8621, WebSockets RFC 8887).
- [x] **TypeScript `jmap-test-suite` (Node.js)**: 309/309 tests PASS (Core, Mail, Multi-account, EventSource, Push, Submissions, Quotas).

---

## Non-Goals & Out-of-Scope Specifications
- **RFC 9670 (JMAP Sharing)**: Explicitly designated as out-of-scope per [AGENTS.md](./AGENTS.md).
- **Legacy XML Mail Auto-Configuration (AutoConfig/AutoDiscover)**: Replaced by native RFC 8620 Session Discovery, DNS标志 SRV/TXT bootstrapping, and IETF PACC JSON autoconfiguration per [AGENTS.md](./AGENTS.md).
- **Process-Restart In-Memory Persistence**: In-memory backend persistence across process restarts is non-goal (state rebuilds on start).
- **DAV Native JMAP Intermixing**: CalDAV/CardDAV/WebDAV protocol handling is isolated in the `dav/` package.