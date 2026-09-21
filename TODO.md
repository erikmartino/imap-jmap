Active session

agy --conversation=a57c9712-9454-4ec8-848b-1dbeddc72cc1


# TODO — Architecture & RFC Conformance Roadmap

**Authoritative Guidelines**: See [`AGENTS.md`](./AGENTS.md) for core principles:
1. *Indistinguishable from a real server* (no stubs, real change tracking, full persistence, standard error objects).
2. *Strict layering & backend encapsulation* (upstream wire protocols encapsulated in clients; domain handlers operate on domain types).
3. *Zero hardcoded or default usernames*.
4. *Standard parsers only — zero ad-hoc formatting or regex scanning*.
5. *RFC 2119 requirement implementation & traceability*.

---

## Active Roadmap

### Phase 1: JMAP Mail Sharing (`draft-ietf-jmap-mail-sharing`)
Extend the RFC 9670 sharing framework to Mailboxes, completing the mail side of Phase 4.
Capability: `urn:ietf:params:jmap:mail:share`.

- [ ] **1.1 Mailbox Sharing Data Model & Capability**
  - Advertise `urn:ietf:params:jmap:mail:share` and add `shareWith` (Principal id → `MailboxRights`) and the `mayShare` right to the Mailbox model.
  - Read `isSubscribed` / `myRights` from the upstream store (IMAP subscription + ACL state).
