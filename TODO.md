# TODO — Architecture & RFC Conformance Roadmap

**Authoritative Guidelines**: See [`AGENTS.md`](./AGENTS.md) for core principles:
1. *Indistinguishable from a real server* (no stubs, real change tracking, full persistence, standard error objects).
2. *Strict layering & backend encapsulation* (upstream wire protocols encapsulated in clients; domain handlers operate on domain types).
3. *Zero hardcoded or default usernames*.
4. *Standard parsers only — zero format assembly via string concatenation and zero brittle hand-rolled validation*.
5. *RFC 2119 requirement implementation & traceability*.

---

## Completed Milestones

- [x] **RFC 8620 (JMAP Core)**: 100% MUST / MUST NOT requirements covered with automated test suite and spec matrix verification.
- [x] **RFC 8621 (JMAP Mail)**: 100% MUST / MUST NOT requirements covered across Mailbox, Email, Thread, Identity, EmailSubmission, and VacationResponse.
- [x] **ShareNotification (RFC 9670)**: Implemented `ShareNotification` data model, handlers (`get`, `changes`, `query`, `queryChanges`, `set` destroy), and push events.
- [x] **Specification Traceability & AST Checker**: `tools/specextract` automated extraction, `spec/` requirement matrices, and `TestSpecCoverage` gating with automatic markdown report generation (`UPDATE_DOCS=1 go test ./spec`).

---

## Active Roadmap

### Priority 1: Standard Parsers, Zero Custom Serialization & Robust Validation Conversion
Audit and convert all ad-hoc serializers, manual string concatenations, and brittle hand-rolled validators across the codebase to canonical, battle-tested standard libraries per `AGENTS.md` Section 3:
- [x] **1.0 Push-back rule codified**: `AGENTS.md` §3 now requires pushing back on any string concatenation / hand-rolled single-delimiter parsing and mandates a standard library instead.
- [x] **1.1 MIME & Message Headers (`jmap/jmapmail/message_id.go`)**
  - `StripBCCHeader` parses/strips headers via `github.com/emersion/go-message/textproto` (`ReadHeader`, `Header.Del("Bcc")`, `WriteHeader`).
  - `EnsureValidMessageID` uses the standard MIME header parser/writer instead of line-splitting strings.
  - `GenerateMessageID` derives the domain with `net/url`, `net/mail` and `github.com/mcnijman/go-emailaddress`; malformed input falls back to `localhost` (no more domain guessing).
- [x] **1.2 CalDAV PROPFIND XML (`jmap/nextcloud/client.go`)**
  - Raw XML string literal in `getCalendarCollections` replaced with `xml.Marshal(&propfindCalendarReq{})`.
- [x] **1.3 CardDAV vCard Construction (`jmap/nextcloud/contacts.go`)**
  - `fmt.Sprintf("BEGIN:VCARD...")` fallback replaced with a structured `vcard.Card` object.
- [x] **1.4 SMTP Auto-Reply MIME Construction (`smtp/receiver.go`)**
  - Vacation auto-reply built with `go-message/mail` `Header` + `CreateSingleInlineWriter`.
- [x] **1.5 Address & Media Type Formatting (`jmap/jmapmail/email_set_handlers.go`, `email_parse.go`)**
  - `fmt.Sprintf("%q <%s>", ...)` replaced with `(&net/mail.Address{...}).String()`.
  - `mime.ParseMediaType`/`mime.FormatMediaType` replace manual `charset=` manipulation (`ensureCharsetUTF8`).
- [x] **1.6 Robust Email and Domain Validation (`jmap/jmapmail/submission_handlers.go`, `jmap/nextcloud/principals.go`, `jmap/jmapauth/auth.go`)**
  - Ad-hoc `strings.Split(email, "@")` / `strings.Contains(email, "@")` replaced with `github.com/mcnijman/go-emailaddress` domain extraction.
