# Testing & Conformance Guide

This document details how to run, configure, and debug all internal and external test suites for `imap-jmap`.

---

## 1. Internal Go Unit & Conformance Tests

All Go unit and conformance tests are located in `jmap/`, `dav/`, and `smtp/`.

### Running Go Tests
Always include a timeout when running Go unit tests to prevent hanging:

```bash
# Run all internal tests with fresh cache
timeout 30s go test -count=1 ./...

# Run tests in a specific package
timeout 30s go test -v ./jmap

# Run specific RFC test suites
go test -v -run "TestRFC8620" ./jmap       # Core JMAP (RFC 8620)
go test -v -run "TestRFC8621" ./jmap       # Mail (RFC 8621)
go test -v -run "TestRFC8984" ./jmap       # Calendars (draft-ietf-jmap-calendars-27 / RFC 8984)
go test -v -run "TestRFC9610" ./jmap       # Contacts (RFC 9610 / RFC 9553)
go test -v -run "TestRFC9661" ./jmap       # Sieve (RFC 9661)
go test -v -run "TestRFC9425" ./jmap       # Quotas (RFC 9425)
go test -v -run "TestRFC9219" ./jmap       # S/MIME (RFC 9219)
go test -v -run "TestRFC9007" ./jmap       # MDN (RFC 9007)
go test -v -run "TestRFC8887" ./jmap       # WebSockets (RFC 8887)
```

### Requirement-Traceability Matrix & Spec Coverage Checker
Every normative clause worked on is documented in the Go-native `spec/` package (`spec.Matrices`). The `TestSpecCoverage` test enforces matrix integrity:

```bash
go test -v -run TestSpecCoverage ./jmap
# or directly on the spec package:
go test -v ./spec
```

---

## 2. Fastmail `JMAP-TestSuite` (Core & Mail Conformance)

