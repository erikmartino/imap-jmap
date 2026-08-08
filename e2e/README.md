# End-to-end tests — Bulwark webmail ↔ imap-jmap

Playwright tests that drive the [Bulwark](https://github.com/bulwarkmail/webmail) webmail client
against the `imap-jmap` server, plus JMAP wire-protocol assertions. This is **not** part of
`go test`; it runs the real client against a running server.

- UI steps use the Bulwark SPA (login, calendar/contacts/mail views).
- Protocol steps use a small `JMAPClient` (`lib/helpers.ts`) that speaks the same JMAP the client
  does (RFC 8620 / 8621 / 9610 and the JMAP-for-Calendars I-D), so behaviour is verified against
  the running server, not a stub.

## Prerequisites

- Docker + the `docker compose` plugin (to run the stack), or a locally built `imap-jmap`.
- Node 20+ and `pnpm`.

## Quick start (Docker)

The bundled [`docker-compose.bulwark.yml`](../docker-compose.bulwark.yml) starts both services:

| Service     | URL                     | Notes                                            |
| ----------- | ----------------------- | ------------------------------------------------ |
| `imap-jmap` | http://localhost:8080   | JMAP + SMTP (1025); built from this repo         |
| `bulwark`   | http://localhost:3000   | webmail; talks to `imap-jmap` at `http://imap-jmap:8080` inside the compose network |

```bash
cd e2e
pnpm install                 # test deps
pnpm install:browsers        # Chromium for Playwright

pnpm docker:up               # build + start imap-jmap and bulwark (detached)
#   equivalently: docker compose -f ../docker-compose.bulwark.yml up -d --build

# Point the tests at the compose endpoints, then run:
export BULWARK_BASE_URL=http://localhost:3000
export JMAP_SERVER_URL=http://localhost:8080
pnpm test                    # all suites (headless)
pnpm test:calendar           # just the calendar suite

pnpm docker:down             # stop the stack
```

`global-setup.ts` fails fast with a clear message if either service is unreachable.

> **Endpoint note.** `JMAP_SERVER_URL` is used two ways: the `JMAPClient` calls it directly, and
> `login()` types it into Bulwark's login form (the compose sets `ALLOW_CUSTOM_JMAP_ENDPOINT=true`,
> so the field is editable). Use the **host-reachable** URL `http://localhost:8080` when running the
> tests from your host. The default in `lib/helpers.ts` is `https://localhost:8443` (a local TLS
> build), so always export `JMAP_SERVER_URL` for Docker runs.

## Running it in a browser (watch it drive Bulwark)

By default tests run **headless**. To actually watch the browser:

```bash
cd e2e
export BULWARK_BASE_URL=http://localhost:3000
export JMAP_SERVER_URL=http://localhost:8080

pnpm test:headed                       # run with a visible Chromium window
pnpm test:headed calendar              # only the calendar suite, headed
pnpm test:ui                           # Playwright UI mode: pick/replay tests, watch, time-travel
pnpm test:debug calendar               # Playwright Inspector: step through, live selectors

# A single test, headed, slowed down:
pnpm exec playwright test calendar -g "renders in the Bulwark calendar" --headed --headed --workers=1
```

After a run, open the trace/report (traces are recorded on every run, video on failure):

```bash
pnpm test:report                       # HTML report with per-step screenshots
pnpm exec playwright show-trace         # then pick a trace.zip from test-results/
```

`--ui` mode is the easiest way to watch a specific test against the live Bulwark instance and
inspect each Playwright action, network call, and DOM snapshot.

## Running without Docker

Build and run `imap-jmap` on the host, and run Bulwark however you prefer (its own container or dev
server), then point the env vars at them:

```bash
# terminal 1 — server from this repo
go run . --port 8080

# terminal 2 — tests
cd e2e
export BULWARK_BASE_URL=http://localhost:3000
export JMAP_SERVER_URL=http://localhost:8080
pnpm test:calendar
```

Any `username == password` at `@example.com` is a valid local account (the memory auth backend),
and fresh accounts are seeded with sample data on first use, so tests can use throwaway accounts
(`uniqueUser()`) without shared-state races.

## What the calendar suite covers (`tests/calendar.spec.ts`)

UI (drives Bulwark):
- Calendar app loads with the seeded **Personal Calendar** and the Month/Week/Day/Agenda controls.
- **Create event** opens the event editor.
- An event created over JMAP **renders in the Bulwark calendar**.

Protocol (the exact wire API the client uses):
- `CalendarEvent` lifecycle: create → query → get → update (partial patch) → destroy.
- `recurrenceRules` **expand into occurrences** with `expandRecurrences` (master id vs. N occurrence ids).
- The **owner sees their own `private` and `secret` events** in full — privacy only restricts
  non-owner sharees (draft-ietf-jmap-calendars-27 §4.2.10).

## Troubleshooting

- **`Bulwark ... not reachable` / `JMAP ... did not require authentication`** — the stack isn't up
  or the env vars point at the wrong ports. Run `pnpm docker:up` and export the two URLs above.
- **Login can't reach JMAP** — confirm `JMAP_SERVER_URL` is host-reachable (`http://localhost:8080`
  for the bundled compose) and that `ALLOW_CUSTOM_JMAP_ENDPOINT=true` for the Bulwark service.
- **Browser missing** — run `pnpm install:browsers`.
