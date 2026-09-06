# TODO — Architecture Refactoring, Code Consolidation & RFC Conformance Roadmap

**Authoritative Reference**: See [`AGENTS.md`](./AGENTS.md) for core principles:
1. *Indistinguishable from a real server* (no hardcoded/empty stubs, real change tracking, persistence, correct error objects).
2. *No hardcoded or default usernames in application code*.
3. *RFC 2119 requirement implementation & traceability*.
4. *Standard parsers & encoders only — never ad-hoc parsing*.

---

## Active Roadmap

### Phase 3: JMAP for Tasks (`draft-ietf-jmap-tasks` / RFC 8984 JSCalendar §5)
Bridge Nextcloud CalDAV `VTODO` collections and tasks to JMAP Tasks:

- [ ] **3.1 Session Capability & Data Models**
  - Advertise `urn:ietf:params:jmap:tasks` capability in JMAP session resource.
  - Define `TaskList` model (`id`, `name`, `color`, `sortOrder`, `isDefault`, `shareWith`).
  - Define `Task` model ([RFC 8984 §5](https://www.rfc-editor.org/rfc/rfc8984.html#section-5)) (`id`, `taskListId`, `title`, `description`, `due`, `start`, `estimatedDuration`, `status`, `progress`, `percentComplete`, `priority`, `subtasks`, `recurrenceRules`).

- [ ] **3.2 Nextcloud CalDAV `VTODO` Backend Integration**
  - Implement `TaskList/get`, `TaskList/set`, `TaskList/changes` against Nextcloud CalDAV task collections.
  - Implement `Task/get`, `Task/set`, `Task/query`, `Task/changes` mapped to CalDAV `VTODO` components via `go-webdav/caldav` and `go-ical`.
  - Maintain delta tracking (`created`, `updated`, `destroyed`, `state`) in `jmap/nextcloud/`.

- [ ] **3.3 Method Handlers & Conformance Tests**
  - Register `Task/*` and `TaskList/*` handlers in JMAP method registry.
  - Add requirement traceability matrix in `spec/jmap_tasks.go` gated by `TestSpecCoverage`.
  - Implement dedicated unit test suite in `jmap/` with `spectest.Require` citations.

---

### Phase 4: JMAP for Notes (`draft-ietf-jmap-notes`)
- [ ] **4.1 Session Capability & Data Models**
  - Advertise `urn:ietf:params:jmap:notes` capability.
  - Define `Note` model (`id`, `title`, `content`, `format`, `categories`, `isFavorite`, `updated`).
- [ ] **4.2 Nextcloud Notes Integration**
  - Map `Note/*` methods (`get`, `set`, `query`, `changes`) to Nextcloud WebDAV (`/Notes/`) or Notes REST API.

---

### Phase 5: Storage Quotas Bridge (RFC 9425)
- [ ] **5.1 Nextcloud User Storage Quota Bridge**
  - Project Nextcloud WebDAV user storage statistics (`quota-available-bytes` / `quota-used-bytes`) into JMAP `urn:ietf:params:jmap:quota` alongside mail quotas.

---

### Phase 6: JMAP Sharing ([RFC 9670](https://www.rfc-editor.org/rfc/rfc9670.html))
Support cross-account access and collaborative sharing for mailboxes, calendars, address books, and task lists:

- [ ] **6.1 Data Models & Session Capabilities**
  - Advertise `urn:ietf:params:jmap:principals:owner` in `accountCapabilities` for accounts supporting sharing (RFC 9670 §1.5.2).
  - Define `ShareNotification` data model ([RFC 9670 §2](https://www.rfc-editor.org/rfc/rfc9670.html#section-2)) (`id`, `created`, `changedBy`, `objectType`, `objectAccountId`, `objectId`, `oldRights`, `myRights`, `name`).
  - Extend shared entity models (`Mailbox`, `Calendar`, `AddressBook`, `TaskList`) with `shareWith` (map of `PrincipalId` to rights) and `myRights` properties (RFC 9670 §5).

- [ ] **6.2 ShareNotification Method Handlers & Push Notifications**
  - Implement `ShareNotification/get` ([RFC 9670 §4.1](https://www.rfc-editor.org/rfc/rfc9670.html#section-4.1)).
  - Implement `ShareNotification/changes` ([RFC 9670 §4.1](https://www.rfc-editor.org/rfc/rfc9670.html#section-4.1)).
  - Implement `ShareNotification/query` and `ShareNotification/queryChanges` ([RFC 9670 §4.1](https://www.rfc-editor.org/rfc/rfc9670.html#section-4.1)).
  - Implement `ShareNotification/set` with destroy-only support per [RFC 9670 §4.2](https://www.rfc-editor.org/rfc/rfc9670.html#section-4.2) (create and update rejected with standard error objects).
  - Emit push `StateChange` events (`ShareNotification` type) on sharing mutations (RFC 9670 §3).

- [ ] **6.3 IMAP ACL & Nextcloud Sharing Translation**
  - Map `Mailbox.shareWith` and `Mailbox.myRights` mutations to IMAP ACL commands (`SETACL`, `DELETEACL`, `GETACL`, `MYRIGHTS` per [RFC 4314](https://www.rfc-editor.org/rfc/rfc4314.html)).
  - Map `Calendar.shareWith`, `AddressBook.shareWith`, and `TaskList.shareWith` mutations to Nextcloud OCS Share API (`/ocs/v2.php/apps/files_sharing/api/v1/shares`) and WebDAV ACLs ([RFC 3744](https://www.rfc-editor.org/rfc/rfc3744.html)).
  - Enforce cross-account authorization and permission checks (`mayRead`, `mayWrite`, `mayAdmin`).

- [ ] **6.4 Requirement Conformance Matrix & Test Verification**
  - Add requirement traceability matrix in `spec/jmap_sharing.go` gated by `TestSpecCoverage`.
  - Implement dedicated hermetic unit test suite (`jmap/rfc9670_sharing_test.go`) covering all RFC 2119 normative clauses using `spectest.Require` or `spectest.Cover`.

---

### Phase 7: Specification Accountability & Full RFC Coverage Architecture
Tie all production code to specification clauses and ensure 100% paragraph-level test coverage across all referenced RFCs:

- [ ] **7.1 Automated RFC Paragraph Extractor (`tools/specextract`)**
  - Build automated CLI tool to fetch and parse official IETF RFC text/XML documents (`RFC8620`, `RFC8621`, `RFC8887`, `RFC9007`, `RFC9219`, `RFC9404`, `RFC9425`, `RFC9610`, `RFC9661`, `RFC9670`, `RFC9698`, etc.).
  - Split documents into sections and paragraphs, extracting every normative RFC 2119 keyword clause (`MUST`, `MUST NOT`, `SHOULD`, `SHOULD NOT`, `MAY`).
  - Generate/update Go-native `spec/<spec>.go` definitions with verbatim text, paragraph indices, and deterministic clause IDs (`<SPEC>#<section>-p<para>-<level>`).

- [ ] **7.2 Deterministic Clause IDs & `spectest` Upgrade**
  - Extend `jmap/spectest` with ID-based requirement registration (`spectest.RequireID(t, clauseID, text)`).
  - Map existing tests across `jmap/` and `smtp/` to exact clause IDs.

- [ ] **7.3 Bi-Directional AST Conformance Enforcement (`TestSpecCoverage`)**
  - Enforce **Spec -> Test**: Every `"covered"` clause in the matrix must be referenced by an existing test function containing `spectest.RequireID`.
  - Enforce **Test -> Spec**: Every `spectest.RequireID` call in test code must exist in the canonical matrix (flagging typos and stale IDs).
  - Enforce **No Untracked Tests**: Any test matching `TestRFC*` or feature suites must cite at least one valid spec clause ID.
  - Report complete coverage audit: tally and report all outstanding `MUST`, `SHOULD`, and `MAY` gaps on every `go test` run.

- [ ] **7.4 Production Code Traceability Linter**
  - Standardize source code doc-comment annotations: `// @spec <clause-id>` on exported methods, dispatch handlers, error generators, and validators.
  - Implement static AST analysis in `spec_coverage_test.go` to ensure all registered JMAP method handlers and protocol mappers reference valid spec clauses.
  - Prevent untracked or non-compliant custom protocol logic from entering the codebase.

- [ ] **7.5 Full Specification Ingestion & Coverage Expansion**
  - Ingest Core & Mail: `RFC8620` (Core JMAP) and `RFC8621` (JMAP Mail) full paragraph matrices.
  - Ingest Extensions: `RFC8887` (WebSockets), `RFC9007` (MDN), `RFC9219` (S/MIME), `RFC9404` (Blob Management), `RFC9425` (Quotas), `RFC9610` (Contacts), `RFC9661` (Sieve), `RFC9670` (Sharing), `RFC9698` (JMAPACCESS), `RFC9749` (VAPID).
  - Transition existing 460+ tests to comprehensive clause-level citations.

