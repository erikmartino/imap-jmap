# Architectural Design Plan: IMAP/SMTP Gateway Backend (`jmap/imapsmtp`)

## Executive Summary
This design plan outlines the architecture for adding a **live IMAP/SMTP gateway backend (`imapsmtp`)** to `imap-jmap` alongside the existing in-memory backend (`jmap/memory`). 

Instead of reading/storing messages in a local memory data store, the `imapsmtp` backend dynamically translates JMAP requests into **IMAP operations** (for fetching mailboxes, listing threads, searching, reading headers/bodies, updating flags, creating/deleting folders, moving/destroying emails) and **SMTP commands** (for submitting outbound emails).

Credential context is extracted directly from the incoming JMAP HTTP request (via `Authorization: Bearer` or `Basic`), ensuring **zero state duplication** and **no static user storage**.

---

## 1. High-Level Architecture Overview

```
                      +-----------------------------+
                      |   JMAP Client (Web / CLI)   |
                      +--------------+--------------+
                                     |
                                     | HTTP / JMAP (JSON)
                                     v
                      +-----------------------------+
                      |   imap-jmap JMAP Engine     |
                      |   (Handlers & Envelopes)    |
                      +--------------+--------------+
                                     |
                       MailBackend / BlobBackend
                                     |
                +--------------------+--------------------+
                |                                         |
                v                                         v
    +-----------------------+                 +-----------------------+
    | Memory Backend        |                 | IMAP/SMTP Gateway     |
    | (jmap/memory)         |                 | Backend (jmap/imapsmtp)|
    +-----------------------+                 +-----------+-----------+
                                                          |
                                          +---------------+---------------+
                                          |                               |
                                          | IMAP4rev1/rev2                | SMTP (AUTH PLAIN)
                                          v                               v
                              +-----------------------+       +-----------------------+
                              | Upstream IMAP Server  |       | Upstream SMTP Server  |
                              | (e.g. Dovecot)        |       | (e.g. Postfix / Mock) |
                              +-----------------------+       +-----------------------+
```

---

## 2. Key Architectural Decisions

1. **Authentication & Credential Flow**:
   - **Option C**: Encrypted/authenticated session token containing upstream IMAP/SMTP credentials.
   - For HTTP Basic auth: credentials extracted directly from `Authorization: Basic <base64>`.
   - For Bearer auth: session token decrypts to username/password or upstream OAuth token. Zero persistence of user credentials on gateway disk.

2. **Cross-Folder Querying & Email IDs**:
   - JMAP Email ID format: `<MailboxID>:<UID>` where `MailboxID` is URL-safe base64 of the IMAP mailbox name.
   - Cross-folder querying (`Email/query` without `inMailbox` constraint) aggregates results across all IMAP mailboxes discovered via `LIST`.

3. **Blob Lifecycle & Staging Storage**:
   - **Option B**: Intermediate uncommitted blobs (attachments / drafts uploaded via `POST /upload` before `Email/set` or `EmailSubmission/set`) are staged as draft messages in the user's IMAP `Drafts` mailbox.

4. **Stateless Composite State & Change Tracking**:
   - The JMAP state string (`EmailState`, `MailboxState`) is a **stateless composite token** encoding per-folder IMAP state markers:
     `v1.<base64url(JSON/compact([folder: {UIDVALIDITY, HIGHESTMODSEQ, UIDNEXT, MESSAGES}]))>`
   - On `Email/changes(sinceState)`:
     - The gateway decodes `sinceState` into prior folder markers and compares against current `STATUS` across mailboxes.
     - With IMAP `CONDSTORE`/`QRESYNC`: queries `CHANGES SINCE <modseq>` and `VANISHED` to return exact `created`, `updated`, and `destroyed` diffs.
     - If `sinceState` indicates invalidated `UIDVALIDITY` or unresolvable legacy state: returns `cannotCalculateChanges: true` per RFC 8620 §5.2.
   - **100% Stateless Gateway**: No database or sticky session memory required; survives server restarts and scales across instances seamlessly.

5. **Threading Strategy**:
   - The gateway probes upstream IMAP capabilities for `THREAD=REFERENCES` and `THREAD=ORDEREDSUBJECT` (RFC 5256).
   - If available, IMAP `THREAD` is used directly; otherwise falls back to calculating thread groups via `Message-ID`, `In-Reply-To`, and `References` headers during envelope scans.

6. **Non-Mail Capabilities**:
   - In `imapsmtp` mode, non-mail capabilities (`jmap.ContactBackend`, `jmap.CalendarBackend`, `jmap.SieveBackend`) utilize the in-memory backend initially. Future phases will integrate live CardDAV, CalDAV, and ManageSieve (RFC 5804) adapters.

---

## 3. Core Modules & Component Architecture

The backend is isolated in `jmap/imapsmtp/` and implements the `jmap.MailBackend` and `jmap.BlobBackend` interfaces.

### Modules:
1. **`client_pool.go` (IMAP/SMTP Connection & Session Pool)**:
   - Manages connection lifecycle to external IMAP and SMTP endpoints per JMAP account context.
   - Supports plain TCP, STARTTLS, and direct TLS (e.g. port 993 / 465).
   - Context-aware caching: Reuses connections within request context scope or pooled authenticated connections.

