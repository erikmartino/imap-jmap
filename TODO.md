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

### Phase 1: JMAP Standards RFC Conformance (Zero MUST Gaps Target)
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
