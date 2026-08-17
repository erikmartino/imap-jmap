#!/usr/bin/env bash
# test:tldr — one command, zero configuration.
#
# Brings up the Bulwark <-> imap-jmap stack from docker-compose.bulwark.yml, waits
# for both services, installs the Playwright browser if needed, points the tests at
# the right endpoints, and runs the suite. Any arguments are forwarded to Playwright
# (e.g. `pnpm test:tldr calendar`, `pnpm test:tldr --headed`).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$E2E_DIR/../docker-compose.bulwark.yml"
cd "$E2E_DIR"

BULWARK_URL="http://localhost:3000"
JMAP_TLS_URL="https://localhost:8443"   # browser CSP requires HTTPS; imap-jmap serves TLS on 8443

# 1. Find a reachable Docker/Podman daemon so no DOCKER_HOST setup is needed.
if ! docker info >/dev/null 2>&1; then
  for ctx in $(docker context ls --format '{{.Name}}' 2>/dev/null || true); do
    host="$(docker context inspect "$ctx" --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)"
    if [ -n "$host" ] && DOCKER_HOST="$host" docker info >/dev/null 2>&1; then
      export DOCKER_HOST="$host"; echo "Using Docker endpoint: $DOCKER_HOST"; break
    fi
  done
fi
docker info >/dev/null 2>&1 || { echo "ERROR: no reachable Docker/Podman daemon. Start Docker Desktop or 'podman machine start' and retry." >&2; exit 1; }

# 2. Compose command: v2 plugin if present, else the standalone binary.
if docker compose version >/dev/null 2>&1; then COMPOSE=(docker compose); else COMPOSE=(docker-compose); fi

# 2b. If mkcert is installed, generate a browser-trusted TLS cert so there is no
#     certificate warning. The compose mounts ./certs into imap-jmap; when absent it
#     falls back to a self-signed cert (which needs a one-time manual accept).
CERTS_DIR="$E2E_DIR/../certs"
if command -v mkcert >/dev/null 2>&1; then
  if [ ! -f "$CERTS_DIR/cert.pem" ] || [ ! -f "$CERTS_DIR/key.pem" ]; then
    echo "==> Generating a browser-trusted TLS cert with mkcert"
    mkdir -p "$CERTS_DIR"
    mkcert -install >/dev/null 2>&1 || true
    if mkcert -cert-file "$CERTS_DIR/cert.pem" -key-file "$CERTS_DIR/key.pem" localhost 127.0.0.1 imap-jmap >/dev/null 2>&1; then
      echo "    wrote certs/cert.pem (trusted — no browser warning)"
    else
      echo "    mkcert failed; imap-jmap will fall back to a self-signed cert"
    fi
  else
    echo "==> Using existing certs/cert.pem"
  fi
else
  echo "==> mkcert not installed; imap-jmap will use a self-signed cert (accept https://localhost:8443 once)"
fi

echo "==> Bringing up the stack (fresh)"
# An optional docker-compose.local.yml next to the main file is included when
# present (e.g. host-networking override for environments without rootless
# bridge networking); it never replaces the canonical compose file.
COMPOSE_FILES=(-f "$COMPOSE_FILE")
if [ -f "$E2E_DIR/../docker-compose.local.yml" ]; then
  COMPOSE_FILES+=(-f "$E2E_DIR/../docker-compose.local.yml")
  echo "==> Using local override docker-compose.local.yml"
fi
# --force-recreate guarantees a clean environment (and that a freshly generated cert
# is loaded, since it is a runtime mount rather than baked into the image).
"${COMPOSE[@]}" "${COMPOSE_FILES[@]}" up -d --build --force-recreate

# 3. Wait for both services to answer.
wait_for() {
  local name="$1" url="$2" want="$3" code=""
  printf 'Waiting for %s ' "$name"
  for _ in $(seq 1 60); do
    code="$(curl -sk -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || true)"
    if [[ "$code" =~ $want ]]; then echo "ready ($code)"; return 0; fi
    printf '.'; sleep 2
  done
  echo " TIMEOUT (last=$code)"; return 1
}
wait_for "imap-jmap (HTTPS 8443)" "$JMAP_TLS_URL/.well-known/jmap" '^401$'
wait_for "bulwark webmail (3000)" "$BULWARK_URL"                    '^(200|301|302|307|401)$'

# 4. Ensure test deps + the Chromium browser are installed (idempotent). Use the
#    local binary directly so we never trigger a pnpm reinstall/purge mid-run.
PW="$E2E_DIR/node_modules/.bin/playwright"
if [ ! -x "$PW" ]; then
  echo "==> Installing test dependencies"
  CI=true pnpm install
fi
"$PW" install chromium >/dev/null 2>&1 || "$PW" install chromium

# 5. Run the suite against the right endpoints.
export BULWARK_BASE_URL="$BULWARK_URL"
export JMAP_SERVER_URL="$JMAP_TLS_URL"
echo "==> Running Playwright ${*:-(all tests)}"
set +e
"$PW" test "$@"
status=$?
set -e

echo
echo "The stack is still running (fast reruns). Stop it with:  pnpm docker:down"
exit $status
