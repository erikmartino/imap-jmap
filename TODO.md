# TODO — remaining RFC feature gaps

Goal (see AGENTS.md): a client must not be able to tell this isn't a real, full-featured server.
Done so far: FileNode backend + queryChanges, Calendar/Card/AddressBook copy, real Email import/parse,
ContactCard/* canonical naming, Identity/set, Mailbox/set update, result references (RFC 8620 §3.7);
DAV: PUT property preservation, REPORT filters, sync-token/etag stability, RFC 6638 scheduling.

## Next up (cross-cutting protocol gaps — tests would have caught these)
- [x] **Negative `position` panics.** `Mailbox/query` (mailbox_handlers.go:203), `Quota/query` (quota_handlers.go:95), and `SieveScript/query` (memory/sieve_store.go:185) slice with `filtered[-1]` → remotely triggerable server crash. Per RFC 8620 §5.5 reject with `invalidArguments` (or clamp consistently across all backends). Add a test per method.
- [x] **`EmailSubmission/query` is a hardcoded stub** (submission_handlers.go:120-121): always returns `ids: []`, `total: 0`, never queries state, ignores RFC 8621 §7.2 filters (`identityIds`, `emailIds`, `threadIds`, `before`, `after`). Implement real backend query + tests.
- [x] **Email filter `from`/`to` are dead fields** (query.go:16-17): declared but never evaluated in `MatchesFilter`, so `{"from": "x"}` matches every email (silent fallthrough). Implement + positive/negative tests.
- [x] **`Blob/lookup` never reports `notFound`** (blob_handlers.go:122): missing blobIds are silently dropped instead of returned per RFC 9404 §6. Fix + tests.
- [x] **Push token inconsistency:** mutations publish `StateChange` tokens `"m<nanotime>"` via `bumpState` while `/changes`/`queryState` return tracker tokens `"~N"` — a pushed token fed into `/changes` hits the unknown-token path (mail_store.go:317-354). Align token formats + regression test.
- [x] **StateChange not wired for calendar/contacts/sieve backends:** `MemoryCalendarsBackend`, `MemoryContactsBackend`, `MemorySieveBackend` have no `SetBroadcaster` (only mail/filenode, main.go:75-76) — every mutation there is invisible to `/eventsource`. Add broadcaster + one SSE StateChange test per type.
- [x] **`properties` partial fetch unimplemented everywhere** (RFC 8620 §5.1): zero `properties` handling in any `/get` handler (Email, Mailbox, Thread, Identity, EmailSubmission, Quota, Calendar, CalendarEvent, AddressBook, Card, SieveScript, FileNode, PushSubscription, IMAPAccount). Implement + tests asserting only requested props (+id) are returned.
- [x] **`anchor`/`anchorOffset` unimplemented everywhere** (RFC 8620 §5.5): zero occurrences in code or tests. Implement on all `*/query` + `anchorNotFound` error + tests.
- [x] **`*/queryChanges` ignore `filter`/`sort`/`upToId`** (Email/Mailbox/Quota/CalendarEvent/Submission) — only FileNode re-evaluates the filter. Deltas must respect the query's filter/sort; `upToId` must truncate `added`. Fix + tests.
- [x] **`/set` error-path correctness:** `Email/set` destroy of a missing id is silently dropped (no `notDestroyed`); `Email/set` has no `notCreated` path (any create "succeeds" with a fabricated blob id); Mailbox/set, Identity/set, SieveScript/set, FileNode/set report `invalidProperties`/`invalidScript` instead of `notFound` for missing update ids. Fix per RFC 8620 §5.3 + tests asserting exact SetError types.
- [x] **Creation references (`#creationId`) only work for FileNode:** resolve for Email `mailboxIds`, Mailbox `parentId`, Card `addressBookIds`, CalendarEvent `calendarIds`, EmailSubmission `emailId` (forward refs, missing/cyclic → `notCreated`). One composite-set test per type.
- [x] **`SieveScript/queryChanges` not registered** (RFC 9661 §4) — returns `unknownMethod`. Register + delta tests.
- [x] **Missing sort comparators** (RFC 8621 §4.5.2): `SortEmails` implements only receivedAt/sentAt/subject/size; missing `from`, `to`, `cc`, `bcc`, `hasKeyword`, `allHeaderKeywords`, `someHeaderKeywords`, `noneHeaderKeywords`, `hasAttachment`; `collation` parsed but unused; unknown comparator silently passes. Implement + order-asserting tests.
- [x] **Missing filter properties:** Mailbox `hasAnyRole`/`isSubscribed` (RFC 8621 §2.4.1); Quota/query ignores `filter` entirely (name/scope/resourceType/type, RFC 9425 §4.4); CalendarEvent `uid`/`updatedBefore`/`updatedAfter`. Implement + pos/neg tests.
- [x] **`Thread/get` with `ids: null` returns empty list** instead of all threads (email_handlers.go:16) (RFC 8621 §3.1). Same defect class: EmailSubmission/get, SearchSnippet/get, Blob/get, Email/verifySmime.
- [x] **`EmailSubmission/set` has no `destroy` support** (RFC 8621 §7.3) — backend has no `DeleteSubmission`; `onSuccessUpdate/DestroyEmail` error paths silently swallowed. Implement + tests.
- [ ] **`EmailSubmission/get`/`Identity/get`/`EmailSubmission/changes`/`Quota/changes`/`Quota/queryChanges`/`Thread/changes`/`Calendar/changes`/`CalendarEvent/changes`/`CalendarEvent/sendResponse`/`ContactCard/*` aliases have zero tests** (several are handler-level-only gaps with backend coverage). Add handler-level tests.

