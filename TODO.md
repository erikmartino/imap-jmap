# TODO — Remaining RFC Feature Gaps & Roadmap

**Goal** (see [AGENTS.md](./AGENTS.md), which is authoritative): A client MUST NOT be able to determine that there is not a real, full-featured server behind the protocol.

**Scope**: JMAP-only server implementation. Tests MUST be filed under the primary RFC for the requirement (calendar JMAP tests under `rfc8984_*_test.go` per repo convention, since JMAP-for-Calendars is governed by **draft-ietf-jmap-calendars-27**). CalDAV/CardDAV/WebDAV are covered by the `dav/` package's dedicated test suite.

---

## Open Tasks

### 1. Authentication & Deployment (OIDC / Keycloak)
Production deployment (`jmap.profundo.dk`) transition from development in-memory auth to Keycloak OIDC authentication.

- [ ] **AUTH-2 — Retire `password == email` in Production**: Remove or disable the fallback in-memory credential path from production environments so plain password matches are rejected when OIDC is active.

### 2. External Conformance Test Suites Verification
Track and continuously execute all verified external test suites against the server:

- [ ] **`jmapio/jscontact-tests` (Python)**: Ingest/run JSContact Card ([RFC 9553](https://www.rfc-editor.org/rfc/rfc9553.html)) and vCard conversion test cases against server endpoints.
- [ ] **MIME Torture Test Suite**: Run standard W3C / MHonArc deeply nested/malformed MIME test fixtures against `Email/parse` and inbound SMTP.

---

## Completed
- **SEC-1 — Sender Authentication**: SPF/DKIM/DMARC verification gates iTIP auto-apply; unauthenticated messages fail closed (delivered to mailbox only).
- **SEC-4 — Real SMTP Auth Boundary**: Unauthenticated inbound MX transport separated from authenticated submission (RFC 6409 / RFC 4954); scheduling trust gated on the transport boundary.
- **Fastmail `JMAP-TestSuite` (Perl)**: 89/89 test files PASS (Core RFC 8620, Mail RFC 8621, WebSockets RFC 8887).
- **TypeScript `jmap-test-suite` (Node.js)**: 309/309 tests PASS (Core, Mail, Multi-account, EventSource, Push, Submissions, Quotas).

---

## Non-Goals & Out-of-Scope Specifications
- **RFC 9670 (JMAP Sharing)**: Explicitly designated as out-of-scope per [AGENTS.md](./AGENTS.md).
- **Legacy XML Mail Auto-Configuration (AutoConfig/AutoDiscover)**: Replaced by native RFC 8620 Session Discovery, DNS SRV/TXT bootstrapping, and IETF PACC JSON autoconfiguration per [AGENTS.md](./AGENTS.md).
- **Process-Restart In-Memory Persistence**: In-memory backend persistence across process restarts is non-goal (state rebuilds on start).
- **DAV Native JMAP Intermixing**: CalDAV/CardDAV/WebDAV protocol handling is isolated in the `dav/` package.