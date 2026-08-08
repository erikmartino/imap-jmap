# TODO — remaining RFC feature gaps

Goal (see [AGENTS.md](./AGENTS.md), which is authoritative): a client must not be able to tell
this isn't a real, full-featured server.

Scope: the sections below are **JMAP-only**. Tests MUST be filed under the primary RFC for the
requirement and MUST NOT mix concerns across RFCs (e.g. JSCalendar data-model tests go in
`rfc8984_*_test.go`; JMAP protocol/method tests for calendars also go under `rfc8984_*` per repo
convention since JMAP-for-Calendars is still an I-D — current **draft-ietf-jmap-calendars-27**,
verified 2026-08-08). DAV/iCal/vCard/iTIP are out of scope here and are covered by the `dav/`
package's own tests.

> **2026-08-08 — Calendar critical review.** The mail/identity/contacts/blobs work previously
> tracked here was completed and has been removed from this file. A critical audit of the calendar
> implementation against `draft-ietf-jmap-calendars-27` and RFC 8984 found that several items
> previously marked done are **incomplete or spec-violating**. They are reopened below as the
> authoritative remaining-work list. Each item is a separate commit.

---

# Calendar — spec-compliance gaps (authoritative)

## CAL-1 — Privacy enforcement is inverted for the calendar owner  *(MUST violation)*
`handleCalendarEventGet` (`calendar_handlers.go:249-266`) censors `private` events (title→"Busy",
strips description/locations/links) and hides `secret` events (into `notFound`) **unconditionally**,
including when the calendar owner reads their own account. Per draft-27 §4.2.10:
- `secret`: the server MUST behave as though the event does not exist **only for users other than
  the owner** — the owner MUST still see it.
- `private`: the reduced-property view is returned **only to non-owner sharees**; the owner sees
  full data. `private` also restricts writes to the owner (non-owner modify → `forbidden`).
- `privacy` MUST be rejected with `invalidProperties` when set to a non-`public` value on a calendar
  the user does not own.
- Missing `hideAttendees` property (owner-only participant visibility).

Fix: remove owner-side censoring in `CalendarEvent/get`; apply the reduced view / secret-hiding only
on the cross-principal (free-busy / shared-calendar) read path. Rewrite
`rfc8984_privacy_and_freebusy_rights_test.go` (it currently asserts the owner sees `"Busy"` /
`notFound`, which is the bug) and add non-owner coverage.

## CAL-2 — Recurrence expansion is a stub  *(major functional gap)*
`ExpandRecurrenceInstances` (`memory/calendar_store.go:971-1065`) only adds `interval` per
`frequency`; it ignores `byDay`, `byMonthDay`, `byMonth`, `byYearDay`, `byWeekNo`, `byHour`,
`byMinute`, `bySecond`, `bySetPosition`, and `skip`, and never applies `recurrenceOverrides`
(incl. per-instance `excluded:true`) or `excludedRecurrenceRules`. Reimplement using
`teambition/rrule-go` (already a dependency). Add expanded-instance tests for `byDay`,
`bySetPosition`, overrides, and excluded rules (`rfc8984_recurrence_expansion_test.go`).

## CAL-3 — Non-spec `CalendarEvent/sendResponse` corrupts event status  *(bug + non-spec method)*
`handleCalendarEventSendResponse` (`calendar_handlers.go:830-881`) is not defined in
draft-ietf-jmap-calendars, and it writes the RSVP reply to the **event-level** `status`
(`UpdateCalendarEvent(..., {"status": "accepted"})`, `:860-862`) — an invalid Event status enum
value that corrupts the event and bumps state. Remove the method. RSVP MUST go through
`CalendarEvent/set` patching `participants/{id}/participationStatus` (already handled in
`setCalendarEventField`) and emit an iTIP REPLY when `sendSchedulingMessages:true`, without ever
touching event-level `status`. Migrate its test.

## CAL-4 — `Calendar/copy` & `CalendarEvent/copy` ignore `fromAccountId`  *(bug)*
Both handlers (`calendar_handlers.go:635-738`) read the source object via
`backend.GetCalendars`/`GetCalendarEvents`, which resolve against the **ctx (destination) account** —
so cross-account copy silently reads the wrong account. Read the source under a `fromAccountId`
context; add the `fromAccountNotFound` method-level error and the `onSuccessDestroyOriginal` /
`destroyFromIfInState` arguments per RFC 8620 §5.4.

## CAL-5 — `ParticipantIdentity/set` update returns a fabricated value  *(spec violation)*
`handleParticipantIdentitySet` sets `updated[id] = {"updated": true}` (`calendar_handlers.go:1080`).
Per RFC 8620 §5.3 the `updated` value MUST be `null` or an object of **server-set** properties the
client must learn. Return `null` for a plain update.

## CAL-6 — Query filter/sort validation is missing  *(rule + spec)*
`MatchCalendarEvent` (`memory/calendar_store.go:846-935`) silently matches any unhandled filter
condition (violates the AGENTS "No Fallthrough Match Defaults" rule), and `CalendarEvent/query`
(`calendar_handlers.go:537`) does not validate sort comparator properties. Reject unknown filter
conditions with `unsupportedFilter` and unknown sort properties with `unsupportedSort` /
`invalidArguments`.

## CAL-7 — Event/Task status enum + `calendarIds` validation  *(validation gap)*
`validateCalendarEventMap` (`calendar_handlers.go:898`) accepts Task statuses
(`needs-action`/`completed`/`in-progress`) on `@type:"Event"` objects; validate `status` against the
object's `@type` (Event: confirmed/tentative/cancelled; Task: needs-action/completed/in-progress/
cancelled). Validate that `calendarIds` reference existing calendars on create
(`CreateCalendarEvent`, `memory/calendar_store.go:354`). `Calendar/set` should reject unknown
properties with `invalidProperties`.

---

## Not a goal
- **RFC 9670 JMAP Sharing** — explicitly out of scope in AGENTS.md.
- Process-restart persistence (in-memory backend data loss across restarts is expected).
- DAV (CalDAV/CardDAV/WebDAV) — the `dav/` package keeps its own RFC tests; revisit only if the
  Bulwark Webmail integration exercises it.
