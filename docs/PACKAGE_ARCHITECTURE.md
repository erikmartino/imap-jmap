# JMAP Package Layered Architecture Refactoring

## Problem

The top-level `jmap` package is a monolith that plays too many roles simultaneously:

- **Core JMAP types** (Id, SetError, envelope) — partly extracted to `jmapcore`
- **Handler infrastructure** (MethodHandler, MethodRegistry, parseProperties, filterList, nilIfEmpty, newSetCreationRefs, SourceAccountContext, ValidateGetLimits, ValidateSetLimits) — still in `jmap`
- **Auth context helpers** (AccountIDFromContext, PrincipalAccountIDFromContext, AccountResolver) — still in `jmap`
- **Backend interfaces** (MailBackend, BlobBackend, PrincipalsBackend, ContactsBackend, SieveBackend, …) — still in `jmap`
- **Domain types** (CalendarEvent, Calendar, JSCalendarParticipant, …) — partially extracted to `jmapcalendar`
- **Domain handlers** (calendar, mail, contacts, sieve, filenode, principals, …) — still in `jmap`

Because the handler infrastructure and backend interfaces live in the top-level `jmap` package, domain sub-packages (like `jmapcalendar`) cannot import them without creating a circular dependency: `jmap` → `jmapcalendar` ← handlers need `jmap`.

The result is that domain-specific handler files that belong logically in their domain sub-package (e.g. `calendar_event_handlers.go` in `jmapcalendar`) are instead left in the flat `jmap` package.

## Goal

A clean, layered architecture where:
1. Each layer only imports **downward**
2. Domain packages are **fully self-contained** (types + backend interface + handlers)
3. The top-level `jmap` package becomes a **thin wiring layer** (server, session, registration, re-exports)
4. All external callers (`nextcloud`, `smtp`, `main.go`, tests) are updated to import the right sub-package directly, removing type aliases from `jmap` once migration is complete

## Proposed Layer Map

```
Layer 0 — jmapcore
    Id, SetError, MethodErrorArgs, Invocation, Request, Response, ResultReference,
    CreationRefs, PatchObject, QueryArgs

Layer 1 — jmaphandler  [NEW]
    MethodHandler, MethodRegistry
    parseProperties, filterList
    nilIfEmpty, newSetCreationRefs, resolvePatchCreationRefs, resolveCreationID,
    runCreateLoop
    SourceAccountContext, mergeCopyOverrides
    ValidateGetLimits, ValidateSetLimits
    (imports: jmapcore)

Layer 2 — jmapauth  [NEW]
    AuthCredentials, AuthBackend, TokenCredentialsExtractor
    AccountIDForSubject, SubjectForAccountID
    AccountIDFromContext, ContextWithAccountID
    SubjectFromContext, ContextWithSubject
    CredentialsFromContext, ContextWithCredentials
    PrincipalAccountIDFromContext, ContextWithPrincipalAccountID
    AccountResolver, PermissionGuard, SelfAccessGuard, PrimaryDomainResolver
    (imports: jmapcore)

Layer 2 — jmapblob  [EXTEND existing]
    BlobBackend, BlobReferenceBackend (move from jmap/backend.go)
    Blob (already here)
    ErrBlobNotFound
    (imports: jmapcore)

Layer 2 — jmapmail  [NEW]
    MailBackend, SMTPAvailableBackend, OutboundMailSender, OutboundDeliveryResult
    Email, Mailbox, Thread, Identity, VacationResponse, EmailSubmission,
    MDN, SmimeVerificationResult, PushSubscription, Quota
    (imports: jmapcore)

Layer 2 — jmapprincipals  [NEW]
    PrincipalsBackend
    Principal, AvailabilityWindow
    (imports: jmapcore)

Layer 2 — jmapcalendar  [EXTEND existing]
    Already has: CalendarsBackend, all calendar types, CalendarsCapability
    Add: calendar_handlers.go, calendar_event_handlers.go,
         calendar_notification_handlers.go, share_notification_handlers.go,
         calendar_recurrence.go, calendar_utils.go,
         ical.go, ical_encode.go, ical_text_escaping.go, itip.go, scheduling.go
    (imports: jmapcore, jmaphandler, jmapauth, jmapblob, jmapmail, jmapprincipals, jmapcopy)

Layer 2 — jmapcontacts  [NEW]
    ContactsBackend, ContactsCapability
    AddressBook, Card
    contacts_handlers.go, contacts_filter.go, contacts_types.go
    (imports: jmapcore, jmaphandler, jmapauth, jmapblob)

Layer 2 — jmapsieve  [NEW]
    SieveBackend, SieveCapability
    SieveScript
    sieve_handlers.go, sieve_types.go
    (imports: jmapcore, jmaphandler, jmapauth)

Layer 3 — jmap  [THIN WIRING]
    Server, session, route registration
    Re-exports (type aliases) for backward compat during migration
    (imports: all layer 0–2 packages)
```

