# TODO — remaining RFC feature gaps

Goal (see AGENTS.md): a client must not be able to tell this isn't a real, full-featured server.
Done so far: FileNode backend + queryChanges, Calendar/Card/AddressBook copy, real Email import/parse,
ContactCard/* canonical naming, Identity/set, Mailbox/set update, result references (RFC 8620 §3.7);
DAV: PUT property preservation, REPORT filters, sync-token/etag stability, RFC 6638 scheduling.

Scope: the sections below are **JMAP-only** (per "focus on JMAP"). Tests MUST be filed under the primary
RFC for the requirement and MUST NOT mix concerns across RFCs (e.g. JSCalendar data-model tests go in
`rfc8984_*_test.go`; JMAP protocol/method tests for calendars also go under `rfc8984_*` per repo
convention since JMAP-for-Calendars is still an I-D). DAV/iCal/vCard/iTIP are out of scope here and are
covered by the `dav/` package's own tests (rfc4791/6352/4918/6350/2426/6638/5545/5546/6047).

---

# Implement first — outbound mail + identity/authorization refactor

Lands before the calendar/addressbook/blob work. Today `EmailSubmission/set` never sends:
`CreateSubmission` (`memory/mail_store.go:1058`) just stores the submission and fabricates
`deliveryStatus: {"user@example.com": {delivered: granted}}`; the `smtp/` server is inbound-only and
hardcodes account `"primary"` (`smtp/receiver.go:28`, `smtp/parser.go:22`). Memory stores currently key
isolation on the **authenticated user id** (`getStoreLocked`, `memory/mail_store.go:40-52`).

Model we're moving to: **authentication** translates a subject → an **accountId** (derived, e.g.
base64url of the subject); an **authorization service (permission guard)** decides whether a principal's
accountId may act on a target accountId; an **account resolver** maps an email address → accountId
(default: every `*@<primaryDomain>`). Memory stores use **accountId** as the discriminator; SMTP uses
the resolver for local delivery (inbound routing + outbound loopback). "The domain of the user" = the
**recipient** address's domain. Design reference: `docs/plans/outbound-mail-identity-authz.md`.

## A. Identity (subject → accountId)
- [x] **`AccountIDForSubject(subject)` helper** in `jmap/auth.go` = `base64.RawURLEncoding` of the subject. Single source of truth reused by auth backend + resolver. Unit test round-trip/stability.
- [x] **Auth backend returns derived accountId.** `MemoryAuthBackend` (`memory/auth_store.go`) maps token→subject and returns `AccountIDForSubject(subject)` from `ValidateCredentials`/`ValidateToken`/`Authenticate` instead of the bare username. Unit test: two subjects → distinct stable accountIds.
- [x] **Memory stores keyed by accountId (confirm/adjust).** `getStoreLocked` already reads `AccountIDFromContext`; with the above it becomes the derived accountId. Adjust `rfc8620_multi_user_isolation_test.go` if it inspects the id value; assert isolation still holds.

## B. Authorization service (permission guard) + account resolver
- [x] **`PermissionGuard` interface + `SelfAccessGuard` default** in new `jmap/authz.go`: `CanAccessAccount(ctx, principalAccountID, targetAccountID) bool`; default = equality. `WithPermissionGuard` option + `Server` field (`jmap/server.go`). Unit test.
- [x] **Lenient dispatch enforcement.** In the JMAP dispatcher resolve each call's `accountId` arg: empty or alias `"primary"` → principal's own account; otherwise require `CanAccessAccount` else method error `accountNotFound`. Keep routing by the ctx accountId so existing `"primary"` tests pass. Tests: self allowed, foreign → `accountNotFound`.
- [x] **`AccountResolver` interface + `PrimaryDomainResolver` default** in `jmap/authz.go`: `ResolveAccountID(ctx, emailAddress) (accountID string, local bool)`; default returns `(AccountIDForSubject(addr), true)` when the address domain == primary domain, else `("", false)`. `WithAccountResolver` option. Unit tests: local vs external.

## C. Primary-domain config + CLI flag
- [x] **`--primary-domain` flag** (default `example.com`, env `PRIMARY_DOMAIN`) in `main.go` alongside `main.go:39-43`; construct `PrimaryDomainResolver` from it and inject into `jmap.Server` + `smtp.NewServer`. Test: flag/env parsing + default resolves to `example.com`.

## D. Outbound send (`EmailSubmission/set`) — tests: `rfc8621_*_test.go`
- [x] **Add `Envelope` (mailFrom/rcptTo) to `EmailSubmission`** (`jmap/mail_types.go:125`, RFC 8621 §7.1) + typed per-recipient `deliveryStatus`. Round-trip test.
- [x] **Thread resolver + allow-list into the submission handler** (`RegisterMailHandlers` → `handleEmailSubmissionSet`, `submission_handlers.go:82`). Read `envelope.rcptTo`, falling back to the email's To/Cc/Bcc.
- [x] **Local delivery via resolver.** For each recipient the resolver marks local, deliver in-process with `MailBackend.CreateEmail` under the recipient's accountId ctx (reuses the inbound primitive at `smtp/receiver.go:94`); set `deliveryStatus[rcpt] = {delivered:"yes", smtpReply:"250 ..."}`. Test: local recipient's account receives the message.
- [x] **External allow-list gate.** `--allowed-recipients` flag (comma-separated; **default empty = none**; env `ALLOWED_RECIPIENTS`) via `WithAllowedRecipients`. Non-local recipient sent only if listed; else `deliveryStatus[rcpt]=failed`. If no recipient deliverable → `notCreated` `forbidden`. Tests: external-listed sent, external-unlisted rejected, empty-list blocks all external.
- [x] **Replace fabricated status.** `CreateSubmission` (`memory/mail_store.go:1075-1080`) accepts the computed per-recipient `deliveryStatus` instead of the hardcoded map.

## E. SMTP receiver routing (inbound) — tests: `smtp/rfc5321_*_test.go`
- [x] **Per-recipient resolver routing.** `smtp/receiver.go` `Data()` (:64) + `NewServer`/`ReceiverBackend` take the resolver; deliver a copy to each local recipient's accountId (ctx per recipient) instead of hardcoded `"primary"`; keep `"primary"` fallback when unresolved. Test: message to a local address lands in that account; outbound submission loopback observed in the receiver.

## F. First-use account seeding — tests: `rfcless_account_seeding_test.go` (non-RFC dev feature; `rfcless_` prefix + descriptive suffix)
- [x] **Seed a fresh account with sample data on first use.** When an account is used for the first time and its state is empty (the lazy per-account store creation path: `newMemoryUserStore` in `memory/mail_store.go:67`, `newMemoryUserCalendarStore` `memory/calendar_store.go:53`, `newMemoryUserContactsStore` `memory/contacts_store.go:54`, and the FileNode store), populate representative sample content so a client never opens onto an empty mailbox:
  - **Emails** across multiple folders — Inbox, Sent, Drafts (and Archive) — mirroring/extending the existing startup-only `SeedSampleData` (`memory/seed.go:9`), which today seeds only the shared default account and only mail+calendar.
  - **Calendar entries** in the default calendar.
  - **Addresses** in the default address book (Contacts — currently NOT seeded at all).
  - **Files in the blob store within a subfolder** — create a FileNode subfolder and a couple of files (with backing blobs) under it.
  - Make seeding per-account and idempotent (run once, keyed off empty state), reusing the existing `Create*` backend primitives. Keep it a memory-backend/dev convenience (a real backend would not auto-seed). Test: the first `Email/query`, `CalendarEvent/get`, `Card/get`, and `FileNode/query` for a brand-new authenticated account each return the seeded items, including the blob-store subfolder.

---

# Calendar — full JMAP support

## RFC 8984 (JSCalendar data model) — tests: `rfc8984_*_test.go`
- [x] **Model missing Event/common properties** (`calendar_types.go:73` CalendarEvent, patch in `calendar_store.go:320` `setCalendarEventField`): `relatedTo` (§4.1.3), `prodId` (§4.1.4), `sequence` (§4.1.7), `method` (§4.1.8), `descriptionContentType` (§4.2.3), `showWithoutTime` (§4.2.4), `locale` (§4.2.8), `categories` (§4.2.10), event-level `color` (§4.2.11), `priority` (§4.4.1), `replyTo` (§4.4.4), `sentBy` (§4.4.5), `requestStatus` (§4.4.7), `useDefaultAlerts` (§4.5.1), `localizations` (§4.6.1), `timeZones` (§4.7.2). Each: struct field + patch path + round-trip test.
- [x] **`locations` map** (§4.2.5): replace the singular `Location` under key `"location"` with the `Id[Location]` map the spec defines (`calendar_types.go:83`). Keep back-compat parse if needed. Test map round-trip.
- [ ] **Complete nested object types + `@type` tags** (currently `@type` is only forced on the top-level Event at `calendar_store.go:303`, never set/validated on nested objects, §3.1/§3.2):
  - `Location` add `@type`, `locationTypes`, `relativeTo`, `coordinates`, `links` (§4.2.5, `calendar_types.go:24`).
  - `VirtualLocation` add `@type`, `features` (§4.2.6, `calendar_types.go:67`).
  - `Link` add `@type`, `size`, `display`; rename `type`→`contentType` (§4.2.7, `calendar_types.go:58`).
  - `Participant` add `roles` map + `participationStatus` (replacing singular `role`/`status`), `@type`, `sendTo`, `delegatedTo`/`delegatedFrom`, `memberOf`, `scheduleAgent`/`scheduleStatus`, `invitedBy`, `links`, `language`, `locationId` (§4.4.6, `calendar_types.go:31`).
  - `RecurrenceRule` add `@type`, `rscale`, `skip`, `firstDayOfWeek`, `byMonthDay`, `byMonth`, `byYearDay`, `byWeekNo`, `byHour`, `byMinute`, `bySecond`, `bySetPosition`; change `byDay` from `[]string` to `NDay[]` (§4.3.3, `calendar_types.go:42`).
  - `Alert` make `trigger` an `OffsetTrigger`/`AbsoluteTrigger` object (not string); add `@type`, `acknowledged`, `relatedTo` (§4.5.2, `calendar_types.go:50`).
  - Add the `Relation`, `NDay`, `OffsetTrigger`, `AbsoluteTrigger` types (§1.4.10/§4.3.3/§4.5.2). Per-object round-trip tests.
- [ ] **Recurrence properties** (`calendar_types.go`): `recurrenceId` (§4.3.1), `recurrenceIdTimeZone` (§4.3.2), `excludedRecurrenceRules` (§4.3.4, `calendar_store.go:392`), `recurrenceOverrides` (§4.3.5), `excluded` (§4.3.6). Struct + patch + tests.
- [ ] **Recurrence expansion engine** (`calendar_store.go:622` `QueryCalendarEvents`): expand `recurrenceRules` with byX filtering, `bySetPosition`, `skip` (§4.3.3.1); apply `recurrenceOverrides` to instances (§4.3.5); make `after`/`before` filter the full recurrence set, not just master start/end (`calendar_store.go:498-507` `MatchCalendarEvent`, §4.3). Tests over expanded instances.
- [ ] **Task object** (`@type:"Task"`: `due`, `start`, `estimatedDuration`, `percentComplete`, `progress`, `progressUpdated`) — model + set/get/query + tests (§5.2).
- [ ] **Group object** (`@type:"Group"`: `entries`, `source`) — model + set/get + tests (§5.3).
- [ ] **Create/set validation** (`calendar_handlers.go:255`, `setCalendarEventField`): reject unknown/invalid properties with `invalidProperties` instead of silently dropping; enum-validate `status`/`privacy`/`freeBusyStatus`/participant roles (§3 + RFC 8620 §5.3). Tests per rejected case.
- [ ] **`uid` auto-generate + require on create** (`calendar_store.go:294` `CreateCalendarEvent`, §4.1.2). Test that a created event always has a stable uid.

## JMAP for Calendars (I-D: methods / query / scheduling) — tests: `rfc8984_*_test.go` (repo convention)

### Query
- [ ] **Sort comparators** (MUST: `start`, `uid`, `recurrenceId`; SHOULD: `created`, `updated`): add `comparators []Comparator` to `CalendarsBackend.QueryCalendarEvents` (`backend.go:135`), parse `sort` in `handleCalendarEventQuery` (`calendar_handlers.go:439`), apply stable ordering. Order-asserting tests.
- [ ] **`expandRecurrences` query arg** (`calendar_handlers.go:406`): return per-occurrence ids when true. Test.
- [ ] **`timeZone` query arg** for floating-time bounds (`calendar_handlers.go:406`) — currently ignored. Test.
- [ ] **`owner` and `attendee` filter conditions** (`calendar_store.go:470` `MatchCalendarEvent`) — pos/neg tests each.
- [ ] **`canCalculateChanges` correctness** (`calendar_handlers.go:449`): stop hardcoding `true` unless a stable order backs it; make `CalendarEvent/queryChanges` sort/position-aware so `added` carries correct indices (`calendar_handlers.go:457`). Test.

### Calendar object, rights & sharing
- [ ] **Missing Calendar properties** (`calendar_types.go:12`): `isSubscribed`, `includeInAvailability`, `defaultAlertsWithTime`, `defaultAlertsWithoutTime`, `timeZone`, `shareWith`. Struct + patch + tests.
- [ ] **`isDefault` is server-set**: reject direct client set on create/update (`calendar_store.go:187,226`); change only via `onSuccessSetIsDefault` (below). Test rejection.
- [ ] **`CalendarRights` spec fields** (`calendar_types.go:3`): replace non-spec `mayWriteItems`/`mayAdmin` with `mayReadFreeBusy`, `mayReadItems`, `mayWriteAll`, `mayWriteOwn`, `mayUpdatePrivate`, `mayRSVP`, `mayDelete`, `mayShare`; enforce the `mayWriteAll ⇒ mayWriteOwn/mayUpdatePrivate/mayRSVP` invariant (`calendar_store.go:181`). Tests on emitted `myRights` names + invariant.
- [ ] **`Calendar/get` MUST hide calendars the principal may only read free-busy on** (`calendar_handlers.go:28`). Test.
- [ ] **`privacy` (`private`/`secret`) enforcement** on other principals' event reads (`calendar_handlers.go:167`). Test.

### Calendar/set lifecycle args
- [ ] **`onDestroyRemoveEvents` arg + `calendarHasEvents` SetError** (`calendar_handlers.go:92`, `calendar_store.go:239` `DeleteCalendar`): non-empty calendar destroy MUST fail unless flag set. Test both paths.
- [ ] **`onSuccessSetIsDefault` arg** on `Calendar/set` (`calendar_handlers.go:92`). Test.

### Scheduling (iTIP dispatch)
- [ ] **Honor `sendSchedulingMessages`** (`calendar_handlers.go:266-386` `handleCalendarEventSet`): currently auto-dispatches iMIP unconditionally; default is `false`. Gate dispatch on the flag. Test both.
- [ ] **`noSupportedScheduleMethods` SetError** path (`calendar_handlers.go:231`). Test.
- [ ] **Participation constraints**: enforce `mayRSVP` / `mayInviteSelf` / `mayInviteOthers` (`calendar_handlers.go:231`). Tests.
- [ ] **RSVP via `CalendarEvent/set`** (patch participant `participationStatus`) rather than the non-spec `CalendarEvent/sendResponse` (`calendar_handlers.go:25,618`). Fix the bug where the reply is written to event-level `status` and `p.Status` is never persisted (`calendar_handlers.go:648-650`, §5.1.3 vs §4.4.6). Test participant status persists.
- [ ] **PatchObject nested paths** (e.g. `participants/x/participationStatus`, `locations/x/name`) in `setCalendarEventField` (`calendar_store.go:320`) — currently only whole-key switches. Test nested patch.

### Method families & capability
- [ ] **`CalendarEvent/parse`** (blobIds → parsed/notParsable/notFound) replacing the non-spec `CalendarEvent/parseInvitation` (`calendar_handlers.go:24,593`); advertise `urn:ietf:params:jmap:calendars:parse` (`session.go:119`). Tests.
- [ ] **`CalendarsCapability` missing fields** (`session.go:119-123`): `minDateTime`, `maxDateTime`, `maxExpandedQueryDuration`, `maxParticipantsPerEvent`. Advertise + test.
- [ ] **ParticipantIdentity** object + `ParticipantIdentity/get`/`changes`/`set` (with `onSuccessSetIsDefault`) — not implemented. Impl + tests. *(I-D; larger.)*
- [ ] **CalendarEventNotification** object + `/get`/`changes`/`set`/`query`/`queryChanges`, and generation of notifications on scheduling changes — not implemented. Impl + tests. *(I-D; larger.)*
- [ ] **Principals + availability** (`urn:ietf:params:jmap:principals`, `:availability`; `maxAvailabilityDuration`; principal `calendarAddress`/`mayGetAvailability`/`mayShareWith`; free-busy querying) — not implemented. Impl + tests. *(I-D; larger.)*

---

# Address Book / Contacts — full JMAP support

## RFC 9610 (JMAP for Contacts) — tests: `rfc9610_*_test.go`

### Methods & query
- [ ] **`ContactCard/queryChanges`** (+ `Card/queryChanges` alias) not registered (`contacts_handlers.go:19-30`) → clients get `unknownMethod` (§3.4). Register + delta tests.
- [ ] **`ContactCard/query` sort/comparators** (MUST: `created`, `updated`; SHOULD: `name/given`, `name/surname`, `name/surname2`, §3.3.2): add `comparators []Comparator` to `ContactsBackend.QueryCards` (`backend.go:113`), parse `sort` in `handleCardQuery` (`contacts_handlers.go:314-361`), order in `QueryCards` (`contacts_store.go:564`). Order-asserting tests.
- [ ] **`ContactCard/query` real `queryState` + `canCalculateChanges`** — stop hardcoding `"0"`/`false` (`contacts_handlers.go:354-355`); reflect real state once queryChanges lands. Round-trip test.
- [ ] **FilterOperator (AND/OR/NOT)** in `MatchCard` (`contacts_store.go:466`) — `operator`/`conditions` currently ignored (§3.3 / RFC 8620 §5.5). Tests.
- [ ] **Filter conditions implemented-but-untested** (§3.3.1, `contacts_store.go:469-559`): `uid`, `hasMember`, `kind`, `createdBefore`, `createdAfter`, `updatedBefore`, `updatedAfter`, `text`, `name`, `name/given`, `name/surname`, `name/surname2`, `nickname`, `organization`, `phone`, `onlineService`, `address`, `note`. Pos + neg test each.

### AddressBook & Card set semantics
- [ ] **`AddressBookRights` spec names** (§2, `contacts_types.go:4-9`): emit `mayRead`, `mayWrite`, `mayShare`, `mayDelete` (currently `mayReadItems`/`mayWriteItems`/`mayAdmin`). Fix seed/create (`contacts_store.go:66-72,181-186`). Test emitted `myRights` keys.
- [ ] **AddressBook `isSubscribed` + `shareWith`** (§2, `contacts_types.go:11-19`; `UpdateAddressBook` `contacts_store.go:197`): model, patch, enforce `mayShare` with `forbidden` SetError. Tests.
- [ ] **AddressBook `isDefault` is server-set** (§2): reject direct create/update set (`contacts_store.go:187-191,218-225`); add **`onSuccessSetIsDefault`** arg to `AddressBook/set` (§2.3, `contacts_handlers.go:97`). Tests.
- [ ] **`onDestroyRemoveContents` + `addressBookHasContents` SetError** (§2.3/§7.4.1): `DeleteAddressBook` (`contacts_store.go:231`) currently deletes unconditionally and orphans cards; destroying a non-empty book MUST fail unless the flag is set. Impl + tests.
- [ ] **Card MUST belong to ≥1 AddressBook** (§3): validate `addressBookIds` non-empty and all values `true` in `CreateCard`/`UpdateCard` (`contacts_store.go:281,433`) → `invalidProperties`. Tests.
- [ ] **Media `blobId` + photo type check** (§3/§3.5): `JSContactMedia` needs a server-set `blobId` (`contacts_types.go:104`); reject non-image files used as photos. Impl + tests.
- [ ] **Enforce `maxAddressBooksPerCard`** on set (§1.4.1, advertised at `session.go:114-117` but unenforced). Test the limit.
- [ ] **Remove/justify non-spec `AddressBook/copy`** (`contacts_handlers.go:16`) — RFC 9610 defines no such method. Decide: drop or document. (No new test.)

## RFC 9553 (JSContact data model) — tests: `rfc9553_*_test.go`
- [ ] **Required top-level `version` + `uid`** (§2.1.2, §2.1.9): add `version` to `Card` (`contacts_types.go:130`), populate/require both on create (`contacts_store.go:281`). Round-trip test.
- [ ] **Missing top-level Card properties** (§2, `contacts_types.go:130-154`): `language` (§2.1.5), `prodId` (§2.1.7), `relatedTo` (§2.1.8), `preferredLanguages` (§2.3.4), `calendars` (§2.4.1), `schedulingAddresses` (§2.4.2), `cryptoKeys` (§2.6.1), `directories` (§2.6.2), `personalInfo` (§2.8.4), `localizations` (§2.7.1). Model + patch + tests.
- [ ] **`speakToAs` correct shape; drop non-spec top-level `gender`** (§2.2.4): `speakToAs = { grammaticalGender, pronouns: Id[Pronouns] }`; remove `JSContactGender`/top-level `gender` (`contacts_types.go:111-115,149`; `JSContactSpeakToAs` `:117-120`). Conformant test.
- [ ] **`Title` field name** (§2.2.5): `JSContactTitle` uses `title` → must be `name`; add `kind`, `organizationId` (`contacts_types.go:71-75`). Test asserts `name`.
- [ ] **`anniversaries` date shape** (§2.8.1): `date` is a Timestamp/PartialDate object + `place`; kinds `birth`/`death`/`wedding`; drop non-spec `label` (`contacts_types.go:122-127`). Test object-form date.
- [ ] **Sub-object field completeness**: `Name` add `isOrdered`/`defaultSeparator`/`phoneticSystem`/`phoneticScript`, make `sortAs` a `String[String]` map (§2.2.1, `contacts_types.go:22-26`); `NameComponent` add `phonetic` (§2.2.1.2, `:29-32`); `Address` use `components`(AddressComponent[])/`isOrdered`/`coordinates`/`timeZone`/`defaultSeparator`/`phoneticSystem`/`phoneticScript` instead of flat street/locality fields (§2.5.1, `:52-62`); `Organization` add `sortAs` (§2.2.3, `:65-69`). Tests per sub-field.
- [ ] **Patch paths for existing-but-unpatchable properties** (§3.5 / RFC 8620 PatchObject): `setCardField` (`contacts_store.go:314-431`) can't patch `links`, `media`, `speakToAs`, `anniversaries`, or mutable `kind` — updates silently drop them. Add paths + persistence tests.
- [ ] **Nested JSON-pointer patch paths** (`name/full`, `emails/e1/pref`, `addressBookIds/ab-1`) in `setCardField` (`contacts_store.go:314`) — currently top-level keys only (RFC 8620 §5.3). Test.

---

# Blobs — full JMAP support

## RFC 9404 (JMAP Blob Management) — tests: `rfc9404_*_test.go`

### Capability object (§3.1)
- [ ] **Top-level `urn:...:blob` capability MUST be an empty object** (currently populated, `session.go:187-190`); the **account-level** blob capability object is entirely missing (is `struct{}{}`, `session.go:236`). Fix placement + test both.
- [ ] **Capability fields**: add `maxSizeBlobSet`, `maxDataSources` (MUST allow ≥64), `supportedTypeNames`; rename `supportedAlgorithms` → `supportedDigestAlgorithms`; drop non-spec `MaxDataAsStream` (`session.go:85-89`). Update the test that asserts the wrong name (`rfc9404_test.go:41`). Impl + tests.

### Blob/upload — DataSourceObject model (§4.1)
- [ ] **Treat `data` as `DataSourceObject[]`** with concatenation (`handleBlobUpload`, `blob_handlers.go:67-83`) instead of a single field. Impl + tests.
- [ ] **`data:asBase64` source** handled; **invalid base64 MUST → `notCreated`** (currently falls back to raw bytes, `blob_handlers.go:76-80`). Impl + tests.
- [ ] **`data:asText` = raw octets** (currently base64-decoded); **invalid UTF-8 MUST → `notCreated`** (`blob_handlers.go:67-80`). Impl + tests.
- [ ] **`{blobId, offset, length}` catenation source** with range semantics (null offset→0, null length→remaining, past-end MUST → `notCreated`) (`blob_handlers.go:65-93`). Impl + tests.
- [ ] **Strict rejection**: MUST NOT guess intent on invalid refs/data — reject with `notCreated` (`blob_handlers.go:83-92`). Test.
- [ ] **Populate `createdIds`** for successful uploads so back-references resolve (`blob_handlers.go:57-100`). Test a later method referencing `#creationId`.
- [ ] **Empty-blob creation** (zero data sources) supported (`handleBlobUpload`). Test.
- [ ] **Enforce `maxSizeBlobSet`** on created/concatenated blobs → SetError (§3.1/§4.1). Test.

### Blob/get (§4.2)
- [ ] **`offset`/`length` range params** applied (`blob_handlers.go:18-54`). Impl + tests.
- [ ] **`properties` selection** (default `data`+`size`; full struct always returned today, `blob_handlers.go:29-52`, `blobs.go:13-21`). Impl + tests.
- [ ] **Result data properties**: produce `data:asText` (with **`isEncodingProblem`** true + null data on non-UTF-8) and `data:asBase64` (`Data` is `json:"-"`, `blobs.go:13-21`,`:20`). Impl + tests.
- [ ] **`digest:<algorithm>`** as base64 (currently base16) and per requested algorithm (`memory/blobs.go:34-47`, `blobs.go:19`). Impl + tests.
- [ ] **`isTruncated`** true when offset+length exceeds the blob; **`size` MUST be whole-blob octet count** regardless of range; **digest MUST cover the selected range** (`blob_handlers.go:48-52`, `memory/blobs.go:33-47`). Impl + tests.

### Blob/lookup (§4.3) & security (§5)
- [ ] **`typeNames` validity from `supportedTypeNames`** (not the hardcoded `{Mailbox,Thread,Email}` map, `blob_handlers.go:113`) and **require the defining capability in the request `using` set** → else `unknownDataType` (§4.3). Impl + tests.
- [ ] **Access-control leakage tests** (§5): a blob referenced only by an object the user can't see is excluded from `matchedIds` (`blob_handlers.go:139-144`); document/decide the empty-array-per-type vs `notFound` behavior for missing blobs (`blob_handlers.go:131-133`) — prose §4.3/§5 vs the §4.3.1 example conflict; do not silently change. Tests.

## RFC 8620 §6 (Core binary data) — tests: `rfc8620_*_test.go`
- [ ] **Blob/copy correct shape** (§6.3): args `blobIds: Id[]`, response `copied: Id[Id]` / `notCopied: Id[SetError]` (currently a `create`/creationId map returning Blob objects, `blob_handlers.go:163-194`). Rewrite the non-compliant test that asserts the old shape (`rfc9404_test.go:296-348`). Add `fromAccountNotFound` method error and full create-SetError set (`blob_handlers.go:176-180`). Impl + tests.
- [ ] **Enforce `maxSizeUpload` → 413** on `/upload` (`HandleUpload` reads unbounded, `blobs.go:38-49`; advertised at `session.go:163`). Impl + test.
- [ ] **Upload response fields** `accountId`/`blobId`/`type` asserted (test only asserts `id`/`size`, `rfc8620_test.go:419-449`). Test.
- [ ] **Download headers**: assert `?type=` → `Content-Type` and `name` → `Content-Disposition` filename (`blobs.go:84-93`; untested at `rfc8620_test.go:721-749`). Set `Cache-Control: immutable` long max-age (RECOMMENDED). Impl(SHOULD) + tests.
- [ ] **RFC 7807 problem-details JSON** for upload/download HTTP errors (SHOULD; currently `http.Error`/`http.NotFound`, `blobs.go:34,40,47,67,80`). Impl + tests.
- [ ] **Uploader-only access to unreferenced blobs** even in shared accounts (§6.1; access keyed only by `accountID`, `memory/blobs.go:57-64`). Impl + test.
- [ ] **Blob lifetime guarantees** (§6): unreferenced blob retained ≥1h, not deleted during the call that removed the last reference, over-quota oldest-first eviction (SHOULD). `BlobBackend` (`backend.go:81-86`) has no expiry/quota surface. Impl(partial) + tests. *(low priority)*

## Not a goal
- RFC 9670 JMAP Sharing (explicitly out of scope in AGENTS.md).
- Process restart persistence (in-memory backend data loss across process restarts is expected).
- DAV (CalDAV/CardDAV/WebDAV, RFC 4791/6352/4918/6350/2426/6638): out of scope for now — revisit only if the Bulwark Webmail integration exercises it. The `dav/` package keeps its own RFC tests regardless.
