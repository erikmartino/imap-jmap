# TODO — Architecture Refactoring, Code Consolidation & RFC Conformance Roadmap

**Authoritative Reference**: See [`AGENTS.md`](./AGENTS.md) for core principles:
1. *Indistinguishable from a real server* (no hardcoded/empty stubs, real change tracking, persistence, correct error objects).
2. *No hardcoded or default usernames in application code*.
3. *RFC 2119 requirement implementation & traceability*.
4. *Standard parsers & encoders only — never ad-hoc parsing*.

Previous task log preserved in [`TODO_PREVIOUS.md`](./TODO_PREVIOUS.md).

---

## Roadmap Overview & Phases

- [x] **Phase 0: Memory Backend Retirement & IMAP/SMTP Backend (`imapsmtp`) Consolidation**
  - [x] 0.1 Fully retired and deleted `jmap/memory/` backend; verified zero imports across codebase.
  - [x] 0.2 Consolidated all mail, blob, submission, identity, vacation response, push subscription, and quota storage into `jmap/imapsmtp/` as the sole reference backend.
  - [x] 0.3 Segregated non-mail mock stores (calendars, contacts/cards, sieve, filenodes) into `jmap/testmock/`.
  - [x] 0.4 Implemented exact email mutation sequence tracking in `CompositeState` and `EmailChanges` to prevent spurious updates on modseq changes.
  - [x] 0.5 Implemented `cannotCalculateChanges` handling for invalid/foreign state tokens in `MailboxChanges` and `EmailChanges` (RFC 8620 §5.6 / §5.2).
  - [x] 0.6 Supported initial state tokens (`"0"`, `"state-0"`) in `DecodeCompositeState`.
  - [x] 0.7 Implemented reverse blob reference tracking (`recordBlobRef`, `deleteBlobRefsForEmail`, `LookupBlobReferences`) per RFC 9404 §4.3.
  - [x] 0.8 Enriched background contexts in `GetBlob` with active account credentials for SMTP/IMAP interop.
  - [x] 0.9 Achieved 100% test pass rate across all packages hermetically in-process with 0 external dependencies (`timeout 10s go test ./...`).

- [x] **Phase 1: Code Consolidation & Common Handler Bug Fixes**
  - [x] 1.1 Deduplicate `parseISODuration` ([`jmap/calendar_utils.go`](./jmap/calendar_utils.go))
  - [x] 1.2 Unify recursive `FilterOperator` (AND/OR/NOT) across occurrences
  - [x] 1.3 Fix and consolidate `*/copy` handlers (account context bug in Card/AddressBook, intra-account copy in Email, missing `destroyFromIfInState` / `onSuccessDestroyOriginal`)
  - [x] 1.4 Eliminate hardcoded usernames/paths in application code ([`dav/memory/`](./dav/memory/) and [`smtp/receiver.go`](./smtp/receiver.go))
  - [x] 1.5 Fix RFC 8620 §3.6.1 invalid JSON error URI (`urn:ietf:params:jmap:error:notJSON`)

- [x] **Phase 2: CalDAV & CardDAV Serving Retirement & Card Conversion**
  - [x] 2.1 Removed `dav/` package and eliminated CalDAV/CardDAV routes (`/caldav/`, `/carddav/`) from `main.go`. Server strictly serves JMAP.
  - [x] 2.2 Retained outbound CalDAV and CardDAV clients in [`jmap/nextcloud/`](./jmap/nextcloud/) to connect to upstream calendar/contacts stores.
  - [x] 2.3 Wire `/convert` endpoint (public, `application/jscontact+json` ⇄ `text/vcard`, 422 for invalid cards) per RFC 9553

