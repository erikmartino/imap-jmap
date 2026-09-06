# TODO — Architecture Refactoring, Code Consolidation & RFC Conformance Roadmap

**Authoritative Reference**: See [`AGENTS.md`](./AGENTS.md) for core principles:
1. *Indistinguishable from a real server* (no hardcoded/empty stubs, real change tracking, persistence, correct error objects).
2. *No hardcoded or default usernames in application code*.
3. *RFC 2119 requirement implementation & traceability*.
4. *Standard parsers & encoders only — never ad-hoc parsing*.

Previous task logs preserved in [`TODO_PREVIOUS.md`](./TODO_PREVIOUS.md).

---

## Active Roadmap

### Phase 1: Complete Elimination of `testmock` via In-Process Nextcloud & Reference Backends
Following the successful retirement of `jmap/memory/` in favor of `imapsmtp` in Phase 0, replace all artificial `testmock` stores with production adapters running in-process:

- [x] **1.1 In-Process Nextcloud Server (`jmap/nextcloud/embedded.go`)**
  - Implement `nextcloud.NewEmbeddedBackend(usernames ...string) (*Client, *CalendarsBackend, *ContactsBackend, *FileNodeBackend, *PrincipalsBackend, func())`.
  - Spin up an in-process `httptest.Server` mounting:
    - **CalDAV** (`github.com/emersion/go-webdav/caldav.Handler`) for calendar events (RFC 4791 / RFC 8984) and principal discovery (`current-user-principal`, `calendar-home-set`).
    - **CardDAV** (`github.com/emersion/go-webdav/carddav.Handler`) for address books and cards (RFC 6352 / RFC 9553) and addressbook home sets.
    - **WebDAV** (`golang.org/x/net/webdav` / `webdav.NewMemFS()`) for root WebDAV file storage (`/remote.php/webdav/`).
    - **OCS API Handler** (`/ocs/v1.php/cloud/users`, `/groups`) with standard OCS JSON envelope for user provisioning and group lookup.
  - Sub-millisecond startup, hermetic in-process execution with 0 external daemons.

- [x] **1.2 Real State & Delta Sync in `jmap/nextcloud`**
  - Implement real delta tracking (`created`, `updated`, `destroyed`, `cannotCalculateChanges`) in [`jmap/nextcloud/calendars.go`](./jmap/nextcloud/calendars.go) (`CalendarEventChanges`, `CalendarChanges`).
  - Implement real delta tracking in [`jmap/nextcloud/contacts.go`](./jmap/nextcloud/contacts.go) (`CardChanges`, `AddressBookChanges`).
  - Implement real delta tracking in [`jmap/nextcloud/filenode.go`](./jmap/nextcloud/filenode.go) (`FileNodeChanges`).
  - Implement real delta tracking in [`jmap/nextcloud/principals.go`](./jmap/nextcloud/principals.go) (`PrincipalChanges`).

- [x] **1.3 Migrate JMAP Server & Test Suites to Nextcloud Backends**
  - Update [`jmap/test_helper_test.go`](./jmap/test_helper_test.go) (`newTestServer`) to wire `nextcloud.NewEmbeddedBackend` for Calendars, Contacts, FileNodes, and Principals.
  - Verify all 45 Calendar tests ([`jmap/rfc8984_*_test.go`](./jmap/)) and Card tests ([`jmap/rfc9610_*_test.go`](./jmap/)) pass against the Nextcloud adapter.

- [x] **1.4 Reference Backends for Remaining Stores**
  - Consolidate Sieve backend into an in-process Sieve store or reference implementation.
  - Consolidate Auth and IMAPAccess backends into `imapsmtp` or reference stores.

- [x] **1.5 Complete Deletion of `jmap/testmock/`**
  - Verify zero imports across the entire repository.
  - Delete `jmap/testmock/` directory completely.

---

### Phase 2: External Test Suites & Conformance Verification
- [ ] **2.1 `jmapio/jscontact-tests` (Python)**
  - Execute JSContact ↔ vCard conversion vectors against `/convert` endpoint (RFC 9553 / RFC 9555).
- [ ] **2.2 MIME Torture Test Suite**
  - Execute malformed/nested MIME torture suite against `Email/parse` and `smtp.Receiver`.
