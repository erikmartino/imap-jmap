# TODO — remaining RFC feature gaps

Goal (see AGENTS.md): a client must not be able to tell this isn't a real, full-featured server.
Done so far: FileNode backend + queryChanges, Calendar/Card/AddressBook copy, real Email import/parse,
ContactCard/* canonical naming, Identity/set, Mailbox/set update.

## Next up (cross-cutting)
- [x] **MailBackend change-tracking.** `MailBackend` has no per-type change log, so these `/changes`
      return empty: `Email`, `Thread`, `Mailbox`, `Identity`, `EmailSubmission`, `Quota`. Plan: switch
      the global mail state to a monotonic counter and record `{id, action}` per type in `bumpState`,
      then add `EmailChanges`/`MailboxChanges`/… and wire the handlers. Unblocks the items below.

## Then
- [x] Real `/queryChanges` delta calculations (`added`/`removed` IDs) for `Email`, `Mailbox`, `EmailSubmission`, `Quota`, and `CalendarEvent`.
- [x] Positive and negative filter condition test coverage for `MatchesFilter` (`inMailboxOtherThan`, complex headers, attachment criteria).
- [ ] CalDAV & CardDAV `REPORT` query filter matching (date-range, text filter component evaluation).
- [x] `Mailbox/copy` implementation and testing for cross-account mailbox duplication.
- [x] `Email/set` data-loss: create drops `from/to/cc/body*/headers/receivedAt` (violates Data-Loss rule).
- [x] `EmailSubmission/set`: ignores `update`/`destroy` and `onSuccessUpdate/DestroyEmail`.
- [x] `ifInState` / `stateMismatch` not honored on any `*/set` (RFC 8620 §5.3).
- [x] `/query` ignoring filter/sort/pagination: `Mailbox`, `EmailSubmission`, `Quota`.
- [x] `Mailbox/copy` always refuses; `Email/copy` ignores overrides/`onSuccessDestroyOriginal`.
- [x] `SearchSnippet/get`: no `<mark>` highlighting; `Blob/get`/`Blob/lookup` ignore properties/offset/types.
- [x] `Email/verifySmime`: returns seeded fake result (no real S/MIME validation).
- [x] `CalendarEvent/queryChanges` registered & dynamic state tracking.
- [x] PushSubscription verification flow (RFC 8620 §7.2.2 `PushVerification`) unimplemented.
- [x] SieveScript activation semantics (`isActive`, `onSuccess(De)ActivateScript`, RFC 9661 §3.3).

## DAV (dav/)
- [x] CalDAV/CardDAV PUT drops most iCal/vCard properties.
- [x] CalDAV/CardDAV REPORT query filter matching (date-range and text filter component evaluation).
- [x] No sync-token / getctag / getetag stability (breaks client sync).
- [x] RFC 6638 scheduling (Inbox/Outbox, auto-iTIP on PUT) absent.

## Not a goal
- RFC 9670 JMAP Sharing (explicitly out of scope in AGENTS.md).
- Process restart persistence (in-memory backend data loss across process restarts is expected).
