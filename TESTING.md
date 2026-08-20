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

---

## 3. `jmapio/jmap-perl` Test Suite

The `jmapio/jmap-perl` repository ([https://github.com/jmapio/jmap-perl](https://github.com/jmapio/jmap-perl)) provides tests across JMAP Core, Mail, Calendars, and Contacts.

### Step 1: Clone the Repository
```bash
cd ~/git
git clone https://github.com/jmapio/jmap-perl.git
cd jmap-perl
```

### Step 2: Run Against `imap-jmap`
With `imap-jmap` running on port `8181`:
```bash
cd ~/git/jmap-perl
# Execute tests against http://localhost:8181
prove -lr t/
```

---

## 4. Playwright End-to-End Suite (`e2e/`)

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
