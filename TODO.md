# TODO — remaining RFC feature gaps

Goal (see AGENTS.md): a client must not be able to tell this isn't a real, full-featured server.
Done so far: FileNode backend + queryChanges, Calendar/Card/AddressBook copy, real Email import/parse,
ContactCard/* canonical naming, Identity/set, Mailbox/set update.

## Next up (cross-cutting)
- [ ] **MailBackend change-tracking.** `MailBackend` has no per-type change log, so these `/changes`
      return empty: `Email`, `Thread`, `Mailbox`, `Identity`, `EmailSubmission`, `Quota`. Plan: switch
      the global mail state to a monotonic counter and record `{id, action}` per type in `bumpState`,
      then add `EmailChanges`/`MailboxChanges`/… and wire the handlers. Unblocks the items below.

## Then
- [ ] `/queryChanges` still empty: `Email`, `Mailbox`, `EmailSubmission`, `Quota` (depends on change log).
- [ ] `Email/set` data-loss: create drops `from/to/cc/body*/headers/receivedAt` (violates Data-Loss rule).
- [ ] `EmailSubmission/set`: ignores `update`/`destroy` and `onSuccessUpdate/DestroyEmail`.
- [ ] `ifInState` / `stateMismatch` not honored on any `*/set` (RFC 8620 §5.3).
- [ ] `/query` ignoring filter/sort/pagination: `Mailbox`, `EmailSubmission`, `Quota`.
- [ ] `Mailbox/copy` always refuses; `Email/copy` ignores overrides/`onSuccessDestroyOriginal`.
- [ ] `SearchSnippet/get`: no `<mark>` highlighting; `Blob/get`/`Blob/lookup` ignore properties/offset/types.
- [ ] `Email/verifySmime`: returns seeded fake result (no real S/MIME validation).
- [ ] `CalendarEvent/queryChanges` not registered; `*/query` `queryState` hardcoded `"0"` in several types.
- [ ] PushSubscription verification flow (RFC 8620 §7.2.2 `PushVerification`) unimplemented.
- [ ] SieveScript activation semantics (`isActive`, `onSuccess(De)ActivateScript`, RFC 9661 §3.3).

## DAV (dav/)
- [ ] CalDAV/CardDAV PUT drops most iCal/vCard properties; queries ignore filters.
- [ ] No sync-token / getctag / getetag stability (breaks client sync).
- [ ] RFC 6638 scheduling (Inbox/Outbox, auto-iTIP on PUT) absent.

## Not a goal
- RFC 9670 JMAP Sharing (explicitly out of scope in AGENTS.md).
