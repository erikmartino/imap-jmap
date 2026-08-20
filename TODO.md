# TODO — Remaining RFC Feature Gaps & Roadmap

**Goal** (see [AGENTS.md](./AGENTS.md), which is authoritative): A client MUST NOT be able to determine that there is not a real, full-featured server behind the protocol.

**Scope**: JMAP-only server implementation. Tests MUST be filed under the primary RFC for the requirement (calendar JMAP tests under `rfc8984_*_test.go` per repo convention, since JMAP-for-Calendars is governed by **draft-ietf-jmap-calendars-27**). CalDAV/CardDAV/WebDAV are covered by the `dav/` package's dedicated test suite.

---

## Open Tasks

### 1. iMIP / iTIP Mail-Path Security Hardening
Inbound scheduling messages (iTIP over iMIP processed by the SMTP receiver) require robust sender verification and authentication boundaries to prevent spoofing and state corruption (RFC 6047, RFC 5546, draft-ietf-jmap-calendars-27).

- [ ] **SEC-1 — Sender Authentication**: Verify SPF ([RFC 7208](https://www.rfc-editor.org/rfc/rfc7208)), DKIM ([RFC 6376](https://www.rfc-editor.org/rfc/rfc6376)), and DMARC ([RFC 7489](https://www.rfc-editor.org/rfc/rfc7489)) on received messages before any iTIP is auto-applied. Fail closed: unauthenticated messages MUST NOT mutate calendar state (deliver to mailbox only, or reject).
- [ ] **SEC-4 — Real SMTP Auth Boundary**: Separate unauthenticated inbound MX transport from authenticated submission ([RFC 6409](https://www.rfc-editor.org/rfc/rfc6409) / [RFC 4954](https://www.rfc-editor.org/rfc/rfc4954)), and gate scheduling trust on the transport boundary.
- [ ] **SEC-6 — Resource Limits**: Bound MIME message size, part count, and nesting depth when parsing inbound mail to prevent DoS on malformed or deeply-nested inputs.
- [ ] **SEC-7 — `scheduleStatus` Reporting**: Record per-participant `scheduleStatus` for outbound delivery outcomes (sent / delivered / failed) so organizer events reflect delivery failures and bounces.

### 2. Authentication & Deployment (OIDC / Keycloak)
Production deployment (`jmap.profundo.dk`) transition from development in-memory auth to Keycloak OIDC authentication.

- [ ] **AUTH-2 — Retire `password == email` in Production**: Remove or disable the fallback in-memory credential path from production environments so plain password matches are rejected when OIDC is active.
- [ ] **AUTH-3 — Basic-Auth Bridge (Optional)**: For clients that only speak HTTP Basic, validate supplied credentials against Keycloak via Resource Owner Password Credentials (ROPC) or issue app-specific passwords.

### 3. DNS Auto-Discovery & Configuration
Configure DNS and auto-configuration endpoints for seamless client discovery.

- [ ] **DNS-1 — JMAP SRV & TXT Discovery Records**:
  - `_jmap._tcp.profundo.dk. 300 IN SRV 0 1 443 jmap.profundo.dk.` (Configured)
  - `_jmaps._tcp.profundo.dk. 300 IN SRV 0 1 443 jmap.profundo.dk.` (Missing SRV)
  - `_jmap._tcp.profundo.dk. 300 IN TXT "v=jmap1 path=/.well-known/jmap"` (Missing TXT)
- [ ] **DNS-2 — Legacy AutoConfig / AutoDiscover XML Endpoints**:
  - `https://autoconfig.profundo.dk/mail/config-v1.1.xml` (Thunderbird)
  - `https://autodiscover.profundo.dk/autodiscover/autodiscover.xml` (Outlook)

---

## Completed Milestones

### External Test Suite Conformance (2026-08-20)
- [x] **Fastmail `JMAP-TestSuite` (Perl)**: **89/89 test files PASS (100%)** on both HTTP and WebSocket (`JMTS_USE_WEBSOCKETS=1`) transports (0 failures, 0 skipped).
- [x] **TypeScript `jmap-test-suite`**: **309/309 tests PASS (100%)** (304 required, 5 recommended, 0 failures, 0 skipped), including multi-account and cross-account copy operations.
- [x] **Internal Go Conformance & Unit Tests**: `go test ./...` **100% PASS** across all packages (`jmap`, `jmap/memory`, `smtp`, `dav`).

### iTIP Scheduling Security Hardening (2026-08-20)
- [x] **SEC-2 — Envelope ↔ iTIP Identity Binding**: Requires authenticated sender to match replying `ATTENDEE` on `REPLY`, and `ORGANIZER` on `REQUEST`/`CANCEL` (RFC 6047 §3, RFC 5546 §5).
- [x] **SEC-3 — Participant Authorization**: For `REPLY`, target event must already list the attendee as participant; for `CANCEL`, sender must be the event organizer.
- [x] **SEC-5 — Replay & Out-of-Order Defense**: Evaluates `SEQUENCE` / `scheduleSequence` to discard stale, duplicate, or reordered iTIP messages before mutating calendar state.

### Calendar Compliance Review & Spec Audit (2026-08-08 & 2026-08-20)
- [x] **CAL-1 — Owner vs Non-Owner Privacy**: `CalendarEvent/get` returns full event data to calendar owners while enforcing private/secret event masking and `hideAttendees` for non-owners.
- [x] **CAL-2 — Recurrence Expansion**: Full RFC 8984 recurrence expansion with `rrule-go` supporting `byX` rules, `bySetPosition`, `firstDayOfWeek`, and `recurrenceOverrides`.
- [x] **CAL-3 — Spec-Compliant RSVP**: Removed non-spec methods; RSVP operates via standard `CalendarEvent/set` updating `participants/{id}/participationStatus`.
- [x] **CAL-4 — Cross-Account Copy**: `Calendar/copy` and `CalendarEvent/copy` properly scoped across accounts with `onSuccessDestroyOriginal` and `destroyFromIfInState`.
- [x] **CAL-5 — ParticipantIdentity/set**: Returns `null` on update per RFC 8620 §5.3.
- [x] **CAL-6 — Filter & Sort Validation**: `CalendarEvent/query` validates filter conditions and sort properties, rejecting unsupported properties with `unsupportedFilter`/`unsupportedSort`.
- [x] **CAL-7 — Validation Boundaries**: Calendar IDs and properties strictly validated against JSCalendar specifications.
- [x] **FU-1–FU-6 — Availability & Free-Busy**: Real busy window calculation in `Principal/getAvailability`, cross-principal account resolution, and `includeInAvailability` evaluations.

### Authentication & Token Validation
- [x] **AUTH-1 — OIDC Bearer Token Validation**: Implemented [`jmap.OIDCAuthBackend`](file:///home/martino/git/imap-jmap/jmap/oidc_auth.go) with JWKS signature verification, token expiration, issuer validation, and account mapping (RFC 8620 §2.1).

---

## Non-Goals & Out-of-Scope Specifications
- **RFC 9670 (JMAP Sharing)**: Explicitly designated as out-of-scope per [AGENTS.md](./AGENTS.md).
- **Process-Restart In-Memory Persistence**: In-memory backend persistence across process restarts is non-goal (state rebuilds on start).
- **DAV Native JMAP Intermixing**: CalDAV/CardDAV/WebDAV protocol handling is isolated in the `dav/` package.
