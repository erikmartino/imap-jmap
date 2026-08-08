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

### Follow-ups worth tracking (not yet done)
- `Principal/getAvailability` collapses each window's end to its start (`memory/principals_store.go`
  GetAvailability), so busy windows are zero-length and ignore recurrence — should expand instances
  and use start+duration; `secret`/`includeInAvailability` should govern contribution.
- JSCalendar `progress` value enum (needs-action/in-process/completed/failed/pending/cancelled) is
  unvalidated; a Task test uses the non-spec `in-progress`.
- `CalendarEvent/query` `canCalculateChanges` is hardcoded `true`.
- Filter uses singular `inCalendar`; the draft's canonical condition is `inCalendars: Id[]`.

---

## Not a goal
- **RFC 9670 JMAP Sharing** — explicitly out of scope in AGENTS.md.
- Process-restart persistence (in-memory backend data loss across restarts is expected).
- DAV (CalDAV/CardDAV/WebDAV) — the `dav/` package keeps its own RFC tests.