- [x] **Phase 3: Core RFC 8620 Conformance & Request Limit Enforcement**
  - [x] 3.1 Enforce `maxCallsInRequest` on `MethodCalls` in [`jmap/server.go`](./jmap/server.go) and WebSocket (`urn:ietf:params:jmap:error:limit`)
  - [x] 3.2 Enforce `maxSizeRequest` on request body size in [`jmap/server.go`](./jmap/server.go) and WebSocket
  - [x] 3.3 Enforce `maxObjectsInGet` in `*/get` handlers and server dispatch (`requestTooLarge`)
  - [x] 3.4 Enforce `maxObjectsInSet` in `*/set` handlers and server dispatch (`requestTooLarge`)
  - [x] 3.5 Support nested and patch Result References in [`jmap/server.go:resolveResultReferences`](./jmap/server.go)

- [ ] **Phase 4: RFC Integration & Feature Completeness**
  - [x] 4.1 Real Quota accounting & `overQuota` enforcement (RFC 9425): update `Used` counters on email create/destroy and enforce limits
  - [x] 4.2 Sieve script execution on incoming SMTP delivery (RFC 5228 / RFC 9661): evaluate recipient's active script (`fileinto`, `discard`, `redirect`, `reject`)
  - [x] 4.3 VacationResponse auto-reply execution on incoming delivery (RFC 8621 §8): evaluate `isEnabled` and date range to send auto-reply
  - [x] 4.4 Real RFC 9007 `MDN/parse` MIME decoding (parse `multipart/report` and `message/disposition-notification`)
  - [ ] 4.5 Web Push event dispatch (RFC 8620 §7.2, RFC 8030, RFC 8291, RFC 9749): send encrypted Web Push notifications on state change
  - [ ] 4.6 Enforce `minDateTime`, `maxDateTime`, and `maxExpandedQueryDuration` on calendar queries (draft-ietf-jmap-calendars-27 §5.11)

- [ ] **Phase 5: External Test Suites & Conformance Verification**
  - [ ] 5.1 `jmapio/jscontact-tests` (Python)
  - [ ] 5.2 MIME Torture Test Suite

---

## Detailed Task Specifications

### Phase 1: Code Consolidation & Common Handler Bug Fixes

#### 1.1 Deduplicate `parseISODuration`
- **Location**: [`jmap/calendar_utils.go:57`](./jmap/calendar_utils.go) and [`jmap/memory/calendar_store_recurrence.go:620`](./jmap/memory/calendar_store_recurrence.go)
- **Problem**: Duplicate character-by-character ISO 8601 duration parsers.
- **Action**: Export `ParseISODuration(raw string) (time.Duration, bool)` from [`jmap/calendar_utils.go`](./jmap/calendar_utils.go) with strict validation, and replace the private function in `calendar_store_recurrence.go`.
- **Validation**: `go test ./jmap/...`

#### 1.2 Unify Recursive `FilterOperator` (AND/OR/NOT)
- **Locations**:
  - `jmap/query.go:235-270`
  - `jmap/mailbox_handlers.go:251-285`
  - `jmap/memory/mail_store_extra.go:400-435`
  - `jmap/memory/contacts_store.go:524-560`
  - `jmap/memory/calendar_store_recurrence.go:49-85`
  - `jmap/memory/calendar_store_notifications.go:240-275`
  - `jmap/imapsmtp/email_read.go:344-380`
- **Problem**: 7 duplicate implementations of recursive boolean evaluation over `conditions`.
- **Action**: Implement generic `EvalFilterOperator(filter map[string]any, matchCondition func(map[string]any) bool) (match bool, isOperator bool)` in [`jmap/query.go`](./jmap/query.go).
- **Validation**: Existing query tests in all packages.

#### 1.3 Fix & Consolidate `*/copy` Handlers [COMPLETED]
- **Locations**:
  - `jmap/contacts_handlers.go:497-605` (`Card/copy`, `AddressBook/copy`)
  - `jmap/email_ops_handlers.go:10-88` (`Email/copy`)
  - `jmap/mailbox_handlers.go:503-562` (`Mailbox/copy`)
  - `jmap/calendar_handlers.go:228-296` (`Calendar/copy`)
  - `jmap/calendar_event_handlers.go:455-520` (`CalendarEvent/copy`)
