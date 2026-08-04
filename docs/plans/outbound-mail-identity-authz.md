# Outbound mail + identity/authorization refactor

## Context
`EmailSubmission/set` never sends: `CreateSubmission` (`jmap/memory/mail_store.go:1058`) stores the
submission and fabricates `deliveryStatus: {"user@example.com": {delivered: granted}}`. The `smtp/`
server is inbound-only and hardcodes account `"primary"` (`smtp/receiver.go:28`, `parser.go:22`).
Data isolation currently keys the memory stores on the **authenticated user id** (whatever
`AuthBackend` returns, injected via `ContextWithAccountID`, read by `getStoreLocked`,
`jmap/memory/mail_store.go:40-52`).

We want a cleaner model, plus real outbound mail:
- **Authentication service** = translate an authenticated subject (credentials/token) → an **accountId**
  (a derived, stable id, e.g. base64url of the subject).
- **Authorization service (permission guard)** = decide whether a principal's accountId may act on a
  target accountId (default: only its own).
- **Account resolution** = map an email address → accountId; default resolves every `*@<primaryDomain>`
  address. Used by SMTP for local delivery (inbound routing + outbound loopback).
- Memory stores use **accountId** as the discriminator (not the raw subject).
- Outbound: local-domain recipients delivered in-process; external recipients gated by an allow-list.
- `--primary-domain` (default `example.com`) and `--allowed-recipients` (default empty) CLI flags.

The corresponding TODO items live under "Implement first — outbound mail + identity/authorization
refactor" in `TODO.md`; this document is the design reference they point at.

## Design

### Identity helper
- `jmap/auth.go`: add `AccountIDForSubject(subject string) string` = `base64.RawURLEncoding` of subject.
  This is the single source of truth for subject→accountId, reused by the auth backend and the resolver.

### Authentication (existing `AuthBackend`, `jmap/auth.go:11`)
- Keep the interface (methods already return an `accountID string`), but change `MemoryAuthBackend`
  (`jmap/memory/auth_store.go`) so the returned accountId is `AccountIDForSubject(subject)` instead of
  the bare username. Token records map token→subject; validation returns the derived accountId.

### Authorization (new `PermissionGuard`, `jmap/authz.go`)
```go
type PermissionGuard interface {
    CanAccessAccount(ctx context.Context, principalAccountID, targetAccountID string) bool
}
```
- Default `SelfAccessGuard`: `principalAccountID == targetAccountID`.
- `WithPermissionGuard(g)` option; `Server` holds it (`jmap/server.go`).
- Wire leniently in the JMAP dispatcher: resolve each call's `accountId` arg — if empty or the alias
  `"primary"`, treat as the principal's own account; otherwise require `CanAccessAccount(...)` →
  else method error `accountNotFound`. The store keeps routing by the ctx accountId (the principal's),
  so existing `"primary"` tests are unaffected.

### Account resolution (new `AccountResolver`, `jmap/authz.go`)
```go
type AccountResolver interface {
    ResolveAccountID(ctx context.Context, emailAddress string) (accountID string, local bool)
}
```
- Default `PrimaryDomainResolver{PrimaryDomain}`: if the address domain equals `PrimaryDomain`,
  return `(AccountIDForSubject(address), true)`; else `("", false)`.

### Config plumbing
- Construct the resolver + allow-list in `main.go` from the new flags, inject into both `jmap.Server`
  (via `WithAccountResolver`, `WithAllowedRecipients`) and `smtp.NewServer` (add a resolver param).
  Flags follow the existing env-default pattern (`PRIMARY_DOMAIN`, `ALLOWED_RECIPIENTS`).

### Outbound send (`EmailSubmission/set`)
- Thread the resolver + allow-list into the submission handler (extend `RegisterMailHandlers` /
  `handleEmailSubmissionSet`, `jmap/submission_handlers.go:82`).
- On create: load the referenced `Email`, build recipient set from `envelope.rcptTo` (add `Envelope`
  to `EmailSubmission`, `jmap/mail_types.go:125`) falling back to the email's To/Cc/Bcc. For each rcpt:
  - `ResolveAccountID` → local: deliver in-process via `MailBackend.CreateEmail` with ctx set to the
    recipient's accountId (reuses the exact inbound primitive at `smtp/receiver.go:94`); mark
    `deliveryStatus[rcpt] = {delivered: "yes", smtpReply: "250 ..."}`.
  - not local: allowed only if in allow-list → (stub external send / mark queued); else
    `deliveryStatus[rcpt] = {delivered: "failed"}`.
  - If no recipient is deliverable → `notCreated` with a `forbidden` SetError.
- Replace the fabricated status in `CreateSubmission` (`jmap/memory/mail_store.go:1075-1080`) with the
  computed per-recipient status passed in.

### SMTP receiver routing (inbound)
- `smtp/receiver.go` `Data()` (:64): for each RCPT TO, `ResolveAccountID`; deliver a copy to each local
  recipient's accountId (ctx per recipient) instead of the hardcoded `"primary"`. Keep `"primary"`
  fallback when no resolver/unresolved so current single-user tests still pass.

## Files
- `jmap/auth.go` (helper), `jmap/authz.go` (new: guard + resolver + defaults),
  `jmap/memory/auth_store.go` (derived accountId), `jmap/server.go` (fields + options + dispatch guard),
  `jmap/mail_types.go` (Envelope on EmailSubmission), `jmap/submission_handlers.go` +
  `jmap/mail_register.go` (thread resolver/allow-list, outbound send),
  `jmap/memory/mail_store.go` (accept computed deliveryStatus), `smtp/server.go` + `smtp/receiver.go`
  (resolver param + per-recipient routing), `main.go` (flags + wiring), `TODO.md` (task entries).

## Sequencing (small, independently-committable pieces)
- **A. TODO.md** — add the identity/authz/resolver tasks alongside the two outbound tasks.
- **B. Identity** — `AccountIDForSubject` + memory auth returns derived accountId (+ unit test; adjust
  isolation test if it inspects the id).
- **C. Authz** — `PermissionGuard` + `AccountResolver` interfaces, default impls, options, lenient
  dispatch guard (+ unit tests: self allowed, foreign → accountNotFound; resolver local/external).
- **D. Primary-domain flag** — config + `--primary-domain` (+ flag/env test).
- **E. Outbound send** — Envelope type, submission handler send path, allow-list, computed
  deliveryStatus (+ `rfc8621_*` tests: local delivered, external-listed sent, external-unlisted &
  empty-list rejected).
- **F. SMTP routing** — receiver per-recipient resolver routing + outbound loopback landing in the
  built-in receiver (+ `smtp/rfc5321_*` test).

## Verification
- `go build ./... && go test ./...`.
- New unit tests per piece (above).
- Manual: run `go run . --primary-domain example.com --allowed-recipients ext@other.com`, create an
  email + `EmailSubmission/set` to a local recipient over HTTP, then `Email/get` in the recipient's
  account (or `GetAllEmails`) to confirm local delivery; submit to an unlisted external recipient and
  confirm `deliveryStatus`/`notCreated` rejection.
