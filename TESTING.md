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
Every normative clause worked on is documented in `docs/conformance/<spec>.json`. The `TestSpecCoverage` test enforces matrix integrity:

```bash
go test -v -run TestSpecCoverage ./jmap
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