- **Problems**:
  - `Card/copy` and `AddressBook/copy` read from `ctx` (target account) instead of `srcCtx` (`fromAccountId`).
  - `destroyFromIfInState` and `onSuccessDestroyOriginal` omitted in `Card/copy`, `AddressBook/copy`, and `Mailbox/copy`.
  - `Email/copy` rejects same-account copy (`fromAccountId == accountId`) which RFC 8620 §5.4 allows.
- **Action**: Consolidate common copy helpers (`ResolveCopyAccountIDs`, `SourceAccountContext`, `ValidateCopyStates`) in [`jmap/copy.go`](./jmap/copy.go) and fix account context and RFC 8620 §5.4 parameter handling across all copy handlers.
- **Validation**: All existing copy tests pass; added `TestRFC8621_Section4_6_EmailCopy_SameAccount` in [`jmap/rfc8621_email_copy_test.go`](./jmap/rfc8621_email_copy_test.go).


#### 1.4 Hardcoded Usernames & Identifiers Cleanup [COMPLETED]
- **Locations**:
  - `dav/memory/caldav_backend.go:122`: replaced `BuildITIPRequest(ev, "user@example.com")` with standards-conformant `EncodeCalDAVEvent(ev)` (RFC 4791 / RFC 5545)
  - `dav/memory/caldav_backend.go:29`: `CurrentUserPrincipal` dynamically checks `SubjectFromContext` and `AccountIDFromContext`
  - `dav/memory/carddav_backend.go:29,150`: `CurrentUserPrincipal` dynamically checks context; `GetAddressObject` / `GetCalendarObject` dynamically derive parent path
  - `smtp/receiver.go:87`: removed hardcoded `user@example.com` fallback in `NewReceiverBackend`; added `WithFallbackAccountID` option
- **Validation**: All tests in `dav` and `smtp` pass; dynamic principal resolution verified in `dav/rfc4791_test.go` and `dav/rfc6352_test.go`.


#### 1.5 Fix RFC 8620 §3.6.1 Invalid JSON Error URI [COMPLETED]
- **Location**: [`jmap/types.go:90`](./jmap/types.go), [`jmap/server.go:473,481,502`](./jmap/server.go), [`jmap/websocket.go:113,156`](./jmap/websocket.go)
- **Problem**: `ErrorInvalidJSON = "urn:ietf:params:jmap:error:invalidJSON"` violated RFC 8620 §3.6.1 (`urn:ietf:params:jmap:error:notJSON`).
- **Action**: Updated constant to `ErrorNotJSON = "urn:ietf:params:jmap:error:notJSON"` with `ErrorInvalidJSON` as a backwards-compatible alias; updated `server.go` and `websocket.go` to emit `ErrorNotJSON` for JSON syntax errors and `ErrorNotRequest` for request structural errors; updated `TestRFC8620_Section3_6_1_RequestErrors_NotJSON`.
- **Validation**: All tests pass.

---

### Phase 2: CalDAV & CardDAV Serving Retirement & Card Conversion

#### 2.1 CalDAV & CardDAV Serving Retirement [COMPLETED]
- **Problem**: Server was serving CalDAV and CardDAV HTTP endpoints (`/caldav/`, `/carddav/`) via the `dav/` package on top of JMAP backends.
- **Action**: Retired and removed the `dav/` package, removed CalDAV and CardDAV handlers and listeners from [`main.go`](./main.go), and designated CalDAV/CardDAV server endpoints as out-of-scope in [`AGENTS.md`](./AGENTS.md). The server strictly serves JMAP.
- **Retention**: Retained the outbound CalDAV and CardDAV clients in [`jmap/nextcloud/`](./jmap/nextcloud/) that bridge Nextcloud calendars and address books into JMAP.
- **Validation**: `go test ./...` passes with 0 failures and 0 external dependencies.

