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

## 2. Core Modules & Component Architecture

The backend will be isolated in the package `jmap/imapsmtp/` and implement the `jmap.MailBackend` and `jmap.BlobBackend` interfaces.

### Modules:
1. **`client_pool.go` (IMAP/SMTP Connection & Session Pool)**:
   - Manages connection lifecycle to external IMAP and SMTP endpoints per JMAP account context.
   - Leverages `github.com/emersion/go-imap/v2` (or `v1`) and `github.com/emersion/go-sasl` / `net/smtp`.
   - Context-aware caching: Reuses connections within request context scope or pooled idle connections authenticated with the user's credentials.

2. **`mailbox_mapper.go` (Mailbox <-> IMAP Folder)**:
   - Maps JMAP Mailbox IDs (e.g. base64-encoded IMAP folder names) to IMAP mailbox paths (`INBOX`, `Sent`, `Drafts`, `Trash`).
   - Translates `Mailbox/get`, `Mailbox/set` (create, rename, delete) to IMAP commands (`CREATE`, `RENAME`, `DELETE`, `LIST`, `STATUS`).

3. **`email_mapper.go` (Email <-> IMAP Message & UID)**:
   - Maps JMAP Email IDs (e.g., `<MailboxID>:<UID>`) to IMAP UIDs.
   - Translates `Email/get` to IMAP `FETCH` (fetching envelopes, headers, MIME structure, or body parts).
   - Translates `Email/query` to IMAP `SEARCH` / `UID SEARCH` with criteria (`SINCE`, `BEFORE`, `FROM`, `HEADER`, `FLAG`).
   - Translates `Email/set` updates (keywords like `$seen`, `$flagged`, `$draft`) to IMAP `STORE` / `UID STORE` (`+FLAGS`, `-FLAGS`).
   - Translates `Email/set` creates/imports to IMAP `APPEND`.

4. **`submission_handler.go` (EmailSubmission <-> SMTP Client)**:
   - Handles JMAP `EmailSubmission/set`.
   - Connects to the external SMTP server using the extracted user credentials.
   - Delivers the raw MIME message bytes over SMTP (`MAIL FROM`, `RCPT TO`, `DATA`).
   - Copies the sent message to the IMAP `Sent` folder via IMAP `APPEND` if required.

5. **`change_tracker.go` (State & High-Water Mark Tracking)**:
   - Maps IMAP `HIGHESTMODSEQ` / `UIDNEXT` / `UIDVALIDITY` (per RFC 7162 CONDSTORE/QRESYNC if supported) or synthesized sequence counters to JMAP `EmailState` and `MailboxState`.

---

## 3. Detailed Data & Protocol Mapping Rules

### A. Identifier Encoding
- **Mailbox ID**: URL-safe base64 encoding of the IMAP folder name (e.g., `INBOX` -> `SU5CT1g`).
- **Email ID**: Composite string format `<MailboxID>:<UID>` or RFC 8620-compliant opaque hash mapped to `(folder, uid)`.
- **Thread ID**: Mapped to IMAP `THREAD` extensions (RFC 5256) or derived from the `Message-ID` / `In-Reply-To` / `References` headers during envelope fetch.

### B. Keywords & Flags
- `$seen` <-> `\Seen`
- `$flagged` <-> `\Flagged`
- `$draft` <-> `\Draft`
- `$answered` <-> `\Answered`
- `$phishing` / custom keywords <-> Custom IMAP keywords / flags.

### C. Outbound Email Submissions
1. Client issues `EmailSubmission/set` referencing an Email ID or blob.
2. `imapsmtp` backend fetches the MIME payload from IMAP/Blob storage.
3. Opens an authenticated SMTP TLS connection (`smtpHost:smtpPort`) using user credentials.
4. Issues `MAIL FROM` and `RCPT TO` for all envelope addresses and transfers `DATA`.
5. Appends the message to the user's `Sent` folder on IMAP.
6. Returns created `EmailSubmission` object with `sendAt` timestamp and status.

---

## 4. Configuration & Server Setup

The server selection between `memory` and `imapsmtp` will be governed by environment flags or initialization options:

```go
// Environment configuration
BACKEND_TYPE=imapsmtp          # Options: "memory", "imapsmtp"
IMAP_SERVER=dovecot:143        # External IMAP server host:port
SMTP_SERVER=smtp:25            # External SMTP server host:port
IMAP_TLS=false
SMTP_TLS=false
```

---

## 5. Implementation Phases & Milestones

| Phase | Description | Deliverables |
| :--- | :--- | :--- |
| **Phase 1: Core Client Pool & Authentication** | Implement dynamic IMAP/SMTP connection pooling with request-context credential extraction. | `jmap/imapsmtp/client_pool.go` |
| **Phase 2: Mailbox Operations** | Implement `Mailbox/get`, `Mailbox/set` (create, rename, delete) over IMAP `LIST`/`STATUS`/`CREATE`. | `jmap/imapsmtp/mailbox.go` |
| **Phase 3: Email Fetching & Querying** | Implement `Email/get`, `Email/query` over IMAP `FETCH` and `SEARCH`. | `jmap/imapsmtp/email_read.go` |
| **Phase 4: Email Mutation & Flags** | Implement `Email/set` (update flags/keywords, move, delete) over IMAP `STORE`/`COPY`/`EXPUNGE`. | `jmap/imapsmtp/email_write.go` |
| **Phase 5: SMTP Outbound Submission** | Implement `EmailSubmission/set` over external SMTP with automatic IMAP `Sent` folder append. | `jmap/imapsmtp/submission.go` |
| **Phase 6: Integration & Test Coverage** | Dedicated unit test suite with live IMAP (Dovecot) & SMTP (Mock-SMTP) backends. | `jmap/imapsmtp/*_test.go` |

---

## 6. Spec Compliance & Architectural Constraints Verification
- **No Hardcoded Usernames**: Credentials extracted strictly from request context.
- **Data Loss Prevention**: Partial flag updates in `Email/set` mutate only explicitly provided keywords.
- **Standard Parsers**: MIME parsing via `go-message`, IMAP protocol handling via `go-imap`.