## Then (test coverage backlog — implemented but unexercised)
- [ ] `stateMismatch` tests for all 9 untested `*/set` handlers (only Email covered).
- [ ] `notDestroyed` asserted for all 10 handlers (0 tests today); `notUpdated` with correct SetError type (only 1 test, type unasserted).
- [ ] `cannotCalculateChanges` tests for all 5 `queryChanges` handlers (0 tests today).
- [ ] Pagination tests for every `*/query`: position/limit slicing, position beyond end, limit 0, negative position, calculateTotal with filter.
- [ ] Mixed valid+invalid `ids` → `notFound` tests for every `/get` (only Blob/get, verifySmime, MDN/parse covered).
- [ ] Sort tests: order assertions (asc/desc, multi-comparator tie-break, default sort) — current test asserts count only.
- [ ] Filter positive+negative per property: Email `cc`, `bcc`, `header` (name-only and name+value), `hasAttachment`, direct `notKeyword`; Mailbox `role`/`parentId`/`name`; Card 18 of 20 RFC 9610 §3.3.1 conditions untested (only `email`); CalendarEvent `inCalendar`/`description`/`location`/`text`; FileNode `name`/`type`/`isFolder`/`parentId:""`; SieveScript `name`/`isValid`.
- [ ] PushSubscription/get + set round-trip (create/get/update/destroy, notCreated/notDestroyed) + real `PushVerification` HTTP POST assertion.
- [ ] `Email/import`/`Email/parse` error paths: `blobNotFound`, missing blobId → `invalidProperties`, `notParsable`, `notFound`; client overrides (keywords/mailboxIds/receivedAt) applied.
- [ ] `Email/verifySmime` payload assertions (result fields), not just key presence.
- [ ] `Email/copy`: `notCreated` for missing source, new id + new threadId, overrides applied, original survives without `onSuccessDestroyOriginal`.
- [ ] `CalendarEvent/copy` full round-trip (mirror Calendar/copy test — currently name-only).
- [ ] `CalendarEvent/sendResponse`: participant status persisted, valid iTIP REPLY, notFound error.
- [ ] `Email/set` data-loss regression over HTTP: partial update (keywords/mailboxIds patch) leaves untouched fields intact.
- [ ] `maxChanges` truncation + `hasMoreChanges` over HTTP; `sinceQueryState` too old.
- [ ] JSON pointer escaping (`~0`/`~1`) and invalid pointer paths in result references.
- [ ] Compile-time assertions `var _ jmap.XxxBackend = (*MemoryXxxBackend)(nil)` for Contacts, Calendar, Sieve, IMAPAccess backends (AGENTS.md requirement).

## Not a goal
- RFC 9670 JMAP Sharing (explicitly out of scope in AGENTS.md).
- Process restart persistence (in-memory backend data loss across process restarts is expected).