#### 2.3 Wire `/convert` Endpoint (RFC 9553) [COMPLETED]
- **Problem**: RFC 9553 conversion HTTP endpoint was not exposed on the HTTP router.
- **Action**: Implemented [`jmap/convert.go`](./jmap/convert.go) exposing `/convert` accepting POST with `application/jscontact+json` or `text/vcard` (bidirectional conversion, auto-detecting Content-Type, returning 422 Unprocessable Entity for invalid cards/vCards, 405 for non-POST, 415 for unsupported media types). Wired `/convert` in [`jmap/server.go`](./jmap/server.go) and [`jmap/auth_middleware.go`](./jmap/auth_middleware.go) as a public unauthenticated endpoint.
- **Validation**: Hermetic unit and HTTP tests in [`jmap/convert_test.go`](./jmap/convert_test.go) (`TestConvert_*`).

---

### Phase 3: Core RFC 8620 Conformance & Request Limit Enforcement

#### 3.1 & 3.2 Request Limits (`maxCallsInRequest`, `maxSizeRequest`, `maxSizeUpload`) [COMPLETED]
- **Locations**: [`jmap/server.go`](./jmap/server.go), [`jmap/websocket.go`](./jmap/websocket.go), [`jmap/blobs.go`](./jmap/blobs.go), [`jmap/session.go`](./jmap/session.go), [`jmap/types.go`](./jmap/types.go), [`jmap/limits.go`](./jmap/limits.go)
- **Problem**: Limits advertised in the Session (`maxCallsInRequest`, `maxSizeRequest`, `maxSizeUpload`) were not enforced on incoming requests, violating RFC 8620 §3.6.1 and RFC 8887 §4.3.4. `RequestError` Problem Details lacked the mandatory `limit` property.
- **Action**:
  - Added `limit` field to `RequestError` per RFC 8620 §3.6.1.
  - Enforced `maxSizeRequest` in HTTP `handleAPI` and WebSocket read loop; rejects oversized bodies with HTTP 413 and `urn:ietf:params:jmap:error:limit` (`limit: "maxSizeRequest"`).
  - Enforced `maxCallsInRequest` in HTTP `handleAPI` and WebSocket dispatch; rejects oversized method call arrays with HTTP 400 and `urn:ietf:params:jmap:error:limit` (`limit: "maxCallsInRequest"`).
  - Enforced `maxSizeUpload` in `HandleUpload` with HTTP 413 and `urn:ietf:params:jmap:error:limit` (`limit: "maxSizeUpload"`).
  - Added `WithCoreCapability` option and context helpers in [`jmap/limits.go`](./jmap/limits.go).
- **Validation**: [`jmap/rfc8620_limits_test.go`](./jmap/rfc8620_limits_test.go) (`TestRFC8620_Section3_6_1_MaxCallsInRequest`, `TestRFC8620_Section3_6_1_MaxSizeRequest`, `TestRFC8887_WebSocket_LimitEnforcement`).

#### 3.3 & 3.4 Method Limits (`maxObjectsInGet`, `maxObjectsInSet`) [COMPLETED]
- **Locations**: [`jmap/server.go`](./jmap/server.go), [`jmap/websocket.go`](./jmap/websocket.go), [`jmap/mailbox_handlers.go`](./jmap/mailbox_handlers.go), [`jmap/email_handlers.go`](./jmap/email_handlers.go), [`jmap/limits.go`](./jmap/limits.go)
- **Problem**: Requests requesting more than `maxObjectsInGet` objects or sending more than `maxObjectsInSet` objects were not rejected with the mandatory `requestTooLarge` method error per RFC 8620 §2.2, §5.1, and §5.3.
- **Action**:
  - Defined `MethodErrorRequestTooLarge = "requestTooLarge"` in [`jmap/types.go`](./jmap/types.go).
  - Enforced `maxObjectsInGet` on `ids` array length in server and WebSocket dispatch for all `*/get` methods.
  - Enforced `maxObjectsInGet` via `ValidateGetLimits` when `ids == nil` fetches all objects across mailboxes and emails.
  - Enforced `maxObjectsInSet` on total `create` + `update` + `destroy` count in server and WebSocket dispatch for all `*/set` methods.
- **Validation**: [`jmap/rfc8620_limits_test.go`](./jmap/rfc8620_limits_test.go) (`TestRFC8620_Section5_1_MaxObjectsInGet`, `TestRFC8620_Section5_3_MaxObjectsInSet`).

