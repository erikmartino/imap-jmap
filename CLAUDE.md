# Project instructions

**Read [`AGENTS.md`](./AGENTS.md) first and follow it.** `AGENTS.md` is the authoritative
source of project rules, RFC-2119 compliance requirements, the "indistinguishable from a real
server" guiding principle, and the official specification reference list. Everything in
`AGENTS.md` applies to all work in this repository and takes precedence over defaults.

Standing rules (summary — `AGENTS.md` is authoritative; consult it, don't rely on this digest):

- Behave **indistinguishably from a real, full-featured server** — correct state/change tracking,
  correct error objects, persistence, reference/creation-id resolution, and push events.
- Implement **and test** every `MUST`/`SHOULD`/`MAY` across the referenced specs, including every
  named error, `notCreated`/`notUpdated`/`notDestroyed`, `ifInState`, pagination and sort/filter
  variations — unless explicitly listed as a non-goal.
- Back every feature with an **interface + in-memory implementation** in `jmap/memory/`, and drive
  tests **through the memory backend** end-to-end (create → get/query → update → destroy).
- Emit a `StateChange` push event on **every** mutation; support creation references, partial
  updates (patches), partial fetches, and partial success.
- Never silently drop, overwrite, or fabricate stored data on update/merge.

## Specification currency (verified 2026-08-08)

- **JMAP for Calendars** is still an IETF Internet-Draft — current: **`draft-ietf-jmap-calendars-27`**
  (July 2026). No RFC number assigned. Per repo convention, JMAP-for-Calendars method tests are
  filed under `rfc8984_*_test.go` alongside the JSCalendar (RFC 8984) data-model tests.
- **JMAP for Principals / Availability** is still an Internet-Draft (`draft-ietf-jmap-principals`).
- **JSCalendar** is **RFC 8984**; **JMAP Core** RFC 8620; **JMAP Contacts** RFC 9610; **JSContact** RFC 9553.