2. **`mailbox_mapper.go` (Mailbox <-> IMAP Folder)**:
   - Maps JMAP Mailbox IDs (URL-safe base64-encoded IMAP folder names) to IMAP mailbox paths (`INBOX`, `Sent`, `Drafts`, `Trash`).
   - Translates `Mailbox/get`, `Mailbox/set` (create, rename, delete) to IMAP commands (`CREATE`, `RENAME`, `DELETE`, `LIST`, `STATUS`).

3. **`email_mapper.go` (Email <-> IMAP Message & UID)**:
   - Maps JMAP Email IDs (`<MailboxID>:<UID>`) to IMAP UIDs.
   - Translates `Email/get` to IMAP `FETCH` (fetching envelopes, headers, MIME structure, or body parts).
   - Translates `Email/query` to IMAP `SEARCH` / `UID SEARCH` with criteria (`SINCE`, `BEFORE`, `FROM`, `HEADER`, `FLAG`).
   - Translates `Email/set` updates (keywords like `$seen`, `$flagged`, `$draft`, `$answered`) to IMAP `STORE` / `UID STORE` (`+FLAGS`, `-FLAGS`).
   - Translates `Email/set` creates/imports to IMAP `APPEND`.

4. **`submission_handler.go` (EmailSubmission <-> SMTP Client)**:
   - Handles JMAP `EmailSubmission/set`.
   - Connects to external SMTP using extracted user credentials.
   - Delivers raw MIME message bytes over SMTP (`MAIL FROM`, `RCPT TO`, `DATA`).
   - Copies sent message to IMAP `Sent` folder via IMAP `APPEND`.

5. **`change_tracker.go` (Composite State & Delta Sync)**:
   - Encodes and decodes composite state tokens.
   - Evaluates mailbox deltas and calculates `created`, `updated`, and `destroyed` lists for `Email/changes` and `Mailbox/changes`.

---

## 4. Detailed Data & Protocol Mapping Rules

### A. Identifier Encoding
- **Mailbox ID**: URL-safe base64 encoding of IMAP folder name (e.g., `INBOX` -> `SU5CT1g`).
- **Email ID**: Composite string format `<MailboxID>:<UID>`.
- **Thread ID**: Mapped to IMAP `THREAD` results or synthesized from `Message-ID` / `In-Reply-To` / `References` headers.

### B. Keywords & Flags
- `$seen` <-> `\Seen`
- `$flagged` <-> `\Flagged`
- `$draft` <-> `\Draft`
- `$answered` <-> `\Answered`
- Custom keywords <-> Custom IMAP keywords / flags.

### C. Outbound Email Submissions
1. Client issues `EmailSubmission/set` referencing an Email ID or blob.
2. `imapsmtp` backend fetches MIME payload from IMAP/Blob staging.
3. Opens authenticated SMTP TLS connection (`smtpHost:smtpPort`) using user credentials.
4. Issues `MAIL FROM` and `RCPT TO` for all envelope addresses and transfers `DATA`.
5. Appends the message to the user's `Sent` folder on IMAP.
6. Returns created `EmailSubmission` object with `sendAt` timestamp and status.

---

## 5. Configuration & Server Setup

Server selection between `memory` and `imapsmtp` is governed by environment flags or initialization options:

```
BACKEND_TYPE=imapsmtp          # Options: "memory", "imapsmtp"
IMAP_SERVER=dovecot:143        # External IMAP server host:port
SMTP_SERVER=smtp:25            # External SMTP server host:port
IMAP_TLS=false
SMTP_TLS=false
```

---

## 6. Implementation Phases & Milestones

| Phase | Description | Deliverables |
| :--- | :--- | :--- |
| **Phase 1: Core Client Pool & Authentication** | Dynamic IMAP/SMTP connection pooling with request-context credential extraction. | `jmap/imapsmtp/client_pool.go` |
| **Phase 2: Mailbox Operations** | Implement `Mailbox/get`, `Mailbox/set` (create, rename, delete) over IMAP `LIST`/`STATUS`/`CREATE`. | `jmap/imapsmtp/mailbox.go` |
| **Phase 3: Email Fetching & Querying** | Implement `Email/get`, `Email/query` over IMAP `FETCH` and `SEARCH` across folders. | `jmap/imapsmtp/email_read.go` |
| **Phase 4: Email Mutation & Flags** | Implement `Email/set` (update flags/keywords, move, delete) over IMAP `STORE`/`COPY`/`EXPUNGE`. | `jmap/imapsmtp/email_write.go` |
| **Phase 5: State & Delta Sync** | Implement composite state tokens and `Email/changes`, `Mailbox/changes` tracking. | `jmap/imapsmtp/change_tracker.go` |
| **Phase 6: SMTP Outbound Submission & Blobs** | Implement `EmailSubmission/set` over SMTP and staging draft blobs in IMAP `Drafts`. | `jmap/imapsmtp/submission.go`, `jmap/imapsmtp/blob.go` |
| **Phase 7: Integration & Test Coverage** | Dedicated unit test suite with live IMAP (Dovecot) & SMTP (Mock-SMTP) backends. | `jmap/imapsmtp/*_test.go` |

---

## 7. Spec Compliance & Architectural Constraints Verification
- **No Hardcoded Usernames**: Credentials extracted strictly from request context.
- **Data Loss Prevention**: Partial flag updates in `Email/set` mutate only explicitly provided keywords.
- **Standard Parsers**: MIME parsing via `go-message`, IMAP protocol handling via `go-imap`.