The official Fastmail conformance test suite is located in `~/git/fastmail/JMAP-TestSuite`. It tests Core ([RFC 8620](https://www.rfc-editor.org/rfc/rfc8620.html)) and Mail ([RFC 8621](https://www.rfc-editor.org/rfc/rfc8621.html)).

### Step 1: Start the JMAP Server
Start the server in a separate terminal:
```bash
cd ~/git/imap-jmap
go run . -port 8181 -https-port 8444 -smtp-port 1026
```

### Step 2: Server Adapter Configuration
In `~/git/fastmail/JMAP-TestSuite`, ensure `imap-jmap.json` is configured:
```json
{
  "adapter": "ImapJmap",
  "base_uri": "http://localhost:8181",
  "credentials": [{
    "username": "user@example.com",
    "password": "user@example.com"
  }]
}
```

### Step 3: Run the Test Suite
From `~/git/fastmail/JMAP-TestSuite`:
```bash
cd ~/git/fastmail/JMAP-TestSuite

# Run the entire test suite (all 89 files)
JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lr t/

# Run a single test subsystem
JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lr t/core/
JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lr t/Mailbox/
JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lr t/Thread/
JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lr t/Email/

# Run a single test file (verbose)
JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lv t/basic.t

# Run with full JMAP request/response telemetry logged to STDERR
JMTS_TELEMETRY=1 JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lv t/basic.t

# Run over WebSockets transport (RFC 8887)
JMTS_USE_WEBSOCKETS=1 JMAP_SERVER_ADAPTER_FILE=imap-jmap.json prove -lv t/basic.t
```

### Conformance Status
- **Status**: **100% PASS (89/89 test files)** across all subsystems.
- See [`JMAP_TEST_SUITE_STATUS.md`](./JMAP_TEST_SUITE_STATUS.md) for the full per-file report.
- **Continuous Non-Regression Gate**: All 89 test files must pass without regression before and after any changes.

### Intentionally Ignored Fastmail Tests

The following upstream tests assume that a pristine account has no mailboxes
other than Inbox. This server intentionally provisions standard role mailboxes
(`sent`, `drafts`, `trash`, `junk`, and `archive`) for every account so real
mail clients can compose, save drafts, send, archive, and delete messages on a
new account. RFC 8621 permits either account shape, but the upstream pristine
account assumption conflicts with this server's interoperability policy.

- `t/Mailbox/get/no-existing-entities.t`
- `t/Mailbox/query/no-existing-entities.t`

These two tests are an explicit exception to the Fastmail non-regression gate.
Do not change account provisioning or suppress standard mailbox roles merely to
make them pass. All other Fastmail `JMAP-TestSuite` tests remain required.

---

## 3. TypeScript `jmap-test-suite` (Core & RFC 8621 Conformance)

The TypeScript/Node.js JMAP conformance test suite is located in `~/git/jmap-test-suite`.

### Step 1: Run TypeScript Suite
With the server running on port `8181`:
```bash
cd ~/git/jmap-test-suite
node dist/cli.js -c imap-jmap.json -f
```

### Conformance Status
- **Status**: **100% PASS (309/309 tests)** (304 required, 5 recommended, 0 failures, 0 skipped).
- Covers multi-account, cross-account `Blob/copy` and `Email/copy`, `EventSource`, `PushSubscription`, `Identity`, `EmailSubmission`, `VacationResponse`, and search snippets.

---

## 4. Cyrus `Cassandane` JMAP Test Suite

Cyrus `Cassandane` ([https://github.com/cyrusimap/cassandane](https://github.com/cyrusimap/cassandane)) is an automated integration and protocol test framework with dedicated JMAP torture tests covering complex queries, concurrency, large payloads, and edge cases.

---

## 5. JSContact (`RFC 9553`) & JSCalendar (`RFC 8984`) Conformance Suites

### A. `jmapio/jscontact-tests`
The official IETF JSContact test suite ([https://github.com/jmapio/jscontact-tests](https://github.com/jmapio/jscontact-tests)) provides JSON test vectors verifying:
- JSContact Card ([RFC 9553](https://www.rfc-editor.org/rfc/rfc9553.html)) data structures and validation boundaries.
- Bidirectional vCard 3.0/4.0 ↔ JSContact object transformation.

### B. `ietf-jmap/jscalendar`
The IETF JSCalendar repository ([https://github.com/ietf-jmap/jscalendar](https://github.com/ietf-jmap/jscalendar)) provides normative test vectors for:
- RFC 8984 JSCalendar event, task, and group models.
- Recurrence rule expansion (`byDay`, `bySetPosition`, `firstDayOfWeek`, `recurrenceOverrides`).
- iCalendar (RFC 5545) ↔ JSCalendar (RFC 8984) conversion.

### C. `stalwartlabs/calcard`
A suite of unit and property tests ([https://github.com/stalwartlabs/calcard](https://github.com/stalwartlabs/calcard)) covering strict JSCalendar and JSContact serialization and round-trip fidelity.

---

## 6. Apache James JMAP Cucumber Test Suite

The Apache James project ([https://github.com/apache/james-project](https://github.com/apache/james-project)) includes an extensive Cucumber-based functional test suite (`server/protocols/jmap-rfc-8621-integration-tests`) covering:
- **Core (RFC 8620)**: Batching, method call limits, capability negotiation, result references.
- **Mail (RFC 8621)**: Threading, mailbox trees, message importation, full-text query snippets.
- **Extensions**: MDN ([RFC 9007](https://www.rfc-editor.org/rfc/rfc9007.html)), Quotas ([RFC 9425](https://www.rfc-editor.org/rfc/rfc9425.html)), Contacts ([RFC 9610](https://www.rfc-editor.org/rfc/rfc9610.html)), Sieve ([RFC 9661](https://www.rfc-editor.org/rfc/rfc9661.html)).

---

## 7. MIME Torture & Robustness Test Suite

MIME torture test vectors ([https://www.w3.org/2001/06/tests/](https://www.w3.org/2001/06/tests/) and `mhonarc` torture suites) test parser resilience against:
- Deeply-nested multipart trees (RFC 2045 / RFC 2046).
- Malformed header fields, folded CRLF lines, unquoted boundaries.
- Boundary smuggling, CTE bypass (base64/quoted-printable), and non-standard charsets.

---

## 8. SMTP & Inbound Mail Compliance (`swaks` & `chasquid`)

### A. `swaks` (Swiss Army Knife for SMTP)
Used to automate ESMTP verification against the receiving endpoint (e.g. port `1026`):
```bash
# Test basic mail delivery
swaks --to user@example.com --from sender@example.com --server 127.0.0.1:1026

# Test oversized message rejection (RFC 5321 §4.2.3 / RFC 1870)
swaks --to user@example.com --from sender@example.com --server 127.0.0.1:1026 --data large_msg.eml
```

### B. `chasquid` SMTP Integration Test Suite
The `chasquid` test suite ([https://github.com/albertito/chasquid](https://github.com/albertito/chasquid)) provides automated Go-based SMTP tests verifying authentication boundaries, SPF validation, and queue handling.

---

## 9. Playwright End-to-End Suite (`e2e/`)

The Bulwark webmail integration tests are located in `e2e/`.

```bash
cd ~/git/imap-jmap/e2e

# Run tests
CI=true pnpm test --run

# Run a specific spec
CI=true pnpm test tests/mail.spec.ts
CI=true pnpm test tests/calendar.spec.ts
CI=true pnpm test tests/pim.spec.ts
```

---

## 10. Autobahn WebSocket Test Suite (RFC 8887 / RFC 6455)

The server provides a native WebSocket transport endpoint at `/jmap/ws` implementing the JMAP subprotocol ([RFC 8887](https://www.rfc-editor.org/rfc/rfc8887.html)). The Autobahn|TestSuite ([https://github.com/crossbario/autobahn-testsuite](https://github.com/crossbario/autobahn-testsuite)) verifies framing and transport robustness:

* **Scope**:
  * UTF-8 validation and boundary slicing (Cases 1.x–6.x).
  * Clean connection close handshakes and abnormal disconnect handling (Case 7.x).
  * Ping/Pong heartbeats and unsolicited pong handling (Case 9.x).
  * Frame masking, chunking, and backpressure behavior under load.
* **Running the Suite**:
  ```bash
  # Run Autobahn fuzzing client against server running on port 8181
  docker run -it --rm \
    -v "${PWD}/test/autobahn:/config" \
    -v "${PWD}/test/autobahn/reports:/reports" \
    --net=host \
    crossbario/autobahn-testsuite \
    wstest -m fuzzingclient -s /config/fuzzingclient.json
  ```
* **Internal Go Tests**:
  ```bash
  go test -v -run "TestRFC8887" ./jmap
  ```

---

## 11. Email Authentication Conformance: SPF, DKIM & DMARC (RFC 7208 / 6376 / 7489)

The inbound SMTP receiver enforces sender verification prior to mailbox delivery and iTIP calendar invitation ingestion:

* **Scope**:
  * **SPF (RFC 7208)**: Evaluation of `ip4`, `ip6`, `a`, `mx`, `include`, `redirect`, `exists`, and `all` mechanisms with canonical `Mail::SPF` test vectors.
  * **DKIM (RFC 6376)**: Canonical OpenDKIM test vectors verifying `simple` vs. `relaxed` header and body canonicalization, key lengths, and signature verification.
  * **DMARC (RFC 7489)**: Strict vs. relaxed identifier alignment (`aspf`/`adkim`), subdomain fallback, and policy enforcement (`none`, `quarantine`, `reject`).
  * **Authentication-Results (RFC 8601)**: Trace header generation and propagation to JMAP Email properties.
* **Internal Go Tests**:
  ```bash
  go test -v -run "TestSenderAuth|TestRFC7208|TestRFC6376|TestRFC7489" ./smtp
  ```

---

## 12. Web Push ECE Encryption & VAPID Test Vectors (RFC 8291 / RFC 9749)

Verifies outgoing push notification encryption and voluntary application server identification for `PushSubscription` resources:

* **Scope**:
  * **RFC 8291 / RFC 8188**: Encrypted Content-Encoding (`aes128gcm`) verified against RFC 8291 Appendix A known-answer test (KAT) vectors.
  * **RFC 9749 / RFC 8292**: VAPID JWT token generation, ES256 ECDSA signing, and `Authorization: vapid t=...,k=...` header construction.
  * **Push Service Feedback**: Handling push service responses (`404`/`410` gone subscription cleanup, `429` rate limiting).
* **Internal Go Tests**:
  ```bash
  go test -v -run "TestRFC8291|TestRFC8620_WebPush" ./jmap
  ```


---

## 13. Dovecot Pigeonhole Sieve Test Suite (RFC 5228 / 5230 / 5232 / 5429 / 5804 / 9661)

Verifies Sieve script evaluation on inbound SMTP message delivery and ManageSieve protocol operations against canonical test vectors from the Dovecot Pigeonhole suite:

* **Scope**:
  * Core Sieve actions: `fileinto`, `discard`, `redirect`, and `keep` (RFC 5228).
  * Extensions: Vacation auto-responder (RFC 5230), IMAP flags (RFC 5232), and reject (RFC 5429).
  * Remote ManageSieve protocol commands: `PUTSCRIPT`, `SETACTIVE`, `CHECK`, `DELETESCRIPT` (RFC 5804).
  * JMAP for Sieve Scripts translation: validation of `SieveScript/set` and `SieveScript/get` against the embedded ManageSieve backend (RFC 9661).
* **Internal Go Tests**:
  ```bash
  go test -v -run "TestRFC5228|TestManageSieve|TestRFC9661" ./smtp ./jmap
  ```