#### 3.5 Nested & Patch Result References [COMPLETED]
- **Locations**: [`jmap/server.go:resolveResultReferences`](./jmap/server.go)
- **Problem**: `resolveResultReferences` only resolved top-level arguments prefixed with `#`, failing to support result references nested within filter conditions, object creations, and patch update keys.
- **Action**: Implemented recursive `resolveValueResultReferences` traversing nested maps and arrays to resolve `#`-prefixed property references (including patch pointers like `#keywords/$flagged`) while preserving `#creationId` keys (RFC 8620 §5.3). Added duplicate property detection (`invalidArguments`) and scalar-to-array coercion for array arguments.
- **Validation**: [`jmap/rfc8620_limits_test.go`](./jmap/rfc8620_limits_test.go) (`TestRFC8620_Section3_7_NestedResultReferences`).

---

### Phase 4: RFC Integration & Feature Completeness

#### 4.1 Real Quota Accounting & `overQuota` Enforcement (RFC 9425) [COMPLETED]
- **Locations**: [`jmap/quota_types.go`](./jmap/quota_types.go), [`jmap/quota_handlers.go`](./jmap/quota_handlers.go), [`jmap/imapsmtp/backend.go`](./jmap/imapsmtp/backend.go), [`jmap/imapsmtp/email_write.go`](./jmap/imapsmtp/email_write.go), [`jmap/creationref.go`](./jmap/creationref.go), [`jmap/email_ops_handlers.go`](./jmap/email_ops_handlers.go)
- **Problem**: Quotas returned hardcoded static values without accounting for actual email count and octet usage, limits were never checked on email creation, and `overQuota` errors were not returned in `notCreated`.
- **Action**:
  - Added `DataTypes []string` to `Quota` struct per RFC 9425 §4.1.
  - Implemented dynamic quota tracking (`octetsUsed`, `messagesUsed`, `octetsLimit`, `messagesLimit`, `emailSizes`) in `IMAPSMTPBackend` with `checkQuota`, `recordEmailQuotaCreated`, and `recordEmailQuotaDeleted`.
  - Enforced `checkQuota` in `CreateEmail`, returning RFC 8620 `overQuota` `SetError` when octet or message limits are exceeded.
  - Updated `Quota/query` filter matching (`name`, `scope`, `resourceType`, `dataTypes`) and rejected unsupported sorts per RFC 9425.
  - Propagated `SetError` from `CreateEmail` into `notCreated` in `creationref.go` and `email_ops_handlers.go`.
- **Validation**: Hermetic tests in [`jmap/rfc9425_quota_accounting_test.go`](./jmap/rfc9425_quota_accounting_test.go).

#### 4.2 Sieve Script Execution on Incoming SMTP Delivery (RFC 5228 / RFC 9661) [COMPLETED]
- **Locations**: [`smtp/receiver.go`](./smtp/receiver.go), [`smtp/server.go`](./smtp/server.go), [`main.go`](./main.go)
- **Problem**: Incoming SMTP delivery always routed directly into the INBOX without executing the recipient's active Sieve filtering script.
- **Action**:
  - Integrated Sieve interpreter execution (`github.com/foxcpp/go-sieve`) in `smtp/receiver.go:evaluateSieve` against incoming message headers and envelope data.
  - Handled `fileinto` action: routes email into target mailbox (creating the mailbox on the fly if needed per IMAP auto-create behavior) and cancels implicit keep in INBOX (RFC 5228 §4.1).
  - Handled `discard` action: silently accepts incoming message with 250 OK and drops message without saving (RFC 5228 §4.3).
  - Handled `redirect` action: cancels implicit keep and forwards raw message to external address via `OutboundSender.SendMail` (RFC 5228 §4.2).
  - Handled `reject` / `ereject` action: returns permanent 550 SMTP rejection (`550 5.7.1 message rejected: <reason>`) per RFC 5429 §2.1.
  - Handled `imap4flags` actions (`addflag`, `setflag`): maps Sieve flags into JMAP keywords (`$flagged`, `$seen`, etc.).
  - Wired `WithSieveBackend` and `WithOutboundSender` into `smtp.NewServer` and `main.go`.
