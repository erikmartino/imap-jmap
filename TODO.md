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

---

## Non-Goals & Out-of-Scope Specifications
- **RFC 9670 (JMAP Sharing)**: Explicitly designated as out-of-scope per [AGENTS.md](./AGENTS.md).
- **Legacy XML Mail Auto-Configuration (AutoConfig/AutoDiscover)**: Replaced by native RFC 8620 Session Discovery, DNS SRV/TXT bootstrapping, and IETF PACC JSON autoconfiguration per [AGENTS.md](./AGENTS.md).
- **Process-Restart In-Memory Persistence**: In-memory backend persistence across process restarts is non-goal (state rebuilds on start).
- **DAV Native JMAP Intermixing**: CalDAV/CardDAV/WebDAV protocol handling is isolated in the `dav/` package.