- [ ] **1.2 IMAP ACL Translation**
  - Map `Mailbox.shareWith` mutations to `SETACL`/`DELETEACL` and read rights back with `GETACL`/`MYRIGHTS` ([RFC 4314](https://www.rfc-editor.org/rfc/rfc4314.html)), keeping the `MailboxRights` ↔ IMAP ACL right mapping consistent (draft §3.2).
  - Emit `ShareNotification` (RFC 9670) on Mailbox sharing changes; the owner MUST NOT appear in `shareWith`.
- [ ] **1.3 Handlers, Traceability & Hermetic Tests**
  - Reuse the `ShareNotification` methods already implemented for calendars.
  - Add `spec/jmap_mail_sharing.go` gated by `TestSpecCoverage` and hermetic tests via the embedded IMAP/SMTP backend.

---

### Phase 2: JMAP Conditional Set (`draft-ietf-jmap-conditional`)
Add a finer, per-object conditional mechanism to `*/set` (an HTTP `If-Match` equivalent) on top of the whole-request `ifInState`. Capability: `urn:ietf:params:jmap:conditional`.

- [ ] **2.1 Capability & Request Semantics**
  - Advertise `urn:ietf:params:jmap:conditional` and accept its per-object condition argument on `*/set`.
  - Return the standard `stateMismatch` error (and per-object `notCreated`/`notUpdated`/`notDestroyed`) when a condition fails.
- [ ] **2.2 Cross-Domain Coverage**
  - Apply to Mail, Calendar, Contacts, FileNode, and Sieve `*/set` handlers so no advertised data type is left behind.
- [ ] **2.3 Traceability & Hermetic Tests**
  - Add `spec/jmap_conditional.go` gated by `TestSpecCoverage` and cover matched/failed/atomic cases.

---

### Phase 3: Storage Quotas Bridge (RFC 9425)
- [ ] **3.1 Nextcloud User Storage Quota Bridge**
  - Project Nextcloud WebDAV user storage statistics (`quota-available-bytes` / `quota-used-bytes`) into JMAP `urn:ietf:params:jmap:quota` alongside mail quotas.

---

### Phase 4: JMAP Sharing ([RFC 9670](https://www.rfc-editor.org/rfc/rfc9670.html))
Support cross-account access and collaborative sharing for mailboxes, calendars, and address books:

- [ ] **4.1 Data Models & Session Capabilities**
  - Advertise `urn:ietf:params:jmap:principals:owner` in `accountCapabilities` for accounts supporting sharing (RFC 9670 §1.5.2).
  - Define `ShareNotification` data model ([RFC 9670 §2](https://www.rfc-editor.org/rfc/rfc9670.html#section-2)) (`id`, `created`, `changedBy`, `objectType`, `objectAccountId`, `objectId`, `oldRights`, `newRights`, `name`).
  - Extend shared entity models (`Mailbox`, `Calendar`, `AddressBook`) with `shareWith` and `myRights` properties (RFC 9670 §5).

- [x] **4.2 ShareNotification Method Handlers & Push Notifications**
  - Implement `ShareNotification/get`, `changes`, `query`, `queryChanges` ([RFC 9670 §3.1–§3.5](https://www.rfc-editor.org/rfc/rfc9670.html#section-3.1)).
  - Implement `ShareNotification/set` with destroy-only support per [RFC 9670 §3.3](https://www.rfc-editor.org/rfc/rfc9670.html#section-3.3).
  - Emit push `StateChange` events (`ShareNotification` type) on sharing mutations (RFC 9670 §3).

- [ ] **4.3 Nextcloud Sharing Translation**
  - Mailbox sharing via IMAP ACL (`SETACL`/`DELETEACL`/`GETACL`/`MYRIGHTS`, [RFC 4314](https://www.rfc-editor.org/rfc/rfc4314.html)) is covered by Phase 1 (`draft-ietf-jmap-mail-sharing`).
  - Map `Calendar.shareWith` and `AddressBook.shareWith` mutations to the Nextcloud OCS Share API (`/ocs/v2.php/apps/files_sharing/api/v1/shares`) and WebDAV ACLs ([RFC 3744](https://www.rfc-editor.org/rfc/rfc3744.html)).
  - Enforce cross-account authorization and permission checks (`mayRead`, `mayWrite`, `mayAdmin`).

- [x] **4.4 Requirement Conformance Matrix & Test Verification**
  - Add requirement traceability matrix in `spec/jmap_sharing.go` gated by `TestSpecCoverage`.
  - Implement dedicated hermetic unit test suite (`jmap/rfc9670_sharing_test.go`) covering all RFC 2119 normative clauses using `spectest.Require` or `spectest.Cover`.

---

### Phase 5: Specification Accountability & AST Traceability Architecture
Tie all production code to specification clauses and ensure 100% test coverage across all referenced RFCs:

- [ ] **5.1 Automated RFC Paragraph Extractor (`tools/specextract`)**
  - Build automated CLI tool to fetch and parse official IETF RFC text/XML documents (`RFC8620`, `RFC8621`, `RFC8887`, `RFC9007`, `RFC9219`, `RFC9404`, `RFC9425`, `RFC9610`, `RFC9661`, `RFC9670`, `RFC9698`, etc.).
  - Extract normative RFC 2119 clauses and generate Go-native `spec/<spec>.go` definitions with deterministic clause IDs.

- [ ] **5.2 Bi-Directional AST Conformance Enforcement (`TestSpecCoverage`)**
  - Enforce **Spec -> Test**: Every `"covered"` clause in the matrix must be referenced by an existing test function containing `spectest.RequireID`.
  - Enforce **Test -> Spec**: Every `spectest.RequireID` call in test code must exist in the canonical matrix.
  - Report complete coverage audit: tally and report all outstanding `MUST`, `SHOULD`, and `MAY` gaps on every test run.


---

## Deferred / Out of Scope

- **JMAP for Tasks (`draft-ietf-jmap-tasks`)**: deferred. The IETF draft is **expired** (last revision `-06`, March 2023) and no JMAP client implements it. Revisit only if the WG republishes a live document and a real client (e.g. Bulwark) adopts it. If revived, the plan is to bridge Nextcloud CalDAV `VTODO` collections to `TaskList`/`Task` under the same layering rules as calendars (CalDAV fully encapsulated in `jmap/nextcloud/client.go`).
- **JMAP for Notes**: out of scope. There is **no IETF JMAP Notes draft** (`draft-ietf-jmap-notes` does not exist in the IETF datatracker); Nextcloud Notes is WebDAV-based, not JMAP.