- **Validation**: Hermetic tests in [`smtp/rfc5228_sieve_delivery_test.go`](./smtp/rfc5228_sieve_delivery_test.go).

#### 4.3 VacationResponse Auto-Reply Execution (RFC 8621 §8) [COMPLETED]
- **Locations**: [`smtp/receiver.go`](./smtp/receiver.go), [`smtp/server.go`](./smtp/server.go), [`jmap/imapsmtp/backend.go`](./jmap/imapsmtp/backend.go)
- **Problem**: Recipient's `VacationResponse` setting was not evaluated on incoming delivery; out-of-office auto-replies were never dispatched.
- **Action**:
  - Implemented `handleVacationResponse` in `smtp/receiver.go` inspecting recipient's `VacationResponse` via `MailBackend.GetVacationResponse`.
  - Checked `isEnabled`, date window (`fromDate` and `toDate` in RFC 3339 format), and active window validation.
  - Constructed RFC 5322 auto-reply containing `Auto-Submitted: auto-replied`, `In-Reply-To`, and `References` headers per RFC 3834 / RFC 5230.
  - Dispatched auto-reply via `OutboundSender.SendMail`.
  - Enforced anti-loop / bounce suppression per RFC 3834 / RFC 5230: suppresses auto-reply when `Auto-Submitted` != "no", `Precedence: bulk|junk|list`, `List-Id` or `List-Unsubscribe` headers are present, or sender is bounce/null `<>`, `postmaster`, `mailer-daemon`, or `no-reply`.
- **Validation**: Hermetic tests in [`smtp/rfc8621_vacation_test.go`](./smtp/rfc8621_vacation_test.go).

#### 4.4 Real RFC 9007 `MDN/parse` MIME Decoding [COMPLETED]
- **Locations**: [`jmap/mdn_types.go`](./jmap/mdn_types.go), [`jmap/mdn_parser.go`](./jmap/mdn_parser.go), [`jmap/imapsmtp/backend.go`](./jmap/imapsmtp/backend.go), [`jmap/rfc9007_test.go`](./jmap/rfc9007_test.go)
- **Problem**: `ParseMDN` was a stub returning dummy disposition details and treating arbitrary blob contents as plain text body without verifying or parsing the standard `multipart/report; report-type=disposition-notification` or `message/disposition-notification` MIME payload.
- **Action**:
  - Implemented `ParseMDNFromBytes` in [`jmap/mdn_parser.go`](./jmap/mdn_parser.go) using `github.com/emersion/go-message` and `net/textproto`.
  - Recursively walked MIME entities to extract human-readable `textBody` (`text/plain`, `text/html`) and machine-readable `message/disposition-notification` parts.
  - Parsed and normalized RFC 8098 §3.2 disposition headers into `MDNDisposition` with strict lowercase normalization per RFC 9007 §2 (`actionMode`: `manual-action`/`automatic-action`, `sendingMode`: `mdn-sent-manually`/`mdn-sent-automatically`, `type`: `deleted`/`dispatched`/`displayed`/`processed`).
  - Extracted `Reporting-UA`, `MDN-Gateway`, `Original-Recipient`, `Final-Recipient`, `Original-Message-ID`, `Error` list, and custom extension fields (`X-*`).
  - Resolved `forEmailId` dynamically by matching `Original-Message-ID` against stored emails' `Message-ID` values per RFC 9007 §3.2; set to empty/null if no matching email was found.
  - Classified non-MDN blobs as `notParsable` and non-existent blobs as `notFound` per RFC 9007 §2.2.
- **Validation**: Updated [`jmap/rfc9007_test.go`](./jmap/rfc9007_test.go) and created dedicated [`jmap/rfc9007_mdn_parse_test.go`](./jmap/rfc9007_mdn_parse_test.go).