- [x] **1.7 SMTP Sender Authentication (`smtp/sender_auth.go`, `smtp/receiver.go`, `smtp/outbound.go`)**
  - `Authentication-Results` header formatted with `github.com/emersion/go-msgauth/authres` + `go-message/textproto`.
  - `addressDomain` / `emailAddressMatches` / outbound recipient routing use `go-emailaddress` instead of `strings.LastIndex`/`strings.Cut` on `@`.
  - `organizationalDomain` uses `golang.org/x/net/publicsuffix` (eTLD+1) instead of the last-two-labels heuristic.
  - TLS `ServerName` derived with `net.SplitHostPort` instead of `strings.LastIndex(host, ":")`.
- [ ] **1.8 vCard serialization (`jmap/vcardconv/encode.go`)**
  - Migrate the hand-rolled vCard folding/escaping encoder to `github.com/emersion/go-vcard`.
- [ ] **1.9 Remaining audit**: `jmap/imapsmtp/blob.go` (MIME by `Sprintf`), `jmap/managesieve/client.go`, `imap/convert.go`, `jmap/jmapmail/email_get_helper.go`, and `cmd/`/`tools/` utilities.
- [x] **1.10 Regression tests for the parser/serializer conversion**
  - `jmap/jmapmail`: `StripBCCHeader` (incl. folded continuation), `EnsureValidMessageID` edge cases (empty/malformed/LF-only), `domainFromMailboxOrDomain`, `ensureCharsetUTF8`, `extractDomainFromAddress`.
  - `jmap/jmapauth`: `PrimaryDomainResolver.ResolveAccountID` (case-insensitive domain, foreign/invalid/default-domain cases).
  - `smtp`: `addressDomain`, `organizationalDomain` fallback, `emailAddressMatches`, `sanitizeEnvelope`, and `AuthenticationResultsHeader` including the "no blank-line terminator" invariant.

---

### Priority 2: RFC 8620 / RFC 8621 SHOULD Coverage
Close the `SHOULD`/`SHOULD NOT` gaps in the generated core and mail matrices.
- [x] **2.1 RFC 8620 core batch 1**: `primaryAccounts` excludes `urn:ietf:params:jmap:core`; request-level `problem+json` bodies; result-reference resolution; `/changes` created/updated/destroyed semantics (incl. calculate from any prior state); `SetError.properties`; blob `Cache-Control: immutable`; `/.well-known/jmap` resolution; per-item sequential `/set` processing; `QueryChanges` `upToId` truncation.
- [x] **2.2 RFC 8621 mail batch 1**: vacation-response default subject and default body generation (RFC 8621 §8).
- [ ] **2.3 RFC 8620 remaining SHOULDs**: localisation (`Accept-Language`), push event-id/state encoding, blob quota, subscription id hashing, authentication/TLS clauses, and client-only clauses (triage as non-goal where the server has no obligation).
- [ ] **2.4 RFC 8621 remaining SHOULDs**: search semantics (RFC 2047 decoding, HTML markup stripping, quoted phrase search, token tokenisation), preview truncation, changes ordering, `Email/import` over-quota, submission RCPT/DATA stage reporting, and client-only clauses.

---

### Phase 2: JMAP Standards RFC Conformance (Zero MUST Gaps Target)
Drive all remaining JMAP RFC requirement matrices in `spec/` to 100% MUST/MUST NOT coverage:
- [ ] **1.1 RFC 8887 (JMAP over WebSocket)**:
  - WebSocket endpoint, subprotocol negotiation (`jmap`), request/response multiplexing, push notifications over WebSocket.
- [ ] **1.2 RFC 9007 (JMAP for MDN)**:
  - MDN data model, `MDN/send` and `MDN/parse` method handlers, disposition headers, and S/MIME compatibility.
- [ ] **1.3 RFC 9404 (JMAP Blob Management)**:
  - `Blob/copy`, `Blob/lookup` handlers, blob upload/download streaming, Digest verification.
- [ ] **1.4 RFC 9425 (JMAP Quotas)**:
  - `Quota/get`, `Quota/changes`, `Quota/query` handlers for mail and storage resource types.
