# Potential Nextcloud Services Exposable via JMAP

This document outlines additional Nextcloud services, applications, and APIs that can be bridged and exposed through the JMAP protocol (JSON Meta Application Protocol) alongside the existing Mail, Calendars (CalDAV), Contacts (CardDAV), and File Storage (WebDAV) implementations.

---

## 1. Tasks & To-Dos (`draft-ietf-jmap-tasks` / RFC 8984 JSCalendar)
- **Nextcloud Component**: Nextcloud Tasks app / CalDAV `VTODO` collections.
- **JMAP Protocol Equivalent**: `urn:ietf:params:jmap:tasks` / JSCalendar `Task` objects (RFC 8984 Section 5).
- **Capabilities & Methods**:
  - `Task/get`, `Task/set`, `Task/query`, `Task/changes`.
  - Task lists mapped to CalDAV task collections (`TaskList/get`, `TaskList/set`).
  - Native tracking of subtasks, percentComplete, progress status, priorities, alarms, and recurrence.
- **Integration Approach**:
  - Connected over CalDAV using `github.com/emersion/go-webdav/caldav` querying `VTODO` components on Nextcloud calendar/task collections.

---

## 2. Notes & Snippets (`draft-ietf-jmap-notes`)
- **Nextcloud Component**: Nextcloud Notes app (stored as Markdown/text files in `/Notes/` via WebDAV and exposed via the Notes REST API).
- **JMAP Protocol Equivalent**: `urn:ietf:params:jmap:notes` / JMAP Note objects.
- **Capabilities & Methods**:
  - `Note/get`, `Note/set`, `Note/query`, `Note/changes`.
  - Properties: `id`, `title`, `content`, `format` (markdown/plain), `categories`, `isFavorite`, `updated`.
- **Integration Approach**:
  - Nextcloud WebDAV file access under `/remote.php/dav/files/{user}/Notes/` or direct integration with Nextcloud Notes REST API (`/index.php/apps/notes/api/v1/notes`).

---

## 3. Deck & Project Management (Kanban Boards)
- **Nextcloud Component**: Nextcloud Deck app (OCS Deck REST API `/index.php/apps/deck/api/v1.0/`).
- **JMAP Protocol Equivalent**: Custom JMAP Deck/Board Extension (`urn:ietf:params:jmap:boards` or JMAP Tasks with board metadata).
- **Capabilities & Methods**:
  - `Board/get`, `Board/set`, `Stack/get`, `Stack/set`, `Card/get`, `Card/set`.
  - Properties: board permissions, stack columns, card labels, assignments, due dates, checklists, and attachments.
- **Integration Approach**:
  - Map OCS REST endpoints to high-performance batch JMAP methods with `StateChange` push events over WebSocket / SSE.

---

## 4. Bookmarks (`urn:ietf:params:jmap:bookmarks`)
- **Nextcloud Component**: Nextcloud Bookmarks app (REST API `/index.php/apps/bookmarks/public/rest/v2/bookmark`).
- **JMAP Protocol Equivalent**: JMAP Bookmarks / Link Library Extension.
- **Capabilities & Methods**:
  - `Bookmark/get`, `Bookmark/set`, `Bookmark/query`, `Bookmark/changes`.
  - Properties: `url`, `title`, `description`, `tags`, `isArchived`, `faviconBlobId`.
- **Integration Approach**:
  - Proxy to Nextcloud Bookmarks REST API with cached change tokens and JMAP result references.

---

## 5. User Status, Principals & Availability (`draft-ietf-jmap-principals`)
- **Nextcloud Component**: Nextcloud User Status app & OCS User Status API (`/ocs/v2.php/apps/user_status/api/v1/user_status`).
- **JMAP Protocol Equivalent**: `urn:ietf:params:jmap:principals`, `urn:ietf:params:jmap:principals:availability`.
- **Capabilities & Methods**:
  - `Principal/get`, `Principal/query`, `Availability/get`.
  - Status messages, clear-at timestamps, predefined icons (online, away, dnd, offline), and busy/free intervals.
- **Integration Approach**:
  - Sync status between JMAP Principal objects and Nextcloud OCS User Status endpoints.

---

## 6. Storage Quotas (RFC 9425 `urn:ietf:params:jmap:quota`)
- **Nextcloud Component**: Nextcloud User Storage & Quota subsystem (OCS User Provisioning API `/ocs/v1.php/cloud/users/{user}`).
- **JMAP Protocol Equivalent**: RFC 9425 JMAP for Quotas.
- **Capabilities & Methods**:
  - `Quota/get`, `Quota/changes`, `Quota/query`.
  - Exposes `used`, `limit`, `resourceType: "octets"`, `scope: "account"`, `warnLimit`, and `softLimit`.
- **Integration Approach**:
  - Query Nextcloud user storage statistics via OCS or PROPFIND `quota-available-bytes` / `quota-used-bytes` and project into RFC 9425 Quota objects.

---

## 7. File Shares & Access Control (`urn:ietf:params:jmap:filenode`)
- **Nextcloud Component**: Nextcloud OCS Share API (`/ocs/v2.php/apps/files_sharing/api/v1/shares`).
- **JMAP Protocol Equivalent**: JMAP FileNode with share descriptors (`FileShare/get`, `FileShare/set`).
- **Capabilities & Methods**:
  - Public links, password-protected shares, user/group shares, expiration dates, and permission masks (read, write, reshare).
- **Integration Approach**:
  - Map Nextcloud OCS Share API onto JMAP FileNode sharing properties and methods.

---

## 8. Notifications & Activity Streams
- **Nextcloud Component**: Nextcloud Notifications app (`/ocs/v2.php/apps/notifications/api/v2/notifications`) and Activity app (`/ocs/v2.php/apps/activity/api/v2/activity`).
- **JMAP Protocol Equivalent**: `Notification/get`, `Notification/set`, `Activity/get` with JMAP Push Event delivery.
- **Capabilities & Methods**:
  - Rich notifications (actions, links, dismissals), activity logs, and real-time state change events over `/eventsource` and WebSockets.
- **Integration Approach**:
  - Poll or webhook OCS Notification API to push events via `jmap.Broadcaster`.

---

## 9. Nextcloud Talk (Chat & Messaging)
- **Nextcloud Component**: Nextcloud Talk / Spreed API (`/ocs/v2.php/apps/spreed/api/v1/chat/{token}`).
- **JMAP Protocol Equivalent**: JMAP Instant Messaging Extension (`urn:ietf:params:jmap:chat` / `Conversation` and `ChatMessage` models).
- **Capabilities & Methods**:
  - `Conversation/get`, `ChatMessage/get`, `ChatMessage/set`, `ChatMessage/query`.
  - Room discovery, message dispatch, typing indicators, and read receipts.
- **Integration Approach**:
  - Translate JMAP chat payloads to Nextcloud Talk room messages with real-time SSE event subscriptions.

---

## 10. Unified Cross-Domain Search
- **Nextcloud Component**: Nextcloud Unified Search API (`/ocs/v2.php/search/providers`).
- **JMAP Protocol Equivalent**: Cross-type JMAP query or SearchProvider capability.
- **Capabilities & Methods**:
  - Single search query returning structured matches across Mail, Files, Contacts, Events, and Tasks.
- **Integration Approach**:
  - Combine local IMAP search indexes with Nextcloud OCS search provider queries.
