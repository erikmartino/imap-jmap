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
  - Add requirement traceability matrix `docs/conformance/jmap-tasks.json` gated by `TestSpecCoverage`.
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
