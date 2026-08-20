# TODO — remaining RFC feature gaps

Goal (see [AGENTS.md](./AGENTS.md), which is authoritative): a client must not be able to tell
this isn't a real, full-featured server.

Scope: JMAP-only. Tests MUST be filed under the primary RFC for the requirement (calendar JMAP
tests under `rfc8984_*_test.go` per repo convention, since JMAP-for-Calendars is still an I-D —
current **draft-ietf-jmap-calendars-27**, verified 2026-08-08). DAV/iCal/vCard/iTIP are covered by
the `dav/` package's own tests.

---

## Highest Priority — `jmapio/jmap-perl` Conformance Goal

Run the community [`jmapio/jmap-perl`](https://github.com/jmapio/jmap-perl) test suite against `imap-jmap` to extend external compliance verification across JMAP Core, Mail, Calendars, and Contacts.

- **Objective**: Clone `jmapio/jmap-perl`, configure adapter against `http://localhost:8181`, run test suite, and achieve 100% PASS rate.
- **Scope**: Core JMAP (RFC 8620), Mail (RFC 8621), Calendars (`draft-ietf-jmap-calendars-27` / RFC 8984), Contacts (RFC 9610 / RFC 9553).
- **Execution**: See [`TESTING.md`](./TESTING.md) for setup and execution commands.

---

## Completed — Calendar spec-compliance review (2026-08-08)

A critical audit against `draft-ietf-jmap-calendars-27` and RFC 8984 reopened items previously
marked done. All are now fixed (one commit each):

- **CAL-1** — privacy was inverted for the owner: `CalendarEvent/get` censored `private` events and
  hid `secret` events even from the calendar owner. The owner now sees full data; the reduced view
  / secret-hiding applies only to non-owner reads. Added the `hideAttendees` property.
- **CAL-2** — recurrence expansion was a stub. Reimplemented on `rrule-go`: all `byX` parts,
  `bySetPosition`, `firstDayOfWeek`, `recurrenceOverrides` (incl. `excluded:true`), and
  `excludedRecurrenceRules`.
- **CAL-3** — removed the non-spec `CalendarEvent/sendResponse`, which corrupted event-level
  `status`. RSVP goes through `CalendarEvent/set` patching `participants/{id}/participationStatus`.
- **CAL-4** — `Calendar/copy` & `CalendarEvent/copy` read the source from the destination account;
  now scoped to `fromAccountId`, with `onSuccessDestroyOriginal` / `destroyFromIfInState`.
- **CAL-5** — `ParticipantIdentity/set` plain update returned a fabricated `{"updated":true}`; now
  returns `null` per RFC 8620 §5.3.
- **CAL-6** — `CalendarEvent/query` accepted any filter/sort. Now rejects unknown filter conditions
  (`unsupportedFilter`) and sort properties (`unsupportedSort`), with AND/OR/NOT + `inCalendars`
  evaluation.
- **CAL-7** — Event `status` no longer accepts Task states; `calendarIds` must reference existing
  calendars; `Calendar/set` rejects unknown properties.

### Follow-ups from the audit — completed (2026-08-08 & 2026-08-20)
- **FU-1** `Principal/getAvailability` now emits real busy windows: end = start+duration, recurrence
  expanded across the query window, and `free`/cancelled/`secret` events excluded.
- **FU-2** JSCalendar Task `progress` enum validated
  (needs-action/in-process/completed/failed/pending/cancelled); fixed the test that used the
  non-spec `in-progress`.
- **FU-3** `CalendarEvent/query` reports `canCalculateChanges=false` when `expandRecurrences` is set
  (synthetic occurrence ids are not change-tracked).
- **FU-4** `inCalendars` (plural `Id[]`) canonical filter condition covered by tests (evaluation
  added in CAL-6).
- **FU-5** `Principal/getAvailability` resolves distinct account contexts for the target principal
  to provide true cross-principal availability.
- **FU-6** Calendar-level `includeInAvailability` ("all"/"none"/"attending") is fully evaluated when
  building free-busy windows.

---

## Open — iMIP/iTIP mail-path security hardening

The mail-based scheduling path (inbound iTIP over iMIP handled by the SMTP receiver) now
imports and applies REQUEST/REPLY/CANCEL with full fidelity, but it currently **trusts
unauthenticated input**: `AuthPlain` is a no-op and `MAIL FROM` / `ATTENDEE` / `ORGANIZER`
are attacker-controlled, so anyone who can reach the SMTP port can spoof an RSVP or inject
events. Close the trust gap (this is distinct from — and was not addressed by — the
MIME-parsing/serialization fixes). See `AGENTS.md` "Standard Parsers & Encoders Only" for
the parsing discipline these build on.

- **SEC-1 — Sender authentication.** Verify SPF ([RFC 7208](https://www.rfc-editor.org/rfc/rfc7208)),
  DKIM ([RFC 6376](https://www.rfc-editor.org/rfc/rfc6376)) and DMARC
  ([RFC 7489](https://www.rfc-editor.org/rfc/rfc7489)) on received messages before any iTIP
  is auto-applied. Fail closed: a message that does not authenticate MUST NOT mutate
  calendar state (deliver to inbox only, or drop).
- **[DONE] SEC-2 — Envelope↔iTIP identity binding.** Require the authenticated sender to match the
  iTIP actor: for REPLY, the sender MUST be the replying `ATTENDEE`; for REQUEST/CANCEL, the
  sender MUST be the `ORGANIZER`. Reject/ignore mismatches (prevents spoofing another
  participant's status). (RFC 6047 §3 security considerations; RFC 5546 §5.)
- **[DONE] SEC-3 — Participant authorization.** For REPLY, the target event (matched by UID) MUST
  already list that attendee as a participant, or the reply is ignored. For CANCEL, the
  sender MUST be the event's organizer. Do not create/patch on unauthorized actors.
- **SEC-4 — Real SMTP auth boundary.** Replace the no-op `AuthPlain`: separate the
  unauthenticated inbound MX path from authenticated submission
  ([RFC 6409](https://www.rfc-editor.org/rfc/rfc6409) / [RFC 4954](https://www.rfc-editor.org/rfc/rfc4954)),
  and gate scheduling trust on which path a message arrived by.
- **[DONE] SEC-5 — Replay / out-of-order defence.** Apply `scheduleSequence` / `scheduleUpdated`
  (draft-ietf-jmap-calendars-27 §5.2.1–5.2.2 / RFC 5546 §2.1.4) to discard stale, duplicate, or out-of-order
  iTIP messages before applying them (email is at-least-once and can reorder).
- **SEC-6 — Resource limits.** Bound MIME message size, part count, and nesting depth when
  parsing inbound mail to prevent DoS on malformed/deeply-nested input.
- **SEC-7 — `scheduleStatus` reporting.** Record per-participant `scheduleStatus` for
  outbound delivery outcomes (sent / delivered / failed) so the organizer's event reflects
  bounces and undeliverable invitations.

---

## Open — real authentication (OIDC / Keycloak)

Production (`jmap.profundo.dk`, see the deployment in `github.com/erikmartino/flux-clusters`)
runs the **development in-memory auth backend** (`memory.NewMemoryAuthBackend`), which accepts
a login only when `password == username` (the email address). This is a severe hole: anyone who
knows an address can authenticate as that user by supplying the address as the password. Keycloak
is already deployed in the cluster (`apps/profundo/keycloak`) but is **not** wired to imap-jmap.

- **[DONE] AUTH-1 — OIDC bearer validation.** Implemented [`jmap.OIDCAuthBackend`](file:///Users/martino/git/imap-jmap/jmap/oidc_auth.go) which validates OAuth 2.0 / OpenID Connect JWT access tokens against an OIDC provider (e.g. Keycloak JWKS signature verification, issuer, and token expiry validation) and maps `preferred_username`/`email`/`sub` claims to `accountID`. Configurable via `-oidc-issuer` / `OIDC_ISSUER` and `-oidc-jwks-url` / `OIDC_JWKS_URL`.
- **AUTH-2 — Retire password==email.** Once OIDC is configured in deployment, remove or disable the fallback in-memory credential path from production environments so `password == username` is rejected.
- **AUTH-3 — Basic-auth bridge (optional).** For clients that only speak HTTP Basic (e.g. some
  IMAP/JMAP clients), validate the supplied password against Keycloak via the resource-owner
  password grant, or issue app-specific passwords — never the accept-anything shim.

See [[deployment-profundo]] for the current deployment facts.

---

## Open — DNS Auto-Discovery Records & Configuration

- **DNS-1 — JMAP SRV & TXT discovery records.** Configure domain DNS for `profundo.dk`:
  - `_jmap._tcp.profundo.dk. 300 IN SRV 0 1 443 jmap.profundo.dk.` (Existing)
  - `_jmaps._tcp.profundo.dk. 300 IN SRV 0 1 443 jmap.profundo.dk.` (Missing SRV)
  - `_jmap._tcp.profundo.dk. 300 IN TXT "v=jmap1 path=/.well-known/jmap"` (Missing TXT)
- **DNS-2 — Legacy AutoConfig / AutoDiscover XML endpoints.** Provide endpoints for non-native JMAP clients:
  - `https://autoconfig.profundo.dk/mail/config-v1.1.xml` (Thunderbird)
  - `https://autodiscover.profundo.dk/autodiscover/autodiscover.xml` (Outlook)

---

## Not a goal
- **RFC 9670 JMAP Sharing** — explicitly out of scope in AGENTS.md.
- Process-restart persistence (in-memory backend data loss across restarts is expected).
- DAV (CalDAV/CardDAV/WebDAV) — the `dav/` package keeps its own RFC tests.
