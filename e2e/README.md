# End-to-end tests — Bulwark webmail ↔ imap-jmap

Playwright tests that drive the [Bulwark](https://github.com/bulwarkmail/webmail) webmail client
against the `imap-jmap` server, plus JMAP wire-protocol assertions. This is **not** part of
`go test`; it runs the real client against a running server.

- UI steps drive the Bulwark SPA (login, calendar/contacts/mail views).
- Protocol steps use a small `JMAPClient` (`lib/helpers.ts`) that speaks the same JMAP the client
  does (RFC 8620 / 8621 / 9610 and the JMAP-for-Calendars I-D), so behaviour is verified against
  the running server, not a stub.

## TL;DR — one command, zero config

```bash
cd e2e
pnpm install
pnpm test:tldr              # brings up the stack, waits, installs the browser, runs everything
pnpm test:tldr calendar     # just the calendar suite
```

`test:tldr` (`scripts/tldr.sh`) finds a working Docker/Podman daemon, runs
`docker-compose.bulwark.yml up -d --build`, waits for both services, installs the Chromium build if
needed, exports the right endpoints, and runs Playwright. Extra arguments are forwarded
(`pnpm test:tldr --headed calendar`). The stack is left running for fast reruns; stop it with
`pnpm docker:down`.

## Prerequisites

- Docker or Podman (a `docker`/`docker-compose` CLI talking to a running daemon).
- Node 20+ and `pnpm`.

## How it fits together (endpoints)

| Service     | URL                        | Notes                                                          |
| ----------- | -------------------------- | -------------------------------------------------------------- |
| `bulwark`   | http://localhost:3000      | webmail SPA; the browser loads it here                         |
| `imap-jmap` | **https://localhost:8443** | JMAP over TLS (self-signed). **This is what the tests use.**   |
|             | http://localhost:8080      | plain HTTP; **not usable from the browser** (see CSP note)     |
|             | smtp://localhost:1025      | SMTP receiver                                                  |

> **Why HTTPS.** Bulwark ships a Content-Security-Policy of `connect-src 'self' https:`, so the
> webmail can only reach the JMAP server over **HTTPS**. `imap-jmap` serves TLS with a self-signed
> cert on **8443** for exactly this reason; Playwright's `ignoreHTTPSErrors` accepts the cert. Plain
> HTTP on 8080 works for `curl`/tooling but the browser blocks it — always point the tests at
> `https://localhost:8443`.

## Manual run (without `test:tldr`)

```bash
cd e2e
pnpm install
pnpm install:browsers

pnpm docker:up                         # docker-compose -f ../docker-compose.bulwark.yml up -d --build

export BULWARK_BASE_URL=http://localhost:3000
export JMAP_SERVER_URL=https://localhost:8443
pnpm test                              # or: pnpm test:calendar

pnpm docker:down
```

`global-setup.ts` fails fast with a clear message if either service is unreachable.

## Running it in a browser (watch it drive Bulwark)

By default tests run headless. To watch the browser (arguments pass through `test:tldr`, or use the
plain scripts once the stack and env are set up):

```bash
pnpm test:tldr --headed calendar       # visible Chromium, calendar suite
pnpm test:tldr --ui                    # Playwright UI mode: pick/replay tests, time-travel
pnpm test:tldr --debug calendar        # Playwright Inspector: step through, live selectors

# equivalently, with the stack up and env exported:
pnpm test:headed
pnpm test:ui
pnpm test:debug calendar
```

After a run, open the trace/report (traces are recorded on every run, video on failure):

```bash
pnpm test:report                       # HTML report with per-step screenshots
pnpm exec playwright show-trace         # then pick a trace.zip under test-results/
```

`--ui` mode is the easiest way to watch a specific test against live Bulwark and inspect each
action, network call, and DOM snapshot.

## What the calendar suite covers (`tests/calendar.spec.ts`)

UI (drives Bulwark):
- Calendar app loads with the seeded **Personal Calendar** and the Month/Week/Day/Agenda controls.
- **Creating an event through the UI** (open editor → fill title → Save) persists server-side,
  verified over JMAP.

Protocol (the exact wire API the client uses):
- `CalendarEvent` lifecycle: create → query → get → update (partial patch) → destroy.
- `recurrenceRules` **expand into occurrences** with `expandRecurrences` (master id vs. N occurrence ids).
- The **owner sees their own `private` and `secret` events** in full — privacy only restricts
  non-owner sharees (draft-ietf-jmap-calendars-27 §4.2.10).

Any `username == password` at `@example.com` is a valid local account (the memory auth backend), and
fresh accounts are seeded with sample data on first use, so tests use throwaway accounts
(`uniqueUser()`) without shared-state races.

## TLS certificate for 8443

imap-jmap serves TLS on 8443. The compose mounts `../certs` into the container and, if
`certs/cert.pem` + `certs/key.pem` are present (e.g. from **mkcert**), uses them so the browser
trusts the endpoint with **no warning**. Otherwise it falls back to a self-signed cert.

- **`pnpm test:tldr` auto-generates a trusted cert** when `mkcert` is installed (runs `mkcert -install`
  and writes `certs/cert.pem`). Nothing to do.
- Manually: `mkcert -install && mkcert -cert-file certs/cert.pem -key-file certs/key.pem localhost 127.0.0.1 imap-jmap`, then restart imap-jmap.

### Self-signed fallback: trust it once

Without mkcert, imap-jmap uses a self-signed cert; a real browser refuses it and Bulwark shows
**"Unable to reach the server. Check your internet connection and try again."** even though the URL
is correct. Accept the certificate once:

1. Open **https://localhost:8443/.well-known/jmap** directly in the same browser.
2. Click **Advanced → Proceed to localhost (unsafe)** (you'll see a 401 — that's expected).
3. Go back to Bulwark (http://localhost:3000) and log in; the JMAP URL is prefilled with
   `https://localhost:8443`.

(The Playwright suite doesn't hit this because its browser context sets `ignoreHTTPSErrors`. For a
permanently-trusted cert, generate one with `mkcert` and mount it instead of the built-in self-signed
cert.)

## Troubleshooting

- **`Bulwark ... not reachable` / `JMAP ... did not require authentication`** — the stack isn't up
  or the env points at the wrong ports. Use `pnpm test:tldr`, or `pnpm docker:up` + export the two
  URLs above.
- **Login shows "Unable to reach the server"** — almost always the untrusted self-signed cert: accept
  it once (see "First-time: trust the self-signed certificate" above). Make sure the JMAP URL is
  `https://localhost:8443` (the browser CSP forbids plain-HTTP `http://localhost:8080`).
- **No Docker daemon** — start Docker Desktop or `podman machine start`; `test:tldr` auto-detects the
  endpoint.
- **Browser missing** — `pnpm install:browsers` (or just use `test:tldr`, which installs it).