## Implementation Steps

Each step is independently buildable and testable. All steps preserve backward compatibility via type aliases until explicitly cleaned up.

### Step 1 — Create `jmaphandler`
Move from `jmap/`:
- `methods.go` → MethodHandler, MethodRegistry, aliasMethod, handleCoreEcho
- `properties.go` → parseProperties, filterProperties, filterList
- `creationref.go` → nilIfEmpty, newSetCreationRefs, resolvePatchCreationRefs, resolveCreationID, runCreateLoop (and re-export CreationRefs alias)
- `copy.go` → SourceAccountContext, mergeCopyOverrides (non-copy-protocol parts)
- Calendar/core limits → ValidateGetLimits, ValidateSetLimits

Re-export in `jmap` via type/var aliases.

### Step 2 — Create `jmapauth`
Move from `jmap/auth.go` and `jmap/authz.go`:
- AuthCredentials, AuthBackend, TokenCredentialsExtractor
- All AccountID/Subject context helpers
- AccountResolver, PermissionGuard, SelfAccessGuard, PrimaryDomainResolver

Re-export in `jmap` via type aliases.

### Step 3 — Move `BlobBackend` into `jmapblob`
Move `BlobBackend`, `BlobReferenceBackend`, `ErrBlobNotFound` from `jmap/backend.go` into `jmap/jmapblob/`.
Re-export in `jmap/backend.go` via type alias.

### Step 4 — Create `jmapmail`
Move from `jmap/backend.go` and `jmap/mail_types.go`, `jmap/keywords.go`:
- MailBackend, SMTPAvailableBackend, OutboundMailSender, OutboundDeliveryResult
- Email, Mailbox, Thread, Identity, VacationResponse, EmailSubmission, MDN, SmimeVerificationResult, PushSubscription, Quota

Re-export in `jmap` via type aliases.

### Step 5 — Create `jmapprincipals`
Move from `jmap/backend.go` and `jmap/principals_types.go`:
- PrincipalsBackend
- Principal, AvailabilityWindow

Re-export in `jmap` via type aliases.

### Step 6 — Move calendar handlers into `jmapcalendar`
Now that all dependencies are in sub-packages, move:
- `calendar_handlers.go`
- `calendar_event_handlers.go`
- `calendar_notification_handlers.go`
- `share_notification_handlers.go`
- `calendar_recurrence.go`
- `calendar_utils.go`
- `ical.go`, `ical_encode.go`, `ical_text_escaping.go`
- `itip.go`
- `scheduling.go`

`jmap/calendar_handlers.go` becomes a thin shim that calls `jmapcalendar.RegisterCalendarHandlers(...)`.

### Step 7 — Move contacts handlers into `jmapcontacts`
Move `contacts_handlers.go`, `contacts_filter.go`, `contacts_types.go`, `vcardconv/`.

### Step 8 — Move sieve handlers into `jmapsieve`
Move `sieve_handlers.go`, `sieve_types.go`.

### Step 9 — Move mail handlers (optional, large scope)
Move `email_handlers.go`, `email_set_handlers.go`, `email_ops_handlers.go`, `email_query_handlers.go`, `email_parse.go`, `mailbox_handlers.go`, `thread_handlers.go`, `submission_handlers.go`, `identity_vacation_handlers.go`, `mdn_handlers.go`, `quota_handlers.go`, `push_handlers.go` into `jmapmail`.

### Step 10 — Remove type aliases from `jmap`
Once all callers (`nextcloud`, `smtp`, test files) import sub-packages directly, remove the backward-compat aliases from `jmap`.

## Current Status

| Step | Status |
|---|---|
| Step 1: `jmaphandler` | ✅ done |
| Step 2: `jmapauth` | ✅ done |
| Step 3: `BlobBackend` → `jmapblob` | ✅ done |
| Step 4: `jmapmail` | ✅ done |
| Step 5: `jmapprincipals` | ✅ done |
| Step 6: calendar handlers → `jmapcalendar` | ✅ done |
| Step 7: contacts → `jmapcontacts` | ✅ done |
| Step 8: sieve → `jmapsieve` | ✅ done |
| Step 9: mail handlers → `jmapmail` | ⬜ todo |
| Step 10: remove aliases | ⬜ todo |
