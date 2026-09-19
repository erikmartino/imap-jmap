# TODO — Architecture & RFC Conformance Roadmap

**Authoritative Guidelines**: See [`AGENTS.md`](./AGENTS.md) for core principles:
1. *Indistinguishable from a real server* (no stubs, real change tracking, full persistence, standard error objects).
2. *Strict layering & backend encapsulation* (upstream wire protocols encapsulated in clients; domain handlers operate on domain types).
3. *Zero hardcoded or default usernames*.
4. *Standard parsers only — zero ad-hoc formatting or regex scanning*.
5. *RFC 2119 requirement implementation & traceability*.

---

## Active Roadmap

### Phase 1: JMAP for Tasks (`draft-ietf-jmap-tasks` / RFC 8984 JSCalendar §5)
Bridge Nextcloud CalDAV `VTODO` collections and tasks to JMAP Tasks:

- [ ] **1.1 Session Capability & Data Models**
  - Advertise `urn:ietf:params:jmap:tasks` capability in JMAP session resource.
  - Define `TaskList` model (`id`, `name`, `color`, `sortOrder`, `isDefault`, `shareWith`).
  - Define `Task` model ([RFC 8984 §5](https://www.rfc-editor.org/rfc/rfc8984.html#section-5)) (`id`, `taskListId`, `title`, `description`, `due`, `start`, `estimatedDuration`, `status`, `progress`, `percentComplete`, `priority`, `subtasks`, `recurrenceRules`).

- [ ] **1.2 Nextcloud CalDAV `VTODO` Backend Encapsulation**
  - Encapsulate CalDAV `VTODO` operations strictly inside `jmap/nextcloud/client.go` using standard libraries (`github.com/emersion/go-webdav/caldav` and `go-ical`).
  - Provide domain-level methods on `*Client` (`ListTaskLists`, `QueryTaskObjects`, `PutTaskObject`, `DeleteTaskObject`).
  - Implement `TasksBackend` adapter in `jmap/nextcloud/` without exposing wire paths or `.ics` extensions to outer handlers.

- [ ] **1.3 Method Handlers & Conformance Tests**
  - Register `Task/*` and `TaskList/*` handlers in JMAP method registry (`jmap/jmaptasks/`).
  - Add requirement traceability matrix in `spec/jmap_tasks.go` gated by `TestSpecCoverage`.
  - Implement hermetic unit tests with `spectest.Require` citations against embedded reference backend.

---

### Phase 2: JMAP for Notes (`draft-ietf-jmap-notes`)
- [ ] **2.1 Session Capability & Data Models**
  - Advertise `urn:ietf:params:jmap:notes` capability.
  - Define `Note` model (`id`, `title`, `content`, `format`, `categories`, `isFavorite`, `updated`).
- [ ] **2.2 Nextcloud Notes Integration**
  - Map `Note/*` methods (`get`, `set`, `query`, `changes`) to Nextcloud WebDAV (`/Notes/`) or Notes REST API.

---

### Phase 3: Storage Quotas Bridge (RFC 9425)
- [ ] **3.1 Nextcloud User Storage Quota Bridge**
  - Project Nextcloud WebDAV user storage statistics (`quota-available-bytes` / `quota-used-bytes`) into JMAP `urn:ietf:params:jmap:quota` alongside mail quotas.

---

### Phase 4: JMAP Sharing ([RFC 9670](https://www.rfc-editor.org/rfc/rfc9670.html))
Support cross-account access and collaborative sharing for mailboxes, calendars, address books, and task lists:

- [ ] **4.1 Data Models & Session Capabilities**
  - Advertise `urn:ietf:params:jmap:principals:owner` in `accountCapabilities` for accounts supporting sharing (RFC 9670 §1.5.2).
  - Define `ShareNotification` data model ([RFC 9670 §2](https://www.rfc-editor.org/rfc/rfc9670.html#section-2)) (`id`, `created`, `changedBy`, `objectType`, `objectAccountId`, `objectId`, `oldRights`, `myRights`, `name`).
  - Extend shared entity models (`Mailbox`, `Calendar`, `AddressBook`, `TaskList`) with `shareWith` and `myRights` properties (RFC 9670 §5).

- [ ] **4.2 ShareNotification Method Handlers & Push Notifications**
  - Implement `ShareNotification/get`, `changes`, `query`, `queryChanges` ([RFC 9670 §4.1](https://www.rfc-editor.org/rfc/rfc9670.html#section-4.1)).
  - Implement `ShareNotification/set` with destroy-only support per [RFC 9670 §4.2](https://www.rfc-editor.org/rfc/rfc9670.html#section-4.2).
  - Emit push `StateChange` events (`ShareNotification` type) on sharing mutations (RFC 9670 §3).

- [ ] **4.3 IMAP ACL & Nextcloud Sharing Translation**
  - Map `Mailbox.shareWith` and `Mailbox.myRights` mutations to IMAP ACL commands (`SETACL`, `DELETEACL`, `GETACL`, `MYRIGHTS` per [RFC 4314](https://www.rfc-editor.org/rfc/rfc4314.html)).
  - Map `Calendar.shareWith`, `AddressBook.shareWith`, and `TaskList.shareWith` mutations to Nextcloud OCS Share API (`/ocs/v2.php/apps/files_sharing/api/v1/shares`) and WebDAV ACLs ([RFC 3744](https://www.rfc-editor.org/rfc/rfc3744.html)).
  - Enforce cross-account authorization and permission checks (`mayRead`, `mayWrite`, `mayAdmin`).

- [ ] **4.4 Requirement Conformance Matrix & Test Verification**
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

## Completed Milestones

- **Autobahn WebSocket Test Suite (RFC 8887 / RFC 6455)**: Full RFC 8887 framing, binary frame rejection, ping/pong heartbeats, pipelining, and `maxSizeRequest` limits.
- **S/MIME Signature Verification (RFC 9219 / RFC 8551)**: PKCS#7/CMS `SignedData` ASN.1 parser, certificate chain validation, and digest verification.
- **Email Authentication (SPF, DKIM & DMARC)**: SPF evaluation, DKIM canonicalization, DMARC alignment, and RFC 8601 `Authentication-Results:` headers.
- **Web Push ECE & VAPID (RFC 8291 / RFC 9749)**: RFC 8291 KAT test vectors with ECDH P-256 HKDF-SHA256, AES-128-GCM, and VAPID ES256 JWT authorization.
- **Dovecot Pigeonhole Sieve (RFC 5228 / 5804 / 9661)**: Inbound Sieve execution, remote ManageSieve server/client, and JMAP Sieve script CRUD/filtering.
- **IMAP/SMTP Reference Backend (`imapsmtp`)**: Full migration of Mail, Blob, Submission, Identity, VacationResponse, PushSubscription, and Quota to the live IMAP/SMTP gateway backend; retirement of legacy in-memory stores.
- **Package Architecture & Layering**: Decoupled top-level `jmap` into self-contained packages (`jmapcore`, `jmaphandler`, `jmapauth`, `jmapblob`, `jmapmail`, `jmapcalendar`, `jmapcontacts`, `jmapsieve`, `jmapprincipals`, `jmappush`).
