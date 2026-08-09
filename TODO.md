# TODO — remaining RFC feature gaps

Goal (see [AGENTS.md](./AGENTS.md), which is authoritative): a client must not be able to tell
this isn't a real, full-featured server.

Scope: JMAP-only. Tests MUST be filed under the primary RFC for the requirement (calendar JMAP
tests under `rfc8984_*_test.go` per repo convention, since JMAP-for-Calendars is still an I-D —
current **draft-ietf-jmap-calendars-27**, verified 2026-08-08). DAV/iCal/vCard/iTIP are covered by
the `dav/` package's own tests.

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

### Follow-ups from the audit — completed (2026-08-08)
- **FU-1** `Principal/getAvailability` now emits real busy windows: end = start+duration, recurrence
  expanded across the query window, and `free`/cancelled/`secret` events excluded.
- **FU-2** JSCalendar Task `progress` enum validated
  (needs-action/in-process/completed/failed/pending/cancelled); fixed the test that used the
  non-spec `in-progress`.
- **FU-3** `CalendarEvent/query` reports `canCalculateChanges=false` when `expandRecurrences` is set
  (synthetic occurrence ids are not change-tracked).
- **FU-4** `inCalendars` (plural `Id[]`) canonical filter condition covered by tests (evaluation
  added in CAL-6).

### Still open
- `Principal/getAvailability` reads events from the caller's account context, not the target
  principal's — genuine cross-principal availability needs account resolution by principal.
- Calendar-level `includeInAvailability` ("all"/"none"/"attending") is stored but not yet consulted
  when building free-busy.

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
- **SEC-2 — Envelope↔iTIP identity binding.** Require the authenticated sender to match the
  iTIP actor: for REPLY, the sender MUST be the replying `ATTENDEE`; for REQUEST/CANCEL, the
  sender MUST be the `ORGANIZER`. Reject/ignore mismatches (prevents spoofing another
  participant's status). (RFC 6047 §3 security considerations; RFC 5546 §5.)
- **SEC-3 — Participant authorization.** For REPLY, the target event (matched by UID) MUST
  already list that attendee as a participant, or the reply is ignored. For CANCEL, the
  sender MUST be the event's organizer. Do not create/patch on unauthorized actors.
- **SEC-4 — Real SMTP auth boundary.** Replace the no-op `AuthPlain`: separate the
  unauthenticated inbound MX path from authenticated submission
  ([RFC 6409](https://www.rfc-editor.org/rfc/rfc6409) / [RFC 4954](https://www.rfc-editor.org/rfc/rfc4954)),
  and gate scheduling trust on which path a message arrived by.
- **SEC-5 — Replay / out-of-order defence.** Apply `scheduleSequence` / `scheduleUpdated`
  (draft-ietf-jmap-calendars-27 §5.2.1–5.2.2) to discard stale, duplicate, or out-of-order
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

- **AUTH-1 — OIDC bearer validation.** Add an `AuthBackend` that validates OAuth 2.0 / OIDC
  access tokens against the Keycloak realm (JWKS signature check, `iss`/`aud`/`exp` validation,
  RFC 9068 JWT access tokens), mapping the verified `sub`/`email` claim to the account. This is
  the RFC 8620 §8.2 path (JMAP defers auth to OAuth 2.0).
- **AUTH-2 — Retire password==email.** Once OIDC is in place, remove the insecure in-memory
  credential path from any non-test build (keep it behind a dev-only flag), so real deployments
  never accept `password == username`.
- **AUTH-3 — Basic-auth bridge (optional).** For clients that only speak HTTP Basic (e.g. some
  IMAP/JMAP clients), validate the supplied password against Keycloak via the resource-owner
  password grant, or issue app-specific passwords — never the accept-anything shim.

See [[deployment-profundo]] for the current deployment facts.

---

## Not a goal
- **RFC 9670 JMAP Sharing** — explicitly out of scope in AGENTS.md.
- Process-restart persistence (in-memory backend data loss across restarts is expected).
- DAV (CalDAV/CardDAV/WebDAV) — the `dav/` package keeps its own RFC tests.