- [ ] **1.5 RFC 9610 (JMAP for Contacts / JSContact RFC 9553)**:
  - `Card/get`, `set`, `query`, `AddressBook/get`, `set` handlers, CardDAV round-trip translation.
- [ ] **1.6 RFC 9661 (JMAP for Sieve Scripts)**:
  - `SieveScript/get`, `set`, `test` handlers, ManageSieve protocol client encapsulation.
- [ ] **1.7 RFC 9698 (JMAPACCESS IMAP)**:
  - JMAPACCESS authentication token exchange and IMAP authorization.
- [ ] **1.8 RFC 9749 (JMAP Push VAPID / Web Push)**:
  - Web Push encryption, VAPID key verification, push subscription delivery.

---

### Phase 2: JMAP Mail Sharing (`draft-ietf-jmap-mail-sharing`)
Extend the RFC 9670 sharing framework to Mailboxes, completing collaborative mail access.
Capability: `urn:ietf:params:jmap:mail:share`.
- [ ] **2.1 Mailbox Sharing Data Model & Capability**
  - Advertise `urn:ietf:params:jmap:mail:share` and add `shareWith` (Principal id → `MailboxRights`) and `mayShare` right to Mailbox model.
  - Read `isSubscribed` / `myRights` from upstream IMAP subscription and ACL state.
- [ ] **2.2 IMAP ACL Translation**
  - Map `Mailbox.shareWith` mutations to IMAP `SETACL`/`DELETEACL` and read rights back with `GETACL`/`MYRIGHTS` ([RFC 4314](https://www.rfc-editor.org/rfc/rfc4314.html)), maintaining draft §3.2 rights mapping.
  - Emit `ShareNotification` (RFC 9670) on Mailbox sharing changes; owner MUST NOT appear in `shareWith`.
- [ ] **2.3 Handlers, Traceability & Hermetic Tests**
  - Register `spec/jmap_mail_sharing.go` gated by `TestSpecCoverage` and verify with embedded in-process IMAP/SMTP tests.

---

### Phase 3: JMAP Conditional Set (`draft-ietf-jmap-conditional`)
Add per-object conditional validation to `*/set` (HTTP `If-Match` equivalent) on top of whole-request `ifInState`.
Capability: `urn:ietf:params:jmap:conditional`.
- [ ] **3.1 Capability & Request Semantics**
  - Advertise `urn:ietf:params:jmap:conditional` and accept per-object condition argument on `*/set`.
  - Return standard `stateMismatch` SetError when a per-object condition fails.
- [ ] **3.2 Cross-Domain Coverage**
  - Apply across Mail, Calendar, Contacts, FileNode, and Sieve `*/set` handlers.
- [ ] **3.3 Traceability & Hermetic Tests**
  - Add `spec/jmap_conditional.go` gated by `TestSpecCoverage` covering matched, failed, and atomic batch cases.

---

### Phase 4: Nextcloud Storage Quotas Bridge (RFC 9425)
- [ ] **4.1 WebDAV Storage Quota Translation**
  - Project Nextcloud WebDAV user storage statistics (`quota-available-bytes` / `quota-used-bytes`) into JMAP `urn:ietf:params:jmap:quota` alongside IMAP mail storage quotas.

---

### Phase 5: Nextcloud OCS Sharing Translation (RFC 9670)
- [ ] **5.1 Calendar & AddressBook Sharing Translation**
  - Map `Calendar.shareWith` and `AddressBook.shareWith` mutations to the Nextcloud OCS Share API (`/ocs/v2.php/apps/files_sharing/api/v1/shares`) and WebDAV ACLs ([RFC 3744](https://www.rfc-editor.org/rfc/rfc3744.html)).
  - Enforce cross-account authorization and permission checks (`mayRead`, `mayWrite`, `mayAdmin`).

---

## Deferred / Out of Scope

- **JMAP for Tasks (`draft-ietf-jmap-tasks`)**: Deferred. The IETF draft is expired (-06, March 2023) and no production JMAP clients implement it.
- **JMAP for Notes**: Out of scope. There is no IETF JMAP Notes draft; Nextcloud Notes uses WebDAV.
