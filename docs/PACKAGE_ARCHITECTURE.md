# JMAP Package Layered Architecture

## Architecture Overview

The `imap-jmap` server codebase follows a strict layered architecture to prevent circular dependencies, ensure clean separation of concerns, and enable modular extensibility:

1. Each layer only imports **downward**.
2. Domain packages are **fully self-contained** (domain types, backend interface, and method handlers).
3. The top-level `jmap` package is a **thin wiring layer** (server routing, session negotiation, method registration).
4. External adapters (`imapsmtp`, `nextcloud`, `managesieve`, `smtp`, `main.go`, and test suites) import domain and infrastructure sub-packages directly.

## Layer Map

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

